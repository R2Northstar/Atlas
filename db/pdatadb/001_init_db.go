package pdatadb

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

func init() {
	migrate(up001, down001)
}

func up001(ctx context.Context, tx *sqlx.Tx) error {
	if _, err := tx.ExecContext(ctx, strings.ReplaceAll(`
		CREATE TABLE pdata (
			uid        INTEGER PRIMARY KEY NOT NULL,
			pdata_comp TEXT NOT NULL COLLATE NOCASE,
			pdata_hash TEXT NOT NULL,
			pdata      BLOB NOT NULL
		) STRICT;
	`, `
		`, "\n")); err != nil {
		return fmt.Errorf("create pdata table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX pdata_hash_idx ON pdata(pdata_hash, uid)`); err != nil {
		return fmt.Errorf("create pdata index: %w", err)
	}
	return nil
}

func down001(ctx context.Context, tx *sqlx.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP INDEX pdata_hash_idx`); err != nil {
		return fmt.Errorf("drop pdata index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE pdata`); err != nil {
		return fmt.Errorf("drop pdata table: %w", err)
	}
	return nil
}
