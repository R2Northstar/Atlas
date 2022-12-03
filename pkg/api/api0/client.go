package api0

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/r2northstar/atlas/pkg/api/api0/api0gameserver"
	"github.com/r2northstar/atlas/pkg/origin"
	"github.com/r2northstar/atlas/pkg/pdata"
	"github.com/r2northstar/atlas/pkg/stryder"
	"github.com/rs/zerolog/hlog"
)

type MainMenuPromos struct {
	NewInfo      MainMenuPromosNew         `json:"newInfo"`
	LargeButton  MainMenuPromosButtonLarge `json:"largeButton"`
	SmallButton1 MainMenuPromosButtonSmall `json:"smallButton1"`
	SmallButton2 MainMenuPromosButtonSmall `json:"smallButton2"`
}

type MainMenuPromosNew struct {
	Title1 string `json:"Title1"`
	Title2 string `json:"Title2"`
	Title3 string `json:"Title3"`
}

type MainMenuPromosButtonLarge struct {
	Title      string `json:"Title"`
	Text       string `json:"Text"`
	Url        string `json:"Url"`
	ImageIndex int    `json:"ImageIndex"`
}

type MainMenuPromosButtonSmall struct {
	Title      string `json:"Title"`
	Url        string `json:"Url"`
	ImageIndex int    `json:"ImageIndex"`
}

func (h *Handler) handleMainMenuPromos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		h.m().client_mainmenupromos_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, HEAD, GET")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.m().client_mainmenupromos_requests_total.success(h.extractLauncherVersion(r)).Inc()
	h.geoCounter2(r, h.m().client_mainmenupromos_requests_map)

	var p MainMenuPromos
	if h.MainMenuPromos != nil {
		p = h.MainMenuPromos(r)
	}
	respJSON(w, r, http.StatusOK, p)
}

