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
)

// Handler serves requests for the original master server API.
type Handler struct {
	// AccountStorage stores accounts. It must be non-nil.
	AccountStorage AccountStorage

	// PdataStorage stores player data. It must be non-nil.
	PdataStorage PdataStorage

	// MainMenuPromos gets the main menu promos to return for a request.
	MainMenuPromos func(*http.Request) MainMenuPromos

	// NotFound handles requests not handled by this Handler.
	NotFound http.Handler

	// InsecureDevNoCheckPlayerAuth is an option you shouldn't use since it
	// makes the server trust that clients are who they say they are. Blame
	// @BobTheBob9 for this option even existing in the first place.
	InsecureDevNoCheckPlayerAuth bool
}

// ServeHTTP routes requests to Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "Atlas")

	switch r.URL.Path {
	case "/client/mainmenupromos":
		h.handleMainMenuPromos(w, r)
	case "/client/auth_with_self":
		h.handleClientAuthWithSelf(w, r)
	case "/accounts/write_persistence":
		h.handleAccountsWritePersistence(w, r)
	case "/accounts/get_username":
		h.handleAccountsGetUsername(w, r)
	case "/accounts/lookup_uid":
		h.handleAccountsLookupUID(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/player/") {
			// TODO: rate limit
			h.handlePlayer(w, r)
			return
		}
		if h.NotFound == nil {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		} else {
			h.NotFound.ServeHTTP(w, r)
		}
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
