// Package a2s implements a small portion of the r2 netchannel used by Northstar
// to probe servers.
package a2s

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"time"
)

const ProbeUID uint64 = 1000000001337

var ErrTimeout = errors.New("connection timed out")

func Probe(addr netip.AddrPort, timeout time.Duration) error {
	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(addr))
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer conn.Close()

	t := time.Now().Add(timeout)
	conn.SetWriteDeadline(t)
	conn.SetReadDeadline(t)

	pkt, err := r2cryptoEncrypt(r2encodeGetChallenge(ProbeUID))
	if err != nil {
		return fmt.Errorf("encrypt connection packet: %w", err)
	}
	if _, err := conn.Write(pkt); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			err = fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("send connection packet: %w", err)
	}

	resp := make([]byte, 1500)
	if n, err := conn.Read(resp); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			err = fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("receive packet: %w", err)
	} else {
		resp = resp[:n]
	}

	dec, err := r2cryptoDecrypt(resp)
	if err != nil {
		return fmt.Errorf("failed to decrypt received packet: %w", err)
	}

	uid, _, err := r2decodeChallenge(dec)
	if err != nil {
		return fmt.Errorf("invalid challenge: %w", err)
	}
	if uid != ProbeUID {
		return fmt.Errorf("invalid challenge")
	}
	return nil
}

const (
	r2cryptoNonceSize = 12
	r2cryptoTagSize   = 16
	r2cryptoKey       = "X3V.bXCfe3EhN'wb"
	r2cryptoAAD       = "\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10"
)

func r2cryptoEncrypt(b []byte) ([]byte, error) {
	pkt := make([]byte, r2cryptoNonceSize+r2cryptoTagSize+len(b))

	nonce := pkt[:r2cryptoNonceSize]
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	c, err := aes.NewCipher([]byte(r2cryptoKey))
	if err != nil {
		panic(err)
	}

	a, err := cipher.NewGCMWithTagSize(c, r2cryptoTagSize)
	if err != nil {
		panic(err)
	}

	// Go appends the ciphertext, then the tag to the dest (nonce)
	tmp := a.Seal(nil, nonce, b, []byte(r2cryptoAAD))
	copy(pkt[r2cryptoNonceSize:], tmp[len(b):])
	copy(pkt[r2cryptoNonceSize+r2cryptoTagSize:], tmp)
	return pkt, nil
}

func r2cryptoDecrypt(b []byte) ([]byte, error) {
	if len(b) < r2cryptoNonceSize+r2cryptoTagSize+1 {
		return nil, fmt.Errorf("packet too small")
	}

	c, err := aes.NewCipher([]byte(r2cryptoKey))
	if err != nil {
		panic(err)
	}

	a, err := cipher.NewGCMWithTagSize(c, r2cryptoTagSize)
	if err != nil {
		panic(err)
	}

	tmp := make([]byte, len(b)-r2cryptoNonceSize)
	copy(tmp, b[r2cryptoNonceSize+r2cryptoTagSize:])
	copy(tmp[len(tmp)-r2cryptoTagSize:], b[r2cryptoNonceSize:])

	if tmp, err = a.Open(tmp[:0], b[:r2cryptoNonceSize], tmp, []byte(r2cryptoAAD)); err != nil {
		return nil, err
	}
	return tmp, nil
}

func r2encodeGetChallenge(uid uint64) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, int32(-1))
	binary.Write(&b, binary.LittleEndian, uint8(72))
	binary.Write(&b, binary.LittleEndian, []byte("connect\x00"))
	binary.Write(&b, binary.LittleEndian, uint64(uid))
	binary.Write(&b, binary.LittleEndian, uint8(2))
	return b.Bytes()
}

func r2decodeChallenge(b []byte) (uint64, int32, error) {
	var pkt struct {
		Seq       int32
		Type      uint8
		Challenge int32
		UID       uint64
	}
	if err := binary.Read(bytes.NewReader(b), binary.LittleEndian, &pkt); err != nil {
		return 0, 0, err
	}
	if pkt.Seq != -1 {
		return 0, 0, fmt.Errorf("not a connectionless packet")
	}
	if pkt.Type != 73 {
		return 0, 0, fmt.Errorf("not a challenge response")
	}
	return pkt.UID, pkt.Challenge, nil
}
