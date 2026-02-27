# tcli

A command-line client for Microsoft Teams. List chats and send messages from your terminal.

## Install

```bash
make install
```

This builds the binary and copies it to `~/.local/bin/tcli`. Make sure `~/.local/bin` is in your `PATH`.

## Prerequisites: Azure App Registration

You need to register an app in Azure AD (Entra ID) to get API access:

1. Go to [Azure Portal > App registrations](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade)
2. Click **New registration**
3. Name it something like "tcli"
4. Under **Supported account types**, choose the option that matches your org
5. Under **Redirect URI**, select **Public client/native** and enter: `https://login.microsoftonline.com/common/oauth2/nativeclient`
6. Click **Register**
7. Note the **Application (client) ID** and **Directory (tenant) ID** from the overview page
8. Go to **API permissions > Add a permission > Microsoft Graph > Delegated permissions** and add:
   - `Chat.Read`
   - `ChatMessage.Send`
9. Click **Grant admin consent** (or ask your admin)

## Install

```
make install
```

## Setup

Configure your credentials:

```bash
tcli config
```

Make sure ~/.local/bin is in your PATH. If not: export PATH="$HOME/.local/bin:$PATH".

This prompts for your Client ID and Tenant ID and saves them to `~/.config/tcli/config.json`.

Alternatively, use environment variables:

```bash
export TCLI_CLIENT_ID="your-client-id"
export TCLI_TENANT_ID="your-tenant-id"
```

## Login

Authenticate using the device code flow:

```bash
tcli login
```

This prints a URL and a code. Open the URL in any browser, enter the code, and sign in with your Microsoft account. The token is cached at `~/.config/tcli/tokens.json`.

## Usage

### List chats

```bash
tcli chats
```

Output is a table with chat ID, type, and name. Use `--json` for machine-readable output:

```bash
tcli chats --json
```

### Send a message

Send inline:

```bash
tcli send <chat-id> "Hello from the CLI"
```

Pipe from stdin:

```bash
echo "Build passed" | tcli send <chat-id> -
```

Pipe command output:

```bash
kubectl get pods | tcli send <chat-id> -
```

## File structure

```
tcli/
├── main.go
├── cmd/
│   ├── root.go       # Root command
│   ├── config.go     # tcli config
│   ├── login.go      # tcli login
│   ├── chats.go      # tcli chats
│   └── send.go       # tcli send
├── internal/
│   ├── auth/
│   │   ├── auth.go   # Device code flow
│   │   └── cache.go  # Token cache
│   └── graph/
│       ├── client.go    # HTTP client for MS Graph
│       ├── chats.go     # List chats
│       └── messages.go  # Send messages
├── config/
│   └── config.go     # App configuration
├── Makefile
├── PLAN.md
└── README.md
```