func (h *Handler) handleClientOriginAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodGet { // no HEAD support intentionally
		h.m().client_originauth_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, POST")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !h.checkLauncherVersion(r) {
		h.m().client_originauth_requests_total.reject_versiongate.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_UNSUPPORTED_VERSION.MessageObj())
		return
	}

	uidQ := r.URL.Query().Get("id")
	if uidQ == "" {
		h.m().client_originauth_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("id param is required"))
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		h.m().client_originauth_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	raddr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to parse remote ip %q", r.RemoteAddr)
		h.m().client_originauth_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	if !h.InsecureDevNoCheckPlayerAuth {
		token := r.URL.Query().Get("token")
		if token == "" {
			h.m().client_originauth_requests_total.reject_bad_request.Inc()
			respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("token param is required"))
			return
		}

		stryderStart := time.Now()

		stryderCtx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()

		stryderRes, err := stryder.NucleusAuth(stryderCtx, token, uid)
		h.m().client_originauth_stryder_auth_duration_seconds.UpdateDuration(stryderStart)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				// ignore
			case errors.Is(err, stryder.ErrInvalidGame):
				h.m().client_originauth_requests_total.reject_stryder_invalidgame.Inc()
			case errors.Is(err, stryder.ErrInvalidToken):
				h.m().client_originauth_requests_total.reject_stryder_invalidtoken.Inc()
			case errors.Is(err, stryder.ErrMultiplayerNotAllowed):
				h.m().client_originauth_requests_total.reject_stryder_mpnotallowed.Inc()
			case errors.Is(err, stryder.ErrStryder):
				h.m().client_originauth_requests_total.reject_stryder_other.Inc()
			default:
				h.m().client_originauth_requests_total.fail_stryder_error.Inc()
			}
			switch {
			case errors.Is(err, stryder.ErrInvalidGame):
				fallthrough
			case errors.Is(err, stryder.ErrInvalidToken):
				fallthrough
			case errors.Is(err, stryder.ErrMultiplayerNotAllowed):
				hlog.FromRequest(r).Info().
					Err(err).
					Uint64("uid", uid).
					Str("stryder_token", string(token)).
					Str("stryder_resp", string(stryderRes)).
					Msgf("invalid stryder token")
				respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAME.MessageObj())
				return
			case errors.Is(err, stryder.ErrStryder):
				hlog.FromRequest(r).Error().
					Err(err).
					Uint64("uid", uid).
					Str("stryder_token", string(token)).
					Str("stryder_resp", string(stryderRes)).
					Msgf("unexpected stryder error")
				respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
				return
			default:
				if !errors.Is(err, context.Canceled) {
					hlog.FromRequest(r).Error().
						Err(err).
						Uint64("uid", uid).
						Str("stryder_token", string(token)).
						Str("stryder_resp", string(stryderRes)).
						Msgf("unexpected stryder error")
				}
				respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObjf("stryder is down: %v", err))
				return
			}
		}
	}

	var username string
	if h.OriginAuthMgr != nil {
		originStart := time.Now()
		if tok, ours, err := h.OriginAuthMgr.OriginAuth(false); err == nil {
			var notfound bool
			if ui, err := origin.GetUserInfo(r.Context(), tok, uid); err == nil {
				if len(ui) == 1 {
					username = ui[0].EAID
					h.m().client_originauth_origin_username_lookup_calls_total.success.Inc()
				} else {
					notfound = true
					h.m().client_originauth_origin_username_lookup_calls_total.notfound.Inc()
				}
			} else if errors.Is(err, origin.ErrAuthRequired) {
				if tok, ours, err := h.OriginAuthMgr.OriginAuth(true); err == nil {
					if ui, err := origin.GetUserInfo(r.Context(), tok, uid); err == nil {
						if len(ui) == 1 {
							username = ui[0].EAID
							h.m().client_originauth_origin_username_lookup_calls_total.success.Inc()
						} else {
							notfound = true
							h.m().client_originauth_origin_username_lookup_calls_total.notfound.Inc()
						}
					}
				} else if ours {
					hlog.FromRequest(r).Error().
						Err(err).
						Msgf("origin auth token refresh failure")
					h.m().client_originauth_origin_username_lookup_calls_total.fail_authtok_refresh.Inc()
				}
			} else if !errors.Is(err, context.Canceled) {
				hlog.FromRequest(r).Error().
					Err(err).
					Msgf("failed to get origin user info")
				h.m().client_originauth_origin_username_lookup_calls_total.fail_other_error.Inc()
			}
			if notfound {
				hlog.FromRequest(r).Warn().
					Err(err).
					Uint64("uid", uid).
					Msgf("no username found for uid")
			}
		} else if ours {
			hlog.FromRequest(r).Error().
				Err(err).
				Msgf("origin auth token refresh failure")
			h.m().client_originauth_origin_username_lookup_calls_total.fail_authtok_refresh.Inc()
		}
		h.m().client_originauth_origin_username_lookup_duration_seconds.UpdateDuration(originStart)
	}

	// note: there's small chance of race conditions here if there are multiple
	// concurrent origin_auth calls, but since we only ever support one session
	// at a time per uid, it's not a big deal which token gets saved (if it is
	// ever a problem, we can change AccountStorage to support transactions)

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		h.m().client_originauth_requests_total.fail_storage_error_account.Inc()
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	if acct != nil && username != "" && acct.Username != username {
		hlog.FromRequest(r).Info().Uint64("uid", acct.UID).Str("username", username).Str("prev_username", acct.Username).Msg("got updated username from origin")
	}
	if acct == nil {
		acct = &Account{
			UID: uid,
		}
		hlog.FromRequest(r).Info().Uint64("uid", acct.UID).Str("username", username).Msg("created new account")
	}
	if username != "" {
		acct.Username = username
	}

	if t, err := cryptoRandHex(32); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to generate random token")
		h.m().client_originauth_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	} else {
		acct.AuthToken = t
	}
	if h.TokenExpiryTime > 0 {
		acct.AuthTokenExpiry = time.Now().Add(h.TokenExpiryTime)
	} else {
		acct.AuthTokenExpiry = time.Now().Add(time.Hour * 24)
	}
	acct.AuthIP = raddr.Addr()

	if err := h.AccountStorage.SaveAccount(acct); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to save account to storage")
		h.m().client_originauth_requests_total.fail_storage_error_account.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	h.m().client_originauth_requests_total.success.Inc()
	h.geoCounter2(r, h.m().client_originauth_requests_map)

	respJSON(w, r, http.StatusOK, map[string]any{
		"success": true,
		"token":   acct.AuthToken,
	})
}

