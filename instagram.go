package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify instagram` (alias: `ig`) — the CLI mirror of
// the app's /instagram-automation page. It covers the full surface:
//
//	account / oauth-url / connect / disconnect
//	media / logs / window / sync / dm
//	rules CRUD (list / get / create / update / toggle / delete)
//
// All commands hit /api/v1/app/instagram/* with the stored oc_live_ token.

// dmMediaTypes is the validated set the backend accepts for both rule DMs
// and the SendDM endpoint.
var dmMediaTypes = map[string]bool{"image": true, "video": true, "audio": true}

// ─── splashify instagram ─────────────────────────────────────────────────────

func cmdInstagram(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "account", "info", "status":
		return runReq("GET", "/app/instagram/account", nil)

	case "oauth-url", "oauth", "authorize-url":
		return runReq("GET", "/app/instagram/oauth-url", nil)

	case "connect":
		return cmdIGConnect(args[1:])

	case "disconnect":
		return runReq("DELETE", "/app/instagram/disconnect", nil)

	case "media":
		return cmdIGMedia(args[1:])

	case "logs":
		return cmdIGLogs(args[1:])

	case "window", "messaging-window":
		return cmdIGWindow(args[1:])

	case "sync", "sync-conversations":
		return runReq("POST", "/app/instagram/sync-conversations", nil)

	case "dm", "message", "send":
		return cmdIGSendDM(args[1:])

	case "rules":
		return cmdIGRules(args[1:])

	case "rule":
		return cmdIGRule(args[1:])

	default:
		return fmt.Errorf("unknown instagram subcommand: %s\nrun: splashify instagram", sub)
	}
}

// ─── connect ─────────────────────────────────────────────────────────────────

// cmdIGConnect completes the Instagram OAuth handshake server-side. The
// `--code` value comes from the redirect after the user opens the
// `oauth-url` in a browser; in a headless flow they paste the `code` query
// parameter back here.
func cmdIGConnect(args []string) error {
	fs := flag.NewFlagSet("instagram connect", flag.ContinueOnError)
	code := fs.String("code", "", "authorization code from the Instagram redirect (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*code) == "" {
		return fmt.Errorf("usage: splashify instagram connect --code <code>\n\nGet the code by:\n  1. splashify instagram oauth-url\n  2. open the authorize_url in a browser\n  3. after redirect, copy the `code` query parameter from the URL")
	}
	return runReq("POST", "/app/instagram/connect", map[string]any{"code": *code})
}

// ─── media ───────────────────────────────────────────────────────────────────

// cmdIGMedia lists the IG posts on the connected account. These are the
// candidates you target with a rule's --media-id.
func cmdIGMedia(args []string) error {
	fs := flag.NewFlagSet("instagram media", flag.ContinueOnError)
	limit := fs.String("limit", "50", "max posts to return (Meta caps this at ~25 per page)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/instagram/media", map[string]string{"limit": *limit}), nil)
}

// ─── logs ────────────────────────────────────────────────────────────────────

func cmdIGLogs(args []string) error {
	fs := flag.NewFlagSet("instagram logs", flag.ContinueOnError)
	limit := fs.String("limit", "50", "max log rows (newest first)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/instagram/logs", map[string]string{"limit": *limit}), nil)
}

// ─── window ──────────────────────────────────────────────────────────────────

// cmdIGWindow reports Meta's 24h / 7d / never reply window for a given
// Instagram conversation. The composer in /messages calls this before every
// send to gate the input; over the CLI it's the same source of truth.
func cmdIGWindow(args []string) error {
	fs := flag.NewFlagSet("instagram window", flag.ContinueOnError)
	conv := fs.String("conversation", "", "conversation_id (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*conv) == "" {
		return fmt.Errorf("usage: splashify instagram window --conversation <conversation_id>")
	}
	return runReq("GET", withQuery("/app/instagram/messages/window", map[string]string{
		"conversation_id": *conv,
	}), nil)
}

// ─── dm ──────────────────────────────────────────────────────────────────────

// cmdIGSendDM sends an Instagram DM in an existing conversation. Either
// --message or --media-url (with --media-type) is required; both can be
// passed and IG renders them as two bubbles, matching the SendDM handler's
// dual-row storage.
func cmdIGSendDM(args []string) error {
	fs := flag.NewFlagSet("instagram dm", flag.ContinueOnError)
	conv := fs.String("conversation", "", "conversation_id (required)")
	text := fs.String("message", "", "text body (required if no --media-url)")
	mediaURL := fs.String("media-url", "", "public media URL (image/video/audio)")
	mediaType := fs.String("media-type", "", "image | video | audio (required when --media-url is set)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*conv) == "" {
		return fmt.Errorf("usage: splashify instagram dm --conversation <id> --message \"…\"\n       splashify instagram dm --conversation <id> --media-url https://… --media-type image [--message \"…\"]")
	}
	if strings.TrimSpace(*text) == "" && strings.TrimSpace(*mediaURL) == "" {
		return fmt.Errorf("--message or --media-url is required")
	}
	if *mediaURL != "" {
		mt := strings.ToLower(strings.TrimSpace(*mediaType))
		if mt != "" && !dmMediaTypes[mt] {
			return fmt.Errorf("--media-type must be image | video | audio — got %q", *mediaType)
		}
	}
	body := map[string]any{"conversation_id": *conv}
	if *text != "" {
		body["message"] = *text
	}
	if *mediaURL != "" {
		body["media_url"] = *mediaURL
		body["media_type"] = strings.ToLower(strings.TrimSpace(*mediaType))
	}
	return runReq("POST", "/app/instagram/messages", body)
}

// ─── rules (plural) ──────────────────────────────────────────────────────────

func cmdIGRules(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/instagram/rules", nil)
	default:
		return fmt.Errorf("unknown rules subcommand: %s\nrun: splashify instagram rules", sub)
	}
}

// ─── rule (singular) ─────────────────────────────────────────────────────────

func cmdIGRule(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify instagram rule <id|create|update|delete|toggle> …")
	}
	switch args[0] {
	case "create", "add", "new":
		return cmdIGRuleCreate(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify instagram rule update <rule_id> [--keyword …] [--dm-message …] […]")
		}
		return cmdIGRuleUpdate(args[1], args[2:])
	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify instagram rule delete <rule_id>")
		}
		return runReq("DELETE", "/app/instagram/rules/"+args[1], nil)
	case "toggle":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify instagram rule toggle <rule_id>")
		}
		return cmdIGRuleToggle(args[1])
	}

	// Default: treat the first arg as a rule_id and show it. The backend has
	// no per-id GET, so we fetch the list and filter — same pattern as
	// canned <id>, member <id>, etc.
	id := args[0]
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/instagram/rules", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success bool             `json:"success"`
		Rules   []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, r := range list.Rules {
		if rid, _ := r["rule_id"].(string); rid == id {
			encoded, _ := json.MarshalIndent(r, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("instagram rule %s not found", id)
}

// ─── rule create ─────────────────────────────────────────────────────────────

// cmdIGRuleCreate POSTs a new comment-to-DM automation rule. Required flags
// are --media-id, --keyword, --dm-message (matching the backend's
// AutomationRuleRequest validation).
func cmdIGRuleCreate(args []string) error {
	fs := flag.NewFlagSet("instagram rule create", flag.ContinueOnError)

	mediaID := fs.String("media-id", "", "IG media (post) id to attach this rule to (required)")
	keyword := fs.String("keyword", "", "trigger_keyword — matched case-insensitively in comments (required)")
	dmMessage := fs.String("dm-message", "", "the DM text sent when a comment matches (required)")

	mediaCaption := fs.String("media-caption", "", "cached caption of the post — shown in the rules list")
	mediaThumbnail := fs.String("media-thumbnail", "", "cached thumbnail URL of the post")
	mediaPermalink := fs.String("media-permalink", "", "cached permalink URL of the post")

	dmMediaURL := fs.String("dm-media-url", "", "optional media attachment URL on the DM")
	dmMediaType := fs.String("dm-media-type", "", "image | video | audio (paired with --dm-media-url)")

	commentReply := fs.String("comment-reply", "", "optional public reply to leave on the matching comment")
	active := fs.String("active", "true", "true|false (default: true)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*mediaID) == "" || strings.TrimSpace(*keyword) == "" || strings.TrimSpace(*dmMessage) == "" {
		return fmt.Errorf("usage: splashify instagram rule create --media-id <id> --keyword \"…\" --dm-message \"…\" [optional flags]")
	}

	isActive, err := parseBool(*active)
	if err != nil {
		return fmt.Errorf("--active must be true or false")
	}

	body := map[string]any{
		"media_id":        *mediaID,
		"media_caption":   *mediaCaption,
		"media_thumbnail": *mediaThumbnail,
		"media_permalink": *mediaPermalink,
		"trigger_keyword": *keyword,
		"dm_message":      *dmMessage,
		"dm_media_url":    *dmMediaURL,
		"dm_media_type":   *dmMediaType,
		"comment_reply":   *commentReply,
		"is_active":       isActive,
	}
	return runReq("POST", "/app/instagram/rules", body)
}

// ─── rule update ─────────────────────────────────────────────────────────────

// cmdIGRuleUpdate is read-modify-write: the backend's PATCH writes every
// editable column on every call (trigger_keyword, dm_message, dm_media_url,
// dm_media_type, comment_reply, is_active), so absent flags would blank the
// other columns. We load the rule first, overlay only the explicitly-passed
// flags, then submit the full body. Same pattern as canned/waba/attribute.
func cmdIGRuleUpdate(ruleID string, args []string) error {
	fs := flag.NewFlagSet("instagram rule update", flag.ContinueOnError)

	keyword := fs.String("keyword", "", "new trigger_keyword")
	dmMessage := fs.String("dm-message", "", "new DM body")
	dmMediaURL := fs.String("dm-media-url", "", `new DM media URL (pass "" to clear)`)
	dmMediaType := fs.String("dm-media-type", "", "image | video | audio")
	commentReply := fs.String("comment-reply", "", `new comment reply (pass "" to clear)`)
	active := fs.String("active", "", "true | false")

	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify instagram rule update <rule_id> [--keyword …] [--dm-message …] [--dm-media-url …] [--dm-media-type …] [--comment-reply …] [--active true|false]")
	}

	current, err := loadIGRule(ruleID)
	if err != nil {
		return err
	}

	body := map[string]any{
		"media_id":        getString(current, "media_id"),
		"media_caption":   getString(current, "media_caption"),
		"media_thumbnail": getString(current, "media_thumbnail"),
		"media_permalink": getString(current, "media_permalink"),
		"trigger_keyword": getString(current, "trigger_keyword"),
		"dm_message":      getString(current, "dm_message"),
		"dm_media_url":    getString(current, "dm_media_url"),
		"dm_media_type":   getString(current, "dm_media_type"),
		"comment_reply":   getString(current, "comment_reply"),
		"is_active":       getBool(current, "is_active"),
	}

	if passed["keyword"] {
		body["trigger_keyword"] = *keyword
	}
	if passed["dm-message"] {
		body["dm_message"] = *dmMessage
	}
	if passed["dm-media-url"] {
		body["dm_media_url"] = *dmMediaURL
	}
	if passed["dm-media-type"] {
		mt := strings.ToLower(strings.TrimSpace(*dmMediaType))
		if mt != "" && !dmMediaTypes[mt] {
			return fmt.Errorf("--dm-media-type must be image | video | audio — got %q", *dmMediaType)
		}
		body["dm_media_type"] = mt
	}
	if passed["comment-reply"] {
		body["comment_reply"] = *commentReply
	}
	if passed["active"] {
		b, err := parseBool(*active)
		if err != nil {
			return fmt.Errorf("--active must be true or false")
		}
		body["is_active"] = b
	}

	return runReq("PATCH", "/app/instagram/rules/"+ruleID, body)
}

// ─── rule toggle ─────────────────────────────────────────────────────────────

// cmdIGRuleToggle is a convenience that flips is_active. The backend has no
// dedicated toggle endpoint for IG rules (unlike canned messages), so we
// load + flip + PATCH — same RMW path as update.
func cmdIGRuleToggle(ruleID string) error {
	current, err := loadIGRule(ruleID)
	if err != nil {
		return err
	}
	body := map[string]any{
		"media_id":        getString(current, "media_id"),
		"media_caption":   getString(current, "media_caption"),
		"media_thumbnail": getString(current, "media_thumbnail"),
		"media_permalink": getString(current, "media_permalink"),
		"trigger_keyword": getString(current, "trigger_keyword"),
		"dm_message":      getString(current, "dm_message"),
		"dm_media_url":    getString(current, "dm_media_url"),
		"dm_media_type":   getString(current, "dm_media_type"),
		"comment_reply":   getString(current, "comment_reply"),
		"is_active":       !getBool(current, "is_active"),
	}
	return runReq("PATCH", "/app/instagram/rules/"+ruleID, body)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// loadIGRule fetches the full rule list and filters to the one matching
// ruleID. The backend has no per-id GET, so this is the standard fallback
// (same pattern as `canned <id>`, `member <id>`, etc.).
func loadIGRule(ruleID string) (map[string]any, error) {
	cfg, err := requireConfig()
	if err != nil {
		return nil, err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/instagram/rules", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Success bool             `json:"success"`
		Rules   []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decode instagram rules list: %w", err)
	}
	for _, r := range list.Rules {
		if rid, _ := r["rule_id"].(string); rid == ruleID {
			return r, nil
		}
	}
	return nil, fmt.Errorf("instagram rule %s not found", ruleID)
}

func getString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func getBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}
