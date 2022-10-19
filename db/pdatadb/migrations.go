package pdatadb

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// TODO: support versions which can't be migrated down from

type migration struct {
	Name string
	Up   func(context.Context, *sqlx.Tx) error
	Down func(context.Context, *sqlx.Tx) error
}

var migrations = map[uint64]migration{}

func migrate(up, down func(context.Context, *sqlx.Tx) error) {
	_, fn, _, ok := runtime.Caller(1)
	if !ok {
		panic("add migration: failed to get filename")
	}
	fn = path.Base(strings.ReplaceAll(fn, `\`, `/`))

	if n, _, ok := strings.Cut(fn, "_"); !ok {
		panic("add migration: failed to parse filename")
	} else if v, err := strconv.ParseUint(n, 10, 64); err != nil {
		panic("add migration: failed to parse filename: " + err.Error())
	} else if v == 0 {
		panic("add migration: version must not be 0")
	} else {
		migrations[v] = migration{strings.TrimSuffix(n, ".go"), up, down}
	}
}

// Version gets the current and required database versions. It should be checked
// before using the database.
func (db *DB) Version() (current, required uint64, err error) {
	if err = db.x.Get(&current, `PRAGMA user_version`); err != nil {
		err = fmt.Errorf("get version: %w", err)
		return
	}
	for v := range migrations {
		if v > required {
			required = v
		}
	}
	return
}

// MigrateUp migrates the database to the provided version.
func (db *DB) MigrateUp(ctx context.Context, to uint64) error {
	tx, err := db.x.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var cv uint64
	if err = tx.GetContext(ctx, &cv, `PRAGMA user_version`); err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if to < cv {
		return fmt.Errorf("target version %d is less than current version %d", to, cv)
	}

	var ms []uint64
	foundC, foundT := cv == 0, to == 0
	for v := range migrations {
		if v == cv {
			foundC = true
		}
		if v == to {
			foundT = true
		}
		if v > cv && v <= to {
			ms = append(ms, v)
		}
	}
	if !foundC {
		return fmt.Errorf("unsupported db version %d", cv)
	}
	if !foundT {
		return fmt.Errorf("unknown db version %d", cv)
	}

	sort.Slice(ms, func(i, j int) bool {
		return ms[i] < ms[j]
	})

	for _, v := range ms {
		if err := migrations[v].Up(ctx, tx); err != nil {
			return fmt.Errorf("migrate %d: %w", v, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = `+strconv.FormatUint(to, 10)); err != nil {
		return fmt.Errorf("update version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// MigrateDown migrates the database down to the provided version. This will
// probably eat your data.
func (db *DB) MigrateDown(ctx context.Context, to uint64) error {
	tx, err := db.x.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var cv uint64
	if err = tx.GetContext(ctx, &cv, `PRAGMA user_version`); err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if cv < to {
		return fmt.Errorf("current version %d is less than target version %d", cv, to)
	}

	var ms []uint64
	foundC, foundT := cv == 0, to == 0
	for v := range migrations {
		if v == cv {
			foundC = true
		}
		if v == to {
			foundT = true
		}
		if v <= cv && v > to {
			ms = append(ms, v)
		}
	}
	if !foundC {
		return fmt.Errorf("unsupported db version %d", cv)
	}
	if !foundT {
		return fmt.Errorf("unknown db version %d", cv)
	}

	sort.Slice(ms, func(i, j int) bool {
		return ms[i] > ms[j]
	})

	for _, v := range ms {
		if err := migrations[v].Down(ctx, tx); err != nil {
			return fmt.Errorf("migrate %d: %w", v, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = `+strconv.FormatUint(to, 10)); err != nil {
		return fmt.Errorf("update version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
