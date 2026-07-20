// Command hushify wraps and unwraps secrets via https://www.hushify.io.
// Options mirror the web UI.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultTTL = "1h"
	maxLinks   = 10
	maxTTLDays = 32
	version    = "1.0.0"
)

var ttlPattern = regexp.MustCompile(`^(\d+)([smhd])$`)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printRootUsage(stderr)
		return 2
	}

	switch args[0] {
	case "wrap":
		return runWrap(args[1:], stdin, stdout, stderr)
	case "unwrap":
		return runUnwrap(args[1:], stdin, stdout, stderr)
	case "help", "-h", "--help":
		printRootUsage(stderr)
		return 0
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "hushify", version)
		return 0
	default:
		// Treat unknown first arg as legacy "wrap" only if it looks like a flag.
		if strings.HasPrefix(args[0], "-") {
			fmt.Fprintf(stderr, "error: specify a command: wrap or unwrap\n\n")
			printRootUsage(stderr)
			return 2
		}
		fmt.Fprintf(stderr, "error: unknown command %q\n\n", args[0])
		printRootUsage(stderr)
		return 2
	}
}

func printRootUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, `hushify — one-time encrypted secret sharing (https://www.hushify.io)

Usage:
  hushify wrap [flags]              Create share URL(s)
  hushify unwrap <url-or-token>     Reveal a secret once
  hushify version

Examples:
  printf 'db-password' | hushify wrap
  hushify wrap -f ./key.pem -t 7d -n 2 -e ops@example.com
  hushify unwrap 'https://www.hushify.io?token=...'
  hushify unwrap k3SkjmTKTXKrKRvmmWe1pNMg

Run "hushify wrap -h" or "hushify unwrap -h" for command flags.
`)
}

