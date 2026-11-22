package github

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

const testToken = "ghp_TESTTOKENVALUE"

type gqlReq struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type fakeGH struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	gql  []gqlReq
	rest []string

	// GraphQL answers the request; it returns the "data" object, or a full
	// response when raw is true.
	GraphQL func(r gqlReq) (status int, header http.Header, body any)
	REST    http.Handler
}

func newFakeGH(t *testing.T) *fakeGH {
	t.Helper()
	f := &fakeGH{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("authorization header = %q", got)
		}
		var req gqlReq
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("bad graphql body: %v", err)
		}
		f.mu.Lock()
		f.gql = append(f.gql, req)
		f.mu.Unlock()
		status, header, body := f.GraphQL(req)
		for k, v := range header {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.rest = append(f.rest, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		if f.REST == nil {
			http.NotFound(w, r)
			return
		}
		f.REST.ServeHTTP(w, r)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGH) client() *Client {
	f.t.Helper()
	c, err := New("", testToken, f.srv.Client(), BaseURLs(f.srv.URL, f.srv.URL+"/graphql"))
	if err != nil {
		f.t.Fatal(err)
	}
	c.now = func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) }
	return c
}

func (f *fakeGH) requests() []gqlReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gqlReq(nil), f.gql...)
}

func ok(data any) (int, http.Header, any) { return 200, nil, map[string]any{"data": data} }

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func issueNode(n int) map[string]any {
	return map[string]any{
		"__typename": "Issue", "number": n, "title": "t", "url": "u", "updatedAt": "2026-11-21T00:00:00Z",
		"author": map[string]any{"login": "x"}, "repository": map[string]any{"name": "a", "owner": map[string]any{"login": "o"}},
		"labels": map[string]any{"nodes": []any{}}, "assignees": map[string]any{"nodes": []any{}}, "comments": map[string]any{"nodes": []any{}},
	}
}

func searchPage(nodes []any, total int, next bool, cursor string) map[string]any {
	return map[string]any{"search": map[string]any{
		"issueCount": total,
		"pageInfo":   map[string]any{"hasNextPage": next, "endCursor": cursor},
		"nodes":      nodes,
	}}
}
