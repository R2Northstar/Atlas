// Package juno implements a client for the EA juno login flow.
package juno

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"math/rand"
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

var (
	ErrCaptchaRequired  = errors.New("captcha required")
	ErrInvalidTwoFactor = errors.New("invalid two factor code")

	ErrJuno                    = junoLoginError{}
	ErrOnlineLoginNotAvailable = junoLoginError{Code: "10001"}
	ErrInvalidCredentials      = junoLoginError{Code: "10002"} // note: triggering this too many times will result in a captcha
	ErrJunoInternalError       = junoLoginError{Code: "10003"}
)

// AuthResult contains authentication tokens.
type AuthResult struct {
	Code string
	SID  SID
}

// SID is a persistent EA login session ID.
type SID string

// AddTo adds the SID cookie to j.
func (s SID) AddTo(j http.CookieJar) {
	j.SetCookies(&url.URL{
		Scheme: "https",
		Host:   "accounts.ea.com",
		Path:   "/connect",
	}, []*http.Cookie{{
		Name:   "sid",
		Value:  string(s),
		Secure: true,
	}})
}

// Login gets the SID fo
func Login(ctx context.Context, rt http.RoundTripper, email, password, otpsecret string) (AuthResult, error) {
	if rt == nil {
		rt = http.DefaultClient.Transport
	}

	s := &junoLoginState{
		Email:    email,
		Password: password,
	}

	if otpsecret != "" {
		b, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.ReplaceAll(otpsecret, " ", "")))
		if err != nil {
			return AuthResult{}, fmt.Errorf("parse totp secret: %w", err)
		}
		s.TOTP = func(t time.Time) string {
			return hotp(totp(t, 0), b, 0, nil)
		}
	}

	j, _ := cookiejar.New(nil)
	c := &http.Client{
		Transport: rt,
		Jar:       j,
	}

	for _, host := range []string{"www.ea.com", "accounts.ea.com", "signin.ea.com"} {
		c.Jar.SetCookies(&url.URL{
			Scheme: "https",
			Host:   host,
		}, []*http.Cookie{
			{Name: "ealocale", Value: "en-us"},
			{Name: "notice_behavior", Value: "implied,us"},
			{Name: "notice_location", Value: "us"},
		})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.ea.com/login", nil)
	if err != nil {
		return AuthResult{}, err
	}
	req.Header.Set("Referrer", "https://www.ea.com/en-us/")

	var reqs []string
	for {
		req.Header.Set("Accept-Language", "en-US;q=0.7,en;q=0.3")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36")

		if reqs = append(reqs, req.Method+" "+req.URL.String()); len(reqs) > 10 {
			return AuthResult{}, fmt.Errorf("too many requests (%q)", reqs)
		}

		resp, err := c.Do(req)
		if err != nil {
			return AuthResult{}, fmt.Errorf("do %s %q: %w", req.Method, req.URL.String(), err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return AuthResult{}, fmt.Errorf("do %s %q: response status %d (%s)", req.Method, req.URL.String(), resp.StatusCode, resp.Status)
		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			return AuthResult{}, fmt.Errorf("do %s %q: read response: %w", req.Method, req.URL.String(), err)
		}

		var via []string
		for last := resp.Request; last != nil; {
			if via = append(via, last.URL.String()); last.Response != nil {
				last = last.Response.Request
			} else {
				last = nil
			}
		}

		switch {
		case asciiEqualFold(resp.Request.URL.Hostname(), "www.ea.com"):
			for last := resp.Request; last != nil; last = last.Response.Request {
				if code := last.URL.Query().Get("code"); code != "" {
					for _, ck := range c.Jar.Cookies(&url.URL{
						Scheme: "https",
						Host:   "accounts.ea.com",
						Path:   "/connect",
					}) {
						if ck.Name == "sid" {
							return AuthResult{
								Code: code,
								SID:  SID(ck.Value),
							}, nil
						}
					}
					return AuthResult{}, fmt.Errorf("missing sid cookie")
				}
			}
			return AuthResult{}, fmt.Errorf("do %s %q: unhandled response url (%q): back to homepage, but could not find auth code", req.Method, req.URL.String(), via)

		case asciiEqualFold(resp.Request.URL.Hostname(), "signin.ea.com"):
			if !strings.HasPrefix(resp.Request.URL.Path, "/p/juno/") {
				return AuthResult{}, fmt.Errorf("do %s %q: unhandled response url (%q): not juno", req.Method, req.URL.String(), via)
			}

		default:
			return AuthResult{}, fmt.Errorf("do %s %q: unhandled response url (%q)", req.Method, req.URL.String(), via)
		}

		doc, err := html.ParseWithOptions(bytes.NewReader(buf), html.ParseOptionEnableScripting(true))
		if err != nil {
			return AuthResult{}, fmt.Errorf("do %s %q: parse document: %w", req.Method, req.URL.String(), err)
		}
		resp.Body.Close()

		req, err = s.junoLoginStep(resp.Request.URL, doc)
		if err != nil {
			return AuthResult{}, err
		}
		req = req.WithContext(ctx)
	}
}

type junoLoginState struct {
	Email    string
	Password string
	TOTP     func(time.Time) string

	seenLogin         bool
	seenTOS           bool
	seenTwoFactor     bool
	seenTwoFactorCode bool
	seenEnd           bool
}

type junoLoginError struct {
	Code string
	Desc string
}

func (err junoLoginError) Error() string {
	var codeDesc string
	switch err.Code {
	case "10001":
		codeDesc = ": online login not available"
	case "10002":
		codeDesc = ": invalid credentials"
	case "10003":
		codeDesc = ": internal error"
	case "10004":
		codeDesc = ": wtf" // idk what this is
	case "":
		return fmt.Sprintf("juno error (%q)", err.Desc)
	}
	if err.Desc == "" {
		return fmt.Sprintf("juno error %s%s (%q)", err.Code, codeDesc, err.Desc)
	}
	return fmt.Sprintf("juno error %s%s (%q)", err.Code, codeDesc, err.Desc)
}

func (err junoLoginError) Is(other error) bool {
	if v, ok := other.(junoLoginError); ok {
		return err.Code == "" || v.Code == err.Code
	}
	return false
}

func (s *junoLoginState) junoLoginStep(u *url.URL, doc *html.Node) (*http.Request, error) {
	if n := qs(doc, "form#login-form"); n != nil {
		r, err := s.junoLoginStepLogin(u, doc, n)
		if err != nil {
			err = fmt.Errorf("handle login: %w", err)
		}
		return r, err
	}
	if n := qs(doc, "form#loginForm:has(#tfa-login)"); n != nil {
		r, err := s.junoStepTwoFactor(u, doc, n)
		if err != nil {
			err = fmt.Errorf("handle two factor auth: %w", err)
		}
		return r, err
	}
	if n := qs(doc, "form#tosForm"); n != nil {
		r, err := s.junoStepTOSUpdate(u, doc, n)
		if err != nil {
			err = fmt.Errorf("handle tos update: %w", err)
		}
		return r, err
	}
	if n := qs(doc, "#login-container-end"); n != nil {
		r, err := s.junoStepEnd(u, doc)
		if err != nil {
			err = fmt.Errorf("handle login end: %w", err)
		}
		return r, err
	}
	var fs []string
	for _, f := range qsa(doc, "form") {
		var (
			id, _   = htmlAttr(f, "id", "")
			name, _ = htmlAttr(f, "name", "")
		)
		fs = append(fs, fmt.Sprintf("form[id=%s][name=%s]", id, name))
	}
	return nil, fmt.Errorf("handle login step (url: %s): unhandled step (forms: %s)", u.String(), strings.Join(fs, ", "))
}

func (s *junoLoginState) junoLoginStepLogin(u *url.URL, doc, form *html.Node) (*http.Request, error) {
	var (
		errorCode, _ = htmlAttr(qs(form, "#errorCode[value]"), "value", "")
		errorDesc    = htmlText(qs(form, "#online-general-error > p"))
	)
	if errorCode != "" || errorDesc != "" {
		return nil, junoLoginError{Code: errorCode, Desc: errorDesc}
	}
	if qs(doc, "#g-recaptcha-response") != nil {
		return nil, fmt.Errorf("%w (recapcha)", ErrCaptchaRequired)
	}
	if qs(doc, "#funcaptcha-solved") != nil {
		return nil, fmt.Errorf("%w (funcaptcha)", ErrCaptchaRequired)
	}
	if s.seenLogin {
		return nil, fmt.Errorf("already seen (and could not find an error)")
	} else {
		s.seenLogin = true
	}
	return junoFillForm(u, form, junoFormData{
		Fill: func(name, defvalue string) (string, error) {
			switch name {
			case "loginMethod":
				return "emailPassword", nil
			case "cid":
				const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXTZabcdefghiklmnopqrstuvwxyz"
				b := make([]byte, 32)
				for i := range b {
					b[i] = charset[rand.Intn(len(charset))]
				}
				return string(b), nil
			case "email":
				if s.Email == "" {
					return "", fmt.Errorf("%w: no email provided", ErrInvalidCredentials)
				}
				return s.Email, nil
			case "password":
				if s.Password == "" {
					return "", fmt.Errorf("%w: no email provided", ErrInvalidCredentials)
				}
				return s.Password, nil
			default:
				return defvalue, nil
			}
		},
		Expect: map[string]bool{
			"email":    true,
			"password": true,
		},
	})
}

func (s *junoLoginState) junoStepTOSUpdate(u *url.URL, doc, form *html.Node) (*http.Request, error) {
	if s.seenTOS {
		return nil, fmt.Errorf("already seen")
	} else {
		s.seenTOS = true
	}
	return junoFillForm(u, form, junoFormData{
		Fill: func(name, defvalue string) (string, error) {
			switch name {
			case "readAccept", "_readAccept":
				return "on", nil
			default:
				return defvalue, nil
			}
		},
		Expect: map[string]bool{
			"readAccept":  true,
			"_readAccept": true,
		},
	})
}

func (s *junoLoginState) junoStepTwoFactor(u *url.URL, doc, form *html.Node) (*http.Request, error) {
	if qs(form, "#btnSendCode") != nil {
		if s.seenTwoFactor {
			return nil, fmt.Errorf("already seen send code page")
		} else {
			s.seenTwoFactor = true
		}
		return junoFillForm(u, form, junoFormData{
			Fill: func(name, defvalue string) (string, error) {
				switch name {
				case "codeType":
					return "APP", nil
				default:
					return defvalue, nil
				}
			},
			Expect: map[string]bool{
				"codeType":    true,
				"oneTimeCode": false,
			},
		})
	}
	req, err := junoFillForm(u, form, junoFormData{
		Fill: func(name, defvalue string) (string, error) {
			switch name {
			case "oneTimeCode":
				if defvalue != "" {
					return "", fmt.Errorf("%w %q", ErrInvalidTwoFactor, defvalue)
				}
				if s.TOTP == nil {
					return "", fmt.Errorf("%w: no totp secret provided", ErrInvalidTwoFactor)
				}
				return s.TOTP(time.Now()), nil
			default:
				return defvalue, nil
			}
		},
		Expect: map[string]bool{
			"oneTimeCode": true,
		},
	})
	if err == nil {
		if s.seenTwoFactorCode {
			return nil, fmt.Errorf("already seen")
		} else {
			s.seenTwoFactorCode = true
		}
	}
	return req, err
}

func (s *junoLoginState) junoStepEnd(u *url.URL, doc *html.Node) (*http.Request, error) {
	if s.seenEnd {
		return nil, fmt.Errorf("already seen")
	} else {
		s.seenEnd = true
	}
	for _, n := range qsa(doc, "script") {
		var d strings.Builder
		htmlWalkDFS(n, func(n *html.Node, _ int) error {
			if n.Type == html.CommentNode || n.Type == html.TextNode {
				d.WriteString(n.Data)
			}
			return nil
		})
		if m := regexp.MustCompile(`(?m)window.location\s*=\s*["'](https://[^"'\\]+/connect/auth[^"'\\]+)["']`).FindStringSubmatch(d.String()); m != nil {
			r, err := u.Parse(string(m[1]))
			if err != nil {
				return nil, fmt.Errorf("resolve js redirect %q against %q: %w", string(m[1]), u.String(), err)
			}

			req, err := http.NewRequest(http.MethodGet, r.String(), nil)
			if err == nil {
				req.Header.Set("Referrer", u.String())
			}
			return req, nil
		}
	}
	return nil, fmt.Errorf("could not find js redirect")
}

type junoFormData struct {
	Fill   func(name, defvalue string) (string, error)
	Expect map[string]bool
}

// junoFillForm fills the provided HTML form, using values from data.Fill
// (returning an error if the returned value is invalid for a select/radio/etc),
// and ensuring that the fields in data.Expect are present (or not) as expected.
func junoFillForm(u *url.URL, form *html.Node, data junoFormData) (*http.Request, error) {
	if form.DataAtom != atom.Form {
		return nil, fmt.Errorf("element is not a form")
	}
	submitURL := &url.URL{
		Scheme:   "https",
		Host:     u.Host,
		Path:     u.Path,
		RawPath:  u.RawPath,
		RawQuery: u.RawQuery,
	}
	for _, a := range form.Attr {
		if a.Namespace == "" {
			switch strings.ToLower(a.Key) {
			case "action":
				if v, err := u.Parse(a.Val); err == nil {
					submitURL = v
				} else {
					return nil, fmt.Errorf("resolve form submit url: %w", err)
				}
			case "method":
				if a.Val != "" && strings.ToLower(a.Val) != "post" {
					return nil, fmt.Errorf("unexpected form method %q", a.Val)
				}
			case "enctype":
				if a.Val != "" && strings.ToLower(a.Val) != "application/x-www-form-urlencoded" {
					return nil, fmt.Errorf("unexpected form method %q", a.Val)
				}
			}
		}
	}

	var (
		formData     = url.Values{}
		formOptions  = map[string][]string{}
		formCheckbox = map[string]string{}
	)
	for _, n := range qsa(form, `[name]`) {
		var (
			eName, _    = htmlAttr(n, "name", "")
			eValue, _   = htmlAttr(n, "value", "")
			eType, _    = htmlAttr(n, "type", "")
			_, eChecked = htmlAttr(n, "checked", "")
		)
		if eName == "" {
			continue
		}
		switch n.DataAtom {
		case atom.A:
			// ignore
		case atom.Input:
			switch {
			case asciiEqualFold(eType, "submit"), asciiEqualFold(eType, "reset"), asciiEqualFold(eType, "image"), asciiEqualFold(eType, "button"):
				continue // ignore buttons
			case asciiEqualFold(eType, "checkbox"):
				if eValue != "" {
					formCheckbox[eName] = eValue
				} else {
					formCheckbox[eName] = "on"
				}
				if eChecked {
					formData[eName] = []string{formCheckbox[eName]}
				} else {
					formData[eName] = nil
				}
			case asciiEqualFold(eType, "radio"):
				if eValue != "" {
					formOptions[eName] = append(formOptions[eName], eValue)
				} else {
					formOptions[eName] = append(formOptions[eName], "on")
				}
				if eChecked {
					if eValue != "" {
						formData[eName] = []string{eValue}
					} else {
						formData[eName] = []string{"on"}
					}
				} else {
					formData[eName] = nil
				}
			default:
				formData[eName] = []string{eValue}
			}
		case atom.Select:
			if _, x := htmlAttr(n, "multiple", ""); x {
				return nil, fmt.Errorf("unhandled form element %s[multiple]", n.DataAtom)
			}
			for i, m := range qsa(n, `option`) {
				if v, ok := htmlAttr(m, "value", ""); ok {
					if _, selected := htmlAttr(m, "selected", ""); selected || i == 0 {
						formData[eName] = []string{v}
					}
					formOptions[eName] = append(formOptions[eName], v)
				}
			}
		default:
			return nil, fmt.Errorf("unhandled form element %s[name=%s]", n.DataAtom, eName)
		}
	}

	for k, v := range formData {
		if data.Expect != nil {
			if expected, ok := data.Expect[k]; ok {
				if expected {
					delete(data.Expect, k)
				} else {
					return nil, fmt.Errorf("have unexpected field %q", k)
				}
			}
		}
		var defvalue string
		if len(v) != 0 {
			defvalue = v[0]
		}
		if value, err := data.Fill(k, defvalue); err != nil {
			return nil, fmt.Errorf("fill field %q: %w", k, err)
		} else if value != defvalue {
			if opts, isSelect := formOptions[k]; isSelect {
				if value == "" {
					formData[k] = nil
				} else {
					var found bool
					for _, opt := range opts {
						if value == opt {
							found = true
							break
						}
					}
					if !found {
						return nil, fmt.Errorf("fill field %q: new value %q not in options %q", k, value, opts)
					}
					formData[k] = []string{value}
				}
			} else {
				formData[k] = []string{value}
			}
		}
	}
	if data.Expect != nil {
		for k, expected := range data.Expect {
			if expected {
				return nil, fmt.Errorf("missing expected field %q", k)
			}
		}
	}

	req, err := http.NewRequest(http.MethodPost, submitURL.String(), strings.NewReader(formData.Encode()))
	if err == nil {
		req.Header.Set("Referrer", u.String())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req, err
}

var (
	htmlWalkBreak = errors.New("break") //lint:ignore ST1012 special error
	htmlWalkSkip  = errors.New("skip")  //lint:ignore ST1012 special error
)

// htmlWalk does a depth-first walk of the provided node.
func htmlWalkDFS(n *html.Node, fn func(n *html.Node, depth int) error) error {
	if n != nil {
		var depth int
		var stk []*html.Node
		for stk = append(stk, n); len(stk) != 0; {
			var cur *html.Node
			if cur, stk = stk[len(stk)-1], stk[:len(stk)-1]; cur != nil {
				var skip bool
				if err := fn(cur, depth); err != nil {
					if err == htmlWalkBreak {
						return nil
					}
					if err != htmlWalkSkip {
						return err
					}
					skip = true
				}
				if !skip && cur.LastChild != nil {
					stk = append(stk, nil)
					for n := cur.LastChild; n != nil; n = n.PrevSibling {
						stk = append(stk, n)
					}
					depth++
				}
			} else {
				depth--
			}
		}
	}
	return nil
}

// htmlText gets the normalized inner text of n.
func htmlText(n *html.Node) string {
	var tok []string
	htmlWalkDFS(n, func(n *html.Node, _ int) error {
		if n.Type == html.TextNode {
			tok = append(tok, strings.Fields(n.Data)...)
		}
		return nil
	})
	return strings.Join(tok, " ")
}

// htmlAttr gets the value of a non-namespaced attribute.
func htmlAttr(n *html.Node, key, defvalue string) (string, bool) {
	if n != nil {
		for _, a := range n.Attr {
			if a.Namespace == "" && asciiEqualFold(a.Key, key) {
				return a.Val, true
			}
		}
	}
	return defvalue, false
}

// qs executes a CSS selector against n, returning a single match.
func qs(n *html.Node, q string) *html.Node {
	if n == nil {
		return nil
	}
	return cascadia.Query(n, cascadia.MustCompile(q))
}

// qsa executes a CSS selector against n, returning all matches.
func qsa(n *html.Node, q string) []*html.Node {
	if n == nil {
		return nil
	}
	return cascadia.QueryAll(n, cascadia.MustCompile(q))
}

// asciiEqualFold is like strings.EqualFold, but ASCII-only.
func asciiEqualFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if asciiLower(s[i]) != asciiLower(t[i]) {
			return false
		}
	}
	return true
}

