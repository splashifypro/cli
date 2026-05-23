package main

import (
	"fmt"
	"strings"
)

// This file implements `splashify username` — the CLI mirror of the app's
// /settings/username page. It manages the WhatsApp business username
// (vanity handle that appears in WhatsApp Search and on the business
// profile card).
//
//	splashify username                    show current username + status
//	splashify username suggestions        list backend-suggested usernames
//	splashify username adopt <name>       claim a username (case-insensitive)
//	splashify username delete             release the current username
//
// Backed by /api/v1/app/phone/username[/suggestions]. The backend enforces
// Meta's vanity-name rules (length, characters, profanity); the CLI just
// forwards.

func cmdUsername(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "show", "get", "status":
		return runReq("GET", "/app/phone/username", nil)

	case "suggestions", "suggest", "ideas":
		return runReq("GET", "/app/phone/username_suggestions", nil)

	case "adopt", "claim", "set", "reserve":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify username adopt <name>")
		}
		name := strings.TrimSpace(args[1])
		if name == "" {
			return fmt.Errorf("username cannot be empty")
		}
		return runReq("POST", "/app/phone/username", map[string]any{"username": name})

	case "delete", "release", "remove", "rm":
		return runReq("DELETE", "/app/phone/username", nil)

	default:
		return fmt.Errorf("unknown username subcommand: %s\n"+
			"run: splashify username  (or suggestions | adopt <name> | delete)", sub)
	}
}
