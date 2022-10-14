// Command origin-login-test debugs Origin login.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cardigann/harhar"
	"github.com/pg9182/atlas/pkg/origin"
	"github.com/spf13/pflag"
)

var opt struct {
	HAR     string
	Verbose bool
	Help    bool
}

func init() {
	pflag.StringVarP(&opt.HAR, "har", "H", "", "Write requests to a HAR file (use http://www.softwareishard.com/har/viewer/ to view it)")
	pflag.BoolVarP(&opt.Verbose, "verbose", "v", false, "Print HTTP requests")
	pflag.BoolVarP(&opt.Help, "help", "h", false, "Show this help text")
}

func main() {
	pflag.Parse()

	if pflag.NArg() != 2 || opt.Help {
		fmt.Printf("usage: %s [options] email password\n\noptions:\n%s\nwarning: do not use this tool repeatedly, or you may trigger additional verification, which will break login\n", os.Args[0], pflag.CommandLine.FlagUsages())
		if opt.Help {
			os.Exit(2)
		}
		os.Exit(0)
	}

	if http.DefaultClient.Transport == nil {
		http.DefaultClient.Transport = http.DefaultTransport
	}

	if opt.Verbose {
		log := &httpLogger{
			PreRequest: func(req *http.Request) {
				fmt.Fprintf(os.Stderr, "http:  req: %s %q\n", req.Method, req.URL.String())
			},
			PostRequest: func(_ *http.Request, resp *http.Response, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "http: resp:    error: %v\n", err)
				}
				if resp.StatusCode >= 300 && resp.StatusCode < 400 {
					fmt.Fprintf(os.Stderr, "http: resp:    %d %s\n", resp.StatusCode, resp.Header.Get("Location"))
				} else {
					fmt.Fprintf(os.Stderr, "http: resp:    %d content-type=%s\n", resp.StatusCode, resp.Header.Get("Content-Type"))
				}
			},
		}
		log.RoundTripper, http.DefaultClient.Transport = http.DefaultClient.Transport, log
	}

	var rec *harhar.Recorder
	if opt.HAR != "" {
		rec = harhar.NewRecorder()
		rec.RoundTripper, http.DefaultClient.Transport = http.DefaultClient.Transport, rec
	}

	var fail bool
	ctx := context.Background()

	sid, err := origin.Login(ctx, pflag.Arg(0), pflag.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "origin: error: %v\n", err)
		fail = true
	} else {
		fmt.Printf("SID=%s\n", sid)
	}

	if !fail {
		token, expiry, err := origin.GetNucleusToken(ctx, sid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "origin: error: %v\n", err)
			fail = true
		} else {
			fmt.Printf("NucleusToken=%s\n", token)
			fmt.Printf("NucleusTokenExpiry=%d\n", expiry.Unix())
			fmt.Printf("NucleusTokenExpirySecs=%.0f\n", time.Until(expiry).Seconds())
			fmt.Printf("NucleusTokenExpiryDuration=%s\n", time.Until(expiry).Truncate(time.Second))
		}
	}

	if opt.HAR != "" {
		if _, err := rec.WriteFile(opt.HAR); err != nil {
			fmt.Fprintf(os.Stderr, "error: write har: %v\n", err)
			fail = true
		}
	}

	if fail {
		os.Exit(1)
	}
}

type httpLogger struct {
	RoundTripper http.RoundTripper
	PreRequest   func(req *http.Request)
	PostRequest  func(req *http.Request, resp *http.Response, err error)
}

func (h *httpLogger) RoundTrip(r *http.Request) (*http.Response, error) {
	if h.PreRequest != nil {
		h.PreRequest(r)
	}
	resp, err := h.RoundTripper.RoundTrip(r)
	if h.PostRequest != nil {
		h.PostRequest(r, resp, err)
	}
	return resp, err
}
