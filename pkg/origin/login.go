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
			case "accounts.ea.com", "signin.ea.com", "www.ea.com":
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

	r2, again, err := login2(ctx, c, r1)
	if err != nil {
		return "", err
	}
	if !again {
		r2, again, err = login2(ctx, c, r2)
		if err != nil {
			return "", err
		}
		if again {
			return "", fmt.Errorf("looped at request to %q", r2.URL.String())
		}
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

	// login page (from the www.ea.com homepage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.ea.com/login", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Referrer", "https://www.ea.com/en-us")

	return req, nil
}

// login1 starts the login flow.
//
//   - GET https://www.ea.com/en-us
//   - 303 https://accounts.ea.com/connect/auth?response_type=code&redirect_uri=https://www.ea.com/login_check&state=...&locale=en_US&client_id=EADOTCOM-WEB-SERVER&display=junoWeb/login
//   - 302 https://signin.ea.com/p/juno/login?fid=...
//   - 302 https://signin.ea.com/p/juno/login?execution=...s1&initref=...
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

	if resp.Request.URL.Path != "/p/juno/login" {
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
			<div class="otkform otkform-inputgroup input-margin-bottom-error-message">
				<div id="email-phone-login-div">
					<div id="toggle-form-email-input">
						<div class="otkform-group">
							<label class="otklabel label-uppercase" for="email">Phone or Email</label>
							<div class="otkinput otkinput-grouped otkform-group-field input-margin-bottom-error-message">
								<input type="text" id="email" name="email" value="" placeholder="Enter your phone or email" autocorrect="off" autocapitalize="none" autocomplete="off">
							</div>
							<div id="online-input-error-email" class="otkform-group-help">
								<p class="otkinput-errormsg otkc"></p>
							</div>
						</div>
					</div>
					<div id="toggle-form-phone-number-input" style="display: none;">
						<div class="otkform-group input-margin-bottom-error-message">
							<label class="otklabel label-uppercase">Phone or Email</label>
							<span class="otkselect" style="display: none;">
								<select id="regionCode" name="regionCode">
									...
								</select>
								<span class="otkselect-label otkselect-placeholder phone-number-placeholder"></span>
								<span class="otkselect-label otkselect-selected phone-number-pad">(+1)</span>
							</span>
							<div style="display: none;" id="hidden-svg-container"></div>
							<div class="otkinput otkinput-grouped otkform-group-field">
								<a href="javascript:void(0);" class="region-select-drop-down-btn">
									<span class="quantum-input-icon">
										<svg>
											<use xlink:href="#ca"></use>
										</svg>
									</span>
									<span class="quantum-input-icon-2">+1</span>
								</a>
								<div class="region-select-drop-down-panel" style="display: none;">...
								</div>
								<input type="text" id="phoneNumber" name="phoneNumber" value="" placeholder="Enter your phone or email" autocorrect="off" autocapitalize="none" autocomplete="tel">
							</div>
						</div>
					</div>
				</div>
				<label class="otklabel label-uppercase" for="password">Password</label>
				<div class="otkinput otkinput-grouped input-margin-bottom-error-message">
					<input type="password" id="password" name="password" placeholder="Enter your password" autocorrect="off" autocapitalize="none" autocomplete="off">
					<i class="otkinput-capslock otkicon otkicon-capslock otkicon-capslock-position"></i>
					<button role="button" aria-label="Show password" id="passwordShow" class="otkbtn passwordShowBtn">
						<span id="showIcon" class="quantum-input-icon eye-icon"><svg height="16" viewBox="0 0 16 16" width="16" xmlns="http://www.w3.org/2000/svg">
								<g fill="none" fill-rule="evenodd">
									<path d="m0 0h16v16h-16z"></path>
									<path d="m8 3.66666667c3.2032732 0 6.6666667 2.54318984 6.6666667 4.33333333s-3.4633935 4.3333333-6.6666667 4.3333333c-3.20327316 0-6.66666667-2.54318981-6.66666667-4.3333333s3.46339351-4.33333333 6.66666667-4.33333333zm0 1.33333333c-1.38181706 0-2.74575629.50269607-3.87107038 1.3290207-.87134632.63983463-1.46226295 1.41228951-1.46226295 1.6709793s.59091663 1.03114467 1.46226295 1.6709793c1.12531409.8263246 2.48925332 1.3290207 3.87107038 1.3290207s2.7457563-.5026961 3.8710704-1.3290207c.8713463-.63983463 1.4622629-1.41228951 1.4622629-1.6709793s-.5909166-1.03114467-1.4622629-1.6709793c-1.1253141-.82632463-2.48925334-1.3290207-3.8710704-1.3290207zm0 1c1.1045695 0 2 .8954305 2 2s-.8954305 2-2 2-2-.8954305-2-2 .8954305-2 2-2zm0 1.33333333c-.36818983 0-.66666667.29847684-.66666667.66666667s.29847684.66666667.66666667.66666667.66666667-.29847684.66666667-.66666667-.29847684-.66666667-.66666667-.66666667z" fill="#fff"></path>
								</g>
							</svg></span><span id="hideIcon" class="quantum-input-icon eye-icon hide-icon"><svg viewBox="0 0 24 24" height="16" width="16" xmlns="http://www.w3.org/2000/svg">
								<g fill-rule="evenodd" fill="none">
									<path d="M14.318 14.404a3 3 0 0 0-4.223-4.223l1.436 1.436a1 1 0 0 1 1.352 1.352l1.435 1.435zm-5.27-2.442a3 3 0 0 0 3.49 3.49l-3.49-3.49z" fill="#fff"></path>
									<path d="M17.484 17.57c.546-.294 1.05-.617 1.506-.951.875-.643 1.597-1.348 2.11-2.02a5.54 5.54 0 0 0 .628-1.01c.15-.323.272-.7.272-1.089s-.122-.766-.272-1.088A5.54 5.54 0 0 0 21.1 10.4c-.514-.67-1.235-1.376-2.11-2.019C17.245 7.1 14.793 6 12 6c-1.824 0-3.503.47-4.934 1.152L8.585 8.67A9.309 9.309 0 0 1 12 8c2.27 0 4.318.9 5.807 1.994.742.545 1.321 1.12 1.704 1.621.192.251.323.468.403.64.071.153.083.231.086.244v.001c-.003.014-.015.092-.086.245a3.57 3.57 0 0 1-.403.64c-.383.5-.962 1.076-1.704 1.622a10.73 10.73 0 0 1-1.812 1.073l1.489 1.49zM6.718 9.632a9.89 9.89 0 0 0-.525.362c-.742.545-1.321 1.12-1.704 1.621a3.57 3.57 0 0 0-.403.64c-.071.153-.083.231-.086.244v.001c.003.014.015.092.086.245.08.172.21.389.403.64.383.5.962 1.076 1.704 1.622C7.683 16.1 9.73 17 12 17a8.92 8.92 0 0 0 1.882-.204l1.626 1.627c-1.08.357-2.26.577-3.508.577-2.792 0-5.245-1.1-6.99-2.381-.875-.643-1.597-1.348-2.11-2.02a5.544 5.544 0 0 1-.628-1.01c-.15-.323-.272-.7-.272-1.089s.122-.766.272-1.088A5.61 5.61 0 0 1 2.9 10.4c.513-.67 1.235-1.376 2.11-2.019.087-.064.176-.127.267-.19l1.44 1.441z" fill="#fff"></path>
									<path d="M3.543 2.793a1 1 0 0 1 1.414 0l17 17a1 1 0 0 1-1.414 1.414l-17-17a1 1 0 0 1 0-1.414z" fill="#fff"></path>
								</g>
							</svg></span>
					</button>
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
				<input type="hidden" name="_eventId" value="submit" id="_eventId">
				<input type="hidden" id="cid" name="cid" value="">

				<input type="hidden" id="showAgeUp" name="showAgeUp" value="true">

				<input type="hidden" id="thirdPartyCaptchaResponse" name="thirdPartyCaptchaResponse" value="">

				<input type="hidden" id="loginMethod" name="loginMethod" value="">

				<span class="otkcheckbox  checkbox-login-first">
					<input type="hidden" name="_rememberMe" value="on">
					<input type="checkbox" id="rememberMe" name="rememberMe" checked="checked">
					<label for="rememberMe">
						<span id="content" class="link-in-message">Remember me</span>

					</label>
				</span>
				<div class="button-top-separator"></div>
				<a role="button" class="otkbtn otkbtn-primary " href="javascript:void(0);" id="logInBtn">Sign in</a>
				<input type="hidden" id="errorCode" value="">
				<input type="hidden" id="errorCodeWithDescription" value="">
				<input type="hidden" id="storeKey" value="">
				<input type="hidden" id="bannerType" value="">
				<input type="hidden" id="bannerText" value="">
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
			if el.DataAtom == atom.Select && eName == "regionCode" {
				eValue = "US"
			} else {
				return nil, fmt.Errorf("start login flow: parse document: unexpected form %s element %s", el.DataAtom, eName)
			}
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
	if !data.Has("loginMethod") || !data.Has("email") || !data.Has("password") {
		return nil, fmt.Errorf("start login flow: parse document: missing username or password field (data=%s)", data.Encode())
	}

	data.Set("loginMethod", "emailPassword")
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
// TOS Update (step 1):
//   - POST https://signin.ea.com/p/juno/login?execution=...s1&initref=https://accounts.ea.com:443/connect/auth?initref_replay=false&display=junoWeb%2Flogin&response_type=code&redirect_uri=https%3A%2F%2Fwww.ea.com%2Flogin_check&locale=en_CA&client_id=EADOTCOM-WEB-SERVER
//     (email=...&regionCode=US&phoneNumber=&password=...&_eventId=submit&cid=...&showAgeUp=true&thirdPartyCaptchaResponse=&loginMethod=emailPassword&_rememberMe=on&rememberMe=on)
//   - 302 https://signin.ea.com/p/juno/login?execution=...s2&initref=https://accounts.ea.com:443/connect/auth?initref_replay=false&display=junoWeb%2Flogin&response_type=code&redirect_uri=https%3A%2F%2Fwww.ea.com%2Flogin_check&locale=en_CA&client_id=EADOTCOM-WEB-SERVER
//
// TOS Update (step 2):
//   - POST https://signin.ea.com/p/juno/login?execution=...s2&initref=https://accounts.ea.com:443/connect/auth?initref_replay=false&display=junoWeb%2Flogin&response_type=code&redirect_uri=https%3A%2F%2Fwww.ea.com%2Flogin_check&locale=en_CA&client_id=EADOTCOM-WEB-SERVER
//     (_readAccept=on&readAccept=on&_eventId=accept)
//     window.location = "https://accounts.ea.com:443/connect/auth?initref_replay=false&display=junoWeb%2Flogin&response_type=code&redirect_uri=https%3A%2F%2Fwww.ea.com%2Flogin_check&locale=en_CA&client_id=EADOTCOM-WEB-SERVER&fid=...";
//
// Normal:
//   - POST https://signin.ea.com/p/juno/login?execution=...s1&initref=https://accounts.ea.com:443/connect/auth?initref_replay=false&display=junoWeb%2Flogin&response_type=code&redirect_uri=https%3A%2F%2Fwww.ea.com%2Flogin_check&locale=en_CA&client_id=EADOTCOM-WEB-SERVER
//     (email=...&regionCode=US&phoneNumber=&password=...&_eventId=submit&cid=...&showAgeUp=true&thirdPartyCaptchaResponse=&loginMethod=emailPassword&_rememberMe=on&rememberMe=on)
//     window.location = "https://accounts.ea.com:443/connect/auth?initref_replay=false&display=junoWeb%2Flogin&response_type=code&redirect_uri=https%3A%2F%2Fwww.ea.com%2Flogin_check&locale=en_CA&client_id=EADOTCOM-WEB-SERVER&fid=...";
//
// Returns another request if another submission is needed to accept the TOS before proceeding.
func login2(ctx context.Context, c *http.Client, r1 *http.Request) (*http.Request, bool, error) {
	resp, err := c.Do(r1)
	if err != nil {
		return nil, false, fmt.Errorf("submit login form: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("submit login form: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "application/json" {
			var obj struct {
				Error            string       `json:"error"`
				ErrorDescription fmt.Stringer `json:"error_description"`
				Code             int          `json:"code"`
			}
			if err := json.Unmarshal(buf, &obj); err == nil && obj.Code != 0 {
				return nil, false, fmt.Errorf("submit login form: %w: error %d: %s (%q)", ErrOrigin, obj.Code, obj.Error, obj.ErrorDescription)
			}
		}
		return nil, false, fmt.Errorf("submit login form: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
	}

	m := login2re.FindSubmatch(buf)
	if m == nil {
		if doc, err := html.Parse(bytes.NewReader(buf)); err == nil {
			if form := cascadia.Query(doc, cascadia.MustCompile(`form#tosForm`)); form != nil {
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
								return nil, false, fmt.Errorf("submit login form: parse document: tos form: resolve form submit url: %w", err)
							}
						case "method":
							if a.Val != "" && strings.ToLower(a.Val) != "post" {
								return nil, false, fmt.Errorf("submit login form: parse document: tos form: unexpected form method %q", a.Val)
							}
						case "enctype":
							if a.Val != "" && strings.ToLower(a.Val) != "application/x-www-form-urlencoded" {
								return nil, false, fmt.Errorf("submit login form: parse document: tos form: unexpected form method %q", a.Val)
							}
						}
					}
				}

				/*
					<form method="post" id="tosForm">
						<div id="tos-update" class="views">
							<section id="tosReview">
								<a href=javascript:void(0); role="button" id=back class="back-btn">
									<i class="otkicon"></i>
									<span class="quantum-input-icon back-btn-icon">
										<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" class="injected-svg css-ojkidl" data-src="/static/media/ARROW_LEFT.e40e5952.svg" xmlns:xlink="http://www.w3.org/1999/xlink">
											<g id="Icons/SM/Navigation/Arrow-Left" fill="none" fill-rule="evenodd">
												<path id="Bounding-Box" d="M0 0h16v16H0z"></path>
												<path id="Primary" fill="#FFF" d="M5.293 4.707a1 1 0 0 1 1.414-1.414l4 4a1 1 0 0 1 0 1.415l-4 4a1 1 0 1 1-1.414-1.415L8.586 8 5.293 4.707z" transform="matrix(-1 0 0 1 16 0)"></path>
											</g>
										</svg> </span>
									Back
								</a>

								<!-- logo -->
								<img class="header" aria-hidden="true" src="https://eaassets-a.akamaihd.net/resource_signin_ea_com/551.0.220928.064.d05eced/p/statics/juno/img/EALogo-New.svg" />

								<h1 id="page_header" class="otktitle otktitle-2">Please review our terms</h1>

								<div class="general-error">
									<div>
										<div></div>
									</div>
								</div>


								<p class="otkc link-in-message">Thank you for choosing EA. Please take a few minutes to review our latest <a href="https://tos.ea.com/legalapp/webterms/CA/en/PC2/" target="_blank">User Agreement</a> and <a href="https://tos.ea.com/legalapp/webprivacy/CA/en/PC2/" target="_blank">Privacy and Cookie Policy</a>.</p>
								<span class="otkcheckbox ">
									<input type="hidden" name="_readAccept" value="on" />
									<input type="checkbox" id="readAccept" name="readAccept" />
									<label for=readAccept>
										<span id="content" class="link-in-message">I have read and accept the <a href="https://tos.ea.com/legalapp/webterms/CA/en/PC2/" target="_blank">User Agreement</a> and <a href="https://tos.ea.com/legalapp/webprivacy/CA/en/PC2/" target="_blank">EA's Privacy and Cookie Policy</a>.</span>

									</label>
								</span>

								<a role="button" class='otkbtn otkbtn-primary  right' href="javascript:void(0);" id="btnNext">Next</a>
								<input type="hidden" name="_eventId" value="accept" id="_eventId" />
							</section>
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
						return nil, false, fmt.Errorf("submit login form: parse document: tos form: unexpected form %s element %s", el.DataAtom, eName)
					}
					if eType == "submit" || eType == "reset" || eType == "image" || eType == "button" {
						continue // ignore buttons
					}
					if eChecked && eValue == "" {
						eValue = "on"
					}
					if (eType == "checkbox" || eType == "radio") && eValue == "" {
						continue
					}
					data.Set(eName, eValue)
				}
				if !data.Has("_readAccept") {
					return nil, false, fmt.Errorf("submit login form: parse document: tos form: missing readAccept field (data=%s)", data.Encode())
				}

				data.Set("_readAccept", "on")
				data.Set("readAccept", "on")

				req, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL.String(), strings.NewReader(data.Encode()))
				if err == nil {
					req.Header.Set("Referrer", resp.Request.URL.String())
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				return req, false, err
			}
			if n := cascadia.Query(doc, cascadia.MustCompile(`#errorCode[value]`)); n != nil {
				for _, a := range n.Attr {
					// based on juno login js
					if a.Namespace == "" && strings.EqualFold(a.Key, "value") {
						switch errCode := a.Val; errCode {
						case "10001": // try offline auth
							return nil, false, fmt.Errorf("submit login form: ea auth error %s: why the fuck does origin think we're offline", errCode)
						case "10002": // credentials
							return nil, false, fmt.Errorf("submit login form: ea auth error %s: %w", errCode, ErrInvalidLogin)
						case "10003": // general error
							return nil, false, fmt.Errorf("submit login form: ea auth error %s: login error", errCode)
						case "10004": // wtf
							return nil, false, fmt.Errorf("submit login form: ea auth error %s: idk wtf this is", errCode)
						case "":
							// no error, but this shouldn't happen
						default:
							return nil, false, fmt.Errorf("submit login form: ea auth error %s", errCode)
						}
					}
				}
			}
		}
		return nil, false, fmt.Errorf("submit login form: could not find JS redirect URL")
	}

	u, err := resp.Request.URL.Parse(string(m[1]))
	if err != nil {
		return nil, false, fmt.Errorf("submit login form: could not resolve JS redirect URL %q against %q", string(m[1]), resp.Request.URL.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err == nil {
		req.Header.Set("Referrer", resp.Request.URL.String())
	}
	return req, true, err
}

// login3 finishes the login flow.
//
//   - GET https://accounts.ea.com/connect/auth?initref_replay=false&display=junoWeb/login&response_type=code&redirect_uri=https://www.ea.com/login_check&locale=en_US&client_id=EADOTCOM-WEB-SERVER&fid=...
//   - 302 https://www.ea.com/login_check?code=...&state=...
//   - 303 https://www.ea.com/
//   - 302 https://www.ea.com/en-us/
//   - 301 https://www.ea.com/en-us
//
// Returns the token.
func login3(_ context.Context, c *http.Client, r2 *http.Request) (string, error) {
	resp, err := c.Do(r2)
	if err != nil {
		return "", fmt.Errorf("finish login flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.Request.URL.Hostname() == r2.URL.Hostname() {
			buf, _ := io.ReadAll(resp.Body)
			var obj struct {
				ErrorCode   string      `json:"error_code"`
				Error       string      `json:"error"`
				ErrorNumber json.Number `json:"error_number"`
			}
			if obj.ErrorCode == "login_required" {
				return "", fmt.Errorf("finish login flow: %w: wants us to login, but we just did that", ErrOrigin)
			}
			if err := json.Unmarshal(buf, &obj); err == nil && obj.Error != "" {
				return "", fmt.Errorf("finish login flow: %w: error %s: %s (%q)", ErrOrigin, obj.ErrorNumber, obj.ErrorCode, obj.Error)
			}
			return "", fmt.Errorf("finish login flow: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
		}
		return "", fmt.Errorf("finish login flow: request %q: response status %d (%s)", resp.Request.URL.String(), resp.StatusCode, resp.Status)
	}

	// don't waste the connection
	_, _ = io.Copy(io.Discard, resp.Body)

	last := resp.Request
	for last != nil && last.URL.Hostname() != r2.URL.Hostname() {
		code := last.URL.Query().Get("code")
		if code != "" {
			return code, nil
		}
		last = last.Response.Request
	}
	return "", fmt.Errorf("finish login flow: failed to extract token from redirect chain ending in %q", resp.Request.URL.String())
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
