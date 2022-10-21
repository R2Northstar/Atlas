package atlasdb

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/r2northstar/atlas/pkg/api/api0/api0testutil"
)

func TestAccountStorage(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "atlas.db"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	cur, tgt, err := db.Version()
	if err != nil {
		panic(err)
	}
	if cur != 0 {
		panic("current version not 0")
	}
	if err := db.MigrateUp(context.Background(), tgt); err != nil {
		panic(err)
	}

	api0testutil.TestAccountStorage(t, db)
}
