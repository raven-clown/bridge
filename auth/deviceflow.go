package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientID is the public OAuth app for bridge. Empty until the app exists.
const ClientID = ""

var (
	ErrNoClientID   = errors.New("device flow login is not available in this build: set BRIDGE_TOKEN to a personal access token")
	ErrAccessDenied = errors.New("sign-in was denied")
	ErrExpired      = errors.New("the sign-in code expired, run the login again")
)

type Prompter interface {
	ShowCode(verificationURL, userCode string)
}

type DeviceFlow struct {
	ClientID string
	Host     string
	// BaseURL overrides https://<Host>, for tests.
	BaseURL string
	HTTP    *http.Client
	Prompt  Prompter
	Sleep   func(context.Context, time.Duration) error
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (d *DeviceFlow) post(ctx context.Context, path string, form url.Values, out any) error {
	base := d.BaseURL
	if base == "" {
		host := d.Host
		if host == "" {
			host = "github.com"
		}
		base = "https://" + host
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *DeviceFlow) Run(ctx context.Context, scopes []string) (string, error) {
	if d.ClientID == "" {
		return "", ErrNoClientID
	}
	sleep := d.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}

	var code struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := d.post(ctx, "/login/device/code", url.Values{
		"client_id": {d.ClientID},
		"scope":     {strings.Join(scopes, " ")},
	}, &code); err != nil {
		return "", fmt.Errorf("start sign-in: %w", err)
	}
	if d.Prompt != nil {
		d.Prompt.ShowCode(code.VerificationURI, code.UserCode)
	}

	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if err := sleep(ctx, interval); err != nil {
			return "", err
		}
		var tok struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if err := d.post(ctx, "/login/oauth/access_token", url.Values{
			"client_id":   {d.ClientID},
			"device_code": {code.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &tok); err != nil {
			return "", fmt.Errorf("finish sign-in: %w", err)
		}
		switch tok.Error {
		case "":
			if tok.AccessToken == "" {
				return "", errors.New("finish sign-in: empty token")
			}
			return tok.AccessToken, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return "", ErrExpired
		case "access_denied":
			return "", ErrAccessDenied
		default:
			return "", fmt.Errorf("finish sign-in: %s", tok.Error)
		}
	}
}
