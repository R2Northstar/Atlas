package eax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// UpdateMgr manages EAX client version information.
type UpdateMgr struct {
	// HTTP client to use. If not provided, [net/http.DefaultClient] will be
	// used.
	Client *http.Client

	// Timeout is the timeout for refreshing tokens. If zero, a reasonable
	// default is used. If negative, there is no timeout.
	Timeout time.Duration

	// Interval to update at. If zero, will not auto-update.
	AutoUpdateInterval time.Duration

	// Auto-update staged roll-out bucket.
	AutoUpdateBucket int

	// Auto-update backoff, if provided, checks if another auto-update is
	// allowed after a failure. If it returns false, ErrAutoUpdateBackoff will be
	// returned from the function triggering the auto-update.
	AutoUpdateBackoff func(err error, time time.Time, count int) bool

	// AutoUpdateHook is called for every auto-update attempt with the new (or
	// current if error) version, and any error which occurred.
	AutoUpdateHook func(v string, err error)

	verInit     sync.Once
	verPf       bool       // ensures only one auto-update runs at a time
	verCv       *sync.Cond // allows other goroutines to wait for that update to complete
	verErr      error      // last auto-update error
	verErrTime  time.Time  // last auto-update error time
	verErrCount int        // consecutive auto-update errors
	ver         string     // current version
	verTime     time.Time  // last update check time
}

var ErrAutoUpdateBackoff = errors.New("not updating eax client version due to backoff")

func (u *UpdateMgr) init() {
	u.verInit.Do(func() {
		u.verCv = sync.NewCond(new(sync.Mutex))
	})
}

// SetVersion sets the current version.
func (u *UpdateMgr) SetVersion(v string) {
	u.init()
	u.verCv.L.Lock()
	for u.verPf {
		u.verCv.Wait()
	}
	u.ver = v
	u.verErr = nil
	u.verTime = time.Now()
	u.verCv.L.Unlock()
}

// Update gets the latest version, following u.AutoUpdateInterval if provided,
// unless the version is not set or force is true. If another update is in
// progress, it waits for the result of it. True is returned (on success or
// failure) if this call performed a update. This function may block for up to
// Timeout.
func (u *UpdateMgr) Update(force bool) (string, bool, error) {
	u.init()
	if u.verCv.L.Lock(); u.verPf {
		for u.verPf {
			u.verCv.Wait()
		}
		defer u.verCv.L.Unlock()
		return u.ver, false, u.verErr
	} else {
		if force || u.ver == "" || (u.AutoUpdateInterval != 0 && time.Since(u.verTime) > u.AutoUpdateInterval) {
			u.verPf = true
			u.verCv.L.Unlock()
			defer func() {
				u.verCv.L.Lock()
				u.verCv.Broadcast()
				u.verPf = false
				u.verCv.L.Unlock()
			}()
		} else {
			defer u.verCv.L.Unlock()
			return u.ver, false, u.verErr
		}
	}
	if u.verErr != nil && u.AutoUpdateBackoff != nil {
		if !u.AutoUpdateBackoff(u.verErr, u.verErrTime, u.verErrCount) {
			return u.ver, true, fmt.Errorf("%w (%d attempts, last error: %v)", ErrAutoUpdateBackoff, u.verErrCount, u.verErr)
		}
	}
	u.verErr = func() error {
		var ctx context.Context
		var cancel context.CancelFunc
		if u.Timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), u.Timeout)
		} else if u.Timeout == 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Second*15)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://autopatch.juno.ea.com/autopatch/upgrade/buckets/"+strconv.Itoa(u.AutoUpdateBucket), nil)
		if err != nil {
			return err
		}
		if u.ver != "" {
			req.Header.Set("User-Agent", "EADesktop/"+u.ver)
		} else {
			req.Header.Set("User-Agent", "")
		}

		cl := u.Client
		if cl == nil {
			cl = http.DefaultClient
		}

		resp, err := cl.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var obj struct {
			Minimum struct {
				Version        string `json:"version"`
				ActivationDate string `json:"activationDate"`
			} `json:"minimum"`
			Recommended struct {
				Version string `json:"version"`
			} `json:"recommended"`
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("response status %d (%s)", resp.StatusCode, resp.Status)
		}
		if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
			return fmt.Errorf("parse autopatch response: %w", err)
		}

		var version string
		if v := obj.Minimum.Version; v != "" {
			version = v
		}
		if v := obj.Recommended.Version; v != "" {
			version = v
		}
		if version == "" {
			return fmt.Errorf("parse autopatch response: missing minimum or recommended version")
		}
		u.ver = version
		u.verTime = time.Now()
		return nil
	}()
	if u.verErrCount != 0 {
		u.verErr = fmt.Errorf("%w (attempt %d)", u.verErr, u.verErrCount)
	}
	if u.verErr != nil {
		u.verErrCount++
		u.verErrTime = time.Now()
	} else {
		u.verErrCount = 0
		u.verErrTime = time.Time{}
	}
	if u.AutoUpdateHook != nil {
		go u.AutoUpdateHook(u.ver, u.verErr)
	}
	return u.ver, true, u.verErr
}
