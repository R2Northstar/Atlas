// Command r2-a2s-probe probes a Titanfall 2 server.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/r2northstar/atlas/pkg/nspkt"
	"github.com/spf13/pflag"
)

var opt struct {
	Addr        string
	Connections int
	Timeout     time.Duration
	Interval    time.Duration
	Silent      bool
	Help        bool
}

func init() {
	pflag.StringVarP(&opt.Addr, "listen", "a", "[::]:0", "UDP listen address")
	pflag.DurationVarP(&opt.Timeout, "timeout", "t", time.Second*3, "Amount of time to wait for a response")
	pflag.DurationVarP(&opt.Interval, "interval", "i", time.Second, "Interval to send packets at")
	pflag.IntVarP(&opt.Connections, "connections", "c", 1, "Number of concurrent connections")
	pflag.BoolVarP(&opt.Silent, "silent", "s", false, "Don't show the result")
	pflag.BoolVarP(&opt.Help, "help", "h", false, "Show this help text")
}

func main() {
	pflag.Parse()

	if pflag.NArg() < 1 || opt.Help {
		fmt.Printf("usage: %s [options] ip:port...\n\noptions:\n%s", os.Args[0], pflag.CommandLine.FlagUsages())
		if opt.Help {
			os.Exit(2)
		}
		os.Exit(0)
	}

	uaddr, err := netip.ParseAddrPort(opt.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: invalid udp listen address: %v\n", err)
		os.Exit(2)
	}

	addr, err := parseAddrPorts(pflag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: invalid server address: %v\n", err)
		os.Exit(2)
	}

	conn, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(uaddr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(2)
	}

	l := nspkt.NewListener()
	go l.Serve(conn)

	queue := make(chan int)
	go func() {
		defer close(queue)
		for i := range addr {
			queue <- i
		}
	}()

	type Result struct {
		Idx int
		Err error
	}
	res := make(chan Result)

	var wg sync.WaitGroup
	for n := 0; n < opt.Connections; n++ {
		wg.Add(1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), opt.Timeout)
			defer cancel()

			defer wg.Done()
			for i := range queue {
				res <- Result{i, probe(ctx, addr[i], l)}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(res)
	}()

	var fail bool
	for r := range res {
		if !opt.Silent {
			if r.Err != nil {
				fmt.Fprintf(os.Stderr, "%s: error: %v\n", addr[r.Idx], r.Err)
			} else {
				fmt.Fprintf(os.Stderr, "%s: ok\n", addr[r.Idx])
			}
		}
		if r.Err != nil {
			fail = true
		}
	}
	if fail {
		os.Exit(1)
	}
}

func probe(ctx context.Context, addr netip.AddrPort, l *nspkt.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	uid := rand.Uint64()

	x := make(chan error, 1)
	go func() {
		t := time.NewTicker(opt.Interval)
		defer t.Stop()

		for {
			if err := l.SendConnect(addr, uid); err != nil {
				select {
				case x <- err:
				default:
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	err := l.WaitConnectReply(ctx, addr, uid)
	if err != nil {
		select {
		case err = <-x:
			// error could be due to an issue sending the packet
		default:
		}
	}
	return err
}

func parseAddrPorts(a []string) ([]netip.AddrPort, error) {
	r := make([]netip.AddrPort, len(a))
	for i, x := range a {
		if v, err := netip.ParseAddrPort(x); err == nil {
			r[i] = v
		} else {
			return nil, err
		}
	}
	return r, nil
}
