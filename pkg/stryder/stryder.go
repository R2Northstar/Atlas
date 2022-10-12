// Package stryder is a client for parts of the Stryder API used by Northstar
// for authentication and license verification.
package stryder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	ErrStryder               = errors.New("internal stryder error")
	ErrInvalidToken          = errors.New("invalid token")
	ErrMultiplayerNotAllowed = errors.New("multiplayer not allowed")
	ErrInvalidGame           = errors.New("invalid game")
)

// NucleusAuth verifies the provided scoped nucleus token and uid for Titanfall
// 2 multiplayer.
func NucleusAuth(ctx context.Context, token string, uid uint64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://r2-pc.stryder.respawn.com/nucleus-oauth.php?qt=origin-requesttoken&type=server_token&code="+url.PathEscape(token)+"&forceTrial=0&proto=0&json=1&&env=production&userId="+strings.ToUpper(strconv.FormatUint(uid, 16)), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return nucleusAuth(resp)
}

func nucleusAuth(r *http.Response) ([]byte, error) {
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	// clean it up a bit
	buf = bytes.TrimSpace(buf)

	// check if the response is empty
	if len(buf) == 0 {
		return buf, fmt.Errorf("%w: empty response", ErrStryder)
	}

	// the subset of the response that we care about
	var obj struct {
		// error
		Success *bool       `json:"success,omitempty"`
		Status  json.Number `json:"status,omitempty"`
		Error   any         `json:"error,omitempty"`

		// success
		StoreURI        string      `json:"storeUri,omitempty"`
		HasOnlineAccess json.Number `json:"hasOnlineAccess,omitempty"`
	}

	// parse it as normal json
	if err = json.Unmarshal(buf, &obj); err != nil {
		// fix nested json objects inserted as-is
		tmp := bytes.ReplaceAll(buf, []byte(`"{`), []byte(`{`))
		tmp = bytes.ReplaceAll(tmp, []byte(`}"`), []byte(`}`))

		// parse the fixed json, but return the original error if it's also bad
		if json.Unmarshal(tmp, &obj) != nil {
			return buf, fmt.Errorf("%w: invalid json response %#q: %v", ErrStryder, string(buf), err)
		}
	}

	// check if it's a stryder error response
	if obj.Success != nil && !*obj.Success {
		// check if the error is an origin one (i.e., a nested json object) and if it's for an invalid/expired token
		if castOr(castOr(obj.Error, map[string]any{})["error"], "") == "invalid_grant" {
			return buf, ErrInvalidToken
		}

		// some other error
		oerr, _ := json.Marshal(obj.Error)
		return buf, fmt.Errorf("%w: error response %#q (status %#v)", ErrStryder, oerr, obj.Status)
	}

	// ensure the token is for the correct game
	if !strings.Contains(obj.StoreURI, "/titanfall-2") {
		return buf, ErrInvalidGame
	}

	// ensure hasOnlineAccess is true
	if obj.HasOnlineAccess != "1" {
		return buf, ErrMultiplayerNotAllowed
	}

	// otherwise it's fine
	return buf, nil
}

func castOr[T any](v any, d T) T {
	if x, ok := v.(T); ok {
		return x
	}
	return d
}
