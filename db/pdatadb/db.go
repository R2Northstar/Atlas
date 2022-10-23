// Package pdatadb implements sqlite3 database storage for pdata.
package pdatadb

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"

	"github.com/jmoiron/sqlx"
	"github.com/klauspost/compress/gzip"
)

// DB stores player data in a sqlite3 database.
type DB struct {
	x *sqlx.DB
}

// Open opens a DB from the provided sqlite3 uri.
func Open(name string) (*DB, error) {
	// note: WAL and a larger pagesize makes our writes and queries MUCH faster
	x, err := sqlx.Connect("sqlite3", (&url.URL{
		Path: name,
		RawQuery: (url.Values{
			"_journal":      {"WAL"},
			"_busy_timeout": {"6000"},
		}).Encode(),
	}).String())
	if err != nil {
		return nil, err
	}
	if _, err := x.Exec(`PRAGMA page_size = 8192`); err != nil {
		panic(err)
	}
	return &DB{x}, nil
}

func (db *DB) Close() error {
	return db.x.Close()
}

func (db *DB) GetPdataHash(uid uint64) (hash [sha256.Size]byte, exists bool, err error) {
	var pdataHash string
	if err := db.x.Get(&pdataHash, `SELECT pdata_hash FROM pdata WHERE uid = ?`, uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hash, false, nil
		}
		return hash, false, err
	}
	if b, err := hex.DecodeString(pdataHash); err != nil || len(b) != len(hash) {
		return hash, false, fmt.Errorf("invalid pdata hash")
	} else {
		copy(hash[:], b)
	}
	return hash, true, nil
}

func (db *DB) GetPdataCached(uid uint64, sha [sha256.Size]byte) (buf []byte, exists bool, err error) {
	tx, err := db.x.Beginx()
	if err != nil {
		return nil, false, nil
	}
	defer tx.Rollback()

	if sha != [sha256.Size]byte{} {
		var pdataHash string
		var pdataHashB [sha256.Size]byte
		if err := db.x.Get(&pdataHash, `SELECT pdata_hash FROM pdata WHERE uid = ?`, uid); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, nil
			}
			return nil, false, err
		}
		if b, err := hex.DecodeString(pdataHash); err != nil || len(b) != len(pdataHashB) {
			return nil, false, fmt.Errorf("invalid pdata hash")
		} else {
			copy(pdataHashB[:], b)
		}
		if pdataHashB == sha {
			return nil, true, nil
		}
	}

	var obj struct {
		PdataComp string `db:"pdata_comp"`
		PdataHash string `db:"pdata_hash"`
		Pdata     []byte `db:"pdata"`
	}
	if err := db.x.Get(&obj, `SELECT pdata_comp, pdata_hash, pdata FROM pdata WHERE uid = ?`, uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	switch obj.PdataComp {
	case "":
	case "gzip":
		var b bytes.Buffer
		zr, err := gzip.NewReader(bytes.NewReader(obj.Pdata))
		if err != nil {
			return nil, false, fmt.Errorf("decompress gzip: %w", err)
		}
		if _, err := b.ReadFrom(zr); err != nil {
			return nil, false, fmt.Errorf("decompress gzip: %w", err)
		}
		if err := zr.Close(); err != nil {
			return nil, false, fmt.Errorf("decompress gzip: %w", err)
		}
		obj.Pdata = b.Bytes()
	default:
		return nil, false, fmt.Errorf("unsupported compression method %q", obj.PdataComp)
	}

	var pdataHashB [sha256.Size]byte
	if b, err := hex.DecodeString(obj.PdataHash); err != nil || len(b) != len(pdataHashB) {
		return nil, false, fmt.Errorf("invalid pdata hash")
	} else {
		copy(pdataHashB[:], b)
	}
	if sha256.Sum256(obj.Pdata) != pdataHashB {
		return nil, false, fmt.Errorf("pdata checksum mismatch")
	}
	return obj.Pdata, true, nil
}

func (db *DB) SetPdata(uid uint64, buf []byte) (n int, err error) {
	hash := sha256.Sum256(buf)
	pdataHash := hex.EncodeToString(hash[:])

	var b bytes.Buffer
	b.Grow(2000)

	zw := gzip.NewWriter(&b)
	if _, err := zw.Write(buf); err != nil {
		return 0, fmt.Errorf("compress pdata: %w", err)
	}
	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("compress pdata: %w", err)
	}

	var pdataComp string
	if b.Len() < len(buf) {
		pdataComp = "gzip"
		buf = b.Bytes()
	}

	if _, err := db.x.NamedExec(`
		INSERT OR REPLACE INTO
		pdata  ( uid,  pdata_comp,  pdata_hash,  pdata)
		VALUES (:uid, :pdata_comp, :pdata_hash, :pdata)
	`, map[string]any{
		"uid":        uid,
		"pdata_comp": pdataComp,
		"pdata_hash": pdataHash,
		"pdata":      buf,
	}); err != nil {
		return 0, err
	}
	return len(buf), nil
}
