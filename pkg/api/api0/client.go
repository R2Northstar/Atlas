package api0

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/pg9182/atlas/pkg/origin"
	"github.com/pg9182/atlas/pkg/pdata"
	"github.com/pg9182/atlas/pkg/stryder"
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

	var p MainMenuPromos
	if h.MainMenuPromos != nil {
		p = h.MainMenuPromos(r)
	}
	respJSON(w, r, http.StatusOK, p)
}

func (h *Handler) handleClientOriginAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodGet { // no HEAD support intentionally
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
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_UNSUPPORTED_VERSION,
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

	if !h.InsecureDevNoCheckPlayerAuth {
		token := r.URL.Query().Get("token")
		if token == "" {
			respJSON(w, r, http.StatusBadRequest, map[string]any{
				"success": false,
				"error":   ErrorCode_BAD_REQUEST,
				"msg":     ErrorCode_BAD_REQUEST.Messagef("token param is required"),
			})
			return
		}

		stryderCtx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()

		stryderRes, err := stryder.NucleusAuth(stryderCtx, token, uid)
		if err != nil {
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
				respJSON(w, r, http.StatusForbidden, map[string]any{
					"success": false,
					"error":   ErrorCode_UNAUTHORIZED_GAME,
					"msg":     ErrorCode_UNAUTHORIZED_GAME.Message(),
				})
				return
			case errors.Is(err, stryder.ErrStryder):
				hlog.FromRequest(r).Error().
					Err(err).
					Uint64("uid", uid).
					Str("stryder_token", string(token)).
					Str("stryder_resp", string(stryderRes)).
					Msgf("unexpected stryder error")
				respJSON(w, r, http.StatusInternalServerError, map[string]any{
					"success": false,
					"error":   ErrorCode_INTERNAL_SERVER_ERROR,
					"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
				})
				return
			default:
				hlog.FromRequest(r).Error().
					Err(err).
					Uint64("uid", uid).
					Str("stryder_token", string(token)).
					Str("stryder_resp", string(stryderRes)).
					Msgf("unexpected stryder error")
				respJSON(w, r, http.StatusInternalServerError, map[string]any{
					"success": false,
					"error":   ErrorCode_INTERNAL_SERVER_ERROR,
					"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Messagef("stryder is down: %v", err),
				})
				return
			}
		}
	}

	var username string
	if h.OriginAuthMgr != nil {
		// TODO: maybe just update this from a different thread since we don't
		// actually need it during the auth process (doing it that way will
		// speed up auth and also allow us to batch the Origin API calls)

		if tok, ours, err := h.OriginAuthMgr.OriginAuth(false); err == nil {
			var notfound bool
			if ui, err := origin.GetUserInfo(r.Context(), tok, uid); err == nil {
				if len(ui) == 1 {
					username = ui[0].EAID
				} else {
					notfound = true
				}
			} else if errors.Is(err, origin.ErrAuthRequired) {
				if tok, ours, err := h.OriginAuthMgr.OriginAuth(true); err == nil {
					if ui, err := origin.GetUserInfo(r.Context(), tok, uid); err == nil {
						if len(ui) == 1 {
							username = ui[0].EAID
						} else {
							notfound = true
						}
					}
				} else if ours {
					hlog.FromRequest(r).Error().
						Err(err).
						Msgf("origin auth token refresh failure")
				}
			} else {
				hlog.FromRequest(r).Error().
					Err(err).
					Msgf("failed to get origin user info")
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
		}
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
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	if acct == nil {
		acct = &Account{
			UID: uid,
		}
	}
	if username != "" {
		acct.Username = username
	}

	if t, err := cryptoRandHex(32); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to generate random token")
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
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
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	respJSON(w, r, http.StatusOK, map[string]any{
		"success": true,
		"token":   acct.AuthToken,
	})
}

func (h *Handler) handleClientAuthWithSelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodPost {
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
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_UNSUPPORTED_VERSION,
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

	playerToken := r.URL.Query().Get("playerToken")
	if playerToken == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("playerToken param is required"),
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

	if !h.InsecureDevNoCheckPlayerAuth {
		if playerToken != acct.AuthToken || !time.Now().Before(acct.AuthTokenExpiry) {
			respJSON(w, r, http.StatusUnauthorized, map[string]any{
				"success": false,
				"error":   ErrorCode_INVALID_MASTERSERVER_TOKEN,
			})
			return
		}
	}

	obj := map[string]any{
		"success": true,
		"id":      acct.UID,
	}

	// the way we encode this is utterly absurd and inefficient, but we need to do it for backwards compatibility
	if b, exists, err := h.PdataStorage.GetPdataCached(acct.UID, [sha256.Size]byte{}); err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", acct.UID).
			Msgf("failed to read pdata from storage")
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
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
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	} else {
		obj["authToken"] = v
	}

	respJSON(w, r, http.StatusOK, obj)
}

/*
  /client/auth_with_server:
    POST:

  /client/servers:
    GET:
*/