func runWrap(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hushify wrap", flag.ContinueOnError)
	fs.SetOutput(stderr)

	secretFlag := fs.String("secret", "", "Secret text to wrap (prefer stdin or -file for sensitive values)")
	fs.StringVar(secretFlag, "s", "", "Shorthand for -secret")
	fileFlag := fs.String("file", "", "Read secret from file (use - for stdin)")
	fs.StringVar(fileFlag, "f", "", "Shorthand for -file")
	ttl := fs.String("ttl", defaultTTL, "Expiry TTL: 1h, 6h, 1d, 7d, or custom like 30m / 5m / 2d (max 32d)")
	fs.StringVar(ttl, "t", defaultTTL, "Shorthand for -ttl")
	links := fs.Int("links", 1, "Number of independent one-time links (1–10)")
	fs.IntVar(links, "n", 1, "Shorthand for -links")
	email := fs.String("email", "", "Send access/expiry notification to this address")
	fs.StringVar(email, "e", "", "Shorthand for -email")
	restrictIP := fs.Bool("restrict-ip", false, "Restrict unwrap to the creating client IP")
	allowIP := fs.String("allow-ip", "", "Comma-separated IPs allowed to unwrap")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON instead of plain URLs")
	quiet := fs.Bool("quiet", false, "Print only share URL(s), one per line")
	fs.BoolVar(quiet, "q", false, "Shorthand for -quiet")

	fs.Usage = func() {
		fmt.Fprintf(stderr, `hushify wrap — create one-time share URLs (same options as the web UI)

Usage:
  hushify wrap [flags]
  echo "my secret" | hushify wrap
  hushify wrap -f secret.txt
  hushify wrap -s "password" -t 1d -n 3 -e you@example.com

Secret input (first match wins):
  -secret / -s     literal text
  -file / -f       path, or "-" for stdin
  stdin            if not a TTY and no -secret/-file

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Share URLs are copied to the clipboard on macOS and Linux (skipped with -json).

Examples:
  printf 'db-password' | hushify wrap
  hushify wrap -f ./key.pem -t 7d -n 2 -e ops@example.com
  hushify wrap -s "token" --restrict-ip
  hushify wrap -s "token" --allow-ip 203.0.113.10,198.51.100.2
`)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	secret, err := readSecret(*secretFlag, *fileFlag, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if strings.TrimSpace(secret) == "" {
		fmt.Fprintln(stderr, "error: secret is empty")
		fs.Usage()
		return 2
	}

	if err := validateTTL(*ttl); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if *links < 1 || *links > maxLinks {
		fmt.Fprintf(stderr, "error: -links must be between 1 and %d\n", maxLinks)
		return 2
	}
	if *email != "" && !looksLikeEmail(*email) {
		fmt.Fprintf(stderr, "error: invalid -email %q\n", *email)
		return 2
	}
	if *restrictIP && strings.TrimSpace(*allowIP) != "" {
		fmt.Fprintln(stderr, "error: use either -restrict-ip or -allow-ip, not both")
		return 2
	}

	req := WrapRequest{
		Secret:    secret,
		WrapTTL:   *ttl,
		Namespace: "public",
		NumLinks:  *links,
	}
	if *email != "" {
		req.NotificationEmail = *email
	}
	if ips := parseIPList(*allowIP); len(ips) > 0 {
		req.AllowedIPs = ips
	} else if *restrictIP {
		req.RestrictToCurrentIP = true
	}

	client := NewClient()
	resp, err := client.Wrap(req)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	infos := resp.Infos()
	urls := make([]string, 0, len(infos))
	for _, info := range infos {
		urls = append(urls, client.ShareURL(info.Token))
	}

	if *jsonOut {
		out := make([]map[string]any, 0, len(infos))
		for i, info := range infos {
			n := info.LinkNumber
			if n == 0 {
				n = i + 1
			}
			out = append(out, map[string]any{
				"link_number": n,
				"token":       info.Token,
				"ttl":         info.TTL,
				"url":         urls[i],
			})
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "error: encode json: %v\n", err)
			return 1
		}
		return 0
	}

	for i, share := range urls {
		if *quiet {
			fmt.Fprintln(stdout, share)
			continue
		}
		if len(urls) == 1 {
			fmt.Fprintf(stdout, "%s\n", share)
		} else {
			n := infos[i].LinkNumber
			if n == 0 {
				n = i + 1
			}
			fmt.Fprintf(stdout, "Link %d:\t%s\n", n, share)
		}
	}

	clipboardText := strings.Join(urls, "\n")
	if err := copyToClipboard(clipboardText); err != nil {
		fmt.Fprintf(stderr, "warning: could not copy to clipboard: %v\n", err)
	} else {
		fmt.Fprintln(stderr, "Copied to clipboard")
	}
	return 0
}

func runUnwrap(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hushify unwrap", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fileFlag := fs.String("file", "", "Read URL or token from file (use - for stdin)")
	fs.StringVar(fileFlag, "f", "", "Shorthand for -file")
	jsonOut := fs.Bool("json", false, "Print JSON {\"secret\":\"...\"}")

	fs.Usage = func() {
		fmt.Fprintf(stderr, `hushify unwrap — reveal a one-time secret from a share URL or token

Usage:
  hushify unwrap <url-or-token>
  hushify unwrap -f token.txt
  echo '<url-or-token>' | hushify unwrap

Accepts:
  full share URL   https://www.hushify.io?token=...
  raw token        k3SkjmTKTXKrKRvmmWe1pNMg

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	raw, err := readUnwrapInput(fs.Args(), *fileFlag, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	token, err := parseTokenInput(raw)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	secret, err := NewClient().Unwrap(token)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		if err := enc.Encode(map[string]string{"secret": secret}); err != nil {
			fmt.Fprintf(stderr, "error: encode json: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintln(stdout, secret)
	return 0
}

func readUnwrapInput(posArgs []string, fileFlag string, stdin io.Reader) (string, error) {
	if len(posArgs) > 1 {
		return "", fmt.Errorf("too many arguments; pass one URL or token")
	}
	if len(posArgs) == 1 && fileFlag != "" {
		return "", fmt.Errorf("use either a positional argument or -file, not both")
	}
	if len(posArgs) == 1 {
		return posArgs[0], nil
	}
	if fileFlag != "" {
		if fileFlag == "-" {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			return strings.TrimSpace(string(data)), nil
		}
		data, err := os.ReadFile(filepath.Clean(fileFlag))
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return "", fmt.Errorf("provide a share URL or token (see hushify unwrap -h)")
}

// parseTokenInput accepts a raw wrap token or a hushify share URL.
func parseTokenInput(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("URL or token is empty")
	}

	// Bare token (no URL structure).
	if !strings.Contains(raw, "://") && !strings.Contains(raw, "?") && !strings.Contains(raw, "token=") {
		return strings.TrimPrefix(raw, "s."), nil
	}

	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + strings.TrimPrefix(candidate, "//")
	}

	u, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	token := u.Query().Get("token")
	if token == "" {
		return "", fmt.Errorf("no token query parameter in URL")
	}
	return strings.TrimPrefix(token, "s."), nil
}

func readSecret(secretFlag, fileFlag string, stdin io.Reader) (string, error) {
	if secretFlag != "" && fileFlag != "" {
		return "", fmt.Errorf("use either -secret or -file, not both")
	}
	if secretFlag != "" {
		return secretFlag, nil
	}
	if fileFlag != "" {
		if fileFlag == "-" {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			return strings.TrimRight(string(data), "\r\n"), nil
		}
		data, err := os.ReadFile(filepath.Clean(fileFlag))
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}

	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}

	fmt.Fprint(os.Stderr, "Secret: ")
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func validateTTL(ttl string) error {
	m := ttlPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(ttl)))
	if m == nil {
		return fmt.Errorf("invalid -ttl %q (use e.g. 300s, 5m, 1h, 6h, 1d, 7d)", ttl)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return fmt.Errorf("invalid -ttl %q: value must be >= 1", ttl)
	}
	unit := m[2]
	var days float64
	switch unit {
	case "s":
		days = float64(n) / 86400
	case "m":
		days = float64(n) / 1440
	case "h":
		days = float64(n) / 24
	case "d":
		days = float64(n)
	}
	if days > maxTTLDays {
		return fmt.Errorf("-ttl exceeds maximum of %d days (web UI limit)", maxTTLDays)
	}
	return nil
}

func parseIPList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.IndexByte(s, '@')
	if at < 1 || at == len(s)-1 {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}
