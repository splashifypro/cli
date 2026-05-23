package main

import (
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify maya` — chat with Maya, Splashify's
// in-app AI assistant, from the terminal. Mirrors the chat dialog the
// app exposes on /dashboard and the right-rail widget.
//
//	splashify maya chat "How do I create a broadcast?"
//	splashify maya feedback --reply-id <id> --rating up|down [--comment "…"]
//
// Backed by /api/v1/app/maya/chat and /api/v1/app/maya/feedback. Reply
// payload shape and tool-call routing live server-side — the CLI is just
// a transport.
//
// Note: the app's /api/ai/rewrite and /api/ai/generate-template endpoints
// proxy to Sarvam directly from the Next.js BFF (not the partnersapi
// backend), so they're intentionally NOT exposed here — they don't share
// the oc_live_ auth surface and can't be called with a CLI token.

func cmdMaya(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify maya <chat|feedback> …")
	}
	switch args[0] {
	case "chat", "ask", "send":
		return cmdMayaChat(args[1:])

	case "feedback", "rate":
		return cmdMayaFeedback(args[1:])

	default:
		return fmt.Errorf("unknown maya subcommand: %s\nrun: splashify maya chat \"…\"", args[0])
	}
}

func cmdMayaChat(args []string) error {
	fs := flag.NewFlagSet("maya chat", flag.ContinueOnError)
	message := fs.String("message", "", "message text (alternative to positional)")
	thread := fs.String("thread-id", "", "optional thread id to continue an existing conversation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Allow positional message: `splashify maya chat "How do I …?"`
	msg := *message
	if msg == "" {
		msg = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if msg == "" {
		return fmt.Errorf("usage: splashify maya chat \"<your question>\"  (or --message \"…\")")
	}
	body := map[string]any{"message": msg}
	if *thread != "" {
		body["thread_id"] = *thread
	}
	return runReq("POST", "/app/maya/chat", body)
}

func cmdMayaFeedback(args []string) error {
	fs := flag.NewFlagSet("maya feedback", flag.ContinueOnError)
	replyID := fs.String("reply-id", "", "id of the Maya reply being rated (required)")
	rating := fs.String("rating", "", "up | down (required)")
	comment := fs.String("comment", "", "optional free-text comment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *replyID == "" || *rating == "" {
		return fmt.Errorf("usage: splashify maya feedback --reply-id <id> --rating up|down [--comment \"…\"]")
	}
	r := strings.ToLower(*rating)
	if r != "up" && r != "down" {
		return fmt.Errorf("--rating must be 'up' or 'down'")
	}
	body := map[string]any{
		"reply_id": *replyID,
		"rating":   r,
	}
	if *comment != "" {
		body["comment"] = *comment
	}
	return runReq("POST", "/app/maya/feedback", body)
}
