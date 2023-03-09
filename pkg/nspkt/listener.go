// Package nspkt interacts with Northstar servers using connectionless packets.
package nspkt

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
)

var ErrListenerClosed = errors.New("listener closed")

// Listener sends and receives Northstar connectionless packets over a UDP
// socket.
type Listener struct {
	mu sync.Mutex

	conn    *net.UDPConn // currently bound socket, nil if unbound
	closing bool
	serve   <-chan struct{} // closed when Serve exits

	mon map[chan<- MonitorPacket]struct{}
	wcr map[wcrKey]map[chan struct{}]struct{}

	metrics struct {
		rx_count, rx_bytes struct {
			invalid         atomic.Uint64
			ignored         atomic.Uint64
			r2_connect_resp atomic.Uint64
			other           atomic.Uint64
		}
		tx_count, tx_bytes struct {
			atlas_sigreq1 atomic.Uint64
			r2_connect    atomic.Uint64
		}
		tx_err_count struct {
			nonce atomic.Uint64
			conn  atomic.Uint64
		}
		rx_wait_count struct {
			r2_connect_resp struct {
				timeout atomic.Uint64
				success atomic.Uint64
			}
		}
	}
}

// wcrKey matches specific connect replies.
type wcrKey struct {
	addr netip.AddrPort
	uid  uint64
}

// NewListener creates a new listener.
func NewListener() *Listener {
	return &Listener{
		mon: make(map[chan<- MonitorPacket]struct{}),
		wcr: make(map[wcrKey]map[chan struct{}]struct{}),
	}
}

// ListenAndServe creates new UDP socket on addr and calls [Listener.Serve].
func (l *Listener) ListenAndServe(addr netip.AddrPort) error {
	conn, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(addr))
	if err != nil {
		return err
	}
	return l.Serve(conn)
}

// Serve binds the listener to conn, which should not be used afterwards. If l
// is already bound, the existing conn will be closed and replaced.
func (l *Listener) Serve(conn *net.UDPConn) error {
	serve := make(chan struct{})
	defer close(serve)
	defer conn.Close()

	l.mu.Lock()
	for l.conn != nil {
		l.mu.Unlock()
		l.Close()
		l.mu.Lock()
	}
	l.conn = conn
	l.closing = false
	l.serve = serve
	l.mu.Unlock()

	for {
		// note: we can't reuse the buffer since MonitorPacket needs a copy
		// note: packets longer will be truncated by ReadFromUDPAddrPort
		pkt := r2crypto(1500)

		n, addr, err := l.conn.ReadFromUDPAddrPort(pkt.Packet())
		if err != nil {
			// note: Go already handles retries for EINTR and EAGAIN

			l.mu.Lock()
			if l.closing {
				err = ErrListenerClosed
			}
			l.conn = nil
			l.mu.Unlock()

			return err
		}

		pkt = pkt.WithPacketLen(n)
		addr = netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())

		if !pkt.Decrypt() {
			l.metrics.rx_count.invalid.Add(1)
			l.metrics.rx_bytes.invalid.Add(uint64(n))
			continue
		}

		var kind uint8
		if len(pkt.Data()) < 4+1 || binary.LittleEndian.Uint32(pkt.Data()) == 0xFFFFFFFF {
			kind = pkt.Data()[4]
		} else {
			l.metrics.rx_count.ignored.Add(1)
			l.metrics.rx_count.invalid.Add(uint64(n))
			continue // not a connectionless packet
		}

		var desc string
		switch {
		case kind == 'I' && len(pkt.Data()) >= 4+1+4+8+len("connect\x00")+4 && string(pkt.Data()[4+1+4+8:][:8]) == "connect\x00":
			l.metrics.rx_count.r2_connect_resp.Add(1)
			l.metrics.rx_bytes.r2_connect_resp.Add(uint64(n))

			// 4: i32 = -1
			// 1: u8  = 'I'
			// 4: i32 = challenge
			// 8: u64 = uid
			// 8: str = "connect\0"
			// 4: ?

			var (
				challenge = int64(binary.LittleEndian.Uint64(pkt.Data()[4+1:]))
				uid       = binary.LittleEndian.Uint64(pkt.Data()[4+1+4:])
			)
			desc = "r2_connect_resp uid=" + strconv.FormatUint(uid, 10) + " challenge=" + strconv.FormatInt(challenge, 10)

			l.mu.Lock()
			key := wcrKey{
				addr: addr,
				uid:  uid,
			}
			for c := range l.wcr[key] {
				close(c)
			}
			delete(l.wcr, key)
			l.mu.Unlock()
		default:
			l.metrics.rx_count.other.Add(1)
			l.metrics.rx_bytes.other.Add(uint64(n))

			desc = "?"
		}

		l.mu.Lock()
		for c := range l.mon {
			select {
			case c <- MonitorPacket{
				In:     true,
				Remote: addr,
				Desc:   desc,
				Data:   pkt.Data(),
			}:
			default:
			}
		}
		l.mu.Unlock()
	}
}

