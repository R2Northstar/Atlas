package api0

import (
	"crypto/sha256"
	"net/netip"
	"time"
)

// Account contains information about a registered account.
type Account struct {
	// UID is the player's Origin UID.
	UID uint64 // required, unique

	// Username is the player's last known in-game username (their EAID).
	Username string // optional (but will usually be there)

	// AuthIP is the IP used for the current auth session.
	AuthIP netip.Addr

	// AuthToken is the random token generated for the current auth session.
	AuthToken string

	// AuthTokenExpiry is the expiry date of the current auth token.
	AuthTokenExpiry time.Time

	// LastServerID is the ID of the last server the account connected to.
	LastServerID string
}

func (a Account) IsOnOwnServer() bool {
	return a.LastServerID == "self"
}

// AccountStorage stores information about registered users. It must be safe
// for concurrent use.
type AccountStorage interface {
	// GetUIDsByUsername gets all known UIDs matching username. If none match, a
	// nil/zero-length slice is returned. If another error occurs, err is
	// non-nil.
	GetUIDsByUsername(username string) ([]uint64, error)

	// GetAccount gets the player matching uid. If none exists, nil is returned.
	// If another error occurs, err is non-nil.
	GetAccount(uid uint64) (*Account, error)

	// SaveAccount creates or replaces an account by its uid.
	SaveAccount(a *Account) error
}

// PdataStorage stores player data for users. It should not make any assumptions
// on the contents of the stored blobs (including validity). It may compress the
// stored data. It must be safe for concurrent use.
type PdataStorage interface {
	// GetPdataHash gets the current pdata hash for uid. If there is not any
	// pdata for uid, exists is false. If another error occurs, err is non-nil.
	GetPdataHash(uid uint64) (hash [sha256.Size]byte, exists bool, err error)

	// GetPdataCached gets the pdata for uid. If there is not any pdata for uid,
	// exists is false. If the provided hash is nonzero and the current pdata
	// matches, buf is nil. If another error occurs, err is non-nil.
	GetPdataCached(uid uint64, sha [sha256.Size]byte) (buf []byte, exists bool, err error)

	// SetPdata sets the raw pdata for uid.
	SetPdata(uid uint64, buf []byte) (err error)
}
