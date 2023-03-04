package origin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/cardigann/harhar"
	"github.com/r2northstar/atlas/pkg/juno"
)

var ErrAuthMgrBackoff = errors.New("not refreshing token due to backoff")

// AuthMgr manages Origin NucleusTokens. It is efficient and safe for concurrent
// use.
//
// For persistence, load the credentials on startup using SetAuth, and store the
// credentials using Updated.
type AuthMgr struct {
	// Timeout is the timeout for refreshing tokens. If zero, a reasonable
	// default is used. If negative, there is no timeout.
	Timeout time.Duration

	// Updated, if provided, is called in a new goroutine when tokens have
	// changed. AuthState is always set and should be saved, even if an error
	// occured.
	Updated func(AuthState, error)

	// Credentials, if provided, is called to get credentials when updating the
	// SID.
	Credentials func() (email, password, otpsecret string, err error)

	// Backoff, if provided, checks if another refresh is allowed after a
	// failure. If it returns false, ErrAuthMgrBackoff will be returned
	// immediately from OriginAuth.
	Backoff func(err error, time time.Time, count int) bool

	// SaveHAR, if provided, is called after every attempt to authenticate.
	SaveHAR func(func(w io.Writer) error, error)

	authInit     sync.Once
	authPf       bool       // ensures only one update runs at a time
	authCv       *sync.Cond // allows other goroutines to wait for that update to complete
	authErr      error      // last auth error
	authErrTime  time.Time  // last auth error time
	authErrCount int        // consecutive auth errors
	auth         AuthState  // current auth tokens
}

// AuthState contains the current authentication tokens.
type AuthState struct {
	SID                juno.SID     `json:"sid,omitempty"`
	NucleusToken       NucleusToken `json:"nucleus_token,omitempty"`
	NucleusTokenExpiry time.Time    `json:"nucleus_token_expiry,omitempty"`
}

func (a *AuthMgr) init() {
	a.authInit.Do(func() {
		a.authCv = sync.NewCond(new(sync.Mutex))
	})
}

// SetAuth sets the current Origin credentials. If authentication is in
// progress, it will block.
func (a *AuthMgr) SetAuth(auth AuthState) {
	a.init()
	a.authCv.L.Lock()
	for a.authPf {
		a.authCv.Wait()
	}
	a.auth = auth
	a.authErr = nil
	a.authCv.L.Unlock()
}

// OriginAuth gets the current NucleusToken. If refresh is true or the nucleus
// token is missing/expired, it generates a new NucleusToken, getting a new SID
// if required. If another refresh is in progress, it waits for the result of
// it. True is returned (on success or failure) if this call performed a
// refresh. This function may block for up to Timeout.
//
// In general, OriginAuth(false) should be used first, then if an API call error
// is ErrAuthRequired, try it again with the token from OriginAuth(true).
func (a *AuthMgr) OriginAuth(refresh bool) (NucleusToken, bool, error) {
	a.init()
	if a.authCv.L.Lock(); a.authPf {
		for a.authPf {
			a.authCv.Wait()
		}
		defer a.authCv.L.Unlock()
		return a.auth.NucleusToken, false, a.authErr
	} else {
		if refresh || a.auth.NucleusToken == "" || !time.Now().Before(a.auth.NucleusTokenExpiry) {
			a.authPf = true
			a.authCv.L.Unlock()
			defer func() {
				a.authCv.L.Lock()
				a.authCv.Broadcast()
				a.authPf = false
				a.authCv.L.Unlock()
			}()
		} else {
			defer a.authCv.L.Unlock()
			return a.auth.NucleusToken, false, a.authErr
		}
	}
	if a.authErr != nil && a.Backoff != nil {
		if !a.Backoff(a.authErr, a.authErrTime, a.authErrCount) {
			return a.auth.NucleusToken, true, fmt.Errorf("%w (%d attempts, last error: %v)", ErrAuthMgrBackoff, a.authErrCount, a.authErrCount)
		}
	}
	a.authErr = func() (err error) {
		t := http.DefaultClient.Transport
		if t == nil {
			t = http.DefaultTransport
		}
		if a.SaveHAR != nil {
			rec := harhar.NewRecorder()
			rec.RoundTripper, t = t, rec
			defer func() {
				go a.SaveHAR(func(w io.Writer) error {
					return json.NewEncoder(w).Encode(rec.HAR)
				}, err)
			}()
		}

		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()

		var ctx context.Context
		var cancel context.CancelFunc
		if a.Timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), a.Timeout)
		} else if a.Timeout == 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Second*15)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		if a.auth.SID != "" {
			if tok, exp, aerr := GetNucleusToken(ctx, t, a.auth.SID); aerr == nil {
				a.auth.NucleusToken = tok
				a.auth.NucleusTokenExpiry = exp
				return
			} else if !errors.Is(aerr, ErrAuthRequired) {
				err = fmt.Errorf("refresh nucleus token: %w", aerr)
				return
			}
		}
		if a.Credentials == nil {
			err = fmt.Errorf("no origin credentials to refresh sid with")
			return
		} else if email, password, otpsecret, aerr := a.Credentials(); aerr != nil {
			err = fmt.Errorf("get origin credentials: %w", aerr)
			return
		} else if res, aerr := juno.Login(ctx, t, email, password, otpsecret); aerr != nil {
			err = fmt.Errorf("refresh sid: %w", aerr)
			return
		} else {
			a.auth.SID = res.SID
		}
		if tok, exp, aerr := GetNucleusToken(ctx, t, a.auth.SID); aerr != nil {
			err = fmt.Errorf("refresh nucleus token with new sid: %w", aerr)
		} else {
			a.auth.NucleusToken = tok
			a.auth.NucleusTokenExpiry = exp
		}
		return
	}()
	if a.authErrCount != 0 {
		a.authErr = fmt.Errorf("%w (attempt %d)", a.authErr, a.authErrCount)
	}
	if a.authErr != nil {
		a.authErrCount++
		a.authErrTime = time.Now()
	} else {
		a.authErrCount = 0
		a.authErrTime = time.Time{}
	}
	if a.Updated != nil {
		go a.Updated(a.auth, a.authErr)
	}
	return a.auth.NucleusToken, true, a.authErr
}
