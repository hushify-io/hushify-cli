# hushify CLI

Command-line client for [hushify.io](https://www.hushify.io) — one-time encrypted secret sharing. Options mirror the web UI.

## How it works

The binary is a thin HTTP client over the public Hushify API. It does not encrypt locally; wrapping and unwrapping happen on the server, same as the website.

```
┌─────────────┐     POST /api/wrap      ┌──────────────────┐
│  hushify    │ ──────────────────────► │  hushify.io API  │
│  wrap       │ ◄────────────────────── │                  │
└─────────────┘   token(s) + TTL        └──────────────────┘
       │
       ▼
  share URL(s)  →  https://www.hushify.io?token=...
                   (also copied to clipboard)

┌─────────────┐     POST /api/unwrap    ┌──────────────────┐
│  hushify    │ ──────────────────────► │  hushify.io API  │
│  unwrap     │ ◄────────────────────── │                  │
└─────────────┘   plaintext secret      └──────────────────┘
```

1. **`wrap`** — Sends your secret to `POST /api/wrap` with TTL, link count, optional email/IP restrictions. The API returns one or more one-time tokens. The CLI prints share URLs (`https://www.hushify.io?token=…`) and copies them to the clipboard (skipped with `-json`).
2. **`unwrap`** — Accepts a share URL or raw token, extracts the token, then calls `POST /api/unwrap`. The secret is printed once; the link is consumed on the server.

Secrets are one-time: after a successful unwrap (or TTL expiry), the link no longer works.

## Install

### Homebrew

```bash
brew tap nvteh/brew-tools
brew install hushify
```

Or in one step:

```bash
brew install nvteh/brew-tools/hushify
```

### From source

Requires [Go 1.23+](https://go.dev/dl/).

```bash
git clone https://github.com/hushify-io/hushify-cli.git
cd hushify-cli
go build -o hushify .
```

Or install directly:

```bash
go install github.com/hushify-io/hushify-cli@latest
```

## Usage

```text
hushify wrap [flags]              Create share URL(s)
hushify unwrap <url-or-token>     Reveal a secret once
hushify version
```

### Wrap a secret

Secret input (first match wins): `-secret` / `-s`, `-file` / `-f`, or stdin if piped.

```bash
# From stdin (preferred for sensitive values)
printf 'db-password' | hushify wrap

# From a file
hushify wrap -f ./key.pem -t 7d -n 2 -e ops@example.com

# Literal flag (shows up in shell history — avoid for real secrets)
hushify wrap -s "token" --restrict-ip
```

| Flag | Description |
|------|-------------|
| `-s`, `-secret` | Secret text |
| `-f`, `-file` | Read secret from file (`-` = stdin) |
| `-t`, `-ttl` | Expiry: `1h` (default), `6h`, `1d`, `7d`, or custom like `30m` / `5m` / `2d` (max 32 days) |
| `-n`, `-links` | Independent one-time links (1–10) |
| `-e`, `-email` | Access/expiry notification address |
| `--restrict-ip` | Only the creating client IP may unwrap |
| `--allow-ip` | Comma-separated IPs allowed to unwrap |
| `-q`, `-quiet` | Print only URL(s), one per line |
| `-json` | Machine-readable JSON (skips clipboard) |

`--restrict-ip` and `--allow-ip` are mutually exclusive.

### Unwrap a secret

```bash
hushify unwrap 'https://www.hushify.io?token=...'
hushify unwrap k3SkjmTKTXKrKRvmmWe1pNMg
echo 'https://www.hushify.io?token=...' | hushify unwrap
hushify unwrap -f token.txt
```

| Flag | Description |
|------|-------------|
| `-f`, `-file` | Read URL/token from file (`-` = stdin) |
| `-json` | Print `{"secret":"..."}` |

## Clipboard

On successful `wrap` (without `-json`), share URLs are copied to the clipboard:

- **macOS** — `pbcopy`
- **Linux** — `wl-copy`, `xclip`, or `xsel` (first found)

Failure to copy is a warning only; URLs are still printed to stdout.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Runtime/API error |
| `2` | Usage / validation error |

## Development

```bash
go test ./...
```
