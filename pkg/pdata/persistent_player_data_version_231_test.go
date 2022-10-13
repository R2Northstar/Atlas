package pdata

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestPdataRoundtrip(t *testing.T) {
	for _, fn := range []string{"placeholder_playerdata.pdata"} {
		fn := fn
		t.Run(fn, func(t *testing.T) {
			obuf, err := os.ReadFile(fn)
			if err != nil {
				panic(err)
			}

			var d1 Pdata
			if err := d1.UnmarshalBinary(obuf); err != nil {
				t.Fatalf("failed to unmarshal %q: %v", fn, err)
			}
			rbuf, err := d1.MarshalBinary()
			if err != nil {
				t.Fatalf("failed to marshal %q: %v", fn, err)
			}
			if !bytes.Equal(obuf, rbuf) {
				t.Errorf("round-trip failed: re-marshaled data does not match")
			}

			var d2 Pdata
			if err := d2.UnmarshalBinary(rbuf); err != nil {
				t.Fatalf("failed to unmarshal marshaled %q: %v", fn, err)
			}
			ebuf, err := d2.MarshalBinary()
			if err != nil {
				t.Fatalf("failed to marshal unmarshaled marshaled %q: %v", fn, err)
			}
			if !bytes.Equal(rbuf, ebuf) {
				t.Errorf("internal round-trip failed: re-marshaled unmarshaled data encoded by marshal does not match")
			}

			if buf, err := d2.MarshalJSON(); err != nil {
				t.Errorf("failed to marshal as JSON: %v", err)
			} else if err = json.Unmarshal(buf, new(map[string]interface{})); err != nil {
				t.Errorf("bad json marshal result: %v", err)
			}
		})
	}
}
