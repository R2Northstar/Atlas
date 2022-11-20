package atlas

import (
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"sync"

	"github.com/pg9182/ip2x/ip2location"
	"github.com/rs/zerolog"
)

// ip2locationMgr wraps a file-backed IP2Location database.
type ip2locationMgr struct {
	file *os.File
	db   *ip2location.DB
	mu   sync.RWMutex
}

// Load replaces the currently loaded database with the specified file. If name
// is empty, the existing database, if any, is reopened.
func (m *ip2locationMgr) Load(name string) error {
	if name == "" {
		m.mu.RLock()
		if m.file == nil {
			return fmt.Errorf("no ip2location database loaded")
		}
		name = m.file.Name()
		m.mu.RUnlock()
	}

	f, err := os.Open(name)
	if err != nil {
		return err
	}

	db, err := ip2location.New(f)
	if err != nil {
		f.Close()
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.file.Close()
	m.file = f
	m.db = db
	return nil
}

// LookupFields calls (*ip2location.DB).LookupFields if a database is loaded.
func (m *ip2locationMgr) LookupFields(ip netip.Addr, mask ip2location.Field) (ip2location.Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return ip2location.Record{}, fmt.Errorf("no ip2location database loaded")
	}
	return m.db.LookupFields(ip, mask)
}

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
