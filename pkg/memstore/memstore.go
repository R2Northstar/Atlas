// Package memstore implements in-memory storage for atlas.
package memstore

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"strings"
	"sync"

	"github.com/pg9182/atlas/pkg/api/api0"
)

// AccountStore stores accounts in-memory.
type AccountStore struct {
	accounts sync.Map
}

// NewPdataStore creates a new MemoryPdataStore.
func NewAccountStore() *AccountStore {
	return &AccountStore{}
}

func (m *AccountStore) GetUIDsByUsername(username string) ([]uint64, error) {
	var uids []uint64
	if username != "" {
		m.accounts.Range(func(_, v any) bool {
			if u := v.(api0.Account); strings.EqualFold(u.Username, username) {
				uids = append(uids, u.UID)
			}
			return true
		})
	}
	return uids, nil
}

func (m *AccountStore) GetAccount(uid uint64) (*api0.Account, error) {
	v, ok := m.accounts.Load(uid)
	if !ok {
		return nil, nil
	}
	a := v.(api0.Account)
	return &a, nil
}

func (m *AccountStore) SaveAccount(a *api0.Account) error {
	if a != nil {
		m.accounts.Store(a.UID, *a)
	}
	return nil
}

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

func (m *PdataStore) GetPdataHash(uid uint64) ([sha256.Size]byte, bool, error) {
	v, ok := m.pdata.Load(uid)
	if !ok {
		return [sha256.Size]byte{}, ok, nil
	}
	return v.(pdataStoreEntry).Hash, ok, nil
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

func (m *PdataStore) SetPdata(uid uint64, buf []byte) (int, error) {
	var b []byte
	if m.gzip {
		var f bytes.Buffer
		w := gzip.NewWriter(&f)
		if _, err := w.Write(buf); err != nil {
			return 0, err
		}
		if err := w.Close(); err != nil {
			return 0, err
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
	return len(b), nil
}
