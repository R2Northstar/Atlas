package api0

import (
	"crypto/sha256"
	"net/http"
	"strconv"
	"time"

	"github.com/pg9182/atlas/pkg/pdata"
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
  /client/origin_auth:
    GET:
  /client/auth_with_server:
    POST:

  /client/servers:
    GET:
*/
