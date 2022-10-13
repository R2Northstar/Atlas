package api0

import (
	"io"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/pg9182/atlas/pkg/pdata"
	"github.com/rs/zerolog/hlog"
)

func (h *Handler) handleAccountsWritePersistence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// - do not ever cache
	// - do not share between users
	w.Header().Set("Cache-Control", "private, no-cache, no-store, max-age=0, must-revalidate") // equivalent to no-store -- but the rest is a fallback
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, POST")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := r.ParseMultipartForm(2 << 20); err != nil {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("failed to parse multipart form: %v", err),
		})
		return
	}

	pf, pfHdr, err := r.FormFile("file.pdata")
	if err != nil {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("missing pdata file: %v", err),
		})
		return
	}
	defer pf.Close()

	if pfHdr.Size > (2 << 20) {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("pdata file is too large"),
		})
		return
	}

	buf, err := io.ReadAll(pf)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to read uploaded data file (size: %d)", pfHdr.Size)
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	var pd pdata.Pdata
	if err := pd.UnmarshalBinary(buf); err != nil {
		hlog.FromRequest(r).Warn().
			Err(err).
			Msgf("invalid pdata rejected")
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("invalid pdata"),
		})
		return
	}

	if len(pd.ExtraData) > 512 { // arbitrary limit
		hlog.FromRequest(r).Warn().
			Err(err).
			Msgf("pdata with too much trailing junk rejected")
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("invalid pdata"),
		})
		return
	}

	uidQ := r.URL.Query().Get("id")
	if uidQ == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("id param is required"),
		})
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"error":   ErrorCode_PLAYER_NOT_FOUND,
		})
		return
	}

	serverID := r.URL.Query().Get("serverId")
	if serverID == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("serverId param is required"),
		})
		return
	}
	// TODO: check serverID

	raddr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to parse remote ip %q", r.RemoteAddr)
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}
	if acct == nil {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"error":   ErrorCode_PLAYER_NOT_FOUND,
		})
		return
	}

	if acct.IsOnOwnServer() {
		if acct.AuthIP != raddr.Addr() {
			respJSON(w, r, http.StatusForbidden, map[string]any{
				"success": false,
				"error":   ErrorCode_UNAUTHORIZED_GAMESERVER,
			})
			return
		}
	} else {
		// TODO: check if gameserver ip matches and that account is on it
	}

	if err := h.PdataStorage.SetPdata(uid, buf); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to save pdata")
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	respJSON(w, r, http.StatusOK, nil)
}

func (h *Handler) handleAccountsLookupUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// - do not ever cache (we want to know about all requests)
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate") // equivalent to no-store -- but the rest is a fallback
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, HEAD, GET")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success":  false,
			"username": "",
			"matches":  []uint64{},
			"error":    ErrorCode_BAD_REQUEST,
			"msg":      ErrorCode_BAD_REQUEST.Messagef("username param is required"),
		})
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	uids, err := h.AccountStorage.GetUIDsByUsername(username)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to find account uids from storage for %q", username)
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success":  false,
			"username": username,
			"matches":  []uint64{},
			"error":    ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":      ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	respJSON(w, r, http.StatusOK, map[string]any{
		"success":  false,
		"username": username,
		"matches":  uids,
	})
}

func (h *Handler) handleAccountsGetUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// - do not ever cache (we want to know about all requests)
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate") // equivalent to no-store -- but the rest is a fallback
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, HEAD, GET")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	uidQ := r.URL.Query().Get("uid")
	if uidQ == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"uid":     "",
			"matches": []string{},
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("uid param is required"),
		})
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"uid":     strconv.FormatUint(uid, 10),
			"matches": []string{},
			"error":   ErrorCode_PLAYER_NOT_FOUND,
		})
		return
	}

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}
	if acct == nil {
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"uid":     strconv.FormatUint(uid, 10),
			"matches": []string{},
			"error":   ErrorCode_PLAYER_NOT_FOUND,
		})
		return
	}

	respJSON(w, r, http.StatusOK, map[string]any{
		"success": true,
		"uid":     strconv.FormatUint(uid, 10),
		"matches": []string{acct.Username}, // yes, this may be an empty string if we don't know what it is
	})
}
