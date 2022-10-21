// Command atlas TODO.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-envparse"
	_ "github.com/mattn/go-sqlite3"
	"github.com/r2northstar/atlas/pkg/atlas"
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
