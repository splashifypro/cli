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
  splashify whoami               Show the connected account
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
