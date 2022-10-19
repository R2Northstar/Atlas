package pdatadb

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrations(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "pdata.db"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	cur, _, err := db.Version()
	if err != nil {
		panic(err)
	}
	if cur != 0 {
		t.Fatalf("current version not 0")
	}

	var ms []uint64
	for m := range migrations {
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool {
		return ms[i] < ms[j]
	})

	for _, to := range ms {
		if err := db.MigrateUp(context.Background(), to); err != nil {
			t.Fatalf("migrate up to %d: %v", to, err)
		}
		if err := db.MigrateDown(context.Background(), 0); err != nil {
			t.Fatalf("migrate down from %d to 0: %v", to, err)
		}
		if err := db.MigrateUp(context.Background(), to); err != nil {
			t.Fatalf("migrate up to %d again: %v", to, err)
		}
		if err := db.MigrateDown(context.Background(), 0); err != nil {
			t.Fatalf("migrate down from %d to 0 again: %v", to, err)
		}
	}
}
