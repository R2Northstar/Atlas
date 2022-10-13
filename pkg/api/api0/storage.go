package api0

import "crypto/sha256"

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
