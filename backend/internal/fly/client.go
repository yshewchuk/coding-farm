// Package fly implements a thin client for the Fly.io Machines REST API
// (https://api.machines.dev) and the Fly Apps REST API used to build workspace
// images. The client is split behind interfaces so the orchestration layer can
// be tested with a mock instead of real Fly infrastructure.
package fly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Doer is the HTTP capability the client depends on. *http.Client satisfies it,
// and tests can inject a recording round-tripper.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client talks to the Fly Machines and Apps REST APIs using a single API token.
// All methods are safe for concurrent use; one Client is meant to be shared.
type Client struct {
	machinesBase string // e.g. https://api.machines.dev
	appsBase     string // e.g. https://api.fly.io
	token        string
	org          string
	doer         Doer
}

// NewClient returns a configured Fly Client.
func NewClient(machinesBase, appsBase, token, org string, doer Doer) *Client {
	if doer == nil {
		doer = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		machinesBase: machinesBase,
		appsBase:     appsBase,
		token:        token,
		org:          org,
		doer:         doer,
	}
}

// do performs a JSON request and decodes the JSON response into out (when non-nil
// and the status is 2xx). Non-2xx responses are returned as an *APIError.
func (c *Client) do(ctx context.Context, method, url string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cloudsandbox-management-api/1.0")

	resp, err := c.doer.Do(req)
	if err != nil {
		return fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return apiError(resp.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// apiError extracts Fly's {"error": "..."} payload when present.
func apiError(status int, body []byte) error {
	var p struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &p); err == nil && p.Error != "" {
		return &APIError{Status: status, Message: p.Error}
	}
	return &APIError{Status: status, Message: fmt.Sprintf("fly api returned %d", status)}
}

// APIError is returned for non-2xx Fly API responses.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("fly api error (status %d): %s", e.Status, e.Message)
}

// IsNotFound reports whether the error is a 404 from the Fly API.
func IsNotFound(err error) bool {
	if ae, ok := err.(*APIError); ok {
		return ae.Status == http.StatusNotFound
	}
	return false
}
