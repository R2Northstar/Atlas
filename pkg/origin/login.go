package origin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/r2northstar/atlas/pkg/juno"
)

type NucleusToken string

// GetNucleusToken generates a Nucleus AuthToken from the active session. Note
// that this token generally lasts ~4h.
//
// If errors.Is(err, ErrAuthRequired), you need a new SID.
func GetNucleusToken(ctx context.Context, t http.RoundTripper, sid juno.SID) (NucleusToken, time.Time, error) {
	if t == nil {
		t = http.DefaultClient.Transport
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{
		Transport: t,
		Jar:       jar,
	}
	sid.AddTo(c.Jar)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://accounts.ea.com/connect/auth?client_id=ORIGIN_JS_SDK&response_type=token&redirect_uri=nucleus:rest&prompt=none&release_type=prod", nil)
	if err != nil {
		return "", time.Time{}, err
	}

	req.Header.Set("Referrer", "https://www.origin.com/")
	req.Header.Set("Origin", "https://www.origin.com/")

	req.Header.Set("Accept-Language", "en-US;q=0.7,en;q=0.3")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36")

	resp, err := c.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get nucleus token: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get nucleus token: %w", err)
	}

	var eobj struct {
		ErrorCode   string      `json:"error_code"`
		Error       string      `json:"error"`
		ErrorNumber json.Number `json:"error_number"`
	}
	if err := json.Unmarshal(buf, &eobj); err == nil && eobj.Error != "" {
		if eobj.ErrorCode == "login_required" {
			return "", time.Time{}, fmt.Errorf("get nucleus token: %w: login required", ErrAuthRequired)
		}
		return "", time.Time{}, fmt.Errorf("get nucleus token: %w: error %s: %s (%q)", ErrOrigin, eobj.ErrorNumber, eobj.ErrorCode, eobj.Error)
	}

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("get nucleus token: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
	}

	var obj struct {
		AccessToken string      `json:"access_token"`
		TokenType   string      `json:"token_type"`
		ExpiresIn   json.Number `json:"expires_in"`
	}
	if err := json.Unmarshal(buf, &obj); err != nil {
		return "", time.Time{}, fmt.Errorf("get nucleus token: %w", err)
	}
	if obj.AccessToken == "" || obj.ExpiresIn == "" {
		return "", time.Time{}, fmt.Errorf("get nucleus token: invalid response %q", string(buf))
	}

	var expiry time.Time
	if v, err := obj.ExpiresIn.Int64(); err == nil {
		expiry = time.Now().Add(time.Duration(v) * time.Second)
	} else {
		return "", time.Time{}, fmt.Errorf("get nucleus token: invalid response %q: invalid expiry: %w", string(buf), err)
	}
	return NucleusToken(obj.AccessToken), expiry, nil
}
