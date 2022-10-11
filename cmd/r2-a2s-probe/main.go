// Command r2-a2s-probe probes a Titanfall 2 server.
package main

import (
	"fmt"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/pg9182/atlas/pkg/a2s"
	"github.com/spf13/pflag"
)

var opt struct {
	Connections int
	Timeout     time.Duration
	Silent      bool
	Help        bool
}

func init() {
	pflag.DurationVarP(&opt.Timeout, "timeout", "t", time.Second*3, "Amount of time to wait for a response")
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

	if opt.Connections < 1 {
		fmt.Fprintf(os.Stderr, "fatal: --connections must be at least 1\n")
		os.Exit(2)
	}

	addr, err := parseAddrPorts(pflag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: invalid server address: %v\n", err)
		os.Exit(2)
	}

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
			defer wg.Done()
			for i := range queue {
				res <- Result{i, a2s.Probe(addr[i], opt.Timeout)}
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
