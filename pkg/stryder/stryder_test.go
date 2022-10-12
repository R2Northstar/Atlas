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
	testNucleusAuth(t, "Success", `{"token":"...","hasOnlineAccess":"1","expiry":"14399","storeUri":"https://www.origin.com/store/titanfall/titanfall-2/standard-edition"}`, nil)
	testNucleusAuth(t, "NoMultiplayer", `{"token":"...","hasOnlineAccess":"0","expiry":"14399","storeUri":"https://www.origin.com/store/titanfall/titanfall-2/standard-edition"}`, ErrMultiplayerNotAllowed)
	testNucleusAuth(t, "InvalidToken", `{"success": false, "status": "400", "error": "{"error":"invalid_grant","error_description":"code is invalid","code":100100}"}`, ErrInvalidToken)
	testNucleusAuth(t, "StryderBadRequest", `{"success": false, "status": "400", "error": "{"error":"invalid_request","error_description":"code is not issued to this environment","code":100119}"}`, ErrStryder)
	testNucleusAuth(t, "StryderBadEndpoint", ``, ErrStryder)
	testNucleusAuth(t, "StryderGoAway", "Go away.\n", ErrStryder)
	testNucleusAuth(t, "InvalidGame", `{"token":"...","hasOnlineAccess":"1","expiry":"1234","storeUri":"https://www.origin.com/store/titanfall/titanfall-3/future-edition"}`, ErrInvalidGame) // never seen this, but test it
}

func testNucleusAuth(t *testing.T, name, resp string, res error) {
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
	})
}
