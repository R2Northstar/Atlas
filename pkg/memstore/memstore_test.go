package memstore

import (
	"testing"

	"github.com/r2northstar/atlas/pkg/api/api0/api0testutil"
)

func TestAccountStore(t *testing.T) {
	api0testutil.TestAccountStorage(t, NewAccountStore())
}

func TestPdataStore(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		api0testutil.TestPdataStorage(t, NewPdataStore(false))
	})
	t.Run("Compressed", func(t *testing.T) {
		api0testutil.TestPdataStorage(t, NewPdataStore(true))
	})
}
