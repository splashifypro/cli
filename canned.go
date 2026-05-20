package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify canned` (aliases: cm, cms, canned-messages) —
// the CLI mirror of the app's Settings → Canned Messages page. It covers the
// full CRUD plus the active/inactive toggle:
//
//	list / get / create / update / toggle / delete
//
// All commands hit /api/v1/app/canned-messages[/:id] with the stored oc_live_
// token. The page-defined message types and Meta-Cloud-API payload shapes are
// faithfully reproduced so payloads stored from the CLI are interchangeable
// with payloads stored from the app UI.

// cannedMessageTypes is the canonical set the backend accepts. The form
// dialog in the app exposes only the first five (TEXT, IMAGE, VIDEO,
// DOCUMENT, AUDIO) interactively; the rest are accepted by the backend and
// reachable via the generic `--payload '<json>'` flag for advanced use.
var cannedMessageTypes = map[string]bool{
	"TEXT":                true,
	"IMAGE":               true,
	"VIDEO":               true,
	"AUDIO":               true,
	"DOCUMENT":            true,
	"LOCATION":            true,
	"CONTACT":             true,
	"INTERACTIVE_BUTTON":  true,
	"INTERACTIVE_LIST":    true,
	"INTERACTIVE_CTA_URL": true,
	"ADDRESS":             true,
}

// ─── splashify canned ────────────────────────────────────────────────────────

func cmdCanned(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return cmdCannedList(args)

	case "create", "add", "new":
		return cmdCannedCreate(args[1:])

	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify canned update <id> [--name …] [--shortcut …] [--description …] [--type …] [--text … | --url … --caption … --filename … | --payload '{…}']")
		}
		return cmdCannedUpdate(args[1], args[2:])

	case "toggle", "toggle-active":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify canned toggle <id>")
		}
		return runReq("POST", "/app/canned-messages/"+args[1]+"/toggle", nil)

	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify canned delete <id>")
		}
		return runReq("DELETE", "/app/canned-messages/"+args[1], nil)

	default:
		// Treat the first arg as a message_id. The backend has no per-id GET,
		// so we fetch the list and filter — same fallback used by
		// `splashify attribute <id>` and `splashify member <id>`.
		return cmdCannedGet(args[0])
	}
}

// ─── list ────────────────────────────────────────────────────────────────────

// cmdCannedList implements the `splashify canned [list]` command. The backend
// endpoint returns the full list with no server-side filtering, so --search
// and --type are applied client-side, mirroring the page's behaviour.
func cmdCannedList(args []string) error {
	fs := flag.NewFlagSet("canned list", flag.ContinueOnError)
	search := fs.String("search", "", "substring match on name, shortcut, or description")
	typeFilter := fs.String("type", "", "filter by message_type (TEXT, IMAGE, VIDEO, AUDIO, DOCUMENT, …)")

	// Skip the leading "list"/"ls" subcommand if present so flag parsing works.
	flagArgs := args
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls" || args[0] == "") {
		flagArgs = args[1:]
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	// No filters → just dump the raw response. Saves a round-trip through
	// our parsing on the common case.
	if *search == "" && *typeFilter == "" {
		return runReq("GET", "/app/canned-messages", nil)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/canned-messages", nil)
	if err != nil {
		return err
	}

	var list struct {
		Success  bool             `json:"success"`
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		// Couldn't parse — surface the raw response so the user can debug.
		printJSON(raw)
		return nil
	}

	needle := strings.ToLower(*search)
	typeWant := strings.ToUpper(strings.TrimSpace(*typeFilter))
	matched := make([]map[string]any, 0, len(list.Messages))
	for _, m := range list.Messages {
		if needle != "" {
			name, _ := m["name"].(string)
			short, _ := m["shortcut"].(string)
			desc, _ := m["description"].(string)
			hay := strings.ToLower(name + " " + short + " " + desc)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		if typeWant != "" {
			t, _ := m["message_type"].(string)
			if strings.ToUpper(t) != typeWant {
				continue
			}
		}
		matched = append(matched, m)
	}
	out := map[string]any{"success": true, "messages": matched, "count": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

// ─── get one ────────────────────────────────────────────────────────────────

// cmdCannedGet is `splashify canned <id>` — no dedicated backend endpoint, so
// we list-and-filter. Same fallback used by other CLI commands when the
// backend exposes only a collection endpoint.
func cmdCannedGet(id string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/canned-messages", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success  bool             `json:"success"`
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, m := range list.Messages {
		if mid, _ := m["id"].(string); mid == id {
			encoded, _ := json.MarshalIndent(m, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("canned message %s not found", id)
}

// ─── create ──────────────────────────────────────────────────────────────────

// cmdCannedCreate POSTs a new canned message. The flag surface mirrors the
// app's CannedMessageFormDialog plus an advanced `--payload` escape hatch for
// the message types the dialog does not yet expose (LOCATION, CONTACT,
// INTERACTIVE_*, ADDRESS).
//
// Required:
//
//	--name <string>
//	--type TEXT | IMAGE | VIDEO | DOCUMENT | AUDIO | LOCATION | CONTACT | …
//
// Payload — pick whichever style matches your --type:
//
//	--text <string>                           (TEXT)
//	--url <link> [--caption <string>]         (IMAGE, VIDEO)
//	--url <link> [--filename <name>] [--caption <string>]  (DOCUMENT)
//	--url <link>                              (AUDIO)
//	--payload '<json>'                        (any type — wins if set)
//
// Optional metadata: --shortcut, --description.
func cmdCannedCreate(args []string) error {
	fs := flag.NewFlagSet("canned create", flag.ContinueOnError)
	name := fs.String("name", "", "display name (required, max 200 chars)")
	shortcut := fs.String("shortcut", "", "short text trigger, e.g. /hi")
	description := fs.String("description", "", "free-text description")
	msgType := fs.String("type", "TEXT", "TEXT|IMAGE|VIDEO|AUDIO|DOCUMENT|… (required)")

	text := fs.String("text", "", "body text (TEXT)")
	urlFlag := fs.String("url", "", "media URL (IMAGE/VIDEO/AUDIO/DOCUMENT)")
	caption := fs.String("caption", "", "caption (IMAGE/VIDEO/DOCUMENT)")
	filename := fs.String("filename", "", "file name hint (DOCUMENT)")
	rawPayload := fs.String("payload", "", `advanced: full message_payload JSON (overrides --text/--url/--caption/--filename)`)

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required\nusage: splashify canned create --name \"Welcome\" --type TEXT --text \"Hi there\"")
	}

	mt := strings.ToUpper(strings.TrimSpace(*msgType))
	if mt == "" {
		mt = "TEXT"
	}
	if !cannedMessageTypes[mt] {
		return fmt.Errorf("--type must be one of TEXT, IMAGE, VIDEO, AUDIO, DOCUMENT, LOCATION, CONTACT, INTERACTIVE_BUTTON, INTERACTIVE_LIST, INTERACTIVE_CTA_URL, ADDRESS — got %q", *msgType)
	}

	payload, err := buildCannedPayload(mt, *text, *urlFlag, *caption, *filename, *rawPayload)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":            *name,
		"message_type":    mt,
		"message_payload": payload,
	}
	if *shortcut != "" {
		body["shortcut"] = *shortcut
	}
	if *description != "" {
		body["description"] = *description
	}

	return runReq("POST", "/app/canned-messages", body)
}

// ─── update ──────────────────────────────────────────────────────────────────

// cmdCannedUpdate is read-modify-write: the backend's PUT replaces every
// column (name, shortcut, description, message_type, message_payload), so
// any field the user does not pass must be loaded from the current record
// and re-sent. Same pattern as `waba update`, `attribute update`, `opt`.
func cmdCannedUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("canned update", flag.ContinueOnError)
	name := fs.String("name", "", "new display name")
	shortcut := fs.String("shortcut", "", "new shortcut")
	description := fs.String("description", "", "new description")
	msgType := fs.String("type", "", "new message_type")

	text := fs.String("text", "", "new body text (TEXT)")
	urlFlag := fs.String("url", "", "new media URL (IMAGE/VIDEO/AUDIO/DOCUMENT)")
	caption := fs.String("caption", "", "new caption (IMAGE/VIDEO/DOCUMENT)")
	filename := fs.String("filename", "", "new filename (DOCUMENT)")
	rawPayload := fs.String("payload", "", `advanced: full message_payload JSON; replaces the existing payload entirely`)

	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify canned update <id> [--name …] [--shortcut …] [--description …] [--type …] [--text … | --url … --caption … --filename … | --payload '{…}']")
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	// Load current record (list-and-filter — no per-id GET).
	raw, err := api.callRaw("GET", "/app/canned-messages", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success  bool             `json:"success"`
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("decode canned messages list: %w", err)
	}
	var current map[string]any
	for _, m := range list.Messages {
		if mid, _ := m["id"].(string); mid == id {
			current = m
			break
		}
	}
	if current == nil {
		return fmt.Errorf("canned message %s not found", id)
	}

	// Seed body with current values, then overlay the flags the user set.
	curName, _ := current["name"].(string)
	curShortcut, _ := current["shortcut"].(string)
	curDesc, _ := current["description"].(string)
	curType, _ := current["message_type"].(string)
	curPayload := current["message_payload"]

	body := map[string]any{
		"name":            curName,
		"shortcut":        curShortcut,
		"description":     curDesc,
		"message_type":    curType,
		"message_payload": curPayload,
	}

	if passed["name"] {
		body["name"] = *name
	}
	if passed["shortcut"] {
		body["shortcut"] = *shortcut
	}
	if passed["description"] {
		body["description"] = *description
	}

	// Decide whether the user is changing the payload at all.
	payloadFlagsTouched := passed["type"] || passed["text"] || passed["url"] ||
		passed["caption"] || passed["filename"] || passed["payload"]

	if payloadFlagsTouched {
		// Resolve the effective message_type: explicit --type wins, else keep current.
		effectiveType := strings.ToUpper(strings.TrimSpace(curType))
		if passed["type"] {
			effectiveType = strings.ToUpper(strings.TrimSpace(*msgType))
			if !cannedMessageTypes[effectiveType] {
				return fmt.Errorf("--type must be one of TEXT, IMAGE, VIDEO, AUDIO, DOCUMENT, LOCATION, CONTACT, INTERACTIVE_BUTTON, INTERACTIVE_LIST, INTERACTIVE_CTA_URL, ADDRESS — got %q", *msgType)
			}
			body["message_type"] = effectiveType
		}

		// If the user passed --payload, that wins entirely.
		// Otherwise, fall back to the convenience flags. If a convenience flag
		// was not passed but is relevant to the effective type, pull the
		// current value out of curPayload so we don't wipe it.
		if passed["payload"] {
			var p any
			if err := json.Unmarshal([]byte(*rawPayload), &p); err != nil {
				return fmt.Errorf("--payload must be valid JSON: %w", err)
			}
			body["message_payload"] = p
		} else if passed["text"] || passed["url"] || passed["caption"] || passed["filename"] || passed["type"] {
			// Build a new payload, seeded by the current values for that type
			// when applicable, so a partial flag set doesn't blank the rest.
			curText, curURL, curCaption, curFilename := extractCannedPayloadFields(effectiveType, curPayload)

			finalText := curText
			if passed["text"] {
				finalText = *text
			}
			finalURL := curURL
			if passed["url"] {
				finalURL = *urlFlag
			}
			finalCaption := curCaption
			if passed["caption"] {
				finalCaption = *caption
			}
			finalFilename := curFilename
			if passed["filename"] {
				finalFilename = *filename
			}

			newPayload, err := buildCannedPayload(effectiveType, finalText, finalURL, finalCaption, finalFilename, "")
			if err != nil {
				return err
			}
			body["message_payload"] = newPayload
		}
	}

	return runReq("PUT", "/app/canned-messages/"+id, body)
}

// ─── payload builder ────────────────────────────────────────────────────────

// buildCannedPayload assembles the Meta-Cloud-API-compatible message_payload
// for a given message type from the convenience flags. The shapes match
// app/(routes)/settings/canned-messages/components/CannedMessageFormDialog.tsx
// → buildPayload, so payloads written by the CLI render identically in the
// app UI.
//
// If rawPayload is non-empty it wins entirely, allowing power users to send
// any shape (LOCATION, CONTACT, INTERACTIVE_*, ADDRESS, etc.) the backend
// will accept.
func buildCannedPayload(msgType, text, mediaURL, caption, filename, rawPayload string) (any, error) {
	if rawPayload != "" {
		var p any
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return nil, fmt.Errorf("--payload must be valid JSON: %w", err)
		}
		return p, nil
	}

	switch msgType {
	case "TEXT":
		return map[string]any{"text": map[string]any{"body": text}}, nil
	case "IMAGE":
		if mediaURL == "" {
			return nil, fmt.Errorf("--url is required for IMAGE (or use --payload '{…}')")
		}
		return map[string]any{"image": map[string]any{"link": mediaURL, "caption": caption}}, nil
	case "VIDEO":
		if mediaURL == "" {
			return nil, fmt.Errorf("--url is required for VIDEO (or use --payload '{…}')")
		}
		return map[string]any{"video": map[string]any{"link": mediaURL, "caption": caption}}, nil
	case "DOCUMENT":
		if mediaURL == "" {
			return nil, fmt.Errorf("--url is required for DOCUMENT (or use --payload '{…}')")
		}
		return map[string]any{"document": map[string]any{
			"link": mediaURL, "filename": filename, "caption": caption,
		}}, nil
	case "AUDIO":
		if mediaURL == "" {
			return nil, fmt.Errorf("--url is required for AUDIO (or use --payload '{…}')")
		}
		return map[string]any{"audio": map[string]any{"link": mediaURL}}, nil
	}

	// Advanced types — the dialog doesn't expose convenience flags, so we
	// require --payload for LOCATION, CONTACT, INTERACTIVE_*, ADDRESS.
	return nil, fmt.Errorf("convenience flags don't cover %s yet — pass --payload '<json>' with the full Meta-Cloud-API body", msgType)
}

// extractCannedPayloadFields pulls (text, mediaURL, caption, filename) out of
// an existing message_payload so update can preserve fields the user didn't
// touch. Returns zero values for shapes it does not understand — the caller
// then relies on whatever flag values the user passed.
func extractCannedPayloadFields(msgType string, payload any) (text, mediaURL, caption, filename string) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	switch msgType {
	case "TEXT":
		if t, ok := m["text"].(map[string]any); ok {
			text, _ = t["body"].(string)
		}
	case "IMAGE":
		if t, ok := m["image"].(map[string]any); ok {
			mediaURL, _ = t["link"].(string)
			caption, _ = t["caption"].(string)
		}
	case "VIDEO":
		if t, ok := m["video"].(map[string]any); ok {
			mediaURL, _ = t["link"].(string)
			caption, _ = t["caption"].(string)
		}
	case "DOCUMENT":
		if t, ok := m["document"].(map[string]any); ok {
			mediaURL, _ = t["link"].(string)
			caption, _ = t["caption"].(string)
			filename, _ = t["filename"].(string)
		}
	case "AUDIO":
		if t, ok := m["audio"].(map[string]any); ok {
			mediaURL, _ = t["link"].(string)
		}
	}
	return
}
