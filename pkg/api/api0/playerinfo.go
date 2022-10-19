package api0

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pg9182/atlas/pkg/pdata"
	"github.com/rs/zerolog/hlog"
)

func pdataFilterInfo(path ...string) bool {
	switch path[0] {
	case "gen", "xp", "activeCallingCardIndex", "activeCallsignIconIndex", "activeCallsignIconStyleIndex", "netWorth":
		return true
	default:
		return false
	}
}

func pdataFilterStats(path ...string) bool {
	switch path[0] {
	case "gen", "xp", "credits", "netWorth", "factionXP", "titanXP", "fdTitanXP", "gameStats", "mapStats", "timeStats",
		"distanceStats", "weaponStats", "weaponKillStats", "killStats", "deathStats", "miscStats", "fdStats", "titanStats",
		"kdratio_lifetime", "kdratio_lifetime_pvp", "winStreak", "highestWinStreakEver":
		return true
	default:
		return false
	}
}

func pdataFilterLoadout(path ...string) bool {
	switch path[0] {
	case "factionChoice", "activePilotLoadout", "activeTitanLoadout", "pilotLoadouts", "titanLoadouts":
		return true
	default:
		return false
	}
}

func (h *Handler) handlePlayer(w http.ResponseWriter, r *http.Request) {
	var pdataFilter func(...string) bool
	var pdataFilterName string
	switch pdataFilterName := strings.TrimPrefix(r.URL.Path, "/player/"); pdataFilterName {
	case "pdata":
		pdataFilter = nil
	case "info":
		pdataFilter = pdataFilterInfo
	case "stats":
		pdataFilter = pdataFilterStats
	case "loadout":
		pdataFilter = pdataFilterLoadout
	default:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if r.Method != http.MethodOptions && r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.m().player_pdata_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// - cache publicly, allow reusing responses for multiple users
	// - allow reusing responses if server is down
	// - cache for up to 30s
	// - check for updates after 15s
	w.Header().Set("Cache-Control", "public, max-age=15, stale-while-revalidate=15")
	w.Header().Set("Expires", time.Now().UTC().Add(time.Second*30).Format(http.TimeFormat))

	// - allow CORS requests from all origins
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, GET, HEAD")
	w.Header().Set("Access-Control-Max-Age", "86400")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, GET, HEAD")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	uidQ := r.URL.Query().Get("id")
	if uidQ == "" {
		h.m().player_pdata_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("id param is required"))
		return
	}

	uid, err := strconv.ParseUint(uidQ, 10, 64)
	if err != nil {
		h.m().player_pdata_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	// if it's a HEAD request, we just need the hash to set the etag
	if r.Method == http.MethodHead {
		hash, exists, err := h.PdataStorage.GetPdataHash(uid)
		if err != nil {
			hlog.FromRequest(r).Error().
				Err(err).
				Uint64("uid", uid).
				Msgf("failed to read pdata hash from storage")
			h.m().player_pdata_requests_total.fail_storage_error_pdata.Inc()
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !exists {
			h.m().player_pdata_requests_total.reject_player_not_found.Inc()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.m().player_pdata_requests_total.success(pdataFilterName).Inc()
		w.Header().Set("ETag", `W/"`+hex.EncodeToString(hash[:])+`"`)
		w.WriteHeader(http.StatusOK)
		return
	}

	buf, exists, err := h.PdataStorage.GetPdataCached(uid, [sha256.Size]byte{})
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Msgf("failed to read pdata from storage")
		h.m().player_pdata_requests_total.fail_storage_error_pdata.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}
	if !exists {
		h.m().player_pdata_requests_total.reject_player_not_found.Inc()
		respFail(w, r, http.StatusNotFound, ErrorCode_PLAYER_NOT_FOUND.MessageObj())
		return
	}

	hash := sha256.Sum256(buf)
	w.Header().Set("ETag", `W/"`+hex.EncodeToString(hash[:])+`"`)

	var pd pdata.Pdata
	if err := pd.UnmarshalBinary(buf); err != nil {
		hlog.FromRequest(r).Warn().
			Err(err).
			Uint64("uid", uid).
			Str("pdata_sha256", hex.EncodeToString(hash[:])).
			Msgf("failed to parse pdata from storage")
		h.m().player_pdata_requests_total.fail_pdata_invalid.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObjf("failed to parse stored pdata"))
		return
	}

	jbuf, err := pd.MarshalJSONFilter(pdataFilter)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Uint64("uid", uid).
			Str("pdata_sha256", hex.EncodeToString(hash[:])).
			Msgf("failed to encode pdata as json")
		h.m().player_pdata_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObjf("failed to encode pdata as json"))
		return
	}
	jbuf = append(jbuf, '\n')

	h.m().player_pdata_requests_total.success(pdataFilterName).Inc()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	respMaybeCompress(w, r, http.StatusOK, jbuf)
}
