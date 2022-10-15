package api0

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// ServerList stores information about registered game servers. It does not do
// any validation of its own except for ensuring ID and addr/port are unique,
// and filtering dead servers.
type ServerList struct {
	// config (must not be changed after the ServerList is used)
	deadTime  time.Duration
	ghostTime time.Duration

	// servers
	mu       sync.RWMutex               // must be held while modifying the order and maps below
	order    atomic.Uint64              // to preserve server insertion order
	servers1 map[netip.AddrPort]*Server // game addr
	servers2 map[string]*Server         // server id
	servers3 map[netip.AddrPort]*Server // auth addr

	// /client/servers json caching
	csNext     atomic.Pointer[time.Time] // latest next update time for the /client/servers response
	csForce    atomic.Bool               // flag to force an update
	csUpdateMu sync.Mutex                // ensures only one update runs at a time
	csUpdateWg sync.WaitGroup            // allows other goroutines to wait for that update to complete
	csBytes    atomic.Pointer[[]byte]    // contents of buffer must not be modified; only swapped

	// /client/servers gzipped json
	csgzUpdate   atomic.Pointer[*byte]  // pointer to the first byte of the last known json (works because it must be swapped, not modified)
	csgzUpdateMu sync.Mutex             // ensures only one update runs at a time
	csgzUpdateWg sync.WaitGroup         // allows other goroutines to wait for that update to complete
	csgzBytes    atomic.Pointer[[]byte] // gzipped

	// for unit tests
	__clock func() time.Time
}

type Server struct {
	Order uint64
	ID    string         // unique, must not be modified after creation
	Addr  netip.AddrPort // unique, must not be modified after creation

	Name        string
	Description string
	AuthPort    uint16
	Password    string // blank for none

	LastHeartbeat time.Time
	PlayerCount   int
	MaxPlayers    int
	Map           string
	Playlist      string

	ServerAuthToken string // used for authenticating the masterserver to the gameserver authserver

	ModInfo []ServerModInfo
}

type ServerModInfo struct {
	Name             string
	Version          string
	RequiredOnClient bool
}

// AuthAddr returns the auth address for the server.
func (s Server) AuthAddr() netip.AddrPort {
	return netip.AddrPortFrom(s.Addr.Addr(), s.AuthPort)
}

// clone returns a deep copy of s.
func (s Server) clone() Server {
	m := make([]ServerModInfo, len(s.ModInfo))
	copy(m, s.ModInfo)
	s.ModInfo = m
	return s
}

type ServerUpdate struct {
	ID       string     // server to update
	ExpectIP netip.Addr // require the server for ID to have this IP address to successfully update

	Heartbeat   bool
	Name        *string
	Description *string
	PlayerCount *int
	MaxPlayers  *int
	Map         *string
	Playlist    *string
}

type ServerListLimit struct {
	// MaxServers limits the number of registered servers. If <= 0, no limit is
	// applied.
	MaxServers int

	// MaxServersPerIP limits the number of registered servers per IP. If <= 0,
	// no limit is applied.
	MaxServersPerIP int
}

// NewServerList initializes a new server list.
//
// deadTime is the time since the last heartbeat after which a server is
// considered dead. A dead server will not be listed on the server list. It must
// be >= verifyTime if nonzero.
//
// ghostTime is the time since the last heartbeat after which a server cannot be
// revived by the same ip/port combination. This allows restarted or crashed
// servers to recover the same server ID. Since a server must be dead to be a
// ghost, GhostTime must be > DeadTime for it to have any effect.
//
// If both are nonzero, they must be positive, and deadTime must be less than
// ghostTime. Otherwise, NewServerList will panic.
func NewServerList(deadTime, ghostTime time.Duration) *ServerList {
	if deadTime < 0 {
		panic("api0: serverlist: deadTime must be >= 0")
	}
	if ghostTime < 0 {
		panic("api0: serverlist: ghostTime must be >= 0")
	}
	if deadTime > ghostTime {
		panic("api0: serverlist: deadTime must be <= ghostTime")
	}
	return &ServerList{
		deadTime:  deadTime,
		ghostTime: ghostTime,
	}
}

