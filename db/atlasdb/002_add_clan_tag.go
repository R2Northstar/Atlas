package atlasdb

import (
    "context"
    "fmt"
    "strings"

    "github.com/jmoiron/sqlx"
)

func init() {
    migrate(up002, down002)
}

func up002(ctx context.Context, tx *sqlx.Tx) error {
    if _, err := tx.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN clan_tag TEXT NOT NULL DEFAULT ''`); err != nil {
        return fmt.Errorf("add clan_tag column: %w", err)
    }
    return nil
}

func down002(ctx context.Context, tx *sqlx.Tx) error {
    if _, err := tx.ExecContext(ctx, strings.ReplaceAll(`
        CREATE TABLE accounts_new (
            uid         TEXT PRIMARY KEY NOT NULL,
            username    TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
            auth_ip     TEXT,
            auth_token  TEXT,
            auth_expiry INTEGER,
            last_server TEXT
        ) STRICT;
    `, "\n", "\n")); err != nil {
        return fmt.Errorf("create accounts_new table: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `INSERT INTO accounts_new(uid, username, auth_ip, auth_token, auth_expiry, last_server) SELECT uid, username, auth_ip, auth_token, auth_expiry, last_server FROM accounts`); err != nil {
        return fmt.Errorf("copy accounts data: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `DROP TABLE accounts`); err != nil {
        return fmt.Errorf("drop accounts: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `ALTER TABLE accounts_new RENAME TO accounts`); err != nil {
        return fmt.Errorf("rename accounts_new: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS accounts_username_idx ON accounts(username, uid)`); err != nil {
        return fmt.Errorf("create accounts index: %w", err)
    }
    return nil
}
