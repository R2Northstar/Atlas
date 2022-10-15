package api0

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/pg9182/atlas/pkg/a2s"
	"github.com/rs/zerolog/hlog"
)

/*
  /server/heartbeat:
    POST:
  /server/update_values:
    POST:
  /server/remove_server:
    DELETE:
*/

func (h *Handler) handleServerAddServer(w http.ResponseWriter, r *http.Request) {
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

	if !h.AllowGameServerIPv6 {
		if raddr.Addr().Is6() {
			respJSON(w, r, http.StatusBadRequest, map[string]any{
				"success": false,
				"error":   ErrorCode_NO_GAMESERVER_RESPONSE,
				"msg":     ErrorCode_NO_GAMESERVER_RESPONSE.Messagef("ipv6 is not currently supported (ip %s)", raddr.Addr()),
			})
			return
		}
	}

	var s Server

	if v := r.URL.Query().Get("port"); v == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("port param is required"),
		})
		return
	} else if n, err := strconv.ParseUint(v, 10, 16); err != nil {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("port param is invalid: %v", err),
		})
		return
	} else {
		s.Addr = netip.AddrPortFrom(raddr.Addr(), uint16(n))
	}

	if v := r.URL.Query().Get("authPort"); v == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("authPort param is required"),
		})
		return
	} else if n, err := strconv.ParseUint(v, 10, 16); err != nil {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("authPort param is invalid: %v", err),
		})
		return
	} else {
		s.AuthPort = uint16(n)
	}

	if v := r.URL.Query().Get("name"); v == "" {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("name param must not be empty"),
		})
		return
	} else {
		// TODO: bad word censoring
		if n := 256; len(v) > n { // NorthstarLauncher@v1.9.7 limits it to 63
			v = v[:n]
		}
		s.Name = v
	}

	if v := r.URL.Query().Get("description"); v != "" {
		// TODO: bad word censoring
		if n := 1024; len(v) > n { // NorthstarLauncher@v1.9.7 doesn't have a limit
			v = v[:n]
		}
		s.Description = v
	}

	if v := r.URL.Query().Get("map"); v != "" {
		if n := 64; len(v) > n { // NorthstarLauncher@v1.9.7 limits it to 31
			v = v[:n]
		}
		s.Map = v
	}

	if v := r.URL.Query().Get("playlist"); v != "" {
		if n := 64; len(v) > n { // NorthstarLauncher@v1.9.7 limits it to 15
			v = v[:n]
		}
		s.Playlist = v
	}

	if n, err := strconv.ParseUint(r.URL.Query().Get("maxPlayers"), 10, 8); err == nil {
		s.MaxPlayers = int(n)
	}

	if v := r.URL.Query().Get("password"); len(v) > 128 {
		respJSON(w, r, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_REQUEST,
			"msg":     ErrorCode_BAD_REQUEST.Messagef("password is too long"),
		})
		return
	} else {
		s.Password = v
	}

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
		modInfoErr = fmt.Errorf("parse multipart form: %w", err)
	}
	if modInfoErr != nil {
		hlog.FromRequest(r).Warn().
			Err(err).
			Msgf("failed to parse modinfo")
	}

	verifyDeadline := time.Now().Add(time.Second * 10)
	if err := func() error {
		ctx, cancel := context.WithDeadline(r.Context(), verifyDeadline)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/verify", s.AuthAddr()), nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Atlas")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		buf, err := io.ReadAll(io.LimitReader(resp.Body, 100))
		if err != nil {
			return err
		}
		if string(bytes.TrimSpace(buf)) != "I am a northstar server!" {
			return fmt.Errorf("unexpected response")
		}
		return nil
	}(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("request timed out")
		}
		respJSON(w, r, http.StatusBadGateway, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_GAMESERVER_RESPONSE,
			"msg":     ErrorCode_BAD_GAMESERVER_RESPONSE.Messagef("failed to connect to auth port: %v", err),
		})
		return
	}
	if err := a2s.Probe(s.Addr, time.Until(verifyDeadline)); err != nil {
		respJSON(w, r, http.StatusBadGateway, map[string]any{
			"success": false,
			"error":   ErrorCode_BAD_GAMESERVER_RESPONSE,
			"msg":     ErrorCode_BAD_GAMESERVER_RESPONSE.Messagef("failed to connect to game port: %v", err),
		})
		return
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

	nsrv, err := h.ServerList.ServerHybridUpdatePut(nil, &s, l)
	if err != nil {
		if errors.Is(err, ErrServerListDuplicateAuthAddr) {
			respJSON(w, r, http.StatusForbidden, map[string]any{
				"success": false,
				"error":   ErrorCode_DUPLICATE_SERVER,
				"msg":     ErrorCode_DUPLICATE_SERVER.Messagef("%v", err),
			})
			return
		}
		if errors.Is(err, ErrServerListLimitExceeded) {
			respJSON(w, r, http.StatusInternalServerError, map[string]any{
				"success": false,
				"error":   ErrorCode_INTERNAL_SERVER_ERROR,
				"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Messagef("%v", err),
			})
			return
		}
		hlog.FromRequest(r).Error().
			Err(err).
			Msgf("failed to add server to list")
		respJSON(w, r, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   ErrorCode_INTERNAL_SERVER_ERROR,
			"msg":     ErrorCode_INTERNAL_SERVER_ERROR.Message(),
		})
		return
	}

	respJSON(w, r, http.StatusInternalServerError, map[string]any{
		"success":         true,
		"id":              nsrv.ID,
		"serverAuthToken": nsrv.ServerAuthToken,
	})
}