func (h *Handler) handleClientAuthWithServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodPost {
		h.m().client_authwithserver_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, POST")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !h.checkLauncherVersion(r) {
		h.m().client_authwithserver_requests_total.reject_versiongate.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_UNSUPPORTED_VERSION.MessageObj())
		return
	}

	uidQ := r.URL.Query().Get("id")
	if uidQ == "" {
		h.m().client_authwithserver_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("id param is required"))
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		h.m().client_authwithserver_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	playerToken := r.URL.Query().Get("playerToken")
	server := r.URL.Query().Get("server")
	password := r.URL.Query().Get("password")

	srv := h.ServerList.GetServerByID(server)
	if srv == nil || srv.Password != password {
		h.m().client_authwithserver_requests_total.reject_password.Inc()
		respFail(w, r, http.StatusUnauthorized, ErrorCode_UNAUTHORIZED_PWD.MessageObj())
		return
	}

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		h.m().client_authwithserver_requests_total.fail_storage_error_account.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}
	if acct == nil {
		h.m().client_authwithserver_requests_total.reject_player_not_found.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	if !h.InsecureDevNoCheckPlayerAuth {
		if playerToken != acct.AuthToken || !time.Now().Before(acct.AuthTokenExpiry) {
			h.m().client_authwithserver_requests_total.reject_masterserver_token.Inc()
			respFail(w, r, http.StatusUnauthorized, ErrorCode_INVALID_MASTERSERVER_TOKEN.MessageObj())
			return
		}
	}

	var authToken string
	if v, err := cryptoRandHex(31); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to generate random token")
		h.m().client_authwithserver_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	} else {
		authToken = v
	}

	var pbuf []byte
	if b, exists, err := h.PdataStorage.GetPdataCached(acct.UID, [sha256.Size]byte{}); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", acct.UID).
			Msgf("failed to read pdata from storage")
		h.m().client_authwithserver_requests_total.fail_storage_error_pdata.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	} else if !exists {
		pbuf = pdata.DefaultPdata
	} else {
		pbuf = b
	}

	{
		authStart := time.Now()

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()

		if err := api0gameserver.AuthenticateIncomingPlayer(ctx, srv.AuthAddr(), acct.UID, acct.Username, authToken, srv.ServerAuthToken, pbuf); err != nil {
			h.m().client_authwithserver_gameserverauth_duration_seconds.UpdateDuration(authStart)
			if errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("request timed out")
			}
			switch {
			case errors.Is(err, api0gameserver.ErrAuthFailed):
				h.m().client_authwithserver_requests_total.reject_gameserverauth.Inc()
				respFail(w, r, http.StatusInternalServerError, ErrorCode_JSON_PARSE_ERROR.MessageObj()) // this is kind of misleading... but it's what the original master server did
			case errors.Is(err, api0gameserver.ErrInvalidResponse):
				hlog.FromRequest(r).Error().
					Err(err).
					Msgf("failed to make gameserver auth request")
				h.m().client_authwithserver_requests_total.fail_gameserverauth.Inc()
				respFail(w, r, http.StatusInternalServerError, ErrorCode_BAD_GAMESERVER_RESPONSE.MessageObj())
			default:
				if !errors.Is(err, context.Canceled) {
					hlog.FromRequest(r).Error().
						Err(err).
						Msgf("failed to make gameserver auth request")
					h.m().client_authwithserver_requests_total.fail_gameserverauth.Inc()
				}
				respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
			}
			return
		}

		h.m().client_authwithserver_gameserverauth_duration_seconds.UpdateDuration(authStart)
	}

	acct.LastServerID = srv.ID

	if err := h.AccountStorage.SaveAccount(acct); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to save account to storage")
		h.m().client_authwithserver_requests_total.fail_storage_error_account.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	h.m().client_authwithserver_requests_total.success.Inc()
	respJSON(w, r, http.StatusOK, map[string]any{
		"success":   true,
		"ip":        srv.Addr.Addr().String(),
		"port":      srv.Addr.Port(),
		"authToken": authToken,
	})
}

