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
	s.ModInfo = m
	return s
}

type ServerUpdate struct {
	Heartbeat   bool
	Name        *string
	Description *string
	PlayerCount *int
	MaxPlayers  *int
	Map         *string
	Playlist    *string
}

// NewServerList initializes a new server list.
//
// deadTime is the time since the last heartbeat after which a server is
// considered dead. A dead server will not be listed on the server list.
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
			if s.isServerAlive(srv, t) {
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
			if s.isServerAlive(srv, t) {
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
		if srv, ok := s.servers2[id]; ok && s.isServerAlive(srv, t) {
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

	var live bool
	if s.servers2 != nil {
		if esrv, exists := s.servers2[id]; exists {
			if s.isServerAlive(esrv, t) {
				live = true
			}
			s.freeServer(esrv)
		}
	}
	return live
}

// GetServerCountByIP gets the number of live servers for the given IP address.
// If ip is the zero netip.Addr, the total number of live servers are returned.
func (s *ServerList) GetServerByIP(ip netip.Addr) int {
	// take a read lock on the server list
	s.mu.RLock()
	defer s.mu.RUnlock()

	// count the servers
	var n int
	if s.servers1 != nil {
		for _, srv := range s.servers1 {
			if !ip.IsValid() || srv.Addr.Addr() != ip {
				n++
			}
		}
	}
	return n
}

// ErrServerListDuplicateAuthAddr is returned by PutServerByAddr if the auth
// addr is already used by another live server.
var ErrServerListDuplicateAuthAddr = errors.New("already have server with auth addr")

// PutServerByAddr creates or replaces a server by x.Addr and returns the new
// server ID (x.ID, x.LastHeartbeat, and x.Order is ignored). The bool
// represents whether a live server was replaced. An error is only returned if
// it fails to generate a new ID, if x.Addr is not set, or
// ErrServerListDuplicateAuthAddr if the auth port is duplicated by another live
// server (if so, the server list remains unchanged). Note that even if a ghost
// server has a matching Addr, a new server with a new ID is created (use
// UpdateServerByID to revive servers).
func (s *ServerList) PutServerByAddr(x *Server) (string, bool, error) {
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

	// force an update when we're finished
	defer s.csForceUpdate()
	defer s.csUpdateNextUpdateTime()

	// deep copy the server info
	nsrv := x.clone()

	// check the addresses
	if !nsrv.Addr.IsValid() {
		return "", false, fmt.Errorf("addr is missing")
	}
	if nsrv.AuthPort == 0 {
		return "", false, fmt.Errorf("authport is missing")
	}

	// error if there's an existing server with a matching auth addr
	if esrv, exists := s.servers3[nsrv.AuthAddr()]; exists {
		if s.isServerAlive(esrv, t) {
			return "", false, fmt.Errorf("%w %s (used for server %s)", ErrServerListDuplicateAuthAddr, nsrv.AuthAddr(), esrv.Addr)
		}
	}

	// allocate a new server ID, skipping any which already exist
	for {
		sid, err := cryptoRandHex(32)
		if err != nil {
			return "", false, fmt.Errorf("failed to generate new server id: %w", err)
		}
		if _, exists := s.servers2[sid]; exists {
			continue // try another id
		}
		nsrv.ID = sid
		break
	}

	// set the heartbeat time to the current time
	nsrv.LastHeartbeat = t

	// set the server order
	nsrv.Order = s.order.Add(1)

	// remove any existing server with a matching game address/port
	var replaced bool
	if esrv, exists := s.servers1[nsrv.Addr]; exists {
		if s.isServerAlive(esrv, t) {
			replaced = true
		}
		s.freeServer(esrv)
	}

	// add it to the indexes (the pointers MUST be the same or stuff will break)
	s.servers1[nsrv.Addr] = &nsrv
	s.servers2[nsrv.ID] = &nsrv
	s.servers3[nsrv.AuthAddr()] = &nsrv

	// return the new ID
	return nsrv.ID, replaced, nil
}

// UpdateServerByID updates values for the server with the provided ID,
// returning false if it is dead (and couldn't be revived by the heartbeat, if
// any).
func (s *ServerList) UpdateServerByID(id string, u *ServerUpdate) bool {
	t := s.now()

	// take a write lock on the server list
	s.mu.Lock()
	defer s.mu.Unlock()

	// if the map isn't initialized, we don't have any servers
	if s.servers2 == nil {
		return false
	}

	// force an update when we're finished
	defer s.csForceUpdate()
	defer s.csUpdateNextUpdateTime()

	// get the server if it's eligible for updates
	esrv, exists := s.servers2[id]
	if !(exists || s.isServerAlive(esrv, t) || (u.Heartbeat && s.isServerGhost(esrv, t))) {
		if s.isServerGone(esrv, t) {
			s.freeServer(esrv)
		}
		return false
	}

	// ensure another server hasn't already taken the auth port (which can
	// happen if it was a ghost)
	if osrv, exists := s.servers3[esrv.AuthAddr()]; exists && esrv != osrv {
		return false
	}

	// do the update
	if u.Heartbeat {
		esrv.LastHeartbeat = t
		s.csUpdateNextUpdateTime()
	}
	if u.Name != nil {
		esrv.Name = *u.Name
	}
	if u.Description != nil {
		esrv.Description = *u.Description
	}
	if u.Map != nil {
		esrv.Map = *u.Map
	}
	if u.Playlist != nil {
		esrv.Playlist = *u.Playlist
	}
	if u.PlayerCount != nil {
		esrv.PlayerCount = *u.PlayerCount
	}
	if u.MaxPlayers != nil {
		esrv.MaxPlayers = *u.MaxPlayers
	}
	s.csForceUpdate()
	return true
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
			if s.isServerGone(srv, t) {
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

// TODO: backup/restore (remember to reset cs cache, ensure heartbeat times are not in the future, and set the order var)

func (s *ServerList) isServerAlive(x *Server, t time.Time) bool {
	if x == nil {
		return false
	}
	if s.deadTime == 0 {
		return true
	}
	d := t.Sub(x.LastHeartbeat)
	return s.deadTime == 0 || d < s.deadTime
}

func (s *ServerList) isServerGhost(x *Server, t time.Time) bool {
	if x == nil {
		return false
	}
	if s.deadTime == 0 || s.ghostTime == 0 {
		return true
	}
	d := t.Sub(x.LastHeartbeat)
	return d >= s.deadTime && d < s.ghostTime
}

func (s *ServerList) isServerGone(x *Server, t time.Time) bool {
	if x == nil {
		return false
	}
	if s.deadTime == 0 || s.ghostTime == 0 {
		return false
	}
	d := t.Sub(x.LastHeartbeat)
	return d >= s.deadTime && d >= s.ghostTime
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
