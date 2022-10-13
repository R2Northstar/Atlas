package api0testutil

import (
	"bytes"
	"crypto/sha256"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pg9182/atlas/pkg/api/api0"
)

// TestPdataStorage tests whether an EMPTY pdata storage instance implements the
// interface correctly.
func TestPdataStorage(t *testing.T, s api0.PdataStorage) {
	// test basic functionality
	{
		user1 := uint64(math.MaxUint64) // to ensure the full uid range is supported
		pdata1 := seqBytes(56306, 0)
		pdata2 := seqBytes(56306, 6)
		zeroSHA := [sha256.Size]byte{}
		pdata1SHA := sha256.Sum256(pdata1)
		pdata2SHA := sha256.Sum256(pdata2)

		t.Run("HashForNonexistentUser1", func(t *testing.T) {
			hash, exists, err := s.GetPdataHash(user1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exists {
				t.Fatalf("exists should be false")
			}
			if hash != zeroSHA {
				t.Fatalf("should not return hash")
			}
		})
		t.Run("GetForNonexistentUser1", func(t *testing.T) {
			for _, tc := range []struct {
				Name string
				SHA  [sha256.Size]byte
			}{
				{"NoCache", zeroSHA},
				{"Cache", pdata1SHA},
			} {
				t.Run(tc.Name, func(t *testing.T) {
					buf, exists, err := s.GetPdataCached(user1, tc.SHA)
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if exists {
						t.Fatalf("exists should be false")
					}
					if buf != nil {
						t.Fatalf("should not return pdata")
					}
				})
			}
		})
		t.Run("PutUser1Pdata1", func(t *testing.T) {
			if err := s.SetPdata(user1, pdata1); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("HashForExistingUser1Pdata1", func(t *testing.T) {
			hash, exists, err := s.GetPdataHash(user1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !exists {
				t.Fatalf("exists should be true")
			}
			if hash != pdata1SHA {
				t.Fatalf("should return correct hash")
			}
		})
		t.Run("GetForExistingUser1", func(t *testing.T) {
			for _, tc := range []struct {
				Name string
				SHA  [sha256.Size]byte
			}{
				{"NoCache", zeroSHA},
				{"CacheHit", pdata1SHA},
				{"CacheMiss", pdata2SHA},
			} {
				t.Run(tc.Name, func(t *testing.T) {
					buf, exists, err := s.GetPdataCached(user1, tc.SHA)
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if !exists {
						t.Fatalf("exists should be true")
					}
					if tc.SHA == pdata1SHA {
						if buf != nil {
							t.Fatalf("should not return pdata when hash matches")
						}
					} else {
						if buf == nil {
							t.Fatalf("should return pdata when hash does not match")
						}
						if !bytes.Equal(buf, pdata1) {
							t.Fatalf("incorrect pdata")
						}
						if &buf[0] == &pdata1[0] {
							t.Fatalf("pdata store must copy the data")
						}
					}
				})
			}
		})
		t.Run("PutUser1Pdata2", func(t *testing.T) {
			if err := s.SetPdata(user1, pdata2); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("HashForExistingUser1Pdata2", func(t *testing.T) {
			hash, exists, err := s.GetPdataHash(user1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !exists {
				t.Fatalf("exists should be true")
			}
			if hash != pdata2SHA {
				t.Fatalf("should return correct hash")
			}
		})
		t.Run("GetForExistingUser2", func(t *testing.T) {
			for _, tc := range []struct {
				Name string
				SHA  [sha256.Size]byte
			}{
				{"NoCache", zeroSHA},
				{"CacheHit", pdata2SHA},
				{"CacheMiss", pdata1SHA},
			} {
				t.Run(tc.Name, func(t *testing.T) {
					buf, exists, err := s.GetPdataCached(user1, tc.SHA)
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if !exists {
						t.Fatalf("exists should be true")
					}
					if tc.SHA == pdata2SHA {
						if buf != nil {
							t.Fatalf("should not return pdata when hash matches")
						}
					} else {
						if buf == nil {
							t.Fatalf("should return pdata when hash does not match")
						}
						if !bytes.Equal(buf, pdata2) {
							t.Fatalf("incorrect pdata")
						}
						if &buf[0] == &pdata2[0] {
							t.Fatalf("pdata store must copy the data")
						}
					}
				})
			}
		})
	}

	// test that it still functions properly with large numbers of users and
	// randomly ordered concurrent writers
	t.Run("Stress", func(t *testing.T) {
		const (
			concurrency = 32
			users       = 4096
		)
		var wg sync.WaitGroup
		var fail atomic.Int32
		sem := make(chan struct{}, concurrency)
		for uid := uint64(0); uid < users; uid++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(uid uint64) {
				defer wg.Done()
				defer func() { <-sem }()

				data1 := seqBytes(64000, uint8(uid))
				data2 := seqBytes(32000, uint8(uid))
				zeroSHA := [sha256.Size]byte{}
				data1sha := sha256.Sum256(data1)
				data2sha := sha256.Sum256(data2)

				if buf, exists, err := s.GetPdataCached(uid, zeroSHA); err != nil || exists || buf != nil {
					fail.Store(1)
					return
				}
				randSched()

				if buf, exists, err := s.GetPdataCached(uid, data1sha); err != nil || exists || buf != nil {
					fail.Store(2)
					return
				}
				randSched()

				if err := s.SetPdata(uid, data1); err != nil {
					fail.Store(3)
					return
				}
				randSched()

				if hash, exists, err := s.GetPdataHash(uid); err != nil || !exists || hash != data1sha {
					fail.Store(4)
					return
				}
				randSched()

				if buf, exists, err := s.GetPdataCached(uid, data1sha); err != nil || !exists || buf != nil {
					fail.Store(5)
					return
				}
				randSched()

				if buf, exists, err := s.GetPdataCached(uid, data2sha); err != nil || !exists || !bytes.Equal(buf, data1) {
					fail.Store(6)
					return
				}
				randSched()

				if err := s.SetPdata(uid, data2); err != nil {
					fail.Store(7)
					return
				}
				randSched()

				if hash, exists, err := s.GetPdataHash(uid); err != nil || !exists || hash != data2sha {
					fail.Store(8)
					return
				}
				randSched()

				if buf, exists, err := s.GetPdataCached(uid, data2sha); err != nil || !exists || buf != nil {
					fail.Store(9)
					return
				}
				randSched()

				if buf, exists, err := s.GetPdataCached(uid, data1sha); err != nil || !exists || !bytes.Equal(buf, data2) {
					fail.Store(10)
					return
				}
				randSched()

			}(uid)
		}
		if wg.Wait(); fail.Load() != 0 {
			t.Fatalf("fail (last %d)", fail.Load())
		}
	})
}

func randSched() {
	if rand.Int63()&1 == 1 {
		runtime.Gosched()
	}
}

func seqBytes(n int, start uint8) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = uint8(i + int(start))
	}
	return b
}
