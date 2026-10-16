# tcli

```
        [ ]
         |
    .---------.
    |  O   O  |     tcli
    |    ^    |     Microsoft Teams, from your terminal
    |  \___/  |
    '---------'
     |||||||||
```

A command-line client for Microsoft Teams. List chats, send messages and files, read replies and react to them from your terminal. No frills, does one thing well.

> **Agent-optimized Teams connector.** Every command runs non-interactively once you're logged in, takes chat aliases, accepts messages on stdin, sends files and emoji reactions, offers `--json` output for `chats` and `replies`, and gives error messages that say what to do next, so AI agents and scripts can use Teams as easily as people can.

## Prerequisites

- [Go](https://go.dev/dl/) 1.25 or newer, and `make`
- A Microsoft 365 work or school account with Teams
- Permission to register an app in your organisation's Azure AD (Entra ID), or an admin who can do it for you

## 1. Install

```bash
make install
```

This builds the binary and copies it to `~/.local/bin/tcli`. If `~/.local/bin` is not on your `PATH`, add it (e.g. in `~/.bashrc`):

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## 2. Register an Azure app (one-time)

tcli talks to Microsoft Graph, which requires an app registration in your tenant.

1. Open [Azure Portal > App registrations](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade) and click **New registration**.
2. Name it (e.g. `tcli`), leave **Supported account types** as **Single tenant**, leave **Redirect URI** empty, and click **Register**.
3. On the app's **Overview** page, copy the **Application (client) ID** and **Directory (tenant) ID** — you'll need both in step 3.
4. Go to **API permissions > Add a permission > Microsoft Graph > Delegated permissions** and add:
   - `Chat.Read`
   - `ChatMessage.Send`
   - `Files.Read.All` — lets `tcli replies --download` fetch files shared in chats
   - `Files.ReadWrite` — lets `tcli send --file` upload files to your OneDrive and share them with the chat

   Keep the default `User.Read` permission — `tcli replies` needs it.
5. Go to **Authentication**, find **Allow public client flows** (under *Advanced settings*), set it to **Yes**, and click **Save**. Device-code login fails without this.
6. *(Only if your organisation blocks user consent)* back in **API permissions**, click **Grant admin consent**, or ask an admin to do it. Otherwise you'll simply be asked to consent the first time you log in.

## 3. Configure

```bash
tcli config
```

This prompts for the Client ID and Tenant ID from step 2 and saves them to `~/.config/tcli/config.json`.

Alternatively, set environment variables (these take precedence over the config file):

```bash
export TCLI_CLIENT_ID="your-client-id"
export TCLI_TENANT_ID="your-tenant-id"
```

## 4. Log in

```bash
tcli login
```

This prints a URL and a code. Open the URL in any browser (it doesn't have to be on the same machine), enter the code, and sign in with your Microsoft account. Tokens are cached in `~/.config/tcli/tokens.json` and refreshed automatically; run `tcli login` again if you're told your session has expired.

Run `tcli logout` to remove the cached tokens.

Check it works:

```bash
tcli chats
```

## Usage

### List chats

```bash
tcli chats          # table: alias, chat ID, type, name
tcli chats --json   # machine-readable output
```

### Aliases

Chat IDs are long (`19:abc123...@thread.v2`). Give the chats you use often a short name:

```bash
tcli alias set standup 19:abc123@thread.v2
tcli alias list
tcli alias rm standup
```

Aliases are stored in `~/.config/tcli/aliases.json` and work anywhere a chat ID is accepted.

### Send a message

```bash
tcli send standup "Hello from the CLI"
```

Pipe from stdin with `-`:

```bash
echo "Build passed" | tcli send standup -
kubectl get pods | tcli send standup -
```

Teams collapses newlines in plain-text messages. Use `--html` when the message needs structure:

```bash
tcli send standup --html "<b>Build passed</b><br>all green"
```

### Send files

Attach files with `--file` (or `-f`, repeatable). The message is optional:

```bash
tcli send standup "Q3 report attached" --file report.pdf
tcli send standup -f notes.txt -f data.csv
```

As in Teams itself, files are uploaded to the **Microsoft Teams Chat Files** folder of your OneDrive (a name clash gets a numbered copy rather than overwriting) and shared **view-only** with the other chat members, who can open or download them. If a member can't be given access — e.g. an external user your organisation doesn't share with — tcli warns and sends anyway; if nobody can, the message isn't sent.

With `--file` and no message, stdin is not read; pass `-` to send piped text along with the files.

### React to a message

```bash
tcli react standup 1712345678901 👍
tcli react standup 1712345678901 heart
tcli react standup 1712345678901 👍 --remove
```

The message ID is shown by `tcli replies` (and in `--json` output) and printed by `tcli send`. Any emoji works; `like`, `heart`, `laugh`, `surprised`, `sad` and `angry` are accepted as names.

### Read replies

Show messages posted in a chat since your last message:

```bash
tcli replies standup
tcli replies standup --json
```

If you've never posted in the chat, recent messages are shown instead. `--max-pages` (default 5) limits how far back it searches for your last message.

Each message is shown with its ID (for `tcli react`). Files, inline images and reactions are listed under each message (📎 for files, 🖼 for pasted images, then reaction counts such as `👍 2  ❤️ 1`). To save the files and images, pass a directory:

```bash
tcli replies standup --download ~/Downloads/standup
```

Existing files are never overwritten — a numeric suffix is added instead (`report-1.pdf`). Inline images are saved as `image-<message-id>-<n>.png` (or `.jpg` etc.).

## Troubleshooting

| Symptom | Fix |
|---|---|
| `AADSTS7000218: The request body must contain ... client_assertion or client_secret` | **Allow public client flows** is off — see step 2.5. |
| `permission denied … Chat.Read, ChatMessage.Send, Files.Read.All and Files.ReadWrite …` | Add the permissions in step 2.4 (and grant admin consent if your org requires it), then run `tcli login` again. |
| `not logged in` / `session expired` | Run `tcli login`. |
| `tcli needs a new permission (AADSTS65001 …)` after upgrading tcli | A new permission was added (most recently `Files.ReadWrite`). Add it as in step 2.4, then run `tcli login` again. |
| A recipient can't open a file you sent | tcli prints a `could not share … with …` warning when that happens — usually an external member your organisation's sharing policy excludes. Share the file from OneDrive (Microsoft Teams Chat Files) manually. |
| `rate limited by Graph API — retry in …` / `Graph API unavailable (HTTP 503) …` | Microsoft Graph is throttling you or briefly unavailable. tcli retries short waits (up to 60s) itself; for longer ones it stops and tells you how long to wait before running the command again. |
| `tcli: command not found` | `~/.local/bin` is not on your `PATH` — see step 1. |