func (h *Handler) handleClientAuthWithSelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodPost {
		h.m().client_authwithself_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, POST")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !h.checkLauncherVersion(r) {
		h.m().client_authwithself_requests_total.reject_versiongate.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_UNSUPPORTED_VERSION.MessageObj())
		return
	}

	uidQ := r.URL.Query().Get("id")
	if uidQ == "" {
		h.m().client_authwithself_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("id param is required"))
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		h.m().client_authwithself_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	playerToken := r.URL.Query().Get("playerToken")

	acct, err := h.AccountStorage.GetAccount(uid)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read account from storage")
		h.m().client_authwithself_requests_total.fail_storage_error_account.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}
	if acct == nil {
		h.m().client_authwithself_requests_total.reject_player_not_found.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	if !h.InsecureDevNoCheckPlayerAuth {
		if playerToken != acct.AuthToken || !time.Now().Before(acct.AuthTokenExpiry) {
			h.m().client_authwithself_requests_total.reject_masterserver_token.Inc()
			respFail(w, r, http.StatusUnauthorized, ErrorCode_INVALID_MASTERSERVER_TOKEN.MessageObj())
			return
		}
	}

	acct.LastServerID = "self"

	if err := h.AccountStorage.SaveAccount(acct); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to save account to storage")
		h.m().client_authwithself_requests_total.fail_storage_error_account.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	obj := map[string]any{
		"success": true,
		"id":      strconv.FormatUint(acct.UID, 10),
	}

	// the way we encode this is utterly absurd and inefficient, but we need to do it for backwards compatibility
	if b, exists, err := h.PdataStorage.GetPdataCached(acct.UID, [sha256.Size]byte{}); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", acct.UID).
			Msgf("failed to read pdata from storage")
		h.m().client_authwithself_requests_total.fail_storage_error_pdata.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	} else if !exists {
		obj["persistentData"] = marshalJSONBytesAsArray(pdata.DefaultPdata)
	} else {
		obj["persistentData"] = marshalJSONBytesAsArray(b)
	}

	// this is also stupid (it doesn't use it for self-auth, but it requires it to be in the response)
	// and of course, it breaks on 32 chars, so we need to give it 31
	if v, err := cryptoRandHex(31); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to generate random token")
		h.m().client_authwithself_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	} else {
		obj["authToken"] = v
	}

	h.m().client_authwithself_requests_total.success.Inc()
	respJSON(w, r, http.StatusOK, obj)
}

func (h *Handler) handleClientServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		h.m().client_servers_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, GET, HEAD")
	w.Header().Set("Access-Control-Max-Age", "86400")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, HEAD, GET")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	var compressed bool
	buf := h.ServerList.csGetJSON()
	for _, e := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if t, _, _ := strings.Cut(e, ";"); strings.TrimSpace(t) == "gzip" {
			if zbuf, ok := h.ServerList.csGetJSONGzip(); ok {
				buf = zbuf
				w.Header().Set("Content-Encoding", "gzip")
				compressed = true
			} else {
				hlog.FromRequest(r).Error().Msg("failed to gzip server list")
			}
			break
		}
	}
	if compressed {
		h.m().client_servers_response_size_bytes.gzip.Update(float64(len(buf)))
	} else {
		h.m().client_servers_response_size_bytes.none.Update(float64(len(buf)))
	}

	lver := h.extractLauncherVersion(r)
	h.m().client_servers_requests_total.success(lver).Inc()
	if lver != "" {
		h.geoCounter2(r, h.m().client_servers_requests_map.northstar)
	} else {
		h.geoCounter2(r, h.m().client_servers_requests_map.other)
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(buf)
	}
}
