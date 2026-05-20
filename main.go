// Command splashify is the CLI for the partnersapi platform. Its headline job
// is linking a user's messaging account to OpenClaw (the local-first AI
// assistant) so the assistant can perform every app action on their behalf.
//
//	splashify connect          connect this machine with an oc_live_ token
//	splashify whoami           show the connected account
//	splashify token list       list access tokens
//	splashify token create     create a new access token
//	splashify token revoke ID  revoke an access token
//	splashify link openclaw    register the splashify MCP server with OpenClaw
//	splashify mcp-config       print the OpenClaw MCP config without applying it
//	splashify doctor           diagnose the local setup
package main

import (
	"fmt"
	"os"
)

// version is populated at build time via:
//
//	go build -ldflags "-X main.version=v0.1.0" ./...
//
// A "dev" default lets `go run`/`go build` work without flags.
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	// Async update check — fire-and-forget, never blocks the user's command.
	// Suppressed entirely when running a dev build or when the user opts out.
	updateCheckDone := startUpdateCheck()

	var err error
	switch args[0] {
	case "connect":
		err = cmdConnect(args[1:])
	case "whoami":
		err = cmdWhoami(args[1:])
	case "token":
		err = cmdToken(args[1:])
	case "link":
		err = cmdLink(args[1:])
	case "mcp-config":
		err = cmdMCPConfig(args[1:])
	case "doctor":
		err = cmdDoctor(args[1:])

	// ── task commands — perform app work directly from the prompt ──────────
	case "message", "msg":
		err = cmdMessage(args[1:])
	case "conversations", "chats":
		err = cmdConversations(args[1:])
	case "conversation", "chat":
		err = cmdConversation(args[1:])
	case "unread":
		err = cmdUnread(args[1:])
	case "contacts":
		err = cmdContacts(args[1:])
	case "contact":
		err = cmdContact(args[1:])
	case "broadcasts":
		err = cmdBroadcasts(args[1:])
	case "broadcast":
		err = cmdBroadcast(args[1:])
	case "waba":
		err = cmdWaba(args[1:])
	case "account":
		err = cmdAccount(args[1:])
	case "billing":
		err = cmdBilling(args[1:])
	case "subscription", "subscriptions", "sub":
		err = cmdSubscription(args[1:])
	case "media":
		err = cmdMedia(args[1:])
	case "opt", "opt-management":
		err = cmdOpt(args[1:])
	case "tags":
		err = cmdTags(args[1:])
	case "tag":
		err = cmdTag(args[1:])
	case "segments":
		err = cmdSegments(args[1:])
	case "segment":
		err = cmdSegment(args[1:])
	case "templates":
		err = cmdTemplates(args[1:])
	case "analytics":
		err = cmdAnalytics(args[1:])
	case "wallet":
		err = cmdWallet(args[1:])
	case "api":
		err = cmdAPI(args[1:])

	case "version", "--version", "-v":
		fmt.Println("splashify", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		usage()
		os.Exit(1)
	}

	// Drain the update check (with a tight cap) so we can print a hint after
	// the command's output without ever delaying the user noticeably.
	finishUpdateCheck(updateCheckDone)

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`splashify — control your messaging account from the command line

Setup:
  splashify connect              Connect this machine with an oc_live_ token
  splashify whoami               Show the connected account (compact)
  splashify account              Full account details (read-only)
  splashify doctor               Diagnose the local setup

Access tokens:
  splashify token list           List your access tokens
  splashify token create         Create a new access token
  splashify token revoke <id>    Revoke an access token

OpenClaw:
  splashify link openclaw        Register the splashify MCP server with OpenClaw
  splashify mcp-config           Print the OpenClaw MCP config (no changes made)

Messaging:
  splashify message send --to +91… --text "…"
  splashify message template --to +91… --name <tpl> [--lang en] [--vars '[…]']
  splashify message media --to +91… --type image --url <url> [--caption …]
  splashify conversations [--page N] [--status open|resolved]
  splashify conversation <id> [resolve]
  splashify unread

Contacts:
  splashify contacts [--search …] [--tag …] [--page N]
  splashify contact <id>
  splashify contact create --phone +91… [--name …] [--email …]
  splashify contact delete|block|unblock <id>
  splashify contact tag <id> --tags vip,lead

Account (read-only):
  splashify account                         Consolidated account details
  splashify account info                    Profile + plan (/app/me)
  splashify account orgs                    Organizations you belong to
  splashify account invitations             Pending invitations received
  splashify account sent-invitations        Invitations you have sent
  splashify account wallet                  Wallet balance

Billing (read-only):
  splashify billing                         Consolidated billing view
  splashify billing profile                 GST profile + billing address
  splashify billing invoices                Invoice list
  splashify billing logs [--period all]     Billing log entries

Subscription (read-only):
  splashify subscription                    Plan + add-ons + eligibility + available plans
  splashify subscription status             Current plan + add-ons
  splashify subscription plans              Available plans
  splashify subscription addons             Add-ons only

WhatsApp Business Account:
  splashify waba                            Show full WABA details (phone, profile, status…)
  splashify waba sync                       Refresh data from Meta
  splashify waba update --about "…" --description "…" --email "…" \
                        --address "…" --vertical RETAIL --websites https://…
  splashify waba register-phone             (Re-)register the phone with Meta
  splashify waba setup-status               High-level setup checklist
  splashify waba oba-status | oba-apply     Official Business Account
  splashify waba request-deletion           Request WABA deletion

Media library:
  splashify media                           List every uploaded file (URL + details)
  splashify media list --type image         Filter by image|video|audio|document
  splashify media storage                   Storage quota + usage
  splashify media upload ./logo.png         Upload a file from disk
  splashify media delete <media_id>         Delete a media row

Opt-out / Opt-in keywords:
  splashify opt                             Show full opt settings
  splashify opt out | in                    Show just one side
  splashify opt out add STOP UNSUBSCRIBE    Add keywords to opt-out list
  splashify opt out remove STOP             Remove keywords from opt-out list
  splashify opt out response "<text>"       Set the auto-response message
  splashify opt out response-on / off       Enable / disable the auto-response
  splashify opt in  …                       Same actions on the in side

Tags (CRUD on the tag library):
  splashify tags                            List every tag
  splashify tags --search vip               Substring filter on tag name
  splashify tag create "VIP"                Create a tag
  splashify tag rename <id> "Important"     Rename a tag
  splashify tag delete <id>                 Delete a tag (unmaps all contacts)

Segments (CRUD + introspection):
  splashify segments [--search …] [--page N] [--limit N]
  splashify segments stats                  Overall segment counts
  splashify segment <id>                    Get one segment
  splashify segment <id> contacts [--page] [--limit]
  splashify segment <id> count              Current member count
  splashify segment <id> refresh            Recompute member count
  splashify segment create --name "VIP" --filters '{…}' [--description …] \
                          [--dynamic true|false] [--active true|false]
  splashify segment update <id> [--name] [--description] [--filters] \
                                [--dynamic] [--active]
  splashify segment delete <id>

Broadcasts & more:
  splashify broadcasts
  splashify broadcast <id> | stats
  splashify broadcast create --name … --template … --audience-type segment …
  splashify templates
  splashify analytics [trends]
  splashify wallet [transactions]

Everything else (any app endpoint):
  splashify api GET  /app/contacts?page=2
  splashify api POST /app/messages/send-text --data '{"phone":"+91…","message":"hi"}'

Get an access token from the app: Settings → Developer → Access Tokens,
or run "splashify connect" and follow the prompts.
`)
}
