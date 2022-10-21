package cloudflare

import (
	"fmt"
	"net/http"
	"net/netip"
)

// RealIP returns middleware to update the remote address to the value of
// CF-Connecting-IP if the request is from a Cloudflare prefix. For this to be
// secure, the Host header must be verified.
func RealIP(onError func(*http.Request, error)) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
				if raddr, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
					if HasIP(raddr.Addr()) {
						if x, err := netip.ParseAddr(cfip); err == nil {
							r2 := *r
							r2.RemoteAddr = netip.AddrPortFrom(x, raddr.Port()).String()
							r = &r2
						} else if onError != nil {
							onError(r, fmt.Errorf("parse CF-Connecting-IP: %w", err))
						}
					} else if onError != nil {
						onError(r, fmt.Errorf("have CF-Connecting-IP, but ip %s is not Cloudflare", raddr.Addr()))
					}
				} else if onError != nil {
					onError(r, fmt.Errorf("parse remote addr: %w", err))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
