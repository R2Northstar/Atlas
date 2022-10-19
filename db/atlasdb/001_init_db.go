package atlasdb

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
		CREATE TABLE accounts (
			uid         TEXT PRIMARY KEY NOT NULL,
			username    TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
			auth_ip     TEXT,
			auth_token  TEXT,
			auth_expiry INTEGER,
			last_server TEXT
		) STRICT;
	`, `
		`, "\n")); err != nil {
		return fmt.Errorf("create accounts table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX accounts_username_idx ON accounts(username, uid)`); err != nil {
		return fmt.Errorf("create accounts index: %w", err)
	}
	return nil
}

func down001(ctx context.Context, tx *sqlx.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP INDEX accounts_username_idx`); err != nil {
		return fmt.Errorf("drop accounts_username_idx index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE accounts`); err != nil {
		return fmt.Errorf("drop accounts table: %w", err)
	}
	return nil
}
