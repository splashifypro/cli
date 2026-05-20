# splashify CLI

Connect a Splashify messaging account to **OpenClaw** (the local-first AI
assistant — https://github.com/openclaw/openclaw) so the assistant can perform
every app action — send WhatsApp/RCS messages, manage contacts, run broadcasts,
edit templates, read analytics — on the user's behalf.

## How it fits together

```
OpenClaw ──MCP/stdio──► splashify-mcp ──HTTPS, Bearer oc_live_──► backend /api/v1/app/*
```

The CLI does three things: hold an `oc_live_` access token, manage tokens, and
register the `splashify-mcp` server with OpenClaw.

## Build

```bash
# the CLI
cd cli && go build -o splashify ./...

# the MCP server it links into OpenClaw (separate Go module)
cd ../mcp && go build -o splashify-mcp ./cmd
```

Put `splashify` and `splashify-mcp` on your `PATH` (same directory works too —
the CLI looks for `splashify-mcp` next to itself).

## Connect

```bash
# Connect this machine. Paste an oc_live_ token from the app
# (Settings → Developer → Access Tokens), or create one here:
splashify connect

splashify token create --name "OpenClaw on laptop"   # mint a token
splashify token list                                 # see your tokens
splashify token revoke <id>                           # kill a token

splashify whoami        # show the connected account
splashify doctor        # diagnose config / token / openclaw / mcp binary
```

Config is stored at `~/.splashify/config.json` (mode 0600) — it holds the
backend URL and the `oc_live_` token, so treat it like a password file.

## Do everything from the command line

Once connected, the CLI can perform every app task directly — no OpenClaw
required. Every command prints the backend's JSON response.

### Messaging

```bash
splashify message send --to +919876543210 --text "Your order has shipped"
splashify message template --to +919876543210 --name order_update \
  --lang en --vars '["John","ORD-1024"]'
splashify message media --to +919876543210 --type image \
  --url https://example.com/receipt.png --caption "Receipt"

splashify conversations --status open          # list chats
splashify conversation <id>                    # read one chat
splashify conversation <id> resolve            # close a chat
splashify unread                               # unread count
```

### Contacts

```bash
splashify contacts --search john --tag vip
splashify contact <id>
splashify contact create --phone +919876543210 --name "John Roe" --email john@x.com
splashify contact tag <id> --tags vip,lead
splashify contact block <id>
splashify contact unblock <id>
splashify contact delete <id>
```

### Broadcasts, templates, analytics, wallet

```bash
splashify broadcasts
splashify broadcast <id>
splashify broadcast stats
splashify broadcast create --name "May Sale" --template may_offer \
  --audience-type segment --audience-id <segment-id> --schedule 2026-06-01T10:00:00Z

splashify templates
splashify analytics            # message analytics summary
splashify analytics trends
splashify wallet               # balance
splashify wallet transactions
```

### Everything else — the generic `api` command

Any `/api/v1/app/*` endpoint is reachable directly, so the CLI covers the
full app surface (segments, AI agents, team, tickets, WABA setup, …):

```bash
splashify api GET  /app/segments
splashify api GET  "/app/contacts?page=2&page_size=50"
splashify api POST /app/messages/send-text --data '{"phone":"+919876543210","message":"hi"}'
splashify api POST /app/ai-agents --data '{"name":"Support Bot"}'
splashify api DELETE /app/contacts/<id>
```

The path may be given with or without the `/api/v1` prefix. Use `--data` to
pass a JSON request body for `POST`/`PUT`/`PATCH`.

## Connect Splashify Pro with OpenClaw

To let the OpenClaw AI assistant drive your account in natural language:

```bash
# 1. install OpenClaw
npm install -g openclaw@latest && openclaw onboard --install-daemon

# 2. connect this machine (if not already done)
splashify connect

# 3. register the splashify MCP server with OpenClaw
splashify link openclaw       # runs `openclaw mcp add splashify …` for you
splashify mcp-config          # or print the command to run manually

# 4. restart the OpenClaw Gateway, then confirm
splashify doctor              # all checks should be ✓
```

Then ask your assistant things like *"send a WhatsApp to +91… saying the order
shipped"* or *"list my VIP contacts"* — it performs them on your account.

**Full step-by-step connect + usage guide:**
[`app-docs/openclaw-integration.md`](../app-docs/openclaw-integration.md) — covers
prerequisites, the manual `openclaw mcp add` command, example assistant
prompts, revoking access, and troubleshooting.

Run `splashify help` for the full command list.

## Token model

- `oc_live_` tokens are long-lived credentials accepted on every `/api/v1/app/*`
  route as an alternative to the session JWT.
- The raw token is shown **once** at creation; only its SHA-256 hash is stored.
- Revoke from `splashify token revoke`, the app UI, or the API — revocation is
  immediate.
- A token resolves to the account owner; it never grants admin/reseller access.
