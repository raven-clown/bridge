package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raven-clown/bridge/forge"
)

func TestNewHosts(t *testing.T) {
	tests := []struct{ host, rest, gql string }{
		{"", "https://api.github.com", "https://api.github.com/graphql"},
		{"github.com", "https://api.github.com", "https://api.github.com/graphql"},
		{"ghe.corp", "https://ghe.corp/api/v3", "https://ghe.corp/api/graphql"},
	}
	for _, tt := range tests {
		c, err := New(tt.host, "t", nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.restBase != tt.rest || c.graphqlURL != tt.gql {
			t.Errorf("%q: rest %q graphql %q", tt.host, c.restBase, c.graphqlURL)
		}
	}
	if _, err := New("", "", nil); err == nil {
		t.Error("empty token: want error")
	}
}

func TestWhoAmIAndRateLimitTracking(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "4321")
		h.Set("X-RateLimit-Reset", "1795000000")
		return 200, h, map[string]any{"data": map[string]any{"viewer": map[string]any{"login": "me"}}}
	}
	c := f.client()
	u, err := c.WhoAmI(context.Background())
	if err != nil || u.Login != "me" {
		t.Fatalf("got %+v err %v", u, err)
	}
	rem, reset := c.RateLimit()
	if rem != 4321 || !reset.Equal(time.Unix(1795000000, 0)) {
		t.Fatalf("rate limit %d %v", rem, reset)
	}
}

func TestRateLimited403RetryAfter(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		h := http.Header{}
		h.Set("Retry-After", "30")
		return 403, h, map[string]any{"message": "You have exceeded a secondary rate limit"}
	}
	c := f.client()
	_, err := c.WhoAmI(context.Background())
	rl, ok := errors.AsType[*forge.RateLimitedError](err)
	if !ok || !rl.RetryAt.Equal(c.now().Add(30*time.Second)) {
		t.Fatalf("got %v", err)
	}
}

func TestRateLimited403RemainingZero(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "0")
		h.Set("X-RateLimit-Reset", "1795000000")
		return 403, h, map[string]any{"message": "API rate limit exceeded"}
	}
	_, err := f.client().WhoAmI(context.Background())
	rl, ok := errors.AsType[*forge.RateLimitedError](err)
	if !ok || !rl.RetryAt.Equal(time.Unix(1795000000, 0)) {
		t.Fatalf("got %v", err)
	}
}

func TestGraphQLRateLimitedError(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		h := http.Header{}
		h.Set("X-RateLimit-Reset", "1795000000")
		return 200, h, map[string]any{"errors": []any{map[string]any{"type": "RATE_LIMITED", "message": "API rate limit exceeded"}}}
	}
	_, err := f.client().WhoAmI(context.Background())
	if _, ok := errors.AsType[*forge.RateLimitedError](err); !ok {
		t.Fatalf("got %v", err)
	}
}

func TestTokenNotInErrors(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		return 401, nil, map[string]any{"message": "Bad credentials for " + testToken}
	}
	_, err := f.client().WhoAmI(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("got %v", err)
	}
}

func TestGraphQLErrorsSurface(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		return 200, nil, map[string]any{"errors": []any{map[string]any{"type": "NOT_FOUND", "message": "Could not resolve"}}}
	}
	_, err := f.client().WhoAmI(context.Background())
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