// csGetJSON efficiently gets the JSON response for /client/servers.
// The returned byte slice must not be modified (and will not be modified).
func (s *ServerList) csGetJSON() []byte {
	t := s.now()

	// if we have a cached response
	//
	// note: it might seem like there's a possibility of a race between the
	// different checks below, but it's okay since if an update starts in the
	// fraction of time between the checks, it's not the end of the world if we
	// return a barely out-of-date list (which means we don't need to have
	// unnecessary mutex locking here)
	if b := s.csBytes.Load(); b != nil {
		// and we don't need to update due to changed values
		if !s.csForce.Load() {
			// and we haven't reached the next heartbeat expiry time
			if forceTime := s.csNext.Load(); forceTime == nil || forceTime.IsZero() || forceTime.After(t) {
				// then return the existing buffer
				return *b
			}
		}
	}

	// take a read lock on the server list
	//
	// note: since the functions which update the server list take a write lock
	// (which can't be taken while there are read locks), it is safe to
	// overwrite csForce to false and csNext to the next update time (i.e., we
	// won't have a chance of accidentally setting it to a later time than the
	// pending server list change)
	//
	// note: since RLock blocks if a write is pending, we can still be
	// guaranteed to get the latest server list within an absolute amount of
	// time even if there is heavy contention on the lock
	s.mu.RLock()
	defer s.mu.RUnlock()

	// ensure only one update runs at once; otherwise wait for the existing one
	// to finish so we don't waste cpu cycles
	if !s.csUpdateMu.TryLock() {
		// wait for the existing update to finish, then return the list (which
		// will be non-nil unless the update did something stupid like
		// panicking, in which case we have bigger problems)
		s.csUpdateWg.Wait()
		return *s.csBytes.Load()
	} else {
		// we've been selected to perform the update
		defer s.csUpdateMu.Unlock()
		s.csUpdateWg.Add(1)
		defer s.csUpdateWg.Done()
	}

	// when we're done, clear the force update flag and schedule the next update
	defer s.csForce.Store(false)
	defer s.csUpdateNextUpdateTime()

	// get the servers in the original order
	ss := make([]*Server, 0, len(s.servers1)) // up to the current size of the servers map
	if s.servers1 != nil {
		for _, srv := range s.servers1 {
			if s.serverState(srv, t) == serverListStateAlive {
				ss = append(ss, srv)
			}
		}
	}
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Order < ss[j].Order
	})

	// generate the json
	//
	// note: we write it manually to avoid copying the entire list and to avoid the perf overhead of reflection
	var b bytes.Buffer
	b.WriteByte('[')
	for i, srv := range ss {
		if i != 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"lastHeartbeat":`)
		b.WriteString(strconv.FormatInt(srv.LastHeartbeat.UnixMilli(), 10))
		b.WriteString(`,"id":`)
		encodeJSONString(&b, []byte(srv.ID))
		b.WriteString(`,"name":`)
		encodeJSONString(&b, []byte(srv.Name))
		b.WriteString(`,"description":`)
		encodeJSONString(&b, []byte(srv.Description))
		b.WriteString(`,"playerCount":`)
		b.WriteString(strconv.FormatInt(int64(srv.PlayerCount), 10))
		b.WriteString(`,"maxPlayers":`)
		b.WriteString(strconv.FormatInt(int64(srv.MaxPlayers), 10))
		b.WriteString(`,"map":`)
		encodeJSONString(&b, []byte(srv.Map))
		b.WriteString(`,"playlist":`)
		encodeJSONString(&b, []byte(srv.Playlist))
		if srv.Password != "" {
			b.WriteString(`,"hasPassword":true`)
		} else {
			b.WriteString(`,"hasPassword":false`)
		}
		b.WriteString(`,"modInfo":{"Mods":[`)
		for j, mi := range srv.ModInfo {
			if j != 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"Name":`)
			encodeJSONString(&b, []byte(mi.Name))
			b.WriteString(`,"Version":`)
			encodeJSONString(&b, []byte(mi.Version))
			if mi.RequiredOnClient {
				b.WriteString(`,"RequiredOnClient":true}`)
			} else {
				b.WriteString(`,"RequiredOnClient":false}`)
			}
		}
		b.WriteString(`]}}`)
	}
	b.WriteByte(']')

	// cache it
	buf := b.Bytes()
	s.csBytes.Store(&buf)

	return b.Bytes()
}

