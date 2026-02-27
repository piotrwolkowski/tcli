package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/piotrwolkowski/tcli/config"
)

// Scopes requested. offline_access is required to receive a refresh token.
const graphScopes = "https://graph.microsoft.com/Chat.Read https://graph.microsoft.com/ChatMessage.Send offline_access"

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func deviceCodeEndpoint(tenantID string) string {
	return "https://login.microsoftonline.com/" + tenantID + "/oauth2/v2.0/devicecode"
}

func tokenEndpoint(tenantID string) string {
	return "https://login.microsoftonline.com/" + tenantID + "/oauth2/v2.0/token"
}

// Login performs the OAuth2 device code flow and caches the resulting tokens.
func Login(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Step 1: request a device code.
	resp, err := http.PostForm(deviceCodeEndpoint(cfg.TenantID), url.Values{
		"client_id": {cfg.ClientID},
		"scope":     {graphScopes},
	})
	if err != nil {
		return fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var dcResp deviceCodeResponse
	if err := json.Unmarshal(body, &dcResp); err != nil {
		return fmt.Errorf("parsing device code response: %w", err)
	}
	if dcResp.Error != "" {
		return fmt.Errorf("device code request failed: %s", dcResp.ErrorDesc)
	}
	if dcResp.DeviceCode == "" {
		return fmt.Errorf("unexpected device code response: %s", string(body))
	}

	fmt.Println(dcResp.Message)

	// Step 2: poll until the user completes auth or the code expires.
	interval := dcResp.Interval
	if interval == 0 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(dcResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		tok, err := postToken(cfg.TenantID, url.Values{
			"client_id":   {cfg.ClientID},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dcResp.DeviceCode},
		})
		if err != nil {
			continue
		}
		switch tok.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
			continue
		case "":
			// success
		default:
			return fmt.Errorf("authentication failed: %s", tok.ErrorDesc)
		}

		cache := &TokenCache{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		}
		if err := SaveCache(cache); err != nil {
			return fmt.Errorf("saving token: %w", err)
		}
		fmt.Println("Login successful.")
		return nil
	}

	return fmt.Errorf("login timed out — device code expired, run: tcli login")
}

// GetToken returns a valid access token, using the cached refresh token if the
// access token has expired. Returns an error directing the user to re-login if
// the refresh token is missing or rejected.
func GetToken(ctx context.Context) (string, error) {
	cache, err := LoadCache()
	if err != nil {
		return "", err
	}
	if cache == nil {
		return "", fmt.Errorf("not logged in — run: tcli login")
	}

	if !cache.IsExpired() {
		return cache.AccessToken, nil
	}

	if cache.RefreshToken == "" {
		return "", fmt.Errorf("session expired — run: tcli login")
	}

	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	tok, err := postToken(cfg.TenantID, url.Values{
		"client_id":     {cfg.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {cache.RefreshToken},
		"scope":         {graphScopes},
	})
	if err != nil || tok.Error != "" {
		return "", fmt.Errorf("session expired — run: tcli login")
	}

	cache.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		cache.RefreshToken = tok.RefreshToken // servers may rotate refresh tokens
	}
	cache.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	_ = SaveCache(cache)

	return cache.AccessToken, nil
}

func postToken(tenantID string, values url.Values) (*tokenResponse, error) {
	resp, err := http.PostForm(tokenEndpoint(tenantID), values)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}
	return &tok, nil
}
