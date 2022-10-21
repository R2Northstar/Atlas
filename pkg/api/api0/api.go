// Package api0 implements the original master server API.
//
// External differences:
//   - Proper HTTP response codes are used (this won't break anything since existing code doesn't check them).
//   - Caching headers are supported and used where appropriate.
//   - Pdiff stuff has been removed (this was never fully implemented to begin with; see docs/PDATA.md).
//   - Error messages have been improved. Enum values remain the same for compatibility.
//   - Some rate limits (no longer necessary due to increased performance and better caching) have been removed.
//   - More HTTP methods and features are supported (e.g., HEAD, OPTIONS, Content-Encoding).
//   - Website split into a separate handler (set Handler.NotFound to http.HandlerFunc(web.ServeHTTP) for identical behaviour).
//   - /accounts/write_persistence returns a error message for easier debugging.
//   - Alive/dead servers can be replaced by a new successful registration from the same ip/port. This eliminates the main cause of the duplicate server error requiring retries, and doesn't add much risk since you need to custom fuckery to start another server when you're already listening on the port.
package api0

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/r2northstar/atlas/pkg/origin"
	"github.com/rs/zerolog/hlog"
	"golang.org/x/mod/semver"
)

// Handler serves requests for the original master server API.
type Handler struct {
	// ServerList stores registered servers.
	ServerList *ServerList

	// AccountStorage stores accounts. It must be non-nil.
	AccountStorage AccountStorage

	// PdataStorage stores player data. It must be non-nil.
	PdataStorage PdataStorage

	// OriginAuthMgr manages Origin nucleus tokens (used for checking
	// usernames). If not provided, usernames will not be updated.
	OriginAuthMgr *origin.AuthMgr

	// CleanBadWords is used to filter bad words from server names and
	// descriptions. If not provided, words will not be filtered.
	CleanBadWords func(s string) string

	// MainMenuPromos gets the main menu promos to return for a request.
	MainMenuPromos func(*http.Request) MainMenuPromos

	// NotFound handles requests not handled by this Handler.
	NotFound http.Handler

	// MaxServers limits the number of registered servers. If -1, no limit is
	// applied. If 0, a reasonable default is used.
	MaxServers int

	// MaxServersPerIP limits the number of registered servers per IP. If -1, no
	// limit is applied. If 0, a reasonable default is used.
	MaxServersPerIP int

	// InsecureDevNoCheckPlayerAuth is an option you shouldn't use since it
	// makes the server trust that clients are who they say they are. Blame
	// @BobTheBob9 for this option even existing in the first place.
	InsecureDevNoCheckPlayerAuth bool

	// MinimumLauncherVersion restricts authentication and server registration
	// to clients with at least this version, which must be valid semver. +dev
	// versions are always allowed.
	MinimumLauncherVersion string

	// TokenExpiryTime controls the expiry of player masterserver auth tokens.
	// If zero, a reasonable a default is used.
	TokenExpiryTime time.Duration

	// AllowGameServerIPv6 controls whether to allow game servers to use IPv6.
	AllowGameServerIPv6 bool

	metricsInit sync.Once
	metricsObj  apiMetrics
}

// ServeHTTP routes requests to Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var notPanicked bool // this lets us catch panics without swallowing them
	defer func() {
		if !notPanicked {
			h.m().request_panics_total.Inc()
		}
	}()

	w.Header().Set("Server", "Atlas")

	switch r.URL.Path {
	case "/client/mainmenupromos":
		h.handleMainMenuPromos(w, r)
	case "/client/origin_auth":
		h.handleClientOriginAuth(w, r)
	case "/client/auth_with_server":
		h.handleClientAuthWithServer(w, r)
	case "/client/auth_with_self":
		h.handleClientAuthWithSelf(w, r)
	case "/client/servers":
		h.handleClientServers(w, r)
	case "/server/add_server", "/server/update_values", "/server/heartbeat":
		h.handleServerUpsert(w, r)
	case "/server/remove_server":
		h.handleServerRemove(w, r)
	case "/accounts/write_persistence":
		h.handleAccountsWritePersistence(w, r)
	case "/accounts/get_username":
		h.handleAccountsGetUsername(w, r)
	case "/accounts/lookup_uid":
		h.handleAccountsLookupUID(w, r)
	case "/player/pdata", "/player/info", "/player/stats", "/player/loadout":
		h.handlePlayer(w, r)
	default:
		if h.NotFound == nil {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		} else {
			notPanicked = true
			h.NotFound.ServeHTTP(w, r)
		}
	}
	notPanicked = true
}