// csGetJSONGzip is like csGetJSON, but returns it gzipped with true, or false
// if an error occurs.
func (s *ServerList) csGetJSONGzip() ([]byte, bool) {
	buf := s.csGetJSON()
	if len(buf) == 0 {
		return nil, false
	}
	cur := &buf[0]

	last := s.csgzUpdate.Load()
	if last != nil && *last == cur {
		if zbuf := s.csgzBytes.Load(); zbuf != nil && *zbuf != nil {
			return *zbuf, true
		}
	}

	if !s.csgzUpdateMu.TryLock() {
		s.csgzUpdateWg.Wait()
		if zbuf := s.csgzBytes.Load(); zbuf != nil && *zbuf != nil {
			return *zbuf, true
		}
		return nil, false
	} else {
		defer s.csgzUpdateMu.Unlock()
		s.csgzUpdateWg.Add(1)
		defer s.csgzUpdateWg.Done()
	}

	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write(buf); err != nil {
		s.csgzBytes.Store(nil)
		s.csgzUpdate.Store(&cur)
		return nil, false
	}
	if err := zw.Close(); err != nil {
		s.csgzBytes.Store(nil)
		s.csgzUpdate.Store(&cur)
		return nil, false
	}

	zbuf := b.Bytes()
	s.csgzBytes.Store(&zbuf)
	s.csgzUpdate.Store(&cur)

	return zbuf, true
}

// csUpdateNextUpdateTime updates the next update time for the cached
// /client/servers response. It must be called after any time updates while
// holding a write lock on s.mu.
func (s *ServerList) csUpdateNextUpdateTime() {
	var u time.Time
	if s.servers1 != nil {
		for _, srv := range s.servers1 {
			if s.deadTime != 0 {
				if x := srv.LastHeartbeat.Add(s.deadTime); u.IsZero() || x.Before(u) {
					u = x
				}
			}
			if s.ghostTime != 0 {
				if x := srv.LastHeartbeat.Add(s.ghostTime); u.IsZero() || x.Before(u) {
					u = x
				}
			}
		}
	}
	// we don't need to check the old value since while we have s.mu, we're the
	// only ones who can write to csNext
	s.csNext.Store(&u)
}

// csForceUpdate forces an update for the cached /client/servers response. It
// must be called after any value updates while holding a write lock on s.mu.
func (s *ServerList) csForceUpdate() {
	s.csForce.Store(true)
}

// GetLiveServers loops over live (i.e., not dead/ghost) servers until fn
// returns false. The order of the servers is non-deterministic.
func (s *ServerList) GetLiveServers(fn func(*Server) bool) {
	t := s.now()

	// take a read lock on the server list
	s.mu.RLock()
	defer s.mu.RUnlock()

	// call fn for live servers
	if s.servers1 != nil {
		for _, srv := range s.servers1 {
			if s.serverState(srv, t) == serverListStateAlive {
				if c := srv.clone(); !fn(&c) {
					break
				}
			}
		}
	}
}

// GetServerByID returns a deep copy of the server with id, or nil if it is
// dead.
func (s *ServerList) GetServerByID(id string) *Server {
	t := s.now()

	// take a read lock on the server list
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.servers2 != nil {
		if srv, ok := s.servers2[id]; ok && s.serverState(srv, t) == serverListStateAlive {
			c := srv.clone()
			return &c
		}
	}
	return nil
}

// DeleteServerByID deletes a server by its ID, returning true if a live server
// was deleted.
func (s *ServerList) DeleteServerByID(id string) bool {
	t := s.now()

	// take a write lock on the server list
	s.mu.Lock()
	defer s.mu.Unlock()

	// force an update when we're finished
	defer s.csForceUpdate()

	// delete the server
	var live bool
	if s.servers2 != nil {
		if esrv, exists := s.servers2[id]; exists {
			if s.serverState(esrv, t) == serverListStateAlive {
				live = true
			}
			s.freeServer(esrv)
		}
	}
	return live
}

var (
	ErrServerListDuplicateAuthAddr = errors.New("already have server with auth addr")
	ErrServerListUpdateServerDead  = errors.New("no server found")
	ErrServerListUpdateWrongIP     = errors.New("wrong server update ip")
	ErrServerListLimitExceeded     = errors.New("would exceed server list limits")
)

