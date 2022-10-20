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
		h.m().accounts_writepersistence_requests_total.http_method_not_allowed.Inc()
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
		h.m().accounts_writepersistence_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_BAD_REQUEST.MessageObjf("failed to parse multipart form: %v", err))
		return
	}

	pf, pfHdr, err := r.FormFile("pdata")
	if err != nil {
		h.m().accounts_writepersistence_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_BAD_REQUEST.MessageObjf("missing pdata file: %v", err))
		return
	}
	defer pf.Close()

	if pfHdr.Size > (2 << 20) {
		h.m().accounts_writepersistence_requests_total.reject_too_large.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_BAD_REQUEST.MessageObjf("pdata file is too large"))
		return
	}

	buf, err := io.ReadAll(pf)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to read uploaded data file (size: %d)", pfHdr.Size)
		h.m().accounts_writepersistence_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	var pd pdata.Pdata
	if err := pd.UnmarshalBinary(buf); err != nil {
		hlog.FromRequest(r).Warn().
			Err(err).
			Msgf("invalid pdata rejected")
		h.m().accounts_writepersistence_requests_total.reject_invalid_pdata.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("invalid pdata"))
		return
	}

	if len(pd.ExtraData) > 512 { // arbitrary limit
		hlog.FromRequest(r).Warn().
			Err(err).
			Msgf("pdata with too much trailing junk rejected")
		h.m().accounts_writepersistence_requests_total.reject_too_much_extradata.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("invalid pdata"))
		return
	}

	h.m().accounts_writepersistence_extradata_size_bytes.Update(float64(len(pd.ExtraData)))

	uidQ := r.URL.Query().Get("id")
	if uidQ == "" {
		h.m().accounts_writepersistence_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("id param is required"))
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		h.m().accounts_writepersistence_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	serverID := r.URL.Query().Get("serverId") // blank on listen server

	raddr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to parse remote ip %q", r.RemoteAddr)
		h.m().accounts_writepersistence_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		h.m().accounts_writepersistence_requests_total.fail_storage_error_account.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}
	if acct == nil {
		h.m().accounts_writepersistence_requests_total.reject_player_not_found.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	if acct.IsOnOwnServer() {
		if acct.AuthIP != raddr.Addr() {
			h.m().accounts_writepersistence_requests_total.reject_unauthorized.Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObj())
			return
		}
	} else {
		srv := h.ServerList.GetServerByID(serverID)
		if srv == nil {
			h.m().accounts_writepersistence_requests_total.reject_unauthorized.Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObjf("no such game server"))
			return
		}
		if srv.Addr.Addr() != raddr.Addr() {
			h.m().accounts_writepersistence_requests_total.reject_unauthorized.Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObj())
			return
		}
		if acct.LastServerID != srv.ID {
			h.m().accounts_writepersistence_requests_total.reject_unauthorized.Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObj())
			return
		}
	}

	if n, err := h.PdataStorage.SetPdata(uid, buf); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to save pdata")
		h.m().accounts_writepersistence_requests_total.fail_storage_error_pdata.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	} else {
		h.m().accounts_writepersistence_stored_size_bytes.Update(float64(n))
	}

	h.m().accounts_writepersistence_requests_total.success.Inc()
	respJSON(w, r, http.StatusOK, nil)
}

func (h *Handler) handleAccountsLookupUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		h.m().accounts_lookupuid_requests_total.http_method_not_allowed.Inc()
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
		h.m().accounts_lookupuid_requests_total.reject_bad_request.Inc()
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success":  false,
			"username": "",
			"matches":  []uint64{},
			"error":    ErrorCode_BAD_REQUEST.MessageObjf("username param is required"),
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
		h.m().accounts_lookupuid_requests_total.fail_storage_error_account.Inc()
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success":  false,
			"username": username,
			"matches":  []uint64{},
			"error":    ErrorCode_INTERNAL_SERVER_ERROR.MessageObj(),
		})
		return
	}

	switch len(uids) {
	case 0:
		h.m().accounts_lookupuid_requests_total.success_nomatch.Inc()
	case 1:
		h.m().accounts_lookupuid_requests_total.success_singlematch.Inc()
	default:
		h.m().accounts_lookupuid_requests_total.success_multimatch.Inc()
	}
	respJSON(w, r, http.StatusOK, map[string]any{
		"success":  true,
		"username": username,
		"matches":  uids,
	})
}

func (h *Handler) handleAccountsGetUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		h.m().accounts_getusername_requests_total.http_method_not_allowed.Inc()
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
		h.m().accounts_getusername_requests_total.reject_bad_request.Inc()
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"uid":     "",
			"matches": []string{},
			"error":   ErrorCode_BAD_REQUEST.MessageObjf("uid param is required"),
		})
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		h.m().accounts_getusername_requests_total.reject_bad_request.Inc()
		respJSON(w, r, http.StatusNotFound, map[string]any{
			"success": false,
			"uid":     strconv.FormatUint(uid, 10),
			"matches": []string{},
			"error":   ErrorCode_PLAYER_NOT_FOUND.MessageObj(),
		})
		return
	}

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		h.m().accounts_getusername_requests_total.fail_storage_error_account.Inc()
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"uid":     strconv.FormatUint(uid, 10),
			"matches": []string{},
			"error":   ErrorCode_INTERNAL_SERVER_ERROR.MessageObj(),
		})
		return
	}

	var username string
	if acct != nil {
		username = acct.Username
	}
	if username == "" {
		h.m().accounts_getusername_requests_total.success_match.Inc()
	} else {
		h.m().accounts_getusername_requests_total.success_missing.Inc()
	}
	respJSON(w, r, http.StatusOK, map[string]any{
		"success": true,
		"uid":     strconv.FormatUint(uid, 10),
		"matches": []string{username}, // yes, this may be an empty string if we don't know what it is
	})
}
