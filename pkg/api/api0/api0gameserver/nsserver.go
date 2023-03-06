// Package api0gameserver interacts with game servers using the original master
// server api.
package api0gameserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
)

var (
	ErrInvalidResponse = errors.New("invalid response")
	ErrAuthFailed      = errors.New("authentication failed")
)

type ConnectionRejectedError string

func (c ConnectionRejectedError) Reason() string {
	if c == "" {
		return "unknown"
	}
	return string(c)
}

func (c ConnectionRejectedError) Error() string {
	if c == "" {
		return "connection rejected"
	}
	return "connection rejected: " + string(c)
}

// VerifyText is the expected server response for /verify.
const VerifyText = "I am a northstar server!"

// Verify checks whether an address is a Northstar auth server. If the HTTP
// request succeeds but the response is incorrect, err is ErrInvalidResponse.
func Verify(ctx context.Context, auth netip.AddrPort) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+auth.String()+"/verify", nil)
	if err != nil {
		return err // shouldn't happen
	}
	req.Header.Set("User-Agent", "Atlas")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(VerifyText)*2)))
	if err != nil {
		return err
	}

	if string(bytes.TrimSpace(buf)) != VerifyText {
		return ErrInvalidResponse
	}
	return nil
}

// AuthenticateIncomingPlayer checks if a player can connect to a game server,
// registers a one-time connection token, and sends the player's pdata. If the
// authentication request returns invalid JSON, err is ErrInvalidResponse. If
// the authentication response .success is false, err is ErrAuthFailed.
func AuthenticateIncomingPlayer(ctx context.Context, auth netip.AddrPort, uid uint64, username, connToken, serverToken string, pdata []byte) error {
	u := "http://" + auth.String() + "/authenticate_incoming_player" +
		"?id=" + strconv.FormatUint(uid, 10) +
		"&authToken=" + url.QueryEscape(connToken) +
		"&serverAuthToken=" + url.QueryEscape(serverToken) +
		"&username=" + url.QueryEscape(username)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(pdata))
	if err != nil {
		return err // shouldn't happen
	}
	req.Header.Set("User-Agent", "Atlas")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var obj struct {
		Success bool   `json:"success"`
		Reject  string `json:"reject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return ErrInvalidResponse
	}
	if obj.Reject != "" {
		return ConnectionRejectedError(obj.Reject)
	}
	if !obj.Success {
		return ErrAuthFailed
	}
	return nil
}