// Close immediately closes the active socket, if any, and unbinds it from the
// Listener, then waits for Serve to return.
func (l *Listener) Close() {
	var serve <-chan struct{}

	l.mu.Lock()
	if l.conn != nil {
		l.closing = true
		l.conn.Close()
		serve = l.serve
	}
	l.mu.Unlock()

	if serve != nil {
		<-serve
	}
}

// LocalAddr gets the local address of the active socket, if any.
func (l *Listener) LocalAddr() net.Addr {
	var a net.Addr

	l.mu.Lock()
	if l.conn != nil {
		a = l.conn.LocalAddr()
	}
	l.mu.Unlock()

	return a
}

func (l *Listener) send(addr netip.AddrPort, buf []byte, desc string) (n int, err error) {
	l.mu.Lock()
	conn := l.conn
	closing := l.closing
	l.mu.Unlock()

	if conn == nil || closing {
		l.metrics.tx_err_count.conn.Add(1)
		return 0, ErrListenerClosed
	}

	pkt := r2crypto(len(buf))
	copy(pkt.Data(), buf)

	if _, err := rand.Read(pkt.Nonce()); err != nil {
		l.metrics.tx_err_count.nonce.Add(1)
		return 0, fmt.Errorf("generate nonce: %w", err)
	}
	pkt.Encrypt()

	n, _, err = conn.WriteMsgUDPAddrPort(pkt.Packet(), nil, addr)
	if err != nil {
		l.metrics.tx_err_count.conn.Add(1)
	} else {
		if !pkt.Decrypt() {
			panic("failed to round-trip packet")
		}

		l.mu.Lock()
		for c := range l.mon {
			select {
			case c <- MonitorPacket{
				In:     false,
				Remote: addr,
				Desc:   desc,
				Data:   pkt.Data(),
			}:
			default:
			}
		}
		l.mu.Unlock()
	}
	return
}

// SendAtlasSigreq1 sends a signed Atlas JSON request.
func (l *Listener) SendAtlasSigreq1(addr netip.AddrPort, key string, obj any) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return l.SendAtlasSigreq1Raw(addr, []byte(key), b)
}

// SendAtlasSigreq1Raw sends a raw `Tsigreq1` packet.
func (l *Listener) SendAtlasSigreq1Raw(addr netip.AddrPort, key, data []byte) error {
	h := hmac.New(sha256.New, key)
	h.Write(data)

	var b []byte
	b = append(b, "\xFF\xFF\xFF\xFF"...)
	b = append(b, 'T')
	b = append(b, "sigreq1\x00"...)
	b = h.Sum(b)
	b = append(b, data...)

	n, err := l.send(addr, b, "atlas_sigreq1")
	if err == nil {
		l.metrics.tx_count.atlas_sigreq1.Add(1)
		l.metrics.tx_bytes.atlas_sigreq1.Add(uint64(n))
	}
	return err
}

