# tcli — Microsoft Teams CLI Client

## Goal

A lightweight CLI tool that lets you interact with Microsoft Teams from the terminal. Pipe command output, send quick messages, and browse chats without leaving your shell.

## Iteration 1 Scope

Two commands only:

1. **`tcli chats`** — List all your chats (1:1, group, meeting chats) with display names and chat IDs.
2. **`tcli send <chat-id> <message>`** — Send a plain-text message to a specific chat. Supports reading from stdin so you can pipe output: `some-command | tcli send <chat-id> -`.

---

## Microsoft Graph API

Teams data is exposed through the Microsoft Graph API. The relevant endpoints:

| Action | Method | Endpoint |
|---|---|---|
| List chats | GET | `/me/chats?$expand=members` |
| Send message | POST | `/me/chats/{chat-id}/messages` |

Authentication uses OAuth 2.0 with delegated permissions:
- `Chat.Read` (list chats)
- `Chat.ReadWrite` or `ChatMessage.Send` (send messages)

### Auth Strategy — Device Code Flow

For a CLI tool on Linux without a browser guaranteed on the same machine, the **device code flow** is the best fit:

1. User runs `tcli login`.
2. App prints a URL and a one-time code.
3. User opens the URL on any device, enters the code, and signs in.
4. App receives tokens and caches them locally.

Tokens are cached in `~/.config/tcli/tokens.json` and refreshed automatically using the refresh token.

### Azure App Registration (one-time setup by user)

The user registers an app in Azure AD (Entra ID):
- Type: Public client / native app (allows device code flow)
- API permissions: `Chat.Read`, `ChatMessage.Send` (delegated)
- Redirect URI: `https://login.microsoftonline.com/common/oauth2/nativeclient`

The resulting **client ID** and **tenant ID** are stored in `~/.config/tcli/config.json` or passed via env vars `TCLI_CLIENT_ID` / `TCLI_TENANT_ID`.

---

## Project Structure

```
tcli/
├── main.go                 # Entrypoint, wires up CLI
├── go.mod
├── go.sum
├── cmd/
│   ├── root.go             # Root cobra command, global flags
│   ├── login.go            # `tcli login`  — device code auth
│   ├── chats.go            # `tcli chats`  — list chats
│   └── send.go             # `tcli send`   — send a message
├── internal/
│   ├── auth/
│   │   ├── auth.go         # Device code flow, token acquire/refresh
│   │   └── cache.go        # Token cache read/write (~/.config/tcli/)
│   └── graph/
│       ├── client.go       # Thin HTTP client for MS Graph
│       ├── chats.go        # List chats, parse response
│       └── messages.go     # Send message, parse response
├── config/
│   └── config.go           # Load/save config (client ID, tenant ID)
├── PLAN.md
└── README.md
```

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework — standard for Go CLIs |
| `github.com/Azure/azure-sdk-for-go/sdk/azidentity` | Azure identity / device code credential |
| `github.com/microsoftgraph/msgraph-sdk-go` | MS Graph SDK for Go (optional — could use raw HTTP) |

Decision: start with **raw HTTP + azidentity** to keep the binary small and the code easy to follow. The Graph SDK adds a large dependency tree. We can always switch later.

## Implementation Steps

### Step 1 — Project skeleton

- `go mod init github.com/piotrwolkowski/tcli`
- Add cobra, azidentity dependencies.
- Create `cmd/root.go` with a root command that prints help.
- Verify it builds and runs: `go run main.go`.

### Step 2 — Configuration

- Implement `config/config.go`:
  - Load `~/.config/tcli/config.json` (`clientId`, `tenantId`).
  - Fall back to env vars `TCLI_CLIENT_ID`, `TCLI_TENANT_ID`.
  - Provide a `tcli config set` helper or document manual file creation.

### Step 3 — Authentication (device code flow)

- Implement `internal/auth/auth.go`:
  - Use `azidentity.DeviceCodeCredential` for the device code flow.
  - On `tcli login`, run the flow and cache the resulting token.
- Implement `internal/auth/cache.go`:
  - Save/load tokens from `~/.config/tcli/tokens.json`.
  - On every authenticated call, check expiry and refresh if needed.
- Wire up `cmd/login.go`.

### Step 4 — List chats

- Implement `internal/graph/client.go`:
  - HTTP client that injects the Bearer token from auth.
  - Base URL: `https://graph.microsoft.com/v1.0`.
- Implement `internal/graph/chats.go`:
  - `GET /me/chats?$expand=members&$top=50` with paging.
  - Parse JSON into a `Chat` struct (id, topic, chatType, members).
- Implement `cmd/chats.go`:
  - Table output: chat ID, type (1:1 / group / meeting), display name or member names.
  - Optional `--json` flag for machine-readable output.

### Step 5 — Send message

- Implement `internal/graph/messages.go`:
  - `POST /me/chats/{id}/messages` with `{ "body": { "content": "..." } }`.
- Implement `cmd/send.go`:
  - `tcli send <chat-id> <message>` — send inline text.
  - `tcli send <chat-id> -` — read message body from stdin.
  - Print confirmation (message ID, timestamp) on success.

### Step 6 — Build & packaging

- `go build -o tcli .` produces a single static binary.
- Add a `Makefile` with `build`, `install` (copies to `~/.local/bin`), and `clean` targets.
- Document install steps in README.

---

## Future Iterations (out of scope for now)

- `tcli chats search <query>` — filter chats by name.
- `tcli read <chat-id>` — read recent messages from a chat.
- `tcli reply <chat-id> <message-id>` — reply to a specific message.
- `tcli channels` — list teams/channels.
- `tcli send --channel <team-id> <channel-id> <message>` — post to a channel.
- Markdown / HTML message formatting.
- File/attachment sending.
- Notifications / watch mode.
- Shell completions (cobra built-in).
- Homebrew / deb / rpm packaging.

---

## Open Questions

1. **Graph SDK vs raw HTTP** — Starting with raw HTTP for simplicity. Revisit if we need complex query patterns.
2. **Token storage security** — Plain JSON file for now. Could integrate with system keyring later (`keyctl` on Linux).
3. **Rate limiting** — Graph API has throttling. Add retry with backoff if we hit 429s.
