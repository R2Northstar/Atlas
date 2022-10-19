package pdatadb

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pg9182/atlas/pkg/api/api0/api0testutil"
)

func TestPdataStorage(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "pdata.db"))
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

	api0testutil.TestPdataStorage(t, db)
}
