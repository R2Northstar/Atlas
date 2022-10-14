package origin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
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
	Credentials func() (email, password string, err error)

	// Backoff, if provided, checks if another refresh is allowed after a
	// failure. If it returns false, ErrAuthMgrBackoff will be returned
	// immediately from OriginAuth.
	Backoff func(err error, time time.Time, count int) bool

	authMu       sync.Mutex     // guards authWg so only one goroutine can get it
	authWg       sync.WaitGroup // guards the variables below and allows waiting for updates
	authErr      error          // last auth error
	authErrTime  time.Time      // last auth error time
	authErrCount int            // consecutive auth errors
	auth         AuthState      // current auth tokens
}

// AuthState contains the current authentication tokens.
type AuthState struct {
	SID                SID          `json:"sid,omitempty"`
	NucleusToken       NucleusToken `json:"nucleus_token,omitempty"`
	NucleusTokenExpiry time.Time    `json:"nucleus_token_expiry,omitempty"`
}

// SetAuth sets the current Origin credentials. If authentication is in
// progress, it will block.
func (a *AuthMgr) SetAuth(auth AuthState) {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	a.authWg.Add(1)
	defer a.authWg.Done()
	a.auth = auth
	a.authErr = nil
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
	if a.auth.NucleusToken == "" || !time.Now().Before(a.auth.NucleusTokenExpiry) {
		refresh = true
	}
	if !refresh {
		// wait for an in-progress auth, if any, to complete
		a.authWg.Wait()
		return a.auth.NucleusToken, false, a.authErr
	}
	if a.authMu.TryLock() {
		// refresh the auth
		defer a.authMu.Unlock()
		// if another goroutine gets scheduled in between us locking and adding
		// to the waitgroup, they'll get outdated auth, but it isn't a big deal
		// since if they try to refresh it right after, they'll end up waiting
		// on us to complete
		a.authWg.Add(1)
		defer a.authWg.Done()
	} else {
		// another goroutine is refreshing
		return a.OriginAuth(false)
	}
	if a.authErr != nil && a.Backoff != nil {
		if !a.Backoff(a.authErr, a.authErrTime, a.authErrCount) {
			return a.auth.NucleusToken, true, fmt.Errorf("%w (%d attempts, last error: %v)", ErrAuthMgrBackoff, a.authErrCount, a.authErrCount)
		}
	}
	a.authErr = func() (err error) {
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
			if tok, exp, aerr := GetNucleusToken(ctx, a.auth.SID); aerr == nil {
				a.auth.NucleusToken = tok
				a.auth.NucleusTokenExpiry = exp
				return
			} else if !errors.Is(err, ErrAuthRequired) {
				err = fmt.Errorf("refresh nucleus token: %w", aerr)
				return
			}
		}
		if a.Credentials == nil {
			err = fmt.Errorf("no origin credentials to refresh sid with")
			return
		} else if email, password, aerr := a.Credentials(); aerr != nil {
			err = fmt.Errorf("get origin credentials: %w", aerr)
			return
		} else if sid, aerr := Login(ctx, email, password); aerr != nil {
			err = fmt.Errorf("refresh sid: %w", aerr)
			return
		} else {
			a.auth.SID = sid
		}
		if tok, exp, aerr := GetNucleusToken(ctx, a.auth.SID); aerr != nil {
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
