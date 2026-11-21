// Package github implements forge.Forge for github.com and GitHub
// Enterprise Server, with GraphQL for reads and REST for most writes.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raven-clown/bridge/forge"
)

type Client struct {
	host       string
	restBase   string
	graphqlURL string
	token      string
	http       *http.Client
	now        func() time.Time

	mu        sync.Mutex
	remaining int
	reset     time.Time
	me        string
}

type Option func(*Client)

// BaseURLs points the client at other endpoints, for tests.
func BaseURLs(rest, graphql string) Option {
	return func(c *Client) { c.restBase, c.graphqlURL = rest, graphql }
}

func New(host, token string, hc *http.Client, opts ...Option) (*Client, error) {
	if token == "" {
		return nil, errors.New("github: empty token")
	}
	if host == "" {
		host = forge.DefaultHost
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{host: host, token: token, http: hc, now: time.Now, remaining: -1}
	if host == forge.DefaultHost {
		c.restBase, c.graphqlURL = "https://api.github.com", "https://api.github.com/graphql"
	} else {
		c.restBase, c.graphqlURL = "https://"+host+"/api/v3", "https://"+host+"/api/graphql"
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func (c *Client) Host() string { return c.host }

// RateLimit returns the last seen remaining budget (-1 before any call).
func (c *Client) RateLimit() (int, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remaining, c.reset
}

func (c *Client) redact(s string) string { return strings.ReplaceAll(s, c.token, "***") }

func (c *Client) track(h http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, err := strconv.Atoi(h.Get("X-RateLimit-Remaining")); err == nil {
		c.remaining = v
	}
	if v, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		c.reset = time.Unix(v, 0)
	}
}

func (c *Client) resetAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reset.IsZero() {
		return c.now().Add(time.Minute)
	}
	return c.reset
}

func (c *Client) do(ctx context.Context, method, url string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "bridge")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New(c.redact(err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()
	c.track(resp.Header)
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, nil
	}
	return nil, c.statusError(resp, data)
}

func (c *Client) statusError(resp *http.Response, data []byte) error {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &body)
	msg := c.redact(body.Message)

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			return &forge.RateLimitedError{RetryAt: c.now().Add(time.Duration(s) * time.Second)}
		}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(msg), "rate limit") {
			return &forge.RateLimitedError{RetryAt: c.resetAt()}
		}
	}
	err := fmt.Errorf("github: %s", resp.Status)
	if msg != "" {
		err = fmt.Errorf("github: %s: %s", resp.Status, msg)
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return errors.Join(forge.ErrNotFound, err)
	case http.StatusForbidden:
		return errors.Join(forge.ErrForbidden, err)
	case http.StatusUnprocessableEntity:
		if strings.Contains(strings.ToLower(msg), "lock") {
			return errors.Join(forge.ErrForbidden, err)
		}
	}
	return err
}

func (c *Client) rest(ctx context.Context, method, path string, body, out any) error {
	data, err := c.do(ctx, method, c.restBase+path, body)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func repoPath(r forge.RepoID) string { return "/repos/" + r.Owner + "/" + r.Name }

func issuePath(id forge.ItemID) string {
	return fmt.Sprintf("%s/issues/%d", repoPath(id.Repo), id.Number)
}

type gqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

// GraphQLErrors carries errors from a response that may also hold data.
type GraphQLErrors []gqlError

func (e GraphQLErrors) Error() string {
	var msgs []string
	for _, g := range e {
		msgs = append(msgs, g.Message)
	}
	return "github: " + strings.Join(msgs, "; ")
}

func (e GraphQLErrors) Is(target error) bool {
	for _, g := range e {
		switch {
		case g.Type == "NOT_FOUND" && target == forge.ErrNotFound,
			(g.Type == "FORBIDDEN" || g.Type == "INSUFFICIENT_SCOPES") && target == forge.ErrForbidden:
			return true
		}
	}
	return false
}

// graphql decodes data into out even when the response has errors, and
// returns those errors as GraphQLErrors so callers can keep partial data.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	data, err := c.do(ctx, http.MethodPost, c.graphqlURL, map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors GraphQLErrors   `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("github: bad graphql response: %w", err)
	}
	for _, e := range resp.Errors {
		if e.Type == "RATE_LIMITED" {
			return &forge.RateLimitedError{RetryAt: c.resetAt()}
		}
	}
	if len(resp.Data) > 0 && string(resp.Data) != "null" && out != nil {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("github: decode graphql data: %w", err)
		}
	}
	for i := range resp.Errors {
		resp.Errors[i].Message = c.redact(resp.Errors[i].Message)
	}
	if len(resp.Errors) > 0 {
		return resp.Errors
	}
	return nil
}

func (c *Client) WhoAmI(ctx context.Context) (forge.User, error) {
	c.mu.Lock()
	me := c.me
	c.mu.Unlock()
	if me != "" {
		return forge.User{Login: me}, nil
	}
	var out struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := c.graphql(ctx, `query { viewer { login } }`, nil, &out); err != nil {
		return forge.User{}, err
	}
	c.mu.Lock()
	c.me = out.Viewer.Login
	c.mu.Unlock()
	return forge.User{Login: out.Viewer.Login}, nil
}