// ServerHybridUpdatePut attempts to update a server by the server ID (if u is
// non-nil) (reviving it if necessary), and if that fails, then attempts to
// create/replace a server by the gameserver ip/port instead (if c is non-nil)
// while following the limits in l. It returns a copy of the resulting Server.
//
// If the returned error is non-nil, it will either be an unavoidable internal
// error (e.g, failure to get random data for the server id) or one of the
// following (use errors.Is):
//
//   - ErrServerListDuplicateAuthAddr - if the auth ip/port of the server to create (if c) or revive (if u and server is a ghost) has already been used by a live server
//   - ErrServerListUpdateServerDead - if no server matching the provided id exists (if u) AND c is not provided
//   - ErrServerListUpdateWrongIP - if a server matching the provided id exists, but the ip doesn't match (if u and u.ExpectIP)
//   - ErrServerListLimitExceeded - if adding the server would exceed server limits (if c and l)
//
// When creating a server using the values from c: c.Order, c.ID,
// c.ServerAuthToken, and c.LastHeartbeat will be generated by this function
// (any existing value is ignored).
func (s *ServerList) ServerHybridUpdatePut(u *ServerUpdate, c *Server, l ServerListLimit) (*Server, error) {
	t := s.now()

	// take a write lock on the server list
	s.mu.Lock()
	defer s.mu.Unlock()

	// ensure maps are initialized
	if s.servers1 == nil {
		s.servers1 = make(map[netip.AddrPort]*Server)
	}
	if s.servers2 == nil {
		s.servers2 = make(map[string]*Server)
	}
	if s.servers3 == nil {
		s.servers3 = make(map[netip.AddrPort]*Server)
	}

	// if we have an update
	if u != nil {

		// check if the server with the ID is alive or that u has a heartbeat
		// and the server is a ghost
		if esrv, exists := s.servers2[u.ID]; exists || s.serverState(esrv, t) == serverListStateAlive || (u.Heartbeat && s.serverState(esrv, t) == serverListStateGhost) {

			// ensure a live server hasn't already taken the auth port (which
			// can happen if it was a ghost and a new server got registered)
			if osrv, exists := s.servers3[esrv.AuthAddr()]; !exists || esrv == osrv {

				// check the update ip
				if u.ExpectIP.IsValid() && esrv.Addr.Addr() != u.ExpectIP {
					return nil, ErrServerListUpdateWrongIP
				}

				// do the update
				var changed bool
				if u.Heartbeat {
					esrv.LastHeartbeat, changed = t, true
					s.csUpdateNextUpdateTime()
				}
				if u.Name != nil {
					esrv.Name, changed = *u.Name, true
				}
				if u.Description != nil {
					esrv.Description, changed = *u.Description, true
				}
				if u.Map != nil {
					esrv.Map, changed = *u.Map, true
				}
				if u.Playlist != nil {
					esrv.Playlist, changed = *u.Playlist, true
				}
				if u.PlayerCount != nil {
					esrv.PlayerCount, changed = *u.PlayerCount, true
				}
				if u.MaxPlayers != nil {
					esrv.MaxPlayers, changed = *u.MaxPlayers, true
				}
				if changed {
					s.csForceUpdate()
				}

				// return a copy of the updated server
				r := esrv.clone()
				return &r, nil
			}
		} else {
			if s.serverState(esrv, t) == serverListStateGone {
				s.freeServer(esrv) // if the server we found shouldn't exist anymore, clean it up
			}
		}
		// fallthough - no eligible server to update, try to create one instead
	}

	// create/replace a server instead if we have s
	if s != nil {

		// deep copy the new server info
		nsrv := c.clone()

		// check the addresses
		if !nsrv.Addr.IsValid() {
			return nil, fmt.Errorf("addr is missing")
		}
		if nsrv.AuthPort == 0 {
			return nil, fmt.Errorf("authport is missing")
		}

		// error if there's an existing server with a matching auth addr (note:
		// same ip as gameserver, different port) but different gameserver addr
		// (it's probably a config mistake on the server owner's side)
		if esrv, exists := s.servers3[nsrv.AuthAddr()]; exists && s.serverState(esrv, t) == serverListStateAlive {

			// we want to allow the server to be replaced if the gameserver and
			// authserver addr are the same since it probably just restarted
			// after a crash (it's not like you can have multiple servers
			// listening on the same port with default config, so presumably the
			// old server must be gone anyways)
			if esrv.Addr != nsrv.Addr {
				return nil, fmt.Errorf("%w %s (used for server %s)", ErrServerListDuplicateAuthAddr, nsrv.AuthAddr(), esrv.Addr)
			}
		}

		// we will need to remove an existing server with a matching game
		// address/port if it exists
		var toReplace *Server
		if esrv, exists := s.servers1[nsrv.Addr]; exists {
			if s.serverState(esrv, t) == serverListStateGone {
				s.freeServer(esrv) // if the server we found shouldn't exist anymore, clean it up
			} else {
				toReplace = esrv
			}
		}

		// check limits
		if l.MaxServers != 0 || l.MaxServersPerIP != 0 {
			nSrv, nSrvIP := 1, 1
			for _, esrv := range s.servers1 {
				if s.serverState(esrv, t) == serverListStateAlive && esrv != toReplace {
					if esrv.Addr.Addr() == nsrv.Addr.Addr() {
						nSrvIP++
					}
					nSrv++
				}
			}
			if l.MaxServers > 0 && nSrv > l.MaxServers {
				return nil, fmt.Errorf("%w: too many servers (%d)", ErrServerListLimitExceeded, nSrv)
			}
			if l.MaxServersPerIP > 0 && nSrvIP > l.MaxServersPerIP {
				return nil, fmt.Errorf("%w: too many servers for ip %s (%d)", ErrServerListLimitExceeded, nsrv.Addr.Addr(), nSrv)
			}
		}

		// generate a new server token
		if tok, err := cryptoRandHex(32); err != nil {
			return nil, fmt.Errorf("generate new server auth token: %w", err)
		} else {
			nsrv.ServerAuthToken = tok
		}

		// allocate a new server ID, skipping any which already exist
		for {
			sid, err := cryptoRandHex(32)
			if err != nil {
				return nil, fmt.Errorf("generate new server id: %w", err)
			}
			if _, exists := s.servers2[sid]; exists {
				continue // try another id since another server already used it
			}
			nsrv.ID = sid
			break
		}

		// set the server order
		nsrv.Order = s.order.Add(1)

		// set the heartbeat time to the current time
		nsrv.LastHeartbeat = t

		// remove the existing server so we can add the new one
		if toReplace != nil {
			s.freeServer(toReplace)
		}

		// add the new one (the pointers MUST be the same or stuff will break)
		s.servers1[nsrv.Addr] = &nsrv
		s.servers2[nsrv.ID] = &nsrv
		s.servers3[nsrv.AuthAddr()] = &nsrv

		// trigger /client/servers updates
		s.csForceUpdate()
		s.csUpdateNextUpdateTime()

		// return a copy of the new server
		r := nsrv.clone()
		return &r, nil
	}

	// if we don't have an update or a new server to create/replace instead, we
	// didn't find any eligible live servers, so...
	return nil, ErrServerListUpdateServerDead
}

