package api0

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/pg9182/ip2x"
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

	if !h.CheckLauncherVersion(r, false) {
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
			LauncherVersion: h.ExtractLauncherVersion(r),
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
		} else if v == "udp" {
			s.AuthPort = 0
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
				var lat, lon float64
				if v, _ := rec.GetFloat32(ip2x.Latitude); v != 0 {
					lat = float64(v)
				}
				if v, _ := rec.GetFloat32(ip2x.Longitude); v != 0 {
					lon = float64(v)
				}
				if canCreate {
					s.Latitude = lat
					s.Longitude = lon
				}
				if canUpdate {
					u.Latitude = &lat
					u.Longitude = &lon
				}

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

		if nsrv.AuthPort != 0 {
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
		}

		if err := h.probeUDP(ctx, s.Addr); err != nil {
			var obj ErrorObj
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				h.m().server_upsert_requests_total.reject_verify_udptimeout(action).Inc()
				obj = ErrorCode_NO_GAMESERVER_RESPONSE.MessageObjf("failed to connect to game port")
			default:
				h.m().server_upsert_requests_total.reject_verify_udperr(action).Inc()
				obj = ErrorCode_INTERNAL_SERVER_ERROR.MessageObjf("failed to connect to game port: %v", err)
			}
			h.m().server_upsert_verify_time_seconds.failure.UpdateDuration(verifyStart)
			respFail(w, r, http.StatusBadGateway, obj)
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

func (h *Handler) probeUDP(ctx context.Context, addr netip.AddrPort) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	uid := rand.Uint64()

	x := make(chan error, 1)
	go func() {
		t := time.NewTicker(time.Second * 3) // note: we don't want to exceed the connectionless rate limit
		defer t.Stop()

		for {
			if err := h.NSPkt.SendConnect(addr, uid); err != nil {
				select {
				case x <- err:
				default:
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	err := h.NSPkt.WaitConnectReply(ctx, addr, uid)
	if err != nil {
		select {
		case err = <-x:
			// error could be due to an issue sending the packet
		default:
		}
	}
	return err
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

func (h *Handler) handleServerConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.m().server_connect_requests_total.http_method_not_allowed.Inc()
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, GET, POST")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	raddr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to parse remote ip %q", r.RemoteAddr)
		h.m().server_connect_requests_total.fail_other_error.Inc()
		respFail(w, r, http.StatusInternalServerError, ErrorCode_INTERNAL_SERVER_ERROR.MessageObj())
		return
	}

	var serverId string
	if v := r.URL.Query().Get("serverId"); v == "" {
		h.m().server_connect_requests_total.reject_bad_request.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("serverId param is required"))
		return
	} else {
		serverId = v
	}

	srv := h.ServerList.GetServerByID(serverId)
	if srv == nil {
		h.m().server_connect_requests_total.reject_server_not_found.Inc()
		respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObjf("no such game server"))
		return
	}
	if srv.Addr.Addr() != raddr.Addr() {
		h.m().server_connect_requests_total.reject_unauthorized_ip.Inc()
		respFail(w, r, http.StatusForbidden, ErrorCode_UNAUTHORIZED_GAMESERVER.MessageObj())
		return
	}

	var state *connectState
	if v := r.URL.Query().Get("token"); v == "" {
		h.m().server_connect_requests_total.reject_invalid_connection_token.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("connection token is required"))
		return
	} else if v, ok := h.connect.Load(connectStateKey{
		ServerID: srv.ID,
		Token:    v,
	}); !ok {
		h.m().server_connect_requests_total.reject_invalid_connection_token.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("no such connection token (has it already been used?)"))
		return
	} else {
		state = v.(*connectState)
	}

	if r.Method == http.MethodGet {
		state.gotPdata.Store(true)
		h.m().server_connect_requests_total.success_pdata.Inc()
		respMaybeCompress(w, r, http.StatusOK, state.pdata)
		return
	}

	var reject string
	if v := r.URL.Query()["reject"]; len(v) != 1 {
		h.m().server_connect_requests_total.reject_invalid_connection_token.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("reject is required (if no rejection reason, set to an empty string)"))
		return
	} else {
		reject = v[0]
	}
	if n := 256; len(reject) > n {
		reject = reject[:n]
	}

	if reject == "" && !state.gotPdata.Load() {
		h.m().server_connect_requests_total.reject_must_get_pdata.Inc()
		respFail(w, r, http.StatusBadRequest, ErrorCode_BAD_REQUEST.MessageObjf("must get pdata before accepting connection"))
		return
	}

	select {
	case state.res <- reject:
	default:
	}

	if reject == "" {
		h.m().server_connect_requests_total.success.Inc()
	} else {
		h.m().server_connect_requests_total.success_reject.Inc()
	}
	respJSON(w, r, http.StatusOK, map[string]any{
		"success": true,
	})
}
