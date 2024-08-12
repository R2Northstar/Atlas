package stryder

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNucleusAuth(t *testing.T) {
	testNucleusAuth(t, "Success", `{"token":"...","hasOnlineAccess":"1","expiry":"14399","storeUri":"https://www.origin.com/store/titanfall/titanfall-2/standard-edition"}`, nil, nil)
	testNucleusAuth(t, "SuccessNew", `{"token":"...","hasOnlineAccess":"1","expiry":"14399","storeUri":"https://www.origin.com/store/titanfall/titanfall-2/standard-edition","userName":"test"}`, strPtr("test"), nil)
	testNucleusAuth(t, "NoMultiplayer", `{"token":"NO_ONLINE_ACCESS","hasOnlineAccess":"0","expiry":"14399","storeUri":"https://www.origin.com/store/titanfall/titanfall-2/standard-edition"}`, nil, ErrMultiplayerNotAllowed)
	testNucleusAuth(t, "NoMultiplayerNew", `{"token":"NO_ONLINE_ACCESS","hasOnlineAccess":"0","expiry":"14399","storeUri":"https://www.origin.com/store/titanfall/titanfall-2/standard-edition","userName":""}`, strPtr(""), ErrMultiplayerNotAllowed)
	testNucleusAuth(t, "InvalidToken", `{"success": false, "status": "400", "error": "{"error":"invalid_grant","error_description":"code is invalid","code":100100}"}`, nil, ErrInvalidToken)
	testNucleusAuth(t, "StryderBadRequest", `{"success": false, "status": "400", "error": "{"error":"invalid_request","error_description":"code is not issued to this environment","code":100119}"}`, nil, ErrStryder)
	testNucleusAuth(t, "StryderBadEndpoint", ``, nil, ErrStryder)
	testNucleusAuth(t, "StryderGoAway", "Go away.\n", nil, ErrStryder)
	testNucleusAuth(t, "InvalidGame", `{"token":"...","hasOnlineAccess":"1","expiry":"1234","storeUri":"https://www.origin.com/store/titanfall/titanfall-3/future-edition"}`, nil, ErrInvalidGame) // never seen this, but test it
}

func testNucleusAuth(t *testing.T, name, resp string, username *string, res error) {
	t.Run(name, func(t *testing.T) {
		buf, err := nucleusAuth(&http.Response{
			Status:     "200 OK",
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(resp)),
		})
		if !bytes.Equal(buf, bytes.TrimSpace([]byte(resp))) {
			t.Errorf("returned response %q doesn't match original response %q", string(buf), string(resp))
		}
		if res == nil {
			if err != nil {
				t.Errorf("unexpected error (resp %q): %v", resp, err)
			}
		} else {
			if !errors.Is(err, res) {
				t.Errorf("expected error %q, got %q", res, err)
			}
		}
		su, err := NucleusAuthUsername(buf)
		if username == nil {
			if err == nil {
				t.Errorf("expected username error for response %q, got none", string(buf))
			}
		} else {
			if err != nil {
				t.Errorf("unexpected username error for response %q: %v", string(buf), err)
			}
			if su != *username {
				t.Errorf("expected username %q for response %q, got %q", *username, string(buf), su)
			}
		}
	})
}

func strPtr(x string) *string {
	return &x
}
