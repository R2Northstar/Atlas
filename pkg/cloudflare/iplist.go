// Package cloudflare contains Cloudflare-related stuff.
package cloudflare

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
)

var iplistMu sync.Mutex
var iplist []netip.Prefix

//go:generate go run iplist_generate.go

// HasIP checks if ip is in a Cloudflare prefix.
func HasIP(ip netip.Addr) bool {
	for _, p := range iplist {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// UpdateIPs updates the Cloudflare IP list.
func UpdateIPs(ctx context.Context) error {
	var ps []netip.Prefix
	for _, v := range []int{4, 6} {
		if v, err := fetchIPs(ctx, "https://www.cloudflare.com/ips-v"+strconv.Itoa(v)); err == nil {
			ps = append(ps, v...)
		} else {
			fmt.Fprintf(os.Stderr, "error: fetch ipv%d list: %v\n", v, err)
			os.Exit(1)
		}
	}
	iplistMu.Lock()
	iplist = ps
	iplistMu.Unlock()
	return nil
}

func fetchIPs(ctx context.Context, u string) ([]netip.Prefix, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status %d (%s)", resp.StatusCode, resp.Status)
	}

	var r []netip.Prefix
	s := bufio.NewScanner(resp.Body)
	for s.Scan() {
		if t := strings.TrimSpace(s.Text()); t != "" {
			if strings.ContainsRune(t, '/') {
				if x, err := netip.ParsePrefix(t); err == nil {
					r = append(r, x)
				} else {
					return nil, fmt.Errorf("invalid prefix %q: %w", t, err)
				}
			} else {
				if x, err := netip.ParseAddr(t); err == nil {
					if p, err := x.Prefix(x.BitLen()); err == nil {
						r = append(r, p)
					} else {
						panic(err)
					}
				} else {
					return nil, fmt.Errorf("invalid ip %q: %w", t, err)
				}
			}
		}
	}
	return r, s.Err()
}
