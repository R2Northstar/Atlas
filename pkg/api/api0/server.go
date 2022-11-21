package api0

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/r2northstar/atlas/pkg/a2s"
	"github.com/r2northstar/atlas/pkg/api/api0/api0gameserver"
	"github.com/rs/zerolog/hlog"
)

func (h *Handler) handleServerUpsert(w http.ResponseWriter, r *http.Request) {
	// note: if the API is confusing, see:
	//  - https://github.com/R2Northstar/NorthstarLauncher/commit/753dda6231bbb2adf585bbc916c0b220e816fcdc
	//  - https://github.com/R2Northstar/NorthstarLauncher/blob/v1.9.7/NorthstarDLL/masterserver.cpp

	var action string
	var isCreate, canCreate, isUpdate, canUpdate bool
	switch action = strings.TrimPrefix(r.URL.Path, "/server/"); action {
	case "add_server":
		isCreate = true
		canCreate = true
	case "update_values":
		canCreate = true
		fallthrough
	case "heartbeat":
		isUpdate = true
		canUpdate = true
	default:
		panic("unhandled path")
	}

	if r.Method != http.MethodOptions && r.Method != http.MethodPost {
		h.m().server_upsert_requests_total.http_method_not_allowed(action).Inc()
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
		h.m().server_upsert_requests_total.reject_versiongate(action).Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_UNSUPPORTED_VERSION.MessageObj())
		return
	}

	raddr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to parse remote ip %q", r.RemoteAddr)
		h.m().server_upsert_requests_total.fail_other_error(action).Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	if !h.AllowGameServerIPv6 {
		if raddr.Addr().Is6() {
			h.m().server_upsert_requests_total.reject_ipv6(action).Inc()
			respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("ipv6 is not currently supported (ip %s)", raddr.Addr()))
			return
		}
	}

	var l ServerListLimit
	if n := h.MaxServers; n > 0 {
		l.MaxServers = n
	} else if n == 0 {
		l.MaxServers = 1000
	}
	if n := h.MaxServersPerIP; n > 0 {
		l.MaxServersPerIP = n
	} else if n == 0 {
		l.MaxServersPerIP = 50
	}

	var s *Server
	if canCreate {
		s = &Server{
			LauncherVersion: h.extractLauncherVersion(r),
		}
	}

	var u *ServerUpdate
	if canUpdate {
		u = &ServerUpdate{
			Heartbeat: true,
			ExpectIP:  raddr.Addr(),
		}
	}

	q := r.URL.Query()

	if canUpdate {
		if v := q.Get("id"); v == "" {
			if isUpdate {
				h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
				respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("port param is required"))
				return
			}
		} else {
			u.ID = v
		}
	}

	if canCreate {
		if v := q.Get("port"); v == "" {
			if isCreate {
				h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
				respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("port param is required"))
				return
			}
		} else if n, err := strconv.ParseUint(v, 10, 16); err != nil {
			h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
			respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("port param is invalid: %v", err))
			return
		} else {
			s.Addr = netip.AddrPortFrom(raddr.Addr(), uint16(n))
		}

		if v := q.Get("authPort"); v == "" {
			if isCreate {
				h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
				respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("authPort param is required"))
				return
			}
		} else if n, err := strconv.ParseUint(v, 10, 16); err != nil {
			h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
			respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("authPort param is invalid: %v", err))
			return
		} else {
			s.AuthPort = uint16(n)
		}

		if v := q.Get("password"); len(v) > 128 {
			if isCreate {
				h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
				respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("password is too long"))
				return
			}
		} else {
			s.Password = v
		}
	}

	if canCreate || canUpdate {
		if h.LookupIP != nil && h.GetRegion != nil {
			if rec, err := h.LookupIP(raddr.Addr()); err == nil {
				region, err := h.GetRegion(raddr.Addr(), rec)
				if err == nil || region != "" { // if an error occurs, we may still have a best-effort region
					if canCreate {
						s.Region = region
					}
					if canUpdate {
						u.Region = &region
					}
				}
				if err != nil {
					h.m().server_upsert_getregion_errors_total.Inc()
					if region == "" {
						hlog.FromRequest(r).Err(err).Str("ip", raddr.Addr().String()).Msg("failed to compute region, no best-effort region available")
					} else {
						hlog.FromRequest(r).Err(err).Str("ip", raddr.Addr().String()).Msgf("failed to compute region, using best-effort region %q", region)
					}
				}
			} else {
				h.m().server_upsert_ip2location_errors_total.Inc()
				hlog.FromRequest(r).Err(err).Str("ip", raddr.Addr().String()).Msg("failed to lookup remote ip in ip2location database")
			}
		}
	}

	if canCreate || canUpdate {
		if v := q.Get("name"); v == "" {
			if isCreate {
				h.m().server_upsert_requests_total.reject_bad_request(action).Inc()
				respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("name param must not be empty"))
				return
			}
		} else {
			if h.CleanBadWords != nil {
				v = h.CleanBadWords(v)
			}
			if n := 256; len(v) > n { // NorthstarLauncher@v1.9.7 limits it to 63
				v = v[:n]
			}
			if canCreate {
				s.Name = v
			}
			if canUpdate {
				u.Name = &v
			}
		}

		if v := q.Get("description"); v != "" {
			if h.CleanBadWords != nil {
				v = h.CleanBadWords(v)
			}
			if n := 1024; len(v) > n { // NorthstarLauncher@v1.9.7 doesn't have a limit
				v = v[:n]
			}
			if canCreate {
				s.Description = v
			}
			if canUpdate {
				u.Description = &v
			}
		}

		if v := q.Get("map"); v != "" {
			if n := 64; len(v) > n { // NorthstarLauncher@v1.9.7 limits it to 31
				v = v[:n]
			}
			if canCreate {
				s.Map = v
			}
			if canUpdate {
				u.Map = &v
			}
		}

		if v := q.Get("playlist"); v != "" {
			if n := 64; len(v) > n { // NorthstarLauncher@v1.9.7 limits it to 15
				v = v[:n]
			}
			if canCreate {
				s.Playlist = v
			}
			if canUpdate {
				u.Playlist = &v
			}
		}

		if n, err := strconv.ParseUint(q.Get("playerCount"), 10, 8); err == nil {
			if canCreate {
				s.PlayerCount = int(n)
			}
			if canUpdate {
				x := int(n)
				u.PlayerCount = &x
			}
		}

		if n, err := strconv.ParseUint(q.Get("maxPlayers"), 10, 8); err == nil {
			if canCreate {
				s.MaxPlayers = int(n)
			}
			if canUpdate {
				x := int(n)
				u.MaxPlayers = &x
			}
		}
	}

	if canCreate {
		var modInfoErr error
		if err := r.ParseMultipartForm(1 << 18 /*.25 MB*/); err == nil {
			if mf, mfHdr, err := r.FormFile("modinfo"); err == nil {
				if mfHdr.Size < 1<<18 {
					var obj struct {
						Mods []struct {
							Name             string `json:"Name"`
							Version          string `json:"Version"`
							RequiredOnClient bool   `json:"RequiredOnClient"`
						} `json:"Mods"`
					}
					if err := json.NewDecoder(mf).Decode(&obj); err == nil {
						for _, m := range obj.Mods {
							if m.Name != "" {
								if m.Version == "" {
									m.Version = "0.0.0"
								}
								s.ModInfo = append(s.ModInfo, ServerModInfo{
									Name:             m.Name,
									Version:          m.Version,
									RequiredOnClient: m.RequiredOnClient,
								})
							}
						}
					} else {
						modInfoErr = fmt.Errorf("parse modinfo file: %w", err)
					}
				} else {
					modInfoErr = fmt.Errorf("get modinfo file: too large (size %d)", mfHdr.Size)
				}
				mf.Close()
			} else {
				modInfoErr = fmt.Errorf("get modinfo file: %w", err)
			}
		} else {
			if isCreate {
				modInfoErr = fmt.Errorf("parse multipart form: %w", err)
			}
		}
		if modInfoErr != nil {
			h.m().server_upsert_modinfo_parse_errors_total(action).Inc()
			hlog.FromRequest(r).Warn().
				Err(err).
				Msgf("failed to parse modinfo")
		}
	}

	nsrv, err := h.ServerList.ServerHybridUpdatePut(u, s, l)
	if err != nil {
		if errors.Is(err, ErrServerListUpdateWrongIP) {
			h.m().server_upsert_requests_total.reject_unauthorized_ip(action).Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObjf("%v", err))
			return
		}
		if errors.Is(err, ErrServerListUpdateServerDead) {
			h.m().server_upsert_requests_total.reject_server_not_found(action).Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObjf("no such server"))
			return
		}
		if errors.Is(err, ErrServerListDuplicateAuthAddr) {
			h.m().server_upsert_requests_total.reject_duplicate_auth_addr(action).Inc()
			respFail(w, r, http.StatusForbidden, ErrorCode_DUPLICATE_SERVER.MessageObjf("%v", err))
			return
		}
		if errors.Is(err, ErrServerListLimitExceeded) {
			h.m().server_upsert_requests_total.reject_limits_exceeded(action).Inc()
			respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObjf("%v", err))
			return
		}
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to update server list")
		h.m().server_upsert_requests_total.fail_serverlist_error(action).Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	if !nsrv.VerificationDeadline.IsZero() {
		verifyStart := time.Now()

		ctx, cancel := context.WithDeadline(r.Context(), nsrv.VerificationDeadline)
		defer cancel()

		if err := api0gameserver.Verify(ctx, s.AuthAddr()); err != nil {
			var code ErrorCode
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				err = fmt.Errorf("request timed out")
				code = ErrorCode_NO_GAMESERVER_RESPONSE
				h.m().server_upsert_requests_total.reject_verify_authtimeout(action).Inc()
			case errors.Is(err, api0gameserver.ErrInvalidResponse):
				code = ErrorCode_BAD_GAMESERVER_RESPONSE
				h.m().server_upsert_requests_total.reject_verify_authresp(action).Inc()
			default:
				code = ErrorCode_NO_GAMESERVER_RESPONSE
				h.m().server_upsert_requests_total.reject_verify_autherr(action).Inc()
			}
			h.m().server_upsert_verify_time_seconds.failure.UpdateDuration(verifyStart)
			respFail(w, r, http.StatusBadGateway, code.MessageObjf("failed to connect to auth port: %v", err))
			return
		}

		if err := a2s.Probe(s.Addr, time.Until(nsrv.VerificationDeadline)); err != nil {
			var code ErrorCode
			switch {
			case errors.Is(err, a2s.ErrTimeout):
				h.m().server_upsert_requests_total.reject_verify_udptimeout(action).Inc()
				code = ErrorCode_NO_GAMESERVER_RESPONSE
			default:
				h.m().server_upsert_requests_total.reject_verify_udperr(action).Inc()
				code = ErrorCode_BAD_GAMESERVER_RESPONSE
			}
			h.m().server_upsert_verify_time_seconds.failure.UpdateDuration(verifyStart)
			respFail(w, r, http.StatusBadGateway, code.MessageObjf("failed to connect to game port: %v", err))
			return
		}
		h.m().server_upsert_verify_time_seconds.success.UpdateDuration(verifyStart)

		if !h.ServerList.VerifyServer(nsrv.ID) {
			h.m().server_upsert_requests_total.reject_verify_udptimeout(action).Inc()
			respFail(w, r, http.StatusBadGateway, ErrorCode_NO_GAMESERVER_RESPONSE.MessageObjf("verification timed out"))
			return
		}

		h.m().server_upsert_requests_total.success_verified(action).Inc()
	} else {
		h.m().server_upsert_requests_total.success_updated(action).Inc()
	}
	respJSON(w, r, http.StatusOK, map[string]any{
		"success":         true,
		"id":              nsrv.ID,
		"serverAuthToken": nsrv.ServerAuthToken,
	})
}

func (h *Handler) handleServerRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodDelete {
		h.m().server_remove_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, DELETE")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	raddr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to parse remote ip %q", r.RemoteAddr)
		h.m().server_remove_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	var id string
	if v := r.URL.Query().Get("id"); v == "" {
		h.m().server_remove_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("id param is required"))
		return
	} else {
		id = v
	}

	srv := h.ServerList.GetServerByID(id)
	if srv == nil {
		h.m().server_remove_requests_total.reject_server_not_found.Inc()
		respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObjf("no such game server"))
		return
	}
	if srv.Addr.Addr() != raddr.Addr() {
		h.m().server_remove_requests_total.reject_unauthorized_ip.Inc()
		respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObj())
		return
	}
	h.ServerList.DeleteServerByID(id)

	h.m().server_remove_requests_total.success.Inc()
	respJSON(w, r, http.StatusOK, map[string]any{
		"success": true,
	})
}
