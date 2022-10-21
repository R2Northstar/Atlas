// Command r2-pdata2json converts pdata to JSON.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/r2northstar/atlas/pkg/pdata"
	"github.com/spf13/pflag"
)

var opt struct {
	Compact bool
	Invert  bool
	Filter  []string
	Help    bool
}

func init() {
	pflag.BoolVarP(&opt.Compact, "compact", "c", false, "Don't format json")
	pflag.BoolVarP(&opt.Invert, "invert", "v", false, "Use filter to include instead of exclude")
	pflag.StringSliceVarP(&opt.Filter, "filter", "e", nil, "Exclude pdef fields (use . to select nested fields)")
	pflag.BoolVarP(&opt.Help, "help", "h", false, "Show this help text")
}

func main() {
	pflag.Parse()

	if pflag.NArg() > 1 || opt.Help {
		fmt.Printf("usage: %s [options] [file|-]\n\noptions:\n%s\npdef version: %d\n", os.Args[0], pflag.CommandLine.FlagUsages(), pdata.Version)
		if opt.Help {
			os.Exit(2)
		}
		os.Exit(0)
	}

	var err error
	var buf []byte
	if pflag.NArg() == 1 && pflag.Arg(0) != "-" {
		buf, err = os.ReadFile(pflag.Arg(0))
	} else {
		buf, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read pdata: %v\n", err)
		os.Exit(1)
	}

	var pd pdata.Pdata
	if err := pd.UnmarshalBinary(buf); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse pdata: %v\n", err)
		os.Exit(1)
	}

	jbuf, err := pd.MarshalJSONFilter(mkfilter(opt.Invert, opt.Filter...))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: encode json: %v\n", err)
		os.Exit(1)
	}

	var fbuf []byte
	if opt.Compact {
		fbuf, err = json.Marshal(json.RawMessage(jbuf))
	} else {
		fbuf, err = json.MarshalIndent(json.RawMessage(jbuf), "", "    ")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: format json: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, string(fbuf))
}

func mkfilter(invert bool, filter ...string) func(path ...string) bool {
	if len(filter) == 0 {
		return nil
	}
	// this is completely unoptimized (n^m^o), but this tool is just for testing, so whatever
	return func(path ...string) bool {
		if !invert {
			pj := strings.Join(path, ".") + "."
			for _, f := range filter {
				f += "."
				if strings.HasPrefix(pj, f) {
					return false // filter is an exact march for the path
				}
			}
			return true
		}

		pj := strings.Join(path, ".") + "."
		for _, f := range filter {
			f += "."
			if strings.HasPrefix(pj, f) {
				return true // filter is an exact match for the path
			}
			if strings.HasPrefix(f, pj) {
				return true // a parent of the filter is an exact match for the path
			}
		}
		return false
	}
}
