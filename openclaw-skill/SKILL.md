---
name: splashify
description: Send WhatsApp and RCS messages, manage contacts, run broadcasts,
  list templates, and read analytics on a Splashify Pro account by driving the
  splashify CLI. Use whenever the user asks to send a WhatsApp/RCS message,
  list/create/tag contacts, start or check a broadcast, see message analytics,
  check wallet balance, or otherwise act on their Splashify Pro account.
license: MIT
metadata:
  openclaw:
    requires:
      bins: [splashify]
---

# Splashify skill

You drive the user's **Splashify Pro** account by running the `splashify`
CLI through the `exec` tool. The CLI carries the user's credentials and
talks to the Splashify Pro API. You carry **no credentials** of your own —
never paste tokens, never read `~/.splashify/config.json`.

The CLI is the single source of truth for what is possible. If a task is
not reachable from the commands listed below or in `reference.md`, fall
back to `splashify api <METHOD> <path>` (see "Generic API" below) rather
than inventing a command.

## Before you act

Run **once per session** before the first non-trivial command, and again
if a later command fails with an auth error:

```
splashify whoami
```

If `whoami` exits non-zero, stop and tell the user the CLI is not
connected — they need to run `splashify connect` first. Do not retry.

## Output contract

Every `splashify` command follows this contract:

- **Success** → JSON (or a tabular summary) on stdout, exit code `0`.
- **Failure** → `error: <message>` on stderr, **non-zero exit code**.

When a command exits non-zero, **relay the `error:` line to the user
verbatim**. Do not invent a result, do not retry blindly, do not
paraphrase the error in a way that loses information (status codes,
template names, phone numbers).

## Phone-number format

Every phone number passed to `--to`, `--phone`, `--country-code` etc.
must include the country code as `+<digits>` with no spaces:

- ✅ `+919876543210`
- ✅ `+14155551234`
- ❌ `9876543210` (missing country code)
- ❌ `+91 98765 43210` (spaces)
- ❌ `091-98765-43210` (zero prefix, dashes)

If the user gives you a number without a country code and the conversation
has not established a default region, **ask** rather than guess.

## Account scope

The user's token is a personal `oc_live_` access token. It is scoped to
**their** Splashify Pro account only. The following are out of reach
and you must refuse politely without invoking the CLI:

- Admin / reseller / platform actions on other accounts.
- Reading or modifying other users' data.
- Anything under `/api/v1/admin/*`, `/api/v1/partner/*`, `/api/v1/prime/*`.

If the user asks for something admin-shaped, say so and stop.

## Template-message rule

WhatsApp template messages require a template that is **already approved**
in Splashify Pro. Before calling `splashify message template …`:

1. If you do not know the exact template name, run `splashify templates`
   to list approved names.
2. Pass the name exactly as listed (case-sensitive, snake_case).
3. Pass `--lang <code>` if the user specifies one; otherwise omit and
   the backend picks the default.

The same rule applies to `splashify rcs send-template` and the call
templates under `splashify calling template …`.

## Command map by task

These are the high-traffic patterns. Full flags live in
[reference.md](reference.md) — load it on demand.

### Send a WhatsApp text message
```
splashify message send --to +91… --text "…"
```
Reply quote: add `--context-message-id <wa_message_id>`.

### Send a WhatsApp template
```
splashify message template --to +91… --name <approved_template_name> \
  [--lang en] [--vars '[{"type":"text","text":"…"}]']
```

### Send WhatsApp media (image / video / document / voice)
```
splashify message media --to +91… --type image --url https://… [--caption "…"]
splashify message media --to +91… --type audio --url https://… --voice true
```

### Send an RCS text message
```
splashify rcs send --to +91… --text "…"
```

### List conversations
```
splashify conversations [--channel whatsapp|rcs|instagram] \
                        [--status open|resolved] [--search "…"] \
                        [--page N] [--limit N]
```
Get one: `splashify conversation <id>`.
Resolve / reopen / assign: `splashify conversation <id> resolve|reopen|assign --to <member_id>`.

