# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**tcli** is a lightweight Go CLI client for Microsoft Teams, using the Microsoft Graph API with OAuth device code flow for headless/terminal environments.

## Build Commands

```bash
make build      # Compile to ./tcli
make install    # Build and install to ~/.local/bin/tcli
make clean      # Remove compiled binary
go test ./...   # Run unit tests (all packages; httptest fakes for Graph and the token endpoint)
```

No linter is configured; use `go vet ./...` for basic static analysis.

## Architecture

The project is a standard Go CLI with four layers:

1. **`cmd/`** — Cobra command handlers. Each file corresponds to one CLI subcommand (`config`, `login`, `logout`, `chats`, `send`, `replies`, `react`, `alias`), plus helpers `download.go` (`replies --download`; names are sanitised, shortened and claimed with `O_EXCL`), `upload.go` (`send --file`) and `htmltext.go` (`htmlToText`, a small tokenizer that lays out Teams HTML as plain text like a browser: collapsing whitespace outside `<pre>`, blocks on their own lines). Commands resolve chat aliases, call the graph layer, and format output.

2. **`internal/auth/`** — OAuth device code flow implemented with raw HTTP against the `login.microsoftonline.com` v2.0 endpoints (no `azidentity`). `auth.go` handles login and `GetToken`, which refreshes the access token with the cached refresh token (`offline_access` scope) and rotates it if the server issues a new one. `cache.go` persists tokens to `~/.config/tcli/tokens.json` (0600) with a 2-minute expiry buffer.

3. **`internal/graph/`** — Thin HTTP client over Microsoft Graph API (`https://graph.microsoft.com/v1.0`). `client.go` injects Bearer tokens (`do`) and, in `send` (also used unauthenticated for pre-authenticated upload-session URLs), retries 429s, plus 503/504 for GET/PUT only (never re-POSTs), up to 3 times honouring `Retry-After` (else exponential backoff), failing fast if asked to wait over 60s; it maps Graph errors to friendly messages; `chats.go` lists chats and chat members with `@odata.nextLink` pagination; `messages.go` sends messages (optionally with `reference` attachments) and lists messages posted after the user's last one; `attachments.go` models message attachments and downloads shared files (via `/shares/{u!…}/driveItem/content`) and inline images (hosted contents); `upload.go` uploads files to the user's OneDrive `Microsoft Teams Chat Files` folder via upload sessions and shares them read-only with chat members (`invite`) — the attachment ID is the GUID in the driveItem `eTag` and its `contentUrl` the `webDavUrl`; `reactions.go` sets/unsets emoji reactions; `me.go` fetches the current user (`/me`).

4. **`config/`** — `config.go` loads Azure `clientId`/`tenantId` from `~/.config/tcli/config.json`, with `TCLI_CLIENT_ID`/`TCLI_TENANT_ID` environment variable overrides taking priority. `aliases.go` manages chat aliases in `~/.config/tcli/aliases.json` (one alias per chat).

## Key Design Decisions

- **Raw HTTP only** — no Microsoft Graph SDK and no `azidentity`; the only dependency is cobra. Keeps the binary small and code simple.
- **Device code flow** — required for headless environments where browser redirect is impossible.
- **Token storage** is plain JSON (not system keyring) — an acknowledged limitation and a possible future improvement.
- **`replies --download <dir>`** saves file attachments and inline images; names are sanitised to their base name, shortened to 200 bytes and never overwrite existing files. Only inline images (names tcli invents) get an extension from the Content-Type.
- **`send` command** accepts `-` as the message argument to read from stdin, enabling pipe-based workflows; `--html` sends HTML content (Teams collapses newlines in plain text). `--file` (repeatable) attaches files; with files and no message, stdin is not read.
- **Scopes** requested at login are `Chat.Read`, `ChatMessage.Send` (also covers reactions), `Files.Read.All` (downloading shared files), `Files.ReadWrite` (uploading and sharing sent files) and `offline_access` (`internal/auth/auth.go`). `/me` (used by `replies`) relies on the app registration's default `User.Read` permission. If you add a scope, update the README's app-registration steps too.

## Graph API notes (verified against learn.microsoft.com, 2026-10)

Non-obvious facts behind the code; re-check the docs before changing these paths.

- **Sending files**: Graph cannot attach bytes to a chat message (only images, inline as hosted contents). Files must live in OneDrive/SharePoint, as Teams itself does. The attachment `id` is the GUID in the driveItem `eTag`, and `contentUrl` must be **`webDavUrl`** (not `webUrl`), which is only returned when `$select`ed.
- **Uploads always use upload sessions**, even for small files, because `@microsoft.graph.conflictBehavior=rename` is only documented there, not on simple PUT. Sessions can't create 0-byte files, and chunk PUTs must **not** carry the bearer token.
- **`invite` returns 207** only when notification emails fail, which can't happen with `sendInvitation:false`. Unshareable members (external users, tenant policy) fail the whole batch with 4xx, hence the per-member fallback in `ShareFile`.
- **Reactions** (`setReaction`/`unsetReaction`) are documented only under `/chats/{id}/…`, not `/me/chats`; `ChatMessage.Send` is enough. `reactionType` is unicode; legacy names (`like`, `heart`, …) only appear when *reading* reactions.
- **Adding a scope breaks every command for existing users** once their access token expires: the refresh asks for an unconsented scope and gets AADSTS65001, which `GetToken` turns into a "new permission" message. Note it in the PR and README troubleshooting.

## Testing conventions

- Tests are offline. `internal/graph` uses `newTestClient` (an httptest server plus a fake token); `internal/auth` uses `setupEnv`; anything touching `~/.config/tcli` isolates `HOME` with `t.Setenv`; `cmd` stdin tests use `withStdin`.
- Test seams are package vars: `auth.authorityHost`, `auth.pollUnit` (milliseconds in tests) and `graph.uploadChunkSize`. Add new ones the same way rather than sleeping: use `Retry-After: 0` for retry tests.
- Never call `t.Fatal`/`t.FailNow` inside an HTTP handler, because it runs on the server goroutine; use `t.Errorf` and return.
- Keep `GOARCH=386 go vet ./...` clean, since constants like `1<<31` overflow `int` on 32-bit.
- When a test exposes a bug, assert the *correct* behaviour and fix the code rather than pinning the bug.

## System dependencies

None — tcli is a standalone tool. Its only external dependencies are Microsoft Graph and the user's own Entra (Azure AD) app registration, both described in the README.

## Current Status

All commands are implemented. The README documents end-user setup (Azure app registration, config, login) and must be kept in sync when commands, flags or required permissions change.
