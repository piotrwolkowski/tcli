package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/piotrwolkowski/tcli/internal/auth"
)

const (
	defaultBaseURL = "https://graph.microsoft.com/v1.0"
	maxRetries     = 3
)

type Client struct {
	http    *http.Client
	baseURL string
	token   func(context.Context) (string, error)
}

func NewClient() *Client {
	return &Client{http: &http.Client{}, baseURL: defaultBaseURL, token: auth.GetToken}
}

type graphErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}

	// Buffer the body so it can be replayed on retries.
	var bodyBytes []byte
	if body != nil {
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
	}

	reqURL := c.resolve(path)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode == 429 {
			resp.Body.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("rate limited by Graph API — try again later")
			}
			wait := retryAfter(resp, attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		if resp.StatusCode >= 400 {
			defer resp.Body.Close()
			return nil, parseGraphError(resp)
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d retries", maxRetries)
}

// resolve turns a relative path into a full URL; absolute URLs (e.g.
// @odata.nextLink) are used as-is.
func (c *Client) resolve(path string) string {
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		return path
	}
	return c.baseURL + path
}

// getJSON GETs path (relative or absolute) and decodes the response into v.
func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	resp, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	return decodeJSON(resp, v)
}

// decodeJSON reads and closes resp.Body, unmarshalling it into v.
func decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}
	return nil
}

// retryAfter returns how long to wait before the next retry, using the
// Retry-After response header when present and falling back to exponential backoff.
func retryAfter(resp *http.Response, attempt int) time.Duration {
	if h := resp.Header.Get("Retry-After"); h != "" {
		if secs, err := strconv.Atoi(h); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	// 2s, 4s, 8s
	return time.Duration(2<<attempt) * time.Second
}

func parseGraphError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var ge graphErrorBody
	parsed := json.Unmarshal(body, &ge) == nil && ge.Error.Code != ""

	switch resp.StatusCode {
	case 401:
		return fmt.Errorf("unauthorized — session may have expired, run: tcli login")
	case 403:
		detail := ""
		if parsed {
			detail = fmt.Sprintf(" (%s: %s)", ge.Error.Code, ge.Error.Message)
		}
		return fmt.Errorf("permission denied%s — check the chat ID/alias and that Chat.Read and ChatMessage.Send are granted in your Azure app registration", detail)
	case 404:
		detail := ""
		if parsed {
			detail = fmt.Sprintf(" (%s: %s)", ge.Error.Code, ge.Error.Message)
		}
		return fmt.Errorf("not found%s — check the chat ID or alias", detail)
	}

	if parsed {
		return fmt.Errorf("Graph API error (%s): %s", ge.Error.Code, ge.Error.Message)
	}
	return fmt.Errorf("Graph API error %d: %s", resp.StatusCode, string(body))
}