### Contacts
```
splashify contacts [--search "…"] [--tag vip] [--page N]
splashify contact <id>
splashify contact create --phone +91… [--name "…"] [--email "…"]
splashify contact update <id> [--name "…"] [--email "…"] [--tags vip,lead]
splashify contact tag <id> --tags vip,lead
splashify contact untag <id> --tags vip
splashify contact block <id>     # also: unblock, delete
```

### Broadcasts
```
splashify broadcasts [--status …] [--search "…"]
splashify broadcast <id>                    # details
splashify broadcast <id> messages           # per-recipient list
splashify broadcast <id> cancel|restart|send-now
splashify broadcast create --name "…" --template <name> \
  --category MARKETING --language en \
  (--segment-ids id1,id2 | --contact-ids id1,id2) \
  [--send-type now|scheduled] [--scheduled-at 2026-06-01T10:00:00Z]
```

### Templates
```
splashify templates                # list every WhatsApp template
splashify template <id>            # details
splashify templates sync           # pull fresh data from Meta
```

### Analytics
```
splashify analytics                # summary
splashify analytics trends         # daily series
```

### Wallet, billing, subscription, account
```
splashify wallet                   # balance
splashify wallet transactions      # ledger
splashify account                  # profile + plan + orgs
splashify billing                  # invoices + GST profile
splashify subscription             # plan + add-ons + eligibility
```

### Generic API passthrough

For anything not covered above (or in `reference.md`), fall back to:

```
splashify api GET  /app/<path>
splashify api POST /app/<path> --data '{"key":"value"}'
```

The `--data` value must be a JSON string. Always confirm with the user
before sending a `POST`, `PUT`, `PATCH`, or `DELETE` you have not run
before — the generic passthrough does not preflight.

## Confirmation rule

Run **read-only commands** (`whoami`, `conversations`, `contacts`,
`templates`, `analytics`, `wallet`, anything starting with
`splashify api GET`) without confirmation.

**Ask first** before any of these:

- sending a WhatsApp / RCS / call message to a real number,
- creating, cancelling, restarting, or rebroadcasting a broadcast,
- deleting / blocking / unblocking a contact,
- mutating templates, segments, attributes, or team members,
- revoking access tokens,
- any `splashify api POST|PUT|PATCH|DELETE …`.

For a send to a real phone, repeat the destination back to the user
("Send to **+91…7890**: '…' — go?") before invoking the CLI.

## When the user is vague

If the user says "send a message", you usually need:

1. **To whom** — phone number with country code, or contact name/id (if
   a name, list contacts first to disambiguate).
2. **Text or template** — free-form text, or a template name + vars.
3. **Channel** — WhatsApp (default), RCS, or Instagram.

Ask for whatever you do not have. Never invent a phone number, never
invent a template name.

## Reading results

CLI output is JSON or a small table. Parse the relevant fields and
**summarize for the user in plain language** — never dump raw JSON
unless the user asks for it.

For lists, surface counts and the first few rows (e.g. "you have 142
contacts; the latest five are …"). For sends, surface the returned
`message_id` / `status` / channel and confirm delivery initiated.

## Reference

For the complete command surface (every flag of every subcommand) load
[`reference.md`](reference.md). Consult it whenever:

- the user asks for a less common task (devices, flows, integrations,
  CTWA, AI agents, support tickets, expenses, calling, email),
- a command in this file does not match what the user described,
- you are about to use `splashify api …` and want to check whether a
  typed wrapper already exists.

## Errors you should know

- `not connected — run "splashify connect" first` → CLI has no token;
  tell the user to run `splashify connect`.
- `token validation failed` → token is invalid, revoked, or expired;
  tell the user to create a new one in **Settings → Developer →
  Access Tokens**.
- `your Splashify Pro account does not have a WhatsApp Business
  Account connected yet` → blocking for any WhatsApp send; relay the
  message verbatim.
- `unknown command: <x>` → either you mistyped or the CLI is older
  than the docs; suggest `splashify --version` and consult
  `reference.md` for the current surface.

## Do not

- Do not run `splashify connect` on the user's behalf — it is
  interactive and prompts for a token.
- Do not write to `~/.splashify/config.json` directly.
- Do not pass the user's token in command lines or environment dumps.
- Do not retry a failed send more than once; surface the error.
- Do not invoke commands outside the `splashify` binary unless the
  user explicitly asks.