// ReapServers deletes dead servers from memory.
func (s *ServerList) ReapServers() {
	t := s.now()

	// take a write lock on the server list
	s.mu.Lock()
	defer s.mu.Unlock()

	// reap servers
	//
	// note: unlike a slice, it's safe to delete while looping over a map
	if s.servers1 != nil {
		for _, srv := range s.servers1 {
			if s.serverState(srv, t) == serverListStateGone {
				s.freeServer(srv)
			}
		}
	}
}

// freeServer frees the provided server from memory. It must be called while a
// write lock is held on s.
func (s *ServerList) freeServer(x *Server) {
	// we need to ensure that we only delete a server from the indexes if the
	// index is pointing to our specific server since a new server with the same
	// address could have replaced it
	if s.servers1 != nil {
		if esrv, exists := s.servers1[x.Addr]; exists && esrv == x {
			delete(s.servers1, x.Addr)
		}
	}
	if s.servers2 != nil {
		if esrv, exists := s.servers2[x.ID]; exists && esrv == x {
			delete(s.servers2, x.ID)
		}
	}
	if s.servers3 != nil {
		if esrv, exists := s.servers3[x.AuthAddr()]; exists && esrv == x {
			delete(s.servers3, x.AuthAddr())
		}
	}
}

type serverListState int

const (
	serverListStatePending serverListState = iota
	serverListStateAlive
	serverListStateGhost
	serverListStateGone
)

