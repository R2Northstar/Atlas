package atlas

import (
	"io"
	"net/http"
	"sync"

	"github.com/rs/zerolog"
)

type zerologWriterLevel struct {
	w io.Writer // or zerolog.LevelWriter
	l zerolog.Level
	m sync.Mutex
}

var _ zerolog.LevelWriter = (*zerologWriterLevel)(nil)

func newZerologWriterLevel(w io.Writer, l zerolog.Level) *zerologWriterLevel {
	return &zerologWriterLevel{w: w, l: l}
}

func (wl *zerologWriterLevel) Write(p []byte) (n int, err error) {
	wl.m.Lock()
	defer wl.m.Unlock()
	if wl.w != nil {
		return wl.w.Write(p)
	}
	return len(p), nil
}

func (wl *zerologWriterLevel) WriteLevel(l zerolog.Level, p []byte) (n int, err error) {
	if l >= wl.l {
		wl.m.Lock()
		defer wl.m.Unlock()
		if wl.w != nil {
			if lw, ok := wl.w.(zerolog.LevelWriter); ok {
				return lw.WriteLevel(l, p)
			}
			return wl.w.Write(p)
		}
	}
	return len(p), nil
}

func (wl *zerologWriterLevel) SwapWriter(fn func(io.Writer) io.Writer) {
	wl.m.Lock()
	defer wl.m.Unlock()
	wl.w = fn(wl.w)
}

type middlewares []func(http.Handler) http.Handler

func (ms *middlewares) Add(m func(http.Handler) http.Handler) *middlewares {
	*ms = append(*ms, m)
	return ms
}

func (ms *middlewares) Then(h http.Handler) http.Handler {
	for i := len(*ms) - 1; i >= 0; i-- {
		h = (*ms)[i](h)
	}
	return h
}