// checkLauncherVersion checks if the r was made by NorthstarLauncher and if it
// is at least MinimumLauncherVersion.
func (h *Handler) checkLauncherVersion(r *http.Request) bool {
	rver, _, _ := strings.Cut(r.Header.Get("User-Agent"), " ")
	if x := strings.TrimPrefix(rver, "R2Northstar/"); rver != x {
		if len(x) > 0 && x[0] != 'v' {
			rver = "v" + x
		} else {
			rver = x
		}
	} else {
		h.m().versiongate_checks_total.reject_notns.Inc()
		return false // deny: not R2Northstar
	}

	mver := h.MinimumLauncherVersion
	if mver != "" {
		if mver[0] != 'v' {
			mver = "v" + mver
		}
	} else {
		h.m().versiongate_checks_total.success_ok.Inc()
		return true // allow: no minimum version
	}
	if !semver.IsValid(mver) {
		hlog.FromRequest(r).Warn().Msgf("not checking invalid minimum version %q", mver)
		h.m().versiongate_checks_total.success_ok.Inc()
		return true // allow: invalid minimum version
	}

	if strings.HasSuffix(rver, "+dev") {
		h.m().versiongate_checks_total.success_dev.Inc()
		return true // allow: dev versions
	}
	if !semver.IsValid(rver) {
		h.m().versiongate_checks_total.reject_invalid.Inc()
		return false // deny: invalid version
	}

	if semver.Compare(rver, mver) < 0 {
		h.m().versiongate_checks_total.reject_old.Inc()
		return false // deny: too old
	}

	h.m().versiongate_checks_total.success_ok.Inc()
	return true
}

// extractLauncherVersion extracts the launcher version from r, returning an
// empty string if it's missing or invalid.
func (h *Handler) extractLauncherVersion(r *http.Request) string {
	rver, _, _ := strings.Cut(r.Header.Get("User-Agent"), " ")
	if x := strings.TrimPrefix(rver, "R2Northstar/"); rver != x {
		if len(x) > 0 && x[0] != 'v' {
			rver = "v" + x
		} else {
			rver = x
		}
	} else {
		rver = ""
	}
	if rver != "" && semver.IsValid(rver) {
		return rver[1:]
	}
	return ""
}

// respFail writes a {success:false,error:ErrorObj} response with the provided
// response status.
func respFail(w http.ResponseWriter, r *http.Request, status int, obj ErrorObj) {
	if rid, ok := hlog.IDFromRequest(r); ok {
		respJSON(w, r, status, map[string]any{
			"success":    false,
			"error":      obj,
			"request_id": rid.String(),
		})
	} else {
		respJSON(w, r, status, map[string]any{
			"success": false,
			"error":   obj,
		})
	}
}

// respJSON writes the JSON encoding of obj with the provided response status.
func respJSON(w http.ResponseWriter, r *http.Request, status int, obj any) {
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}
	buf, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	hlog.FromRequest(r).Trace().Msgf("json api response %.2048s", string(buf))
	buf = append(buf, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	w.WriteHeader(status)
	w.Write(buf)
}

// respMaybeCompress writes buf with the provided response status, compressing
// it with gzip if the client supports it and the result is smaller.
func respMaybeCompress(w http.ResponseWriter, r *http.Request, status int, buf []byte) {
	for _, e := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if t, _, _ := strings.Cut(e, ";"); strings.TrimSpace(t) == "gzip" {
			var cbuf bytes.Buffer
			gw := gzip.NewWriter(&cbuf)
			if _, err := gw.Write(buf); err != nil {
				break
			}
			if err := gw.Close(); err != nil {
				break
			}
			if cbuf.Len() < int(float64(len(buf))*0.8) {
				buf = cbuf.Bytes()
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Del("ETag") // to avoid breaking caching proxies since ETag must be unique if Content-Encoding is different
			}
			break
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(buf)
	}
}

// cryptoRandHex gets a string of random hex digits with length n.
func cryptoRandHex(n int) (string, error) {
	b := make([]byte, (n+1)/2) // round up
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:n], nil
}

// marshalJSONBytesAsArray marshals b as an array of numbers (rather than the
// default of base64).
func marshalJSONBytesAsArray(b []byte) json.RawMessage {
	var e bytes.Buffer
	e.Grow(2 + len(b)*3)
	e.WriteByte('[')
	for i, c := range b {
		if i != 0 {
			e.WriteByte(',')
		}
		e.WriteString(strconv.FormatUint(uint64(c), 10))
	}
	e.WriteByte(']')
	return json.RawMessage(e.Bytes())
}