func (s *ServerList) serverState(x *Server, t time.Time) serverListState {
	if x == nil {
		return serverListStateGone
	}

	d := t.Sub(x.LastHeartbeat)
	if s.deadTime == 0 || d < s.deadTime {
		return serverListStateAlive
	}
	if s.ghostTime == 0 || d < s.ghostTime {
		return serverListStateGhost
	}
	return serverListStateGone
}

func (s *ServerList) now() time.Time {
	if s.__clock != nil {
		return s.__clock()
	}
	return time.Now()
}

// jsonSafeSet is encoding/json.safeSet.
var jsonSafeSet = [utf8.RuneSelf]bool{
	' ':      true,
	'!':      true,
	'"':      false,
	'#':      true,
	'$':      true,
	'%':      true,
	'&':      true,
	'\'':     true,
	'(':      true,
	')':      true,
	'*':      true,
	'+':      true,
	',':      true,
	'-':      true,
	'.':      true,
	'/':      true,
	'0':      true,
	'1':      true,
	'2':      true,
	'3':      true,
	'4':      true,
	'5':      true,
	'6':      true,
	'7':      true,
	'8':      true,
	'9':      true,
	':':      true,
	';':      true,
	'<':      true,
	'=':      true,
	'>':      true,
	'?':      true,
	'@':      true,
	'A':      true,
	'B':      true,
	'C':      true,
	'D':      true,
	'E':      true,
	'F':      true,
	'G':      true,
	'H':      true,
	'I':      true,
	'J':      true,
	'K':      true,
	'L':      true,
	'M':      true,
	'N':      true,
	'O':      true,
	'P':      true,
	'Q':      true,
	'R':      true,
	'S':      true,
	'T':      true,
	'U':      true,
	'V':      true,
	'W':      true,
	'X':      true,
	'Y':      true,
	'Z':      true,
	'[':      true,
	'\\':     false,
	']':      true,
	'^':      true,
	'_':      true,
	'`':      true,
	'a':      true,
	'b':      true,
	'c':      true,
	'd':      true,
	'e':      true,
	'f':      true,
	'g':      true,
	'h':      true,
	'i':      true,
	'j':      true,
	'k':      true,
	'l':      true,
	'm':      true,
	'n':      true,
	'o':      true,
	'p':      true,
	'q':      true,
	'r':      true,
	's':      true,
	't':      true,
	'u':      true,
	'v':      true,
	'w':      true,
	'x':      true,
	'y':      true,
	'z':      true,
	'{':      true,
	'|':      true,
	'}':      true,
	'~':      true,
	'\u007f': true,
}

// encodeJSONString is based on encoding/json.encodeState.stringBytes.
func encodeJSONString(e *bytes.Buffer, s []byte) {
	const hex = "0123456789abcdef"

	e.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if jsonSafeSet[b] {
				i++
				continue
			}
			if start < i {
				e.Write(s[start:i])
			}
			e.WriteByte('\\')
			switch b {
			case '\\', '"':
				e.WriteByte(b)
			case '\n':
				e.WriteByte('n')
			case '\r':
				e.WriteByte('r')
			case '\t':
				e.WriteByte('t')
			default:
				// This encodes bytes < 0x20 except for \t, \n and \r.
				// If escapeHTML is set, it also escapes <, >, and &
				// because they can lead to security holes when
				// user-controlled strings are rendered into JSON
				// and served to some browsers.
				e.WriteString(`u00`)
				e.WriteByte(hex[b>>4])
				e.WriteByte(hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRune(s[i:])
		if c == utf8.RuneError && size == 1 {
			if start < i {
				e.Write(s[start:i])
			}
			e.WriteString(`\ufffd`)
			i += size
			start = i
			continue
		}
		// U+2028 is LINE SEPARATOR.
		// U+2029 is PARAGRAPH SEPARATOR.
		// They are both technically valid characters in JSON strings,
		// but don't work in JSONP, which has to be evaluated as JavaScript,
		// and can lead to security holes there. It is valid JSON to
		// escape them, so we do so unconditionally.
		// See http://timelessrepo.com/json-isnt-a-javascript-subset for discussion.
		if c == '\u2028' || c == '\u2029' {
			if start < i {
				e.Write(s[start:i])
			}
			e.WriteString(`\u202`)
			e.WriteByte(hex[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	if start < len(s) {
		e.Write(s[start:])
	}
	e.WriteByte('"')
}
