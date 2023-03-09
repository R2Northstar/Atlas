package nspkt

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

//go:embed monitor.html
var monitorHTML []byte

// DebugMonitorHandler returns a HTTP handler which serves a webpage to monitor
// sent and received connectionless packets in real-time.
func DebugMonitorHandler(l *Listener) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-cache, no-store")
		w.Header().Set("Expires", "0")
		w.Header().Set("Pragma", "no-cache")

		if r.URL.RawQuery != "sse" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Length", strconv.Itoa(len(monitorHTML)))
			w.WriteHeader(http.StatusOK)
			w.Write(monitorHTML)
			return
		}

		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "cannot stream events", http.StatusInternalServerError)
			return
		}

		c := make(chan MonitorPacket, 16)
		go l.Monitor(r.Context(), c)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		io.WriteString(w, "event: init\ndata: ")
		if addr := l.LocalAddr(); addr != nil {
			io.WriteString(w, addr.String())
		}
		io.WriteString(w, "\n\n")
		f.Flush()

		e := json.NewEncoder(w)
		for p := range c {
			io.WriteString(w, "event: packet\ndata: ")
			e.Encode(map[string]any{
				"in":     p.In,
				"remote": p.Remote.String(),
				"desc":   p.Desc,
				"data":   hex.Dump(p.Data),
			})
			io.WriteString(w, "\n")
			f.Flush()
		}
	})
}
