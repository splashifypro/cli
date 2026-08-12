package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify webhooks` — the CLI mirror of webhook
// management. It covers:
//
//	webhooks                    list all registered webhooks
//	webhooks create --url <url> --events event1,event2 ...
//	                            register a new webhook
//	webhooks <id>               show one webhook
//	webhooks <id> test          send a synthetic test event
//	webhooks <id> rotate-secret rotate the signing secret
//	webhooks <id> delete        unregister a webhook
//
// Webhook events may include: incoming_message, message_sent, message_delivered,
// message_read, message_failed, team_member_assigned, team_member_unassigned,
// wallet_low, wallet_empty, etc.

// ─── splashify webhooks ──────────────────────────────────────────────────────

func cmdWebhooks(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/webhooks", nil)
	}
	switch args[0] {
	case "create", "new", "add":
		return cmdWebhookCreate(args[1:])
	default:
		return cmdWebhook(args)
	}
}

// cmdWebhookCreate registers a new webhook. Required: url and events.
// Optional: description.
//
//	splashify webhooks create --url "https://..." --events "incoming_message,message_delivered"
//	                           [--description "..."]
func cmdWebhookCreate(args []string) error {
	fs := flag.NewFlagSet("webhooks create", flag.ContinueOnError)
	url := fs.String("url", "", "webhook URL (required)")
	events := fs.String("events", "", "comma-separated event types (required)")
	desc := fs.String("description", "", "optional description")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || *events == "" {
		return fmt.Errorf(`usage: splashify webhooks create --url "https://..." --events "incoming_message,message_delivered" [--description "..."]`)
	}

	eventList := splitTags(*events)
	if len(eventList) == 0 {
		return fmt.Errorf("--events must contain at least one event type")
	}

	body := map[string]any{
		"url":    *url,
		"events": eventList,
	}
	if *desc != "" {
		body["description"] = *desc
	}

	return runReq("POST", "/app/webhooks", body)
}

// cmdWebhook handles per-webhook actions: show, test, rotate-secret, delete.
//
//	splashify webhooks <id>                show one webhook
//	splashify webhooks <id> test           send synthetic event
//	splashify webhooks <id> rotate-secret  get new signing secret
//	splashify webhooks <id> delete         unregister
func cmdWebhook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify webhooks <webhook_id> [test|rotate-secret|delete]")
	}

	id := args[0]
	if len(args) == 1 {
		return runReq("GET", "/app/webhooks/"+id, nil)
	}

	switch args[1] {
	case "test", "send-test":
		return runReq("POST", "/app/webhooks/"+id+"/test", nil)

	case "rotate", "rotate-secret", "rotate-key", "new-secret":
		return runReq("POST", "/app/webhooks/"+id+"/rotate-secret", nil)

	case "delete", "remove", "rm", "unregister":
		return runReq("DELETE", "/app/webhooks/"+id, nil)

	case "update", "edit", "patch":
		return cmdWebhookUpdate(id, args[2:])

	default:
		return fmt.Errorf("unknown webhook action: %s\nrun: splashify webhooks", args[1])
	}
}

// cmdWebhookUpdate PATCHes a webhook. Accepts partial updates: --url, --events,
// --description.
//
//	splashify webhooks <id> update --url "https://..." --events "..."
func cmdWebhookUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("webhooks update", flag.ContinueOnError)
	url := fs.String("url", "", "new webhook URL")
	events := fs.String("events", "", "comma-separated event types")
	desc := fs.String("description", "", "new description")
	if err := fs.Parse(args); err != nil {
		return err
	}

	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify webhooks %s update [--url ...] [--events ...] [--description ...]", id)
	}

	body := map[string]any{}
	if passed["url"] {
		body["url"] = *url
	}
	if passed["events"] {
		eventList := splitTags(*events)
		if len(eventList) == 0 {
			return fmt.Errorf("--events must contain at least one event type")
		}
		body["events"] = eventList
	}
	if passed["description"] {
		body["description"] = *desc
	}

	return runReq("PATCH", "/app/webhooks/"+id, body)
}
