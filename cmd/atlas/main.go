// Command atlas TODO.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"net/http/pprof"

	"github.com/hashicorp/go-envparse"
	_ "github.com/mattn/go-sqlite3"
	"github.com/r2northstar/atlas/pkg/atlas"
	"github.com/r2northstar/atlas/pkg/nspkt"
	"github.com/spf13/pflag"
)

var opt struct {
	Help bool
}

func init() {
	pflag.BoolVarP(&opt.Help, "help", "h", false, "Show this help text")
}

func main() {
	pflag.Parse()

	if pflag.NArg() > 1 || opt.Help {
		fmt.Printf("usage: %s [options] [env_file]\n\noptions:\n%s\nnote: if env_file is provided, config from the environment is ignored\n", os.Args[0], pflag.CommandLine.FlagUsages())
		if opt.Help {
			os.Exit(2)
		}
		os.Exit(0)
	}

	var e []string
	if pflag.NArg() == 0 {
		e = os.Environ()
	} else {
		if x, err := readEnv(pflag.Arg(0)); err == nil {
			e = x
		} else {
			fmt.Fprintf(os.Stderr, "error: read env file: %v\n", err)
			os.Exit(1)
		}
		if v, ok := os.LookupEnv("NOTIFY_SOCKET"); ok {
			e = append(e, "NOTIFY_SOCKET="+v)
		}
	}

	dbg := http.NewServeMux()
	if dbgAddr, _ := getEnvList("INSECURE_DEBUG_SERVER_ADDR", e, os.Environ()); dbgAddr != "" {
		go func() {
			fmt.Fprintf(os.Stderr, "warning: running insecure debug server on %q\n", dbgAddr)
			if err := http.ListenAndServe(dbgAddr, dbg); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to start debug server: %v\n", err)
			}
		}()
	}

	dbg.HandleFunc("/debug/pprof/", pprof.Index)
	dbg.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	dbg.HandleFunc("/debug/pprof/profile", pprof.Profile)
	dbg.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	dbg.HandleFunc("/debug/pprof/trace", pprof.Trace)

	var c atlas.Config
	if err := c.UnmarshalEnv(e, false); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse config: %v\n", err)
		os.Exit(1)
	}

	s, err := atlas.NewServer(&c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: initialize server: %v\n", err)
		os.Exit(1)
	}

	dbg.Handle("/nspkt", nspkt.DebugMonitorHandler(s.API0.NSPkt))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hch := make(chan os.Signal, 1)
	signal.Notify(hch, syscall.SIGHUP)

	go func() {
		for range hch {
			fmt.Println("got SIGHUP")
			s.HandleSIGHUP()
		}
	}()

	if err := s.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "error: run server: %v\n", err)
		os.Exit(1)
	}
}

func getEnvList(k string, e ...[]string) (string, bool) {
	for _, l := range e {
		for _, x := range l {
			if xk, xv, ok := strings.Cut(x, "="); ok && xk == k {
				return xv, true
			}
		}
	}
	return "", false
}

func readEnv(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m, err := envparse.Parse(f)
	if err != nil {
		return nil, err
	}

	var r []string
	for k, v := range m {
		r = append(r, k+"="+v)
	}
	return r, nil
}
