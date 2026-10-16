package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
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

// do sends an authenticated Graph request with a JSON body.
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

	return c.send(ctx, method, c.resolve(path), bodyBytes, http.Header{
		"Authorization": {"Bearer " + token},
		"Content-Type":  {"application/json"},
	})
}

// send performs a request with exactly the given headers, retrying 429s and
// mapping errors. do adds authentication; upload-session URLs are
// pre-authenticated and must be sent without it.
func (c *Client) send(ctx context.Context, method, reqURL string, body []byte, header http.Header) (*http.Response, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		for k, v := range header {
			req.Header[k] = v
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if retryable(method, resp.StatusCode) {
			resp.Body.Close()
			wait := retryAfter(resp, attempt)
			if wait > maxRetryWait {
				// Retrying sooner than asked would only be refused again.
				return nil, fmt.Errorf("%s — retry in %s", unavailable(resp.StatusCode), wait)
			}
			if attempt == maxRetries {
				return nil, fmt.Errorf("%s — try again later", unavailable(resp.StatusCode))
			}
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

// retryable reports whether a response is worth retrying. A 429 always is:
// throttled requests are not processed. 503/504 are retried only for requests
// that are safe to repeat (GETs and upload-chunk PUTs), since a POST that
// timed out may already have posted its message.
func retryable(method string, status int) bool {
	switch status {
	case http.StatusTooManyRequests:
		return true
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return method == http.MethodGet || method == http.MethodPut
	}
	return false
}

func unavailable(status int) string {
	if status == http.StatusTooManyRequests {
		return "rate limited by Graph API"
	}
	return fmt.Sprintf("Graph API unavailable (HTTP %d)", status)
}

// maxRetryWait is the longest tcli waits before a retry; when Graph asks for
// longer, the request fails instead of stalling the CLI.
const maxRetryWait = 60 * time.Second

// maxRetryAfterSecs bounds a parsed Retry-After so converting it to a
// time.Duration cannot overflow.
const maxRetryAfterSecs = math.MaxInt32

// retryAfter returns how long to wait before the next retry, using the
// Retry-After response header when present (negative values count as 0) and
// falling back to exponential backoff.
func retryAfter(resp *http.Response, attempt int) time.Duration {
	if h := resp.Header.Get("Retry-After"); h != "" {
		// 64-bit parse so huge values are capped, not rejected, on 32-bit builds too.
		if secs, err := strconv.ParseInt(h, 10, 64); err == nil {
			return time.Duration(min(max(secs, 0), maxRetryAfterSecs)) * time.Second
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
		return fmt.Errorf("permission denied%s — check the chat ID/alias and that Chat.Read, ChatMessage.Send, Files.Read.All and Files.ReadWrite are granted in your Azure app registration", detail)
	case 404:
		detail := ""
		if parsed {
			detail = fmt.Sprintf(" (%s: %s)", ge.Error.Code, ge.Error.Message)
		}
		return fmt.Errorf("not found%s — check the chat ID or alias, and any message ID", detail)
	// Requests that are safe to repeat were already retried by send, so these
	// are requests that must not be repeated blindly, such as posting a message.
	case 503:
		return fmt.Errorf("Graph API unavailable (HTTP 503) — try again later")
	case 504:
		return fmt.Errorf("Graph API timed out (HTTP 504) — the request may still have gone through; if you were sending, check the chat before retrying")
	}

	if parsed {
		return fmt.Errorf("Graph API error (%s): %s", ge.Error.Code, ge.Error.Message)
	}
	return fmt.Errorf("Graph API error %d: %s", resp.StatusCode, string(body))
}