// SendConnect sends a `Hconnect` packet to addr for uid.
func (l *Listener) SendConnect(addr netip.AddrPort, uid uint64) error {
	var b []byte
	b = append(b, "\xFF\xFF\xFF\xFF"...)
	b = append(b, 'H')
	b = append(b, "connect\x00"...)
	b = binary.LittleEndian.AppendUint64(b, uid)
	b = append(b, 2)

	n, err := l.send(addr, b, "r2_connect uid="+strconv.FormatUint(uid, 10))
	if err == nil {
		l.metrics.tx_count.r2_connect.Add(1)
		l.metrics.tx_bytes.r2_connect.Add(uint64(n))
	}
	return err
}

// WaitConnectReply waits for a reply to `Hconnect` from addr with uid.
func (l *Listener) WaitConnectReply(ctx context.Context, addr netip.AddrPort, uid uint64) error {
	key := wcrKey{
		addr: netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port()),
		uid:  uid,
	}

	c := make(chan struct{})

	l.mu.Lock()
	if l.wcr[key] == nil {
		l.wcr[key] = make(map[chan struct{}]struct{})
	}
	l.wcr[key][c] = struct{}{}
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		delete(l.wcr[key], c)
		l.mu.Unlock()
	}()

	select {
	case <-c:
		l.metrics.rx_wait_count.r2_connect_resp.success.Add(1)
		return nil
	case <-ctx.Done():
		l.metrics.rx_wait_count.r2_connect_resp.timeout.Add(1)
		return ctx.Err()
	}
}

// MonitorPacket describes a sent/received unencrypted connectionless packet.
type MonitorPacket struct {
	In     bool
	Remote netip.AddrPort
	Desc   string
	Data   []byte
}

// Monitor writes unencrypted sent/received packets to c until ctx is cancelled,
// discarding them if c doesn't have room.
func (l *Listener) Monitor(ctx context.Context, c chan<- MonitorPacket) {
	l.mu.Lock()
	l.mon[c] = struct{}{}
	l.mu.Unlock()

	<-ctx.Done()

	l.mu.Lock()
	delete(l.mon, c)
	l.mu.Unlock()
}

// WritePrometheus writes prometheus text metrics to w.
func (l *Listener) WritePrometheus(w io.Writer) {
	fmt.Fprintln(w, `atlas_nspkt_rx_count{type="invalid"}`, l.metrics.rx_count.invalid.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_count{type="ignored"}`, l.metrics.rx_count.ignored.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_count{type="r2_connect_resp"}`, l.metrics.rx_count.r2_connect_resp.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_count{type="other"}`, l.metrics.rx_count.other.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_bytes{type="invalid"}`, l.metrics.rx_bytes.invalid.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_bytes{type="ignored"}`, l.metrics.rx_bytes.ignored.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_bytes{type="r2_connect_resp"}`, l.metrics.rx_bytes.r2_connect_resp.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_bytes{type="other"}`, l.metrics.rx_bytes.other.Load())
	fmt.Fprintln(w, `atlas_nspkt_tx_count{type="atlas_sigreq1"}`, l.metrics.tx_count.atlas_sigreq1.Load())
	fmt.Fprintln(w, `atlas_nspkt_tx_count{type="r2_connect"}`, l.metrics.tx_count.r2_connect.Load())
	fmt.Fprintln(w, `atlas_nspkt_tx_bytes{type="atlas_sigreq1"}`, l.metrics.tx_bytes.atlas_sigreq1.Load())
	fmt.Fprintln(w, `atlas_nspkt_tx_bytes{type="r2_connect"}`, l.metrics.tx_bytes.r2_connect.Load())
	fmt.Fprintln(w, `atlas_nspkt_tx_err_count{cause="nonce"}`, l.metrics.tx_err_count.nonce.Load())
	fmt.Fprintln(w, `atlas_nspkt_tx_err_count{cause="conn"}`, l.metrics.tx_err_count.conn.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_wait_count{type="r2_connect_resp",result="timeout"}`, l.metrics.rx_wait_count.r2_connect_resp.timeout.Load())
	fmt.Fprintln(w, `atlas_nspkt_rx_wait_count{type="r2_connect_resp",result="success"}`, l.metrics.rx_wait_count.r2_connect_resp.success.Load())
}
