// Package origin is a client for parts of the Origin API used by Northstar for
// authentication.
package origin

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

var (
	ErrInvalidResponse = errors.New("invalid origin api response")
	ErrOrigin          = errors.New("origin api error")
	ErrAuthRequired    = errors.New("origin authentication required")
)

// Base is the base path for the Origin API.
var Base = "https://api1.origin.com"

// UserInfo contains information about an Origin account.
type UserInfo struct {
	UserID    int
	PersonaID string
	EAID      string
}

// GetUserInfo gets information about Origin accounts by their Origin UserID.
//
// If errors.Is(err, ErrAuthRequired), you need a new NucleusToken.
func GetUserInfo(ctx context.Context, token NucleusToken, uid ...int) ([]UserInfo, error) {
	uids := make([]string, len(uid))
	for _, x := range uid {
		uids = append(uids, strconv.Itoa(x))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Base+"/atom/users?userIds="+strings.Join(uids, ","), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("AuthToken", string(token))
	req.Header.Set("X-Origin-Platform", "UnknownOS")
	req.Header.Set("Referrer", "https://www.origin.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf, root, err := checkResponseXML(resp)
	if err != nil {
		return nil, err
	}
	return parseUserInfo(buf, root)
}

func checkResponseXML(resp *http.Response) ([]byte, xml.Name, error) {
	var root xml.Name
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return buf, root, err
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "application/xml" && mt != "text/xml" {
		if resp.StatusCode != http.StatusOK {
			return buf, root, fmt.Errorf("%w: response status %d (%s)", ErrOrigin, resp.StatusCode, resp.Status)
		}
		return buf, root, fmt.Errorf("%w: expected xml, got %q", ErrOrigin, mt)
	}
	if err := xml.Unmarshal(buf, &root); err != nil {
		return buf, root, fmt.Errorf("%w: invalid xml: %v", ErrInvalidResponse, err)
	}
	if root.Local == "error" {
		var obj struct {
			Code    int `xml:"code,attr"`
			Failure []struct {
				Field string `xml:"field,attr"`
				Cause string `xml:"cause,attr"`
				Value string `xml:"value,attr"`
			} `xml:"failure"`
		}
		if err := xml.Unmarshal(buf, &obj); err != nil {
			return buf, root, fmt.Errorf("%w: response %#q (unmarshal: %v)", ErrOrigin, string(buf), err)
		}
		for _, f := range obj.Failure {
			if f.Cause == "invalid_token" {
				return buf, root, fmt.Errorf("%w: invalid token", ErrAuthRequired)
			}
		}
		if len(obj.Failure) == 1 {
			return buf, root, fmt.Errorf("%w: error %d: %s (%s) %q", ErrOrigin, obj.Code, obj.Failure[0].Cause, obj.Failure[0].Field, obj.Failure[0].Value)
		}
		return buf, root, fmt.Errorf("%w: error %d: response %#q", ErrOrigin, obj.Code, string(buf))
	}
	return buf, root, nil
}

func parseUserInfo(buf []byte, root xml.Name) ([]UserInfo, error) {
	var obj struct {
		User []struct {
			UserID    string `xml:"userId"`
			PersonaID string `xml:"personaId"`
			EAID      string `xml:"EAID"`
		} `xml:"user"`
	}
	if root.Local != "users" {
		return nil, fmt.Errorf("%w: unexpected %s response", ErrInvalidResponse, root.Local)
	}
	if err := xml.Unmarshal(buf, &obj); err != nil {
		return nil, fmt.Errorf("%w: invalid xml: %v", ErrInvalidResponse, err)
	}
	res := make([]UserInfo, len(obj.User))
	for i, x := range obj.User {
		var v UserInfo
		if uid, err := strconv.Atoi(x.UserID); err == nil {
			v.UserID = uid
		} else {
			return nil, fmt.Errorf("parse userId %q: %w", x.UserID, err)
		}
		v.PersonaID = x.PersonaID
		v.EAID = x.EAID
		res[i] = v
	}
	return res, nil
}
