package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raven-clown/bridge/config"
)

const secret = "gho_SECRETSECRET"

type fakeKeyring struct {
	mu     sync.Mutex
	data   map[string]string
	gets   int
	setErr error
}

func newKeyring() *fakeKeyring { return &fakeKeyring{data: map[string]string{}} }

func (k *fakeKeyring) Get(service, user string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.gets++
	v, ok := k.data[service+"/"+user]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (k *fakeKeyring) Set(service, user, s string) error {
	if k.setErr != nil {
		return fmt.Errorf("%w: refusing to store %s", k.setErr, s)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[service+"/"+user] = s
	return nil
}

func (k *fakeKeyring) Delete(service, user string) error {
	delete(k.data, service+"/"+user)
	return nil
}

type prompt struct{ url, code string }

func (p *prompt) ShowCode(url, code string) { p.url, p.code = url, code }

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

var keyringCfg = config.AuthConfig{Source: "keyring"}

func TestTokenFromEnvWins(t *testing.T) {
	kr := newKeyring()
	kr.data["bridge/github.com"] = "from-keyring"
	got, err := Token(context.Background(), keyringCfg, "github.com", env(map[string]string{"BRIDGE_TOKEN": "a", "GITHUB_TOKEN": "b"}), kr, nil)
	if err != nil || got != "a" || kr.gets != 0 {
		t.Fatalf("got %q err %v gets %d", got, err, kr.gets)
	}
	got, _ = Token(context.Background(), keyringCfg, "github.com", env(map[string]string{"GITHUB_TOKEN": "b"}), kr, nil)
	if got != "b" {
		t.Fatalf("GITHUB_TOKEN: got %q", got)
	}
}

func TestTokenFromKeyring(t *testing.T) {
	kr := newKeyring()
	kr.data["bridge/ghe.corp"] = "k"
	got, err := Token(context.Background(), keyringCfg, "ghe.corp", env(nil), kr, nil)
	if err != nil || got != "k" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestTokenEnvSourceSkipsKeyring(t *testing.T) {
	kr := newKeyring()
	kr.data["bridge/github.com"] = "k"
	_, err := Token(context.Background(), config.AuthConfig{Source: "env"}, "github.com", env(nil), kr, nil)
	if !errors.Is(err, ErrNoToken) || kr.gets != 0 {
		t.Fatalf("err %v gets %d", err, kr.gets)
	}
}

func TestTokenNoneGivesErrNoToken(t *testing.T) {
	_, err := Token(context.Background(), keyringCfg, "github.com", env(nil), newKeyring(), nil)
	if !errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "bridge auth login") {
		t.Fatalf("got %v", err)
	}
}

type flowServer struct {
	t         *testing.T
	responses []map[string]any
	calls     int
}

func (s *flowServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("client_id") != "cid" || r.FormValue("scope") != Scopes {
			s.t.Errorf("code request form = %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dc", "user_code": "ABCD-1234", "verification_uri": "https://github.com/login/device",
			"expires_in": 900, "interval": 5,
		})
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("device_code") != "dc" || r.FormValue("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			s.t.Errorf("token request form = %v", r.Form)
		}
		resp := s.responses[s.calls]
		s.calls++
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

func newFlow(t *testing.T, responses ...map[string]any) (*DeviceFlow, *[]time.Duration, *prompt) {
	t.Helper()
	fs := &flowServer{t: t, responses: responses}
	srv := httptest.NewServer(fs.handler())
	t.Cleanup(srv.Close)
	var slept []time.Duration
	p := &prompt{}
	return &DeviceFlow{
		ClientID: "cid",
		BaseURL:  srv.URL,
		HTTP:     srv.Client(),
		Prompt:   p,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}, &slept, p
}

func TestDeviceFlowHappyPath(t *testing.T) {
	df, slept, p := newFlow(t, map[string]any{"error": "authorization_pending"}, map[string]any{"access_token": secret})
	kr := newKeyring()
	got, err := Token(context.Background(), config.AuthConfig{Source: "device_flow"}, "github.com", env(nil), kr, df)
	if err != nil || got != secret {
		t.Fatalf("got %q err %v", got, err)
	}
	if p.code != "ABCD-1234" || p.url != "https://github.com/login/device" {
		t.Fatalf("prompt = %+v", p)
	}
	if fmt.Sprint(*slept) != "[5s 5s]" {
		t.Fatalf("slept %v", *slept)
	}
	if kr.data["bridge/github.com"] != secret {
		t.Fatal("token not stored in keyring")
	}
}

func TestDeviceFlowWhenKeyringEmpty(t *testing.T) {
	df, _, _ := newFlow(t, map[string]any{"access_token": secret})
	got, err := Token(context.Background(), keyringCfg, "github.com", env(nil), newKeyring(), df)
	if err != nil || got != secret {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestDeviceFlowSlowDownAddsFive(t *testing.T) {
	df, slept, _ := newFlow(t, map[string]any{"error": "slow_down"}, map[string]any{"access_token": secret})
	if _, err := df.Run(context.Background(), strings.Fields(Scopes)); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(*slept) != "[5s 10s]" {
		t.Fatalf("slept %v", *slept)
	}
}

func TestDeviceFlowDenied(t *testing.T) {
	df, _, _ := newFlow(t, map[string]any{"error": "access_denied"})
	if _, err := df.Run(context.Background(), strings.Fields(Scopes)); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestDeviceFlowExpired(t *testing.T) {
	df, _, _ := newFlow(t, map[string]any{"error": "expired_token"})
	if _, err := df.Run(context.Background(), strings.Fields(Scopes)); !errors.Is(err, ErrExpired) {
		t.Fatalf("got %v", err)
	}
}

func TestDeviceFlowNoClientID(t *testing.T) {
	df := &DeviceFlow{}
	_, err := df.Run(context.Background(), nil)
	if !errors.Is(err, ErrNoClientID) || !strings.Contains(err.Error(), "BRIDGE_TOKEN") {
		t.Fatalf("got %v", err)
	}
}

func TestTokenNeverInError(t *testing.T) {
	df, _, _ := newFlow(t, map[string]any{"access_token": secret})
	kr := newKeyring()
	kr.setErr = errors.New("locked")
	_, err := Token(context.Background(), keyringCfg, "github.com", env(nil), kr, df)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the token: %v", err)
	}
}
