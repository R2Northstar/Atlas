// Package memstore implements in-memory storage for atlas.
package memstore

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"sync"
)

// PdataStore stores pdata in-memory, with optional compression.
type PdataStore struct {
	gzip  bool
	pdata sync.Map
}

type pdataStoreEntry struct {
	Hash [sha256.Size]byte
	Data []byte
}

// NewPdataStore creates a new MemoryPdataStore.
func NewPdataStore(compress bool) *PdataStore {
	return &PdataStore{
		gzip: compress,
	}
}

func (m *PdataStore) GetPdataCached(uid uint64, sha [sha256.Size]byte) ([]byte, bool, error) {
	v, ok := m.pdata.Load(uid)
	if !ok {
		return nil, ok, nil
	}
	e := v.(pdataStoreEntry)
	if sha != [sha256.Size]byte{} && sha == e.Hash {
		return nil, ok, nil
	}
	var b []byte
	if m.gzip {
		r, err := gzip.NewReader(bytes.NewReader(e.Data))
		if err != nil {
			return nil, ok, err
		}
		b, err = io.ReadAll(r)
		if err != nil {
			return nil, ok, err
		}
	} else {
		b = make([]byte, len(e.Data))
		copy(b, e.Data)
	}
	return b, ok, nil
}

func (m *PdataStore) SetPdata(uid uint64, buf []byte) error {
	var b []byte
	if m.gzip {
		var f bytes.Buffer
		w := gzip.NewWriter(&f)
		if _, err := w.Write(buf); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		b = f.Bytes()
	} else {
		b = make([]byte, len(buf))
		copy(b, buf)
	}
	m.pdata.Store(uid, pdataStoreEntry{
		Hash: sha256.Sum256(buf),
		Data: b,
	})
	return nil
}
