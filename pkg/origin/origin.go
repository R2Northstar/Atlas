// Package origin is a client for parts of the Origin API used by Northstar for
// authentication.
package origin

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

type SIDStore interface {
	GetSID(ctx context.Context) (string, error)
	SetSID(ctx context.Context, sid string) error
}

type MemorySIDStore struct {
	SID atomic.Pointer[string]
}

var _ SIDStore = (*MemorySIDStore)(nil)

func (s *MemorySIDStore) GetSID(ctx context.Context) (string, error) {
	if v := s.SID.Load(); v != nil {
		return *v, nil
	}
	return "", nil
}

func (s *MemorySIDStore) SetSID(ctx context.Context, sid string) error {
	s.SID.Store(&sid)
	return nil
}

type Client struct {
	Endpoint  string
	Username  string
	Password  string
	SIDStore  SIDStore
	Transport http.Transport
}

func (c *Client) endpoint() string {
	if c.Endpoint != "" {
		return strings.TrimRight(c.Endpoint, "/")
	}
	return "https://api1.origin.com"
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	return (&http.Client{
		Transport: &c.Transport,
		Jar:       nil,
	}).Do(req)
}

func (c *Client) Login(ctx context.Context) error {
	panic("not implemented")
}

type UserInfo struct {
	UserID    int
	PersonaID string
	EAID      string
}

func (c *Client) GetUserInfo(ctx context.Context, uid ...int) ([]UserInfo, error) {
	return c.getUserInfo(true, ctx, uid...)
}

func (c *Client) getUserInfo(retry bool, ctx context.Context, uid ...int) ([]UserInfo, error) {
	uids := make([]string, len(uid))
	for _, x := range uid {
		uids = append(uids, strconv.Itoa(x))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint()+"/atom/users?userIds="+strings.Join(uids, ","), nil)
	if err != nil {
		return nil, err
	}

	sid, err := c.SIDStore.GetSID(ctx)
	if err != nil {
		return nil, err
	}

	req.Header.Set("AuthToken", sid)

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if needAuth, err := checkResponse(resp); err != nil {
		if retry && needAuth {
			if err := c.Login(ctx); err != nil {
				return nil, err
			}
			return c.getUserInfo(false, ctx, uid...)
		}
		return nil, err
	}
	return parseUserInfo(resp.Body)
}

func checkResponse(resp *http.Response) (bool, error) {
	// TODO: return true and err for auth required
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("response status %q", resp.Status)
	}
	return false, nil
}

func parseUserInfo(r io.Reader) ([]UserInfo, error) {
	var obj struct {
		XMLName xml.Name `xml:"users"`
		User    []struct {
			UserID    string `xml:"userId"`
			PersonaID string `xml:"personaId"`
			EAID      string `xml:"EAID"`
		} `xml:"user"`
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if err := xml.Unmarshal(buf, &obj); err != nil {
		return nil, err
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
