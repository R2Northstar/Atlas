// Package eax queries the EA App API.
package eax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
)

type Client struct {
	// The [net/http.Client] to use for requests. If not provided,
	// [net/http.DefaultClient] is used.
	Client *http.Client

	// The UpdateMgr for requests which require version information.
	UpdateMgr *UpdateMgr
}

var ErrVersionRequired = errors.New("client version is required for this endpoint")

// PlayerID contains basic identifiers and names for a player.
type PlayerID struct {
	PD          uint64 // origin ID
	PSD         uint64 // ?
	DisplayName string // in-game name
	Nickname    string // social name?
}

// PlayerByPd gets basic information about an Origin ID. If the player does not
// exist, nil will be returned.
func (c *Client) PlayerIDByPD(ctx context.Context, pd uint64) (*PlayerID, error) {
	var obj struct {
		PlayerByPD *struct {
			PD          string `json:"pd"`
			PSD         string `json:"psd"`
			DisplayName string `json:"displayName"`
			Nickname    string `json:"nickname"`
		} `json:"playerByPd"`
	}
	if err := c.gql1(ctx, true, `query { playerByPd (pd: `+strconv.FormatUint(pd, 10)+`) { pd psd displayName nickname } }`, &obj); err != nil {
		return nil, err
	}
	if obj.PlayerByPD == nil {
		return nil, nil
	}
	res := &PlayerID{
		DisplayName: obj.PlayerByPD.DisplayName,
		Nickname:    obj.PlayerByPD.Nickname,
	}
	if s := obj.PlayerByPD.PD; s != "" {
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			res.PD = v
		} else {
			return res, fmt.Errorf("parse pd %q: %w", s, err)
		}
	}
	if s := obj.PlayerByPD.PSD; s != "" {
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			res.PD = v
		} else {
			return res, fmt.Errorf("parse psd %q: %w", s, err)
		}
	}
	return res, nil
}

// gql1 performs a basic GraphQL query.
func (c *Client) gql1(ctx context.Context, ver bool, query string, out any) error {
	req, err := c.req(ctx, ver, http.MethodGet, "https://service-aggregation-layer.juno.ea.com/graphql?query="+url.QueryEscape(query), nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "application/json" {
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("response status %d (%s) with content-type %q", resp.StatusCode, resp.Status, mt)
		}
		return fmt.Errorf("unexpected content-type %q", mt)
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var obj struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(buf, &obj); err != nil {
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("response status %d (%s) with invalid json", resp.StatusCode, resp.Status)
		}
		return fmt.Errorf("invalid json resp: %w", err)
	}
	if len(obj.Errors) != 0 {
		return fmt.Errorf("got %d errors, including %q", len(obj.Errors), obj.Errors[0].Message)
	}
	if err := json.Unmarshal([]byte(obj.Data), out); err != nil {
		return fmt.Errorf("invalid json data: %w", err)
	}
	return nil
}

func (c *Client) do(r *http.Request) (*http.Response, error) {
	if c.Client == nil {
		return http.DefaultClient.Do(r)
	}
	return c.Client.Do(r)
}

func (c *Client) req(ctx context.Context, ver bool, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err == nil {
		if ver {
			if c.UpdateMgr == nil {
				return nil, ErrVersionRequired
			}
			ver, _, err := c.UpdateMgr.Update(false)
			if err != nil {
				return nil, fmt.Errorf("%w: failed to update version: %v", ErrVersionRequired, err)
			}
			if ver == "" {
				return nil, ErrVersionRequired
			}
			req.Header.Set("User-Agent", "EADesktop/"+ver)
		}
		req.Header.Set("x-client-id", "EAX-JUNO-CLIENT")
	}
	return req, err
}
