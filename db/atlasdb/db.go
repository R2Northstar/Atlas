// Package atlasdb implements sqlite3 database storage for accounts and other atlas data.
package atlasdb

import (
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/r2northstar/atlas/pkg/api/api0"
)

// DB stores atlas data in a sqlite3 database.
type DB struct {
	x *sqlx.DB
}

// Open opens a DB from the provided sqlite3 filename.
func Open(name string) (*DB, error) {
	// note: WAL and a larger cache makes our writes and queries MUCH faster
	x, err := sqlx.Connect("sqlite3", (&url.URL{
		Path: name,
		RawQuery: (url.Values{
			"_journal":      {"WAL"},
			"_cache_size":   {"-32000"},
			"_busy_timeout": {"6000"},
		}).Encode(),
	}).String())
	if err != nil {
		return nil, err
	}
	return &DB{x}, nil
}

func (db *DB) Close() error {
	return db.x.Close()
}

func (db *DB) GetUIDsByUsername(username string) ([]uint64, error) {
	var u []uint64
	if username != "" {
		if err := db.x.Select(&u, `SELECT uid FROM accounts WHERE username = ?`, username); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func (db *DB) GetAccount(uid uint64) (*api0.Account, error) {
	var obj struct {
		UID        uint64 `db:"uid"`
		Username   string `db:"username"`
		AuthIP     string `db:"auth_ip"`
		AuthToken  string `db:"auth_token"`
		AuthExpiry int64  `db:"auth_expiry"`
		LastServer string `db:"last_server"`
	}
	if err := db.x.Get(&obj, `SELECT * FROM accounts WHERE uid = ?`, uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	var authExpiry time.Time
	if obj.AuthExpiry != 0 {
		authExpiry = time.Unix(obj.AuthExpiry, 0)
	}

	var authIP netip.Addr
	if obj.AuthIP != "" {
		if v, err := netip.ParseAddr(obj.AuthIP); err == nil {
			authIP = v
		} else {
			return nil, fmt.Errorf("parse auth_ip: %w", err)
		}
	}

	return &api0.Account{
		UID:             obj.UID,
		Username:        obj.Username,
		AuthIP:          authIP,
		AuthToken:       obj.AuthToken,
		AuthTokenExpiry: authExpiry,
		LastServerID:    obj.LastServer,
	}, nil
}

func (db *DB) SaveAccount(a *api0.Account) error {
	var authExpiry int64
	if !a.AuthTokenExpiry.IsZero() {
		authExpiry = a.AuthTokenExpiry.Unix()
	}

	var authIP string
	if a.AuthIP.IsValid() {
		authIP = a.AuthIP.StringExpanded()
	}

	if _, err := db.x.NamedExec(`
		INSERT OR REPLACE INTO
		accounts ( uid,  username,  auth_ip,  auth_token,  auth_expiry,  last_server)
		VALUES   (:uid, :username, :auth_ip, :auth_token, :auth_expiry, :last_server)
	`, map[string]any{
		"uid":         a.UID,
		"username":    a.Username,
		"auth_ip":     authIP,
		"auth_token":  a.AuthToken,
		"auth_expiry": authExpiry,
		"last_server": a.LastServerID,
	}); err != nil {
		return err
	}
	return nil
}
