package origin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrInvalidLogin = errors.New("invalid credentials")

type SID string
type NucleusToken string

// Login logs into an Origin account and returns the SID cookie.
func Login(ctx context.Context, email, password string) (SID, error) {
	jar, _ := cookiejar.New(nil)

	c := &http.Client{
		Transport: http.DefaultClient.Transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			switch host, _, _ := strings.Cut(req.URL.Host, ":"); strings.ToLower(host) {
			case "accounts.ea.com", "signin.ea.com", "www.origin.com":
			default:
				return fmt.Errorf("domain %q is not whitelisted", host)
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	r0, err := login0(ctx, c)
	if err != nil {
		return "", err
	}

	r1, err := login1(ctx, c, r0, email, password)
	if err != nil {
		return "", err
	}

	r2, err := login2(ctx, c, r1)
	if err != nil {
		return "", err
	}

	_, err = login3(ctx, c, r2)
	if err != nil {
		return "", err
	}

	for _, ck := range c.Jar.Cookies(&url.URL{
		Scheme: "https",
		Host:   "accounts.ea.com",
		Path:   "/connect",
	}) {
		if ck.Name == "sid" {
			return SID(ck.Value), nil
		}
	}
	return "", fmt.Errorf("missing sid cookie")
}

// login0 initializes the login flow.
//
// Returns a HTTP request for opening the login form.
func login0(ctx context.Context, c *http.Client) (*http.Request, error) {
	// init locale and cookie settings
	for _, host := range []string{"www.origin.com", "accounts.ea.com", "signin.ea.com"} {
		c.Jar.SetCookies(&url.URL{
			Scheme: "https",
			Host:   host,
		}, []*http.Cookie{
			{Name: "ealocale", Value: "en-us"},
			{Name: "notice_behavior", Value: "implied,us"},
			{Name: "notice_location", Value: "us"},
		})
	}

	// login page (opened with window.open from the Origin webapp)
	return http.NewRequestWithContext(ctx, http.MethodGet, "https://accounts.ea.com/connect/auth?response_type=code&client_id=ORIGIN_SPA_ID&display=originXWeb/login&locale=en_US&release_type=prod&redirect_uri=https://www.origin.com/views/login.html", nil)
}

// login1 starts the login flow.
//
//	GET https://accounts.ea.com/connect/auth?response_type=code&client_id=ORIGIN_SPA_ID&display=originXWeb/login&locale=en_US&release_type=prod&redirect_uri=https://www.origin.com/views/login.html
//	302 https://signin.ea.com/p/originX/login?fid=...
//	302 https://signin.ea.com/p/originX/login?execution=e678590193s1&initref=...
//
// Returns a HTTP request for submitting the login form.
func login1(ctx context.Context, c *http.Client, r0 *http.Request, email, password string) (*http.Request, error) {
	resp, err := c.Do(r0)
	if err != nil {
		return nil, fmt.Errorf("start login flow: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("start login flow: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "application/json" {
			var obj struct {
				Error            string       `json:"error"`
				ErrorDescription fmt.Stringer `json:"error_description"`
				Code             int          `json:"code"`
			}
			if err := json.Unmarshal(buf, &obj); err == nil && obj.Code != 0 {
				return nil, fmt.Errorf("start login flow: %w: error %d: %s (%q)", ErrOrigin, obj.Code, obj.Error, obj.ErrorDescription)
			}
		}
		return nil, fmt.Errorf("start login flow: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
	}

	if resp.Request.URL.Path != "/p/originX/login" {
		return nil, fmt.Errorf("start login flow: unexpected login form path %q (the code probably needs to be updated)", resp.Request.URL.Path)
	}

	doc, err := html.ParseWithOptions(bytes.NewReader(buf), html.ParseOptionEnableScripting(true))
	if err != nil {
		return nil, fmt.Errorf("start login flow: parse document: %w", err)
	}

	form := cascadia.Query(doc, cascadia.MustCompile(`form#login-form`))
	if form == nil {
		return nil, fmt.Errorf("start login flow: parse document: failed to find login-form element")
	}

	submitURL := &url.URL{
		Scheme:   "https",
		Host:     resp.Request.URL.Host,
		Path:     resp.Request.URL.Path,
		RawPath:  resp.Request.URL.RawPath,
		RawQuery: resp.Request.URL.RawQuery,
	}
	for _, a := range form.Attr {
		if a.Namespace == "" {
			switch strings.ToLower(a.Key) {
			case "action":
				if v, err := resp.Request.URL.Parse(a.Val); err == nil {
					submitURL = v
				} else {
					return nil, fmt.Errorf("start login flow: parse document: resolve form submit url: %w", err)
				}
			case "method":
				if a.Val != "" && strings.ToLower(a.Val) != "post" {
					return nil, fmt.Errorf("start login flow: parse document: unexpected form method %q", a.Val)
				}
			case "enctype":
				if a.Val != "" && strings.ToLower(a.Val) != "application/x-www-form-urlencoded" {
					return nil, fmt.Errorf("start login flow: parse document: unexpected form method %q", a.Val)
				}
			}
		}
	}

	/*
		<form id="login-form" method="post">

			<div class="otkform otkform-inputgroup">

				<div class="otkinput otkinput-grouped">
					<i class="otkinput-icon otkicon otkicon-profile"></i>
					<input type="text" id="email" name="email" value="" placeholder="Email Address" autocorrect="off" autocapitalize="off" autocomplete="off" />
				</div>
				<div class="otkinput otkinput-grouped">
					<i class="otkinput-icon otkicon otkicon-lockclosed"></i>
					<input type="password" id="password" name="password" placeholder="Password" autocorrect="off" autocapitalize="off" autocomplete="off" />
					<i class="otkinput-capslock otkicon otkicon-capslock otkicon-capslock-position"></i>
					<span id="passwordShow" class="otkbtn otkbtn-light">SHOW</span>
				</div>
			</div>

			<div id="online-general-error" class="otkform-group-help">
				<p class="otkinput-errormsg otkc"></p>
			</div>
			<div id="offline-general-error" class="otkform-group-help">
				<p class="otkinput-errormsg otkc">You must be online when logging in for the first time.</p>
			</div>
			<div id="offline-auth-error" class="otkform-group-help">
				<p class="otkinput-errormsg otkc">Your credentials are incorrect or have expired. Please try again or reset your password.</p>
			</div>

			<div id="captcha-container">
			</div>

			<div class="panel-action-area">
				<input type="hidden" name="_eventId" value="submit" id="_eventId" />
				<input type="hidden" id="cid" name="cid" value="">

				<input type="hidden" id="showAgeUp" name="showAgeUp" value="true">

				<input type="hidden" id="thirdPartyCaptchaResponse" name="thirdPartyCaptchaResponse" value="">

				<span class="otkcheckbox  checkbox-login-first">
					<input type="hidden" name="_rememberMe" value="on" />
					<input type="checkbox" id="rememberMe" name="rememberMe" checked="checked" />
					<label for=rememberMe>
						<span id="content">Remember me</span>


					</label>
				</span>
				<a class='otkbtn otkbtn-primary ' href="#" id="logInBtn">Sign in</a>
				<input type="hidden" id="errorCode" value="" />
				<input type="hidden" id="errorCodeWithDescription" value="" />
				<input type="hidden" id="storeKey" value="" />
				<input type="hidden" id="bannerType" value="" />
				<input type="hidden" id="bannerText" value="" />
			</div>

		</form>
	*/

	data := url.Values{}
	for _, el := range cascadia.QueryAll(form, cascadia.MustCompile(`[name]`)) {
		if el.DataAtom == atom.A {
			continue
		}
		var eName, eValue, eType string
		var eChecked bool
		for _, a := range el.Attr {
			if a.Namespace == "" {
				switch strings.ToLower(a.Key) {
				case "name":
					eName = a.Val
				case "value":
					eValue = a.Val
				case "type":
					eType = strings.ToLower(a.Val)
				case "checked":
					eChecked = true
				}
			}
		}
		if el.DataAtom != atom.Input {
			return nil, fmt.Errorf("start login flow: parse document: unexpected form %s element %s", el.DataAtom, eName)
		}
		if eType == "submit" || eType == "reset" || eType == "image" || eType == "button" {
			continue // ignore buttons
		}
		if eChecked && eValue == "" {
			eValue = "on"
		}
		if eName == "cid" {
			eValue = generateCID() // populated by js
		}
		if (eType == "checkbox" || eType == "radio") && eValue == "" {
			continue
		}
		data.Set(eName, eValue)
	}
	if !data.Has("email") || !data.Has("password") {
		return nil, fmt.Errorf("start login flow: parse document: missing username or password field (data=%s)", data.Encode())
	}

	data.Set("email", email)
	data.Set("password", password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL.String(), strings.NewReader(data.Encode()))
	if err == nil {
		req.Header.Set("Referrer", resp.Request.URL.String())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req, err
}

var login2re = regexp.MustCompile(`(?m)window.location\s*=\s*["'](https://[^"'\\]+/auth[^"'\\]+)["']`)

// login2 submits the login form.
//
//	POST https://signin.ea.com/p/originX/login?execution=...s1&initref=... (email=...&password=...&_eventId=submit&cid=...&showAgeUp=true&thirdPartyCaptchaResponse=&_rememberMe=on&rememberMe=on)
//	window.location = "https://accounts.ea.com:443/connect/auth?display=originXWeb%2Flogin&response_type=code&release_type=prod&redirect_uri=https%3A%2F%2Fwww.origin.com%2Fviews%2Flogin.html&locale=en_US&client_id=ORIGIN_SPA_ID&fid=...";
//
// Returns the redirect request.
func login2(ctx context.Context, c *http.Client, r1 *http.Request) (*http.Request, error) {
	resp, err := c.Do(r1)
	if err != nil {
		return nil, fmt.Errorf("submit login form: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("submit login form: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "application/json" {
			var obj struct {
				Error            string       `json:"error"`
				ErrorDescription fmt.Stringer `json:"error_description"`
				Code             int          `json:"code"`
			}
			if err := json.Unmarshal(buf, &obj); err == nil && obj.Code != 0 {
				return nil, fmt.Errorf("submit login form: %w: error %d: %s (%q)", ErrOrigin, obj.Code, obj.Error, obj.ErrorDescription)
			}
		}
		return nil, fmt.Errorf("submit login form: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
	}

	m := login2re.FindSubmatch(buf)
	if m == nil {
		if doc, err := html.Parse(bytes.NewReader(buf)); err == nil {
			if n := cascadia.Query(doc, cascadia.MustCompile(`#errorCode[value]`)); n != nil {
				for _, a := range n.Attr {
					// based on origin login js
					if a.Namespace == "" && strings.EqualFold(a.Key, "value") {
						switch errCode := a.Val; errCode {
						case "10001": // try offline auth
							return nil, fmt.Errorf("submit login form: ea auth error %s: why the fuck does origin think we're offline", errCode)
						case "10002": // credentials
							return nil, fmt.Errorf("submit login form: ea auth error %s: %w", errCode, ErrInvalidLogin)
						case "10003": // general error
							return nil, fmt.Errorf("submit login form: ea auth error %s: login error", errCode)
						case "10004": // wtf
							return nil, fmt.Errorf("submit login form: ea auth error %s: idk wtf this is", errCode)
						case "":
							// no error, but this shouldn't happen
						default:
							return nil, fmt.Errorf("submit login form: ea auth error %s", errCode)
						}
					}
				}
			}
		}
		return nil, fmt.Errorf("submit login form: could not find JS redirect URL")
	}

	u, err := resp.Request.URL.Parse(string(m[1]))
	if err != nil {
		return nil, fmt.Errorf("submit login form: could not resolve JS redirect URL %q against %q", string(m[1]), resp.Request.URL.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err == nil {
		req.Header.Set("Referrer", resp.Request.URL.String())
	}
	return req, err
}

// login3 finishes the login flow.
//
//	GET https://accounts.ea.com:443/connect/auth?display=originXWeb%2Flogin&response_type=code&release_type=prod&redirect_uri=https%3A%2F%2Fwww.origin.com%2Fviews%2Flogin.html&locale=en_US&client_id=ORIGIN_SPA_ID&fid=...
//	302 https://www.origin.com/views/login.html?code=QUOxACG9yPs6t_IHz2K1adbc5yV4UPG-1hb_v2HY
//
// Returns the token.
func login3(_ context.Context, c *http.Client, r2 *http.Request) (string, error) {
	resp, err := c.Do(r2)
	if err != nil {
		return "", fmt.Errorf("finish login flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if host, _, _ := strings.Cut(resp.Request.URL.Host, ":"); strings.ToLower(host) == "accounts.ea.com" {
			buf, _ := io.ReadAll(resp.Body)
			var obj struct {
				ErrorCode   string      `json:"error_code"`
				Error       string      `json:"error"`
				ErrorNumber json.Number `json:"error_number"`
			}
			if obj.ErrorCode == "login_required" {
				return "", fmt.Errorf("get nucleus token: %w: wants us to login, but we just did that", ErrOrigin)
			}
			if err := json.Unmarshal(buf, &obj); err == nil && obj.Error != "" {
				return "", fmt.Errorf("get nucleus token: %w: error %s: %s (%q)", ErrOrigin, obj.ErrorNumber, obj.ErrorCode, obj.Error)
			}
			return "", fmt.Errorf("get nucleus token: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
		}
		return "", fmt.Errorf("finish login flow: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
	}

	code := resp.Request.URL.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("finish login flow: failed to extract token from redirect URL %q", resp.Request.URL.String())
	}

	// don't waste the connection
	_, _ = io.Copy(io.Discard, resp.Body)

	return code, nil
}

// GetNucleusToken generates a Nucleus AuthToken from the active session. Note
// that this token generally lasts ~4h.
//
// If errors.Is(err, ErrAuthRequired), you need a new SID.
func GetNucleusToken(ctx context.Context, sid SID) (NucleusToken, time.Time, error) {
	jar, _ := cookiejar.New(nil)

	c := &http.Client{
		Transport: http.DefaultClient.Transport,
		Jar:       jar,
	}

	c.Jar.SetCookies(&url.URL{
		Scheme: "https",
		Host:   "accounts.ea.com",
		Path:   "/connect",
	}, []*http.Cookie{{
		Name:   "sid",
		Value:  string(sid),
		Secure: true,
	}})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://accounts.ea.com/connect/auth?client_id=ORIGIN_JS_SDK&response_type=token&redirect_uri=nucleus:rest&prompt=none&release_type=prod", nil)
	if err != nil {
		return "", time.Time{}, err
	}

	req.Header.Set("Referrer", "https://www.origin.com/")
	req.Header.Set("Origin", "https://www.origin.com/")

	resp, err := c.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get nucleus token: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get nucleus token: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var obj struct {
			ErrorCode   string      `json:"error_code"`
			Error       string      `json:"error"`
			ErrorNumber json.Number `json:"error_number"`
		}
		if obj.ErrorCode == "login_required" {
			return "", time.Time{}, fmt.Errorf("get nucleus token: %w: login required", ErrAuthRequired)
		}
		if err := json.Unmarshal(buf, &obj); err == nil && obj.Error != "" {
			return "", time.Time{}, fmt.Errorf("get nucleus token: %w: error %s: %s (%q)", ErrOrigin, obj.ErrorNumber, obj.ErrorCode, obj.Error)
		}
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

// generateCID generates a login nonce using the algorithm in the Origin login
// js script.
func generateCID() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXTZabcdefghiklmnopqrstuvwxyz"
	b := make([]byte, 32)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