// asciiLower gets the ASCII lowercase version of b.
func asciiLower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// totp returns the RFC6238 time-based counter for hotp.
func totp(t time.Time, s time.Duration) uint64 {
	if t.IsZero() {
		t = time.Now()
	}
	if s == 0 {
		s = time.Second * 30
	}
	return uint64(math.Floor(float64(t.Unix()) / s.Seconds()))
}

// hotp computes a RFC4226 otp.
func hotp(c uint64, k []byte, n int, h func() hash.Hash) string {
	if n == 0 {
		n = 6
	}
	if h == nil {
		h = sha1.New
	}
	if n <= 0 || n > 8 {
		panic("otp: must be 0 < n <= 8")
	}
	if len(k) == 0 {
		panic("otp: key must not be empty")
	}
	hsh := hmac.New(h, k)
	binary.Write(hsh, binary.BigEndian, c)
	dst := hsh.Sum(nil)
	off := dst[len(dst)-1] & 0xf
	val := int64(((int(dst[off]))&0x7f)<<24 |
		((int(dst[off+1] & 0xff)) << 16) |
		((int(dst[off+2] & 0xff)) << 8) |
		((int(dst[off+3]) & 0xff) << 0))
	return fmt.Sprintf("%0*d", n, val%int64(math.Pow10(n)))
}
