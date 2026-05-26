# `splashify` CLI — full command reference

Loaded by the Splashify skill on demand. Mirrors the output of
`splashify help`. Look here when the high-traffic patterns in
[SKILL.md](SKILL.md) do not match what the user asked for, or before
falling back to `splashify api …`.

Conventions:

- `--to` always takes a phone number with country code (`+91…`).
- Booleans accept `true`/`false`.
- `--vars`, `--data`, `--payload`, `--filters`, `--components`,
  `--contacts` take JSON strings (single-quote the value to keep your
  shell quoting honest).
- Lists like `--tags vip,lead` are comma-separated.
- Every command prints JSON on success and `error: …` on failure (see
  [SKILL.md → Output contract](SKILL.md#output-contract)).

## Setup

```
splashify connect                Connect this machine with an oc_live_ token
splashify whoami                 Show the connected account (compact)
splashify account                Full account details (read-only)
splashify doctor                 Diagnose the local setup
```

## Access tokens

```
splashify token list             List your access tokens
splashify token create           Create a new access token
splashify token revoke <id>      Revoke an access token
```

## OpenClaw

```
splashify link openclaw [--path <skills-dir>] [--print]
  Install the Splashify skill bundle into OpenClaw's workspace.
  --path overrides the default skills directory.
  --print shows the target path without writing.
```

## WhatsApp messaging

```
splashify message send --to +91… --text "…" [--context-message-id <wa_id>]
splashify message template --to +91… --name <tpl> [--lang en] [--vars '[…]']
splashify message media --to +91… --type image|video|audio|document --url <url>
                        [--caption "…"] [--voice true]
splashify message location --to +91… --lat <n> --lng <n> [--name "…"] [--address "…"]
splashify message reaction --to +91… --message-id <wa_id> --emoji "👍"
splashify message contact --to +91… --contacts '[…]'      (or --file ./contact.json)
splashify message typing --to +91…                          (typing-indicator bubble)
```

## Conversations

```
splashify conversations [--channel whatsapp|rcs|instagram] [--status open|resolved]
                        [--search "…"] [--page N] [--limit N]
splashify conversation <id>                Show one conversation + messages
splashify conversation <id> resolve | reopen
splashify conversation <id> assign --to <member_id>   (pass --to "" to unassign)
splashify unread                            Unread-message count
```

## RCS

```
splashify rcs send --to +91… --text "…"
splashify rcs send-template --to +91… --template-id <uuid>
splashify rcs templates                          List RCS templates
splashify rcs template <id> [check-status]
splashify rcs templates upload-media <file> [--height SHORT|MEDIUM|TALL]
splashify rcs templates create --name "…" --type basic|rich_card|carousel
                                [--text "…" | --data '{…}' | --file ./template.json]
splashify rcs templates delete <id>
```

## Instagram

```
splashify instagram                          Show connected IG account
splashify instagram oauth-url                Get the OAuth URL
splashify instagram connect --code <code>    Complete OAuth handshake
splashify instagram disconnect
splashify instagram media [--limit N]        List IG posts
splashify instagram logs [--limit N]         Automation activity feed
splashify instagram window --conversation <id>
splashify instagram sync                     Backfill conversations
splashify instagram dm --conversation <id> --message "…"
                       [--media-url … --media-type image|video|audio]
splashify instagram rules
splashify instagram rule <id>
splashify instagram rule create --media-id <id> --keyword "buy"
                                --dm-message "Here's the link …"
                                [--dm-media-url … --dm-media-type …]
                                [--comment-reply "…"] [--active true|false]
splashify instagram rule update <id> [flags as above]
splashify instagram rule toggle <id>
splashify instagram rule delete <id>
```

## Calling

```
splashify calling                                Overview
splashify calling settings                       GET calling settings
splashify calling settings update --data '{…}'   PUT calling settings
splashify calling analytics [--start-time --end-time --granularity --dimensions]
splashify calling calls [--search --status --agent --page --limit]
splashify call <call_id>                         Show one call
splashify call initiate --to +91…                Backend-side dial (preflight)
splashify call upload-recording <call_id> <file>
splashify calling permission-status --phone +91…
splashify calling permissions [--search --status --agent]
splashify calling templates
splashify calling template status --name <tpl>
splashify calling template create-call-button --name --body-text --button-label
                                              --phone-number [--language en]
splashify calling template create-permission --name --body-text [--language en]
splashify calling template set-default <template_id>
splashify calling send call-button --to --body-text --button-label --phone-number [--yes]
splashify calling send permission --to [--body-text | --type template --template '{…}'] [--yes]
splashify calling send template --to --name [--language] [--vars '[…]'] [--yes]
splashify calling send permission-template --to --name [--language] [--yes]
```

## Contacts

```
splashify contacts [--search "…"] [--tag vip] [--page N]
splashify contact <id>
splashify contact create --phone +91… [--name "…"] [--email "…"]
splashify contact update <id> [--name] [--email] [--website] [--notes]
                              [--opted-out true|false] [--tags vip,lead] [--data '{…}']
splashify contact delete|block|unblock <id>
splashify contact tag <id> --tags vip,lead       Add tags
splashify contact untag <id> --tags vip,lead     Remove tags
```

## Tags

```
splashify tags [--search "…"]
splashify tag create "VIP"
splashify tag rename <id> "Important"
splashify tag delete <id>                        Unmaps all contacts
```

## Segments

```
splashify segments [--search] [--page] [--limit]
splashify segments stats
splashify segment <id>
splashify segment <id> contacts [--page] [--limit]
splashify segment <id> count
splashify segment <id> refresh
splashify segment create --name "VIP" --filters '{…}' [--description "…"]
                         [--dynamic true|false] [--active true|false]
splashify segment update <id> [--name] [--description] [--filters] [--dynamic] [--active]
splashify segment delete <id>
```

## Attributes (custom contact columns)

```
splashify attributes [--search "…"]
splashify attribute <id>
splashify attribute create --label "Company" --type TEXT
                           [--options "a,b,c"] [--visible true|false]
                           [--required true|false] [--default "…"] [--help "…"]
splashify attribute update <id> [--label] [--type] [--visible] [--required]
                                [--options] [--default] [--help]
splashify attribute delete <id>
splashify attribute toggle-visibility <id>
splashify attribute reorder <id> up | down
```

## Account / billing / subscription

```
splashify account                Consolidated account details
splashify account info           Profile + plan (/app/me)
splashify account orgs           Organizations you belong to
splashify account invitations    Pending invitations received
splashify account sent-invitations
splashify account wallet         Wallet balance

splashify billing                Consolidated billing
splashify billing profile        GST profile + billing address
splashify billing invoices       Invoice list
splashify billing logs [--period all]

splashify subscription           Plan + add-ons + eligibility + available plans
splashify subscription status
splashify subscription plans
splashify subscription addons
```

## WABA

```
splashify waba                   Full WABA details
splashify waba sync              Refresh data from Meta
splashify waba update --about "…" --description "…" --email "…"
                     --address "…" --vertical RETAIL --websites https://…
splashify waba register-phone
splashify waba setup-status
splashify waba oba-status | oba-apply
splashify waba request-deletion
```

## Media

```
splashify media                  List uploaded files
splashify media list --type image|video|audio|document
splashify media storage          Storage quota
splashify media upload ./file
splashify media delete <media_id>
```

## Opt management

```
splashify opt                    Show full opt settings
splashify opt out | in           One side
splashify opt out add STOP UNSUBSCRIBE
splashify opt out remove STOP
splashify opt out response "<text>"
splashify opt out response-on | response-off
splashify opt in …               Same actions on the in side
```

## Team / agents

```
splashify team                   List members
splashify member <id>
splashify team add --name --email --country-code +91 --phone <digits>
                   [--role agent|manager]
                   [--all read_write | --permissions '{…}'
                    | --rw messages,contacts --read analytics --none wallet]
                   [--otp <code> | --no-verify]
splashify team verify <member_id> --otp <code>
splashify team resend-otp <member_id>
splashify team resend-invite <member_id>
splashify team update <member_id> [--role] [--all|--permissions|--rw|--read|--none]
splashify team set-role <member_id> agent | manager
splashify team set-permissions <member_id> […]
splashify team delete <member_id>
```

## Canned messages

```
splashify canned                 List canned messages
splashify canned list [--search] [--type TEXT|IMAGE|VIDEO|AUDIO|DOCUMENT]
splashify canned <id>
splashify canned create --name "…" --type TEXT --text "…"
                        [--shortcut "/hi"] [--description "…"]
splashify canned create --name "…" --type IMAGE --url https://… [--caption "…"]
splashify canned create --name "…" --type DOCUMENT --url https://…
                        --filename file.pdf [--caption "…"]
splashify canned create --name "…" --type AUDIO --url https://…
splashify canned create --name "…" --type INTERACTIVE_LIST --payload '{…}'
splashify canned update <id> [flags as above]
splashify canned toggle <id>     Flip is_active
splashify canned delete <id>
```

## Flows

```
splashify flows                  List every flow
splashify flows sync             Pull fresh data from Meta
splashify flows create-url       Print URL to create a flow in Meta Flow Builder
splashify flow <id>
splashify flow <id> responses [--page] [--limit]
splashify flow <id> deprecate
splashify flow response <response_id>
```

## Devices / sessions

```
splashify devices                 List active login sessions
splashify devices list [--platform web|android|ios|mobile] [--ip <addr>]
splashify session <session_id>
splashify devices logout <session_id>
splashify devices logout-all [--yes] [--platform …] [--ip …]
```

## Activity logs

```
splashify activity                Latest 100 logs
splashify activity --limit 200
splashify activity --action login
splashify activity --entity contact [--entity-id <id>]
splashify activity --actor <user_id>
splashify activity --search "…"
```

## AI credits

```
splashify credits                 AI credits + voice AI rate + agents
splashify credits ai              AI credit balance
splashify credits transactions    AI credit ledger
splashify credits voice           Voice AI rate, balance, trial, minutes
splashify credits agents          Voice AI agents
```

## Support tickets

```
splashify support                 List tickets
splashify support list [--status open|in_progress|waiting_for_user|resolved|closed]
                       [--search "…"]
splashify ticket <id>
splashify ticket create --title "…" --description "…"
                        --category bug|billing|feature_request|account|general
                        [--priority low|medium|high|urgent]
splashify ticket <id> reply "<message>"
splashify ticket <id> close
```

## Expenses

```
splashify expenses                Summary (default 30d)
splashify expenses summary    [--period 7d|30d|3m|6m|all]
splashify expenses categories [--period …]
splashify expenses countries  [--period …]
splashify expenses trends     [--period …]
splashify expenses logs       [--period …] [--limit N] [--category …]
                              [--country IN] [--free-trial true|false]
splashify expenses export     [--period …] [--limit N] [--out file.csv]
```

## Email marketing

```
splashify email                   Dashboard stats
splashify email stats
splashify email domains
splashify email domain <domain>
splashify email domain add|verify|delete <domain>
splashify email templates
splashify email template <id>
splashify email template create --name --subject --file ./template.json
splashify email template update <id> [--name] [--subject] [--file|--data]
splashify email template delete <id>
splashify email template preview --file ./template.json [--vars '{…}']
splashify email audience
splashify email audience stats
splashify email audience contacts [--status] [--search]
splashify email audience contact <id>
splashify email audience contact add --emails "a@x.com,b@x.com"
                                     [--metadata '{…}'] [--segments id1,id2]
splashify email audience contact update <id> [--status] [--metadata]
splashify email audience contact delete <id>
splashify email audience segments
splashify email audience segment <id>
splashify email audience segment create --name [--description]
splashify email audience segment update <id> [--name] [--description]
splashify email audience segment delete <id>
splashify email audience segment <id> add-contacts <id1>,<id2>,…
splashify email audience segment <id> remove-contacts <id1>,<id2>,…
splashify email campaigns
splashify email campaign <id>
splashify email campaign create --name --template-id --from-name --from-email
                                [--reply-to] [--segment-ids|--contact-ids]
                                [--scheduled-at]
splashify email campaign send <id>
splashify email campaign cancel <id>
```

## Broadcasts

```
splashify broadcasts [--status] [--search] [--page] [--limit]
splashify broadcast stats [--period 7d|30d|all]
splashify broadcast <id> [--recompute]
splashify broadcast <id> messages [--status] [--channel whatsapp|rcs]
                                  [--page] [--limit] [--search] [--cumulative true|false]
splashify broadcast <id> cohorts
splashify broadcast <id> export [--status ALL] [--csv]
splashify broadcast <id> cancel | restart | send-now
splashify broadcast <id> rebroadcast --cohort failed|sent|delivered|read|all
                                     [--name] [--template] [--template-id] [--params]
                                     [--media-url] [--send-at] [--yes]
splashify broadcast create --name "…" --template <name>
                           --category MARKETING --language en
                           [--template-id] [--params '{…}'] [--media-url …]
                           (--segment-ids id1,id2 | --contact-ids id1,id2)
                           [--send-type now|scheduled] [--scheduled-at 2026-06-01T10:00:00Z]
                           [--rate-limit N] [--batch-size N]
                           [--rcs | --rcs-fallback-template-id …
                                  --rcs-fallback-template-name …]
                           [--yes]
splashify broadcast <id> progress         Follow SSE until terminal status
splashify broadcast <id> progress --once  Snapshot
splashify broadcast <id> progress --max N
```

## WhatsApp templates

```
splashify templates                List every WhatsApp template
splashify template <id>
splashify templates sync           Sync ALL templates from Meta
splashify templates sync <id>      Sync one
splashify templates upload-media <file>
splashify templates create --name "…" --language en --category MARKETING --text "…"
splashify templates create --name "…" --language en --category MARKETING
                            --components '[{"type":"BODY","text":"…"}]'
splashify templates create --file ./template.json
splashify templates delete <id> [--name <template_name>]
```

## AI agents

```
splashify ai-agents                List every AI agent
splashify ai-agent <id>
splashify ai-agent create --name "…" --agent-type support
                          [--channel whatsapp|instagram] [--industry …]
                          [--use-case …] [--role …] [--goal …] [--instructions …]
splashify ai-agent update <id> [--name] [--role] [--goal] [--instructions]
                                [--status] [--tone] [--ai-provider] [--ai-model]
                                [--temperature 0.7] [--processing-msg true|false]
splashify ai-agent set-default <id> | unset-default <id>
splashify ai-agent delete <id>
splashify ai-agent knowledge <id> [list]
splashify ai-agent knowledge <id> upload <file>    (.pdf|.docx|.md|.txt, ≤15MB)
splashify ai-agent knowledge <id> delete <file_id>
```

## Integrations

```
splashify integrations             List per-slug configs (default)
splashify integrations configs
splashify integrations config <slug>
splashify integrations config <slug> save [--enabled true|false] [--template <id>]
                                           [--template-name] [--template-language]
                                           [--config '{…}'] [--vars '{…}']
                                           [--phone-field …] [--events '{…}']
splashify integrations config <slug> delete
splashify integrations accounts
splashify integrations account <id> disconnect
splashify integrations token       Mint a connect-token for OAuth callbacks
splashify integrations logs [--limit N] [--slug <slug>]
splashify integrations log <log_id>
```

## IP allowlist

```
splashify allowed-ips              List entries
splashify allowed-ips add --name "Office" --mode single --ip 203.0.113.4
splashify allowed-ips add --name "VPN" --mode range
                          --start 10.0.0.0 --end 10.0.0.255
splashify allowed-ips delete <entry_id>
```

## CTWA (Click-to-WhatsApp Ads)

```
splashify ctwa                     Show CAPI status
splashify ctwa capi
splashify ctwa capi save [--dataset-id] [--lead-enabled] [--lead-trigger]
                          [--lead-tag] [--purchase-enabled] [--purchase-trigger]
                          [--purchase-tag] [--purchase-currency] [--purchase-value]
splashify ctwa capi send-event --event-type lead|purchase --phone +91…
                                [--value 99.99] [--currency INR]
splashify ctwa exchange-code --code <code> [--granted-scopes '[…]'] [--redirect-uri …]
splashify ctwa refresh-token
splashify ctwa ads
splashify ctwa ad <ad_id>
```

## Profile / username / company / dashboard

```
splashify profile                  Show current profile
splashify profile update --first-name "…" --last-name "…"
splashify profile change-password --current <old> --new <new>
splashify profile whatsapp send-otp --country-code +91 --mobile <digits>
splashify profile whatsapp verify --country-code +91 --mobile <digits> --otp <code>
splashify profile picture ./avatar.png
splashify profile 2fa enable | disable

splashify username                 Current username + status
splashify username suggestions
splashify username adopt mybrand
splashify username delete

splashify company                  Show company profile
splashify company update --company-name "…" --industry "…"
                         --company-size <range> --website https://…
                         --country IN --state KA --pincode 560001
                         --timezone Asia/Calcutta

splashify dashboard                Consolidated snapshot
splashify dashboard setup-status
splashify dashboard whatsapp-status
splashify dashboard kyc
```

## Maya (in-app assistant)

```
splashify maya chat "How do I create a broadcast?"
splashify maya chat --thread-id <id> "follow-up question"
splashify maya feedback --reply-id <id> --rating up|down [--comment "…"]
```

## Analytics

```
splashify analytics                Summary
splashify analytics trends         Daily series
```

## Wallet

```
splashify wallet                   Balance
splashify wallet transactions      Ledger
```

## Generic API passthrough

```
splashify api GET  /app/contacts?page=2
splashify api POST /app/messages/send-text --data '{"phone":"+91…","message":"hi"}'
splashify api PUT  /app/<path> --data '{…}'
splashify api DELETE /app/<path>
```

Reach any `/api/v1/app/*` endpoint. The path may start with `/app/…` or
`/api/v1/app/…`; both work. The `--data` value is a JSON string. Use
this only when no typed wrapper above covers the action.
