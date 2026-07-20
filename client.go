package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	productionBase = "https://www.hushify.io"
	defaultTimeout = 30 * time.Second
)

// Overridable in tests only — not exposed as user flags.
var (
	apiBaseURL   = productionBase
	frontBaseURL = productionBase
)

// WrapRequest mirrors the web UI / OpenAPI WrapRequest body.
type WrapRequest struct {
	Secret              string   `json:"secret"`
	WrapTTL             string   `json:"wrapTtl,omitempty"`
	Namespace           string   `json:"namespace,omitempty"`
	NumLinks            int      `json:"numLinks,omitempty"`
	NotificationEmail   string   `json:"notificationEmail,omitempty"`
	RestrictToCurrentIP bool     `json:"restrictToCurrentIP,omitempty"`
	AllowedIPs          []string `json:"allowedIPs,omitempty"`
}

// WrapInfo is a single wrapped token returned by the API.
type WrapInfo struct {
	Token      string `json:"token"`
	TTL        int    `json:"ttl"`
	LinkNumber int    `json:"link_number"`
}

// WrapResponse covers both single- and multi-link API responses.
type WrapResponse struct {
	WrapInfo  *WrapInfo  `json:"wrap_info"`
	WrapInfos []WrapInfo `json:"wrap_infos"`
	NumLinks  int        `json:"num_links"`
	Error     string     `json:"error"`
}

// Client talks to the production Hushify HTTP API.
type Client struct {
	APIBase   string
	FrontBase string
	HTTP      *http.Client
}

func NewClient() *Client {
	return &Client{
		APIBase:   strings.TrimRight(apiBaseURL, "/"),
		FrontBase: strings.TrimRight(frontBaseURL, "/"),
		HTTP:      &http.Client{Timeout: defaultTimeout},
	}
}

func (c *Client) Wrap(req WrapRequest) (*WrapResponse, error) {
	endpoint, err := url.JoinPath(c.APIBase, "/api/wrap")
	if err != nil {
		return nil, fmt.Errorf("build wrap URL: %w", err)
	}

	raw, status, err := c.postJSON(endpoint, req)
	if err != nil {
		return nil, err
	}

	var parsed WrapResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %w\nbody: %s", status, err, truncate(string(raw), 200))
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("API error (HTTP %d): %s", status, apiErrorMessage(parsed.Error, raw, status))
	}

	if parsed.WrapInfo == nil && len(parsed.WrapInfos) == 0 {
		return nil, fmt.Errorf("API returned no wrap tokens")
	}

	return &parsed, nil
}

// Unwrap fetches the secret for a wrap token (one-time).
func (c *Client) Unwrap(token string) (string, error) {
	endpoint, err := url.JoinPath(c.APIBase, "/api/unwrap")
	if err != nil {
		return "", fmt.Errorf("build unwrap URL: %w", err)
	}

	raw, status, err := c.postJSON(endpoint, map[string]string{"token": token})
	if err != nil {
		return "", err
	}

	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &errBody)

	if status < 200 || status >= 300 {
		return "", fmt.Errorf("API error (HTTP %d): %s", status, apiErrorMessage(errBody.Error, raw, status))
	}

	secret, err := extractSecret(raw)
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (c *Client) postJSON(endpoint string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "hushify-cli/1.0")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	return raw, resp.StatusCode, nil
}

// ShareURL builds the same ?token= link the web UI uses.
func (c *Client) ShareURL(token string) string {
	return fmt.Sprintf("%s?token=%s", c.FrontBase, url.QueryEscape(token))
}

func (r *WrapResponse) Infos() []WrapInfo {
	if len(r.WrapInfos) > 0 {
		return r.WrapInfos
	}
	if r.WrapInfo != nil {
		return []WrapInfo{*r.WrapInfo}
	}
	return nil
}

// extractSecret pulls the plaintext from typical Hushify unwrap JSON shapes.
func extractSecret(raw []byte) (string, error) {
	var top struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return "", fmt.Errorf("decode unwrap response: %w", err)
	}
	if len(top.Data) == 0 || string(top.Data) == "null" {
		return "", fmt.Errorf("unwrap response missing data")
	}

	// {"data":{"secret":"..."}}
	var nested struct {
		Secret json.RawMessage `json:"secret"`
	}
	if err := json.Unmarshal(top.Data, &nested); err == nil && len(nested.Secret) > 0 && string(nested.Secret) != "null" {
		return stringifyJSONValue(nested.Secret)
	}

	// {"data":"plain string"} or other JSON values
	return stringifyJSONValue(top.Data)
}

func stringifyJSONValue(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("decode secret value: %w", err)
	}
	switch t := v.(type) {
	case string:
		return t, nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func apiErrorMessage(parsed string, raw []byte, status int) string {
	if parsed != "" {
		return parsed
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return http.StatusText(status)
	}
	return truncate(msg, 200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
