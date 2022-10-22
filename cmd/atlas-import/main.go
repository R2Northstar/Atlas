// Command atlas-import imports data from the old Northstar Master Server.
package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/r2northstar/atlas/db/atlasdb"
	"github.com/r2northstar/atlas/db/pdatadb"
	"github.com/r2northstar/atlas/pkg/api/api0"
	"github.com/r2northstar/atlas/pkg/pdata"
	"github.com/spf13/pflag"
)

var opt struct {
	Progress bool
	Help     bool
}

func init() {
	pflag.BoolVarP(&opt.Progress, "progress", "p", false, "Show progress")
	pflag.BoolVarP(&opt.Help, "help", "h", false, "Show this help text")
}

func main() {
	pflag.Parse()

	if pflag.NArg() != 3 || opt.Help {
		fmt.Printf("usage: %s [options] northstar_db atlas_db pdata_db\n\noptions:\n%s", os.Args[0], pflag.CommandLine.FlagUsages())
		if opt.Help {
			os.Exit(2)
		}
		os.Exit(0)
	}

	na, np, err := migrate(pflag.Arg(0), pflag.Arg(1), pflag.Arg(2))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("imported %d accounts, %d pdata blobs\n", na, np)
}

type nsacct struct {
	ID                             uint64  `db:"id"`
	CurrentAuthToken               string  `db:"currentAuthToken"`
	CurrentAuthTokenExpirationTime int64   `db:"currentAuthTokenExpirationTime"`
	CurrentServerID                *string `db:"currentServerId"`
	PersistentDataBaseline         []byte  `db:"persistentDataBaseline"`
	LastAuthIP                     *string `db:"lastAuthIp"`
	Username                       string  `db:"username"`
}

func migrate(nsfn, atlasfn, pdatafn string) (int, int, error) {
	ctx := context.Background()

	nsdb, err := sqlx.Connect("sqlite3", nsfn+"?mode=ro")
	if err != nil {
		return 0, 0, fmt.Errorf("open northstar db %q: %w", nsfn, err)
	}
	defer nsdb.Close()

	if _, err := os.Stat(atlasfn); err == nil {
		return 0, 0, fmt.Errorf("create atlas db: %q already exists", atlasfn)
	}
	if _, err := os.Stat(pdatafn); err == nil {
		return 0, 0, fmt.Errorf("create pdata db: %q already exists", pdatafn)
	}

	adb, err := atlasdb.Open(atlasfn)
	if err != nil {
		return 0, 0, fmt.Errorf("create atlas db: %w", err)
	}
	defer adb.Close()

	pdb, err := pdatadb.Open(pdatafn)
	if err != nil {
		return 0, 0, fmt.Errorf("create atlas db: %w", err)
	}
	defer pdb.Close()

	if _, to, err := adb.Version(); err != nil {
		return 0, 0, fmt.Errorf("migrate atlas db: %w", err)
	} else if err = adb.MigrateUp(ctx, to); err != nil {
		return 0, 0, fmt.Errorf("migrate atlas db: %w", err)
	}

	if _, to, err := pdb.Version(); err != nil {
		return 0, 0, fmt.Errorf("migrate pdata db: %w", err)
	} else if err = pdb.MigrateUp(ctx, to); err != nil {
		return 0, 0, fmt.Errorf("migrate pdata db: %w", err)
	}

	rows, err := nsdb.Queryx(`SELECT * FROM accounts`)
	if err != nil {
		return 0, 0, fmt.Errorf("query northstar db: %w", err)
	}
	defer rows.Close()

	var na, np int
	for rows.Next() {
		var n nsacct
		if err := rows.StructScan(&n); err != nil {
			return 0, 0, fmt.Errorf("query northstar db: scan row: %w", err)
		}
		if err := insertA(&n, adb); err != nil {
			return 0, 0, fmt.Errorf("migrate uid %d (%s): %w", n.ID, n.Username, err)
		} else {
			na++
		}
		if done, err := insertP(&n, pdb); err != nil {
			return 0, 0, fmt.Errorf("migrate uid %d (%s): %w", n.ID, n.Username, err)
		} else if done {
			np++
		}
		if opt.Progress && na%1000 == 0 {
			fmt.Printf("done %d\n", na)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("query northstar db: scan rows: %w", err)
	}
	return na, np, nil
}

func insertA(n *nsacct, a *atlasdb.DB) error {
	var x api0.Account
	if n.ID != 0 {
		x.UID = n.ID
	} else {
		return fmt.Errorf("uid is zero")
	}
	if n.Username != "" {
		x.Username = n.Username
	}
	if n.LastAuthIP != nil && *n.LastAuthIP != "" {
		if v, err := netip.ParseAddr(*n.LastAuthIP); err == nil {
			x.AuthIP = v
		} else {
			fmt.Fprintf(os.Stderr, "warning: uid %d (%s): failed to parse last auth ip %q (%v), ignoring\n", n.ID, n.Username, *n.LastAuthIP, err)
		}
	}
	x.AuthToken = n.CurrentAuthToken
	if v := time.Unix(n.CurrentAuthTokenExpirationTime, 0); time.Now().Before(v.Add(time.Hour * -24)) {
		x.AuthTokenExpiry = v
	}
	if n.CurrentServerID != nil && *n.CurrentServerID != "" {
		x.LastServerID = *n.CurrentServerID
	}
	if err := a.SaveAccount(&x); err != nil {
		return err
	}
	return nil
}

func insertP(n *nsacct, p *pdatadb.DB) (bool, error) {
	var pd pdata.Pdata
	if err := pd.UnmarshalBinary(n.PersistentDataBaseline); err == nil {
		if len(pd.ExtraData) < 140 {
			if !bytes.Equal(n.PersistentDataBaseline, pdata.DefaultPdata) && isOldDefaultPdata(n.PersistentDataBaseline) {
				if sz, err := p.SetPdata(n.ID, n.PersistentDataBaseline); err != nil {
					return false, err
				} else if sz > 2200 {
					fmt.Fprintf(os.Stderr, "info: uid %d (%s): large compressed pdata size %d\n", n.ID, n.Username, sz)
				}
				return true, nil
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: uid %d (%s): %d junk in pdata, discarding\n", n.ID, n.Username, len(pd.ExtraData))
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: uid %d (%s): invalid pdata (%v), discarding\n", n.ID, n.Username, err)
	}
	return false, nil
}

func isOldDefaultPdata(b []byte) bool {
	ss := sha1.Sum(b)
	return hex.EncodeToString(ss[:]) == "9dab70c01c475bf976689d4af525aa39db6d73bc"
}
