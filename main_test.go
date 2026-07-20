package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateTTL(t *testing.T) {
	t.Parallel()
	ok := []string{"1s", "300s", "5m", "1h", "6h", "1d", "7d", "32d", "1H"}
	for _, ttl := range ok {
		if err := validateTTL(ttl); err != nil {
			t.Errorf("validateTTL(%q) unexpected error: %v", ttl, err)
		}
	}
	bad := []string{"", "1", "h1", "0h", "33d", "1w", "abc"}
	for _, ttl := range bad {
		if err := validateTTL(ttl); err == nil {
			t.Errorf("validateTTL(%q) expected error", ttl)
		}
	}
}

func TestParseIPList(t *testing.T) {
	t.Parallel()
	got := parseIPList(" 1.1.1.1, 2.2.2.2 , ")
	if len(got) != 2 || got[0] != "1.1.1.1" || got[1] != "2.2.2.2" {
		t.Fatalf("unexpected: %#v", got)
	}
	if parseIPList("  ") != nil {
		t.Fatal("expected nil for blank")
	}
}

func TestParseTokenInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"abc123", "abc123"},
		{"s.abc123", "abc123"},
		{"https://www.hushify.io?token=tok%2B1", "tok+1"},
		{"https://www.hushify.io/?token=xyz", "xyz"},
		{"www.hushify.io?token=xyz", "xyz"},
		{"?token=only", "only"},
	}
	for _, tc := range cases {
		got, err := parseTokenInput(tc.in)
		if err != nil {
			t.Errorf("parseTokenInput(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseTokenInput(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	if _, err := parseTokenInput("https://www.hushify.io/"); err == nil {
		t.Fatal("expected error for URL without token")
	}
}

func TestShareURL(t *testing.T) {
	prevAPI, prevFront := apiBaseURL, frontBaseURL
	t.Cleanup(func() {
		apiBaseURL, frontBaseURL = prevAPI, prevFront
	})
	apiBaseURL = "https://api.example"
	frontBaseURL = "https://www.hushify.io"

	c := NewClient()
	got := c.ShareURL("abc+def")
	want := "https://www.hushify.io?token=abc%2Bdef"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExtractSecret(t *testing.T) {
	t.Parallel()
	got, err := extractSecret([]byte(`{"data":{"secret":"hello"}}`))
	if err != nil || got != "hello" {
		t.Fatalf("nested secret: got %q err=%v", got, err)
	}
	got, err = extractSecret([]byte(`{"data":"plain"}`))
	if err != nil || got != "plain" {
		t.Fatalf("string data: got %q err=%v", got, err)
	}
}

func TestClientWrapAndUnwrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/wrap":
			var req WrapRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Secret != "hello" || req.WrapTTL != "1h" || req.NumLinks != 1 {
				t.Fatalf("unexpected request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(WrapResponse{
				WrapInfo: &WrapInfo{Token: "tok123", TTL: 3600, LinkNumber: 1},
			})
		case "/api/unwrap":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req["token"] != "tok123" {
				t.Fatalf("unexpected token %q", req["token"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]string{"secret": "hello"},
			})
		default:
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prevAPI, prevFront := apiBaseURL, frontBaseURL
	t.Cleanup(func() {
		apiBaseURL, frontBaseURL = prevAPI, prevFront
	})
	apiBaseURL = srv.URL
	frontBaseURL = "https://www.hushify.io"

	client := NewClient()
	resp, err := client.Wrap(WrapRequest{Secret: "hello", WrapTTL: "1h", Namespace: "public", NumLinks: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Infos()[0].Token != "tok123" {
		t.Fatalf("unexpected wrap: %+v", resp)
	}

	secret, err := client.Unwrap("tok123")
	if err != nil || secret != "hello" {
		t.Fatalf("unwrap: secret=%q err=%v", secret, err)
	}
}

func TestRunWrapQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(WrapResponse{
			WrapInfos: []WrapInfo{
				{Token: "a", LinkNumber: 1},
				{Token: "b", LinkNumber: 2},
			},
			NumLinks: 2,
		})
	}))
	defer srv.Close()

	prevAPI, prevFront := apiBaseURL, frontBaseURL
	prevCopy := copyToClipboard
	var copied string
	t.Cleanup(func() {
		apiBaseURL, frontBaseURL = prevAPI, prevFront
		copyToClipboard = prevCopy
	})
	apiBaseURL = srv.URL
	frontBaseURL = "https://www.hushify.io"
	copyToClipboard = func(text string) error {
		copied = text
		return nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run([]string{"wrap", "-s", "secret", "-n", "2", "-q"}, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %q", stdout.String())
	}
	if !strings.Contains(lines[0], "token=a") || !strings.Contains(lines[1], "token=b") {
		t.Fatalf("unexpected urls: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Copied to clipboard") {
		t.Fatalf("expected clipboard confirmation in stderr, got %q", stderr.String())
	}
	wantClip := lines[0] + "\n" + lines[1]
	if copied != wantClip {
		t.Fatalf("clipboard got %q want %q", copied, wantClip)
	}
}

func TestRunWrapJSONSkipsClipboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(WrapResponse{
			WrapInfo: &WrapInfo{Token: "tok", TTL: 3600, LinkNumber: 1},
		})
	}))
	defer srv.Close()

	prevAPI, prevFront := apiBaseURL, frontBaseURL
	prevCopy := copyToClipboard
	called := false
	t.Cleanup(func() {
		apiBaseURL, frontBaseURL = prevAPI, prevFront
		copyToClipboard = prevCopy
	})
	apiBaseURL = srv.URL
	frontBaseURL = "https://www.hushify.io"
	copyToClipboard = func(text string) error {
		called = true
		return nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run([]string{"wrap", "-s", "secret", "-json"}, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	if called {
		t.Fatal("clipboard should be skipped for -json")
	}
	if strings.Contains(stderr.String(), "Copied to clipboard") {
		t.Fatalf("unexpected clipboard message: %q", stderr.String())
	}
}

func TestRunUnwrapURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/unwrap" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{"secret": "revealed"},
		})
	}))
	defer srv.Close()

	prevAPI, prevFront := apiBaseURL, frontBaseURL
	t.Cleanup(func() {
		apiBaseURL, frontBaseURL = prevAPI, prevFront
	})
	apiBaseURL = srv.URL

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := run([]string{"unwrap", "https://www.hushify.io?token=abc"}, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "revealed" {
		t.Fatalf("got %q", stdout.String())
	}
}
