package api0testutil

import (
	"bytes"
	"crypto/sha256"
	"math"
	"math/rand"
	"net/netip"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pg9182/atlas/pkg/api/api0"
)

// TestAccountStorage tests whether an EMPTY account storage instance implements
// the interface correctly.
func TestAccountStorage(t *testing.T, s api0.AccountStorage) {
	// test basic functionality
	{
		uid0 := uint64(999999)
		uid1 := uint64(math.MaxUint64 >> 1)
		act0 := &api0.Account{
			UID:      uid0,
			Username: "act0",
		}
		act1 := &api0.Account{
			UID:      uid1,
			Username: "act1",
		}
		t.Run("GetNonexistent", func(t *testing.T) {
			acct, err := s.GetAccount(uid0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct != nil {
				t.Fatalf("account should be nil")
			}
		})
		t.Run("GetNonexistentMax", func(t *testing.T) {
			acct, err := s.GetAccount(uid1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct != nil {
				t.Fatalf("account should be nil")
			}
		})
		t.Run("SaveNew", func(t *testing.T) {
			if err := s.SaveAccount(act0); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("SaveNewMax", func(t *testing.T) {
			if err := s.SaveAccount(act1); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("Get", func(t *testing.T) {
			acct, err := s.GetAccount(uid0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct == nil {
				t.Fatalf("account should not be nil")
			}
			if acct == act0 {
				t.Fatalf("must copy the data")
			}
			if !reflect.DeepEqual(*act0, *acct) {
				t.Fatalf("incorrect account data")
			}
		})
		t.Run("Update", func(t *testing.T) {
			act0.Username = "act1"
			if err := s.SaveAccount(act0); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			acct, err := s.GetAccount(uid0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct == nil {
				t.Fatalf("account should not be nil")
			}
			if acct == act0 {
				t.Fatalf("must copy the data")
			}
			if !reflect.DeepEqual(*act0, *acct) {
				t.Fatalf("incorrect account data")
			}
		})
		t.Run("GetLeakage", func(t *testing.T) {
			acct1, err := s.GetAccount(uid0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct1 == nil {
				t.Fatalf("account should not be nil")
			}
			acct2, err := s.GetAccount(uid0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct2 == nil {
				t.Fatalf("account should not be nil")
			}
			acct1.Username = "test"
			if acct2.Username == acct1.Username {
				t.Fatalf("account leaks internal pointers")
			}
		})
		t.Run("GetUIDsNonexistent", func(t *testing.T) {
			u, err := s.GetUIDsByUsername("act0")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(u) != 0 {
				t.Fatalf("uids should be empty")
			}
		})
		t.Run("GetUIDsEmpty", func(t *testing.T) {
			u, err := s.GetUIDsByUsername("act0")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(u) != 0 {
				t.Fatalf("uids should be empty")
			}
		})
		t.Run("GetUIDs", func(t *testing.T) {
			u, err := s.GetUIDsByUsername("act1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			sort.Slice(u, func(i, j int) bool {
				return u[i] < u[j]
			})
			if !reflect.DeepEqual(u, []uint64{act0.UID, act1.UID}) {
				t.Fatalf("uids should contain all matches")
			}
		})
		t.Run("UpdateClearUsername", func(t *testing.T) {
			act0.Username = ""
			if err := s.SaveAccount(act0); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			acct, err := s.GetAccount(uid0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if acct == nil {
				t.Fatalf("account should not be nil")
			}
			if !reflect.DeepEqual(*act0, *acct) {
				t.Fatalf("incorrect account data")
			}
		})
		t.Run("GetUIDsEmpty", func(t *testing.T) {
			u, err := s.GetUIDsByUsername("")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(u) != 0 {
				t.Fatalf("uids should be empty")
			}
		})
	}

	// test that it still functions properly with large numbers of users and
	// randomly ordered concurrent writers
	t.Run("Stress", func(t *testing.T) {
		const (
			concurrency = 32
			users       = 16384
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

				// base account
				uacct := &api0.Account{
					UID: uid,
				}

				// ensure the account doesn't exist
				if acct, err := s.GetAccount(uid); err != nil || acct != nil {
					fail.Store(1)
					return
				}
				randSched()

				// create the account
				if err := s.SaveAccount(uacct); err != nil {
					fail.Store(2)
					return
				}
				randSched()

				// ensure the account is saved
				if acct, err := s.GetAccount(uid); err != nil || acct == nil || !reflect.DeepEqual(*acct, *uacct) {
					fail.Store(3)
					return
				}
				randSched()

				// simulate auth
				uacct.Username = "user" + strconv.Itoa(rand.Intn(users/8)) // generate a username with overlap
				uacct.AuthIP = netip.MustParseAddr("127.0.0.1")
				uacct.AuthToken = "dummy"
				uacct.AuthTokenExpiry = time.Now().Add(time.Minute * 30).Truncate(time.Second)
				uacct.LastServerID = "self"

				// update the account
				if err := s.SaveAccount(uacct); err != nil {
					fail.Store(4)
					return
				}
				randSched()

				// ensure the account is up-to-date
				if acct, err := s.GetAccount(uid); err != nil || acct == nil || !reflect.DeepEqual(*acct, *uacct) {
					fail.Store(5)
					return
				}
				randSched()

				// ensure the uid is found for the username
				if uids, err := s.GetUIDsByUsername(uacct.Username); err != nil {
					fail.Store(6)
					return
				} else {
					var found bool
					for _, u := range uids {
						if u == uacct.UID {
							found = true
							break
						}
					}
					if !found {
						fail.Store(7)
						return
					}
				}
				randSched()

				// generate a new username
				oldu := uacct.Username
				uacct.Username = "user" + strconv.Itoa(rand.Intn(users/8)) + "new"

				// update the account
				if err := s.SaveAccount(uacct); err != nil {
					fail.Store(8)
					return
				}
				randSched()

				// ensure the old username is not returned for the uid
				if uids, err := s.GetUIDsByUsername(oldu); err != nil {
					fail.Store(9)
					return
				} else {
					var found bool
					for _, u := range uids {
						if u == uacct.UID {
							found = true
							break
						}
					}
					if found {
						fail.Store(10)
						return
					}
				}
				randSched()

				// ensure the new username is returned for the uid
				if uids, err := s.GetUIDsByUsername(uacct.Username); err != nil {
					fail.Store(11)
					return
				} else {
					var found bool
					for _, u := range uids {
						if u == uacct.UID {
							found = true
							break
						}
					}
					if !found {
						fail.Store(7)
						return
					}
				}

				// ensure the entire account is up-to-date
				if acct, err := s.GetAccount(uid); err != nil || acct == nil || !reflect.DeepEqual(*acct, *uacct) {
					fail.Store(12)
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

// TestPdataStorage tests whether an EMPTY pdata storage instance implements the
// interface correctly.
func TestPdataStorage(t *testing.T, s api0.PdataStorage) {
	// test basic functionality
	{
		user1 := uint64(math.MaxUint64 >> 1) // to ensure the full uid range is supported
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
			if _, err := s.SetPdata(user1, pdata1); err != nil {
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
			if _, err := s.SetPdata(user1, pdata2); err != nil {
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

				if _, err := s.SetPdata(uid, data1); err != nil {
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

				if _, err := s.SetPdata(uid, data2); err != nil {
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
