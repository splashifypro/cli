package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// This file implements `splashify templates` / `splashify template <id>` and
// `splashify rcs templates` / `splashify rcs template <id>` — the CLI mirror
// of the app's /templates, /templates/create, and /templates/rcs/create
// pages. It covers:
//
//	WhatsApp templates:
//	  list, get one, sync (all + per-id), upload-media, create, delete
//
//	RCS templates:
//	  list, get one, upload-media, create (basic | rich_card | carousel),
//	  delete, check-status
//
// Creates are JSON-body POSTs; the page's HTML form just funnels everything
// into the same component/template_data shapes the CLI accepts here.

// ─── splashify templates (WhatsApp) ──────────────────────────────────────────

func cmdTemplates(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/templates", nil)

	case "sync", "refresh":
		// `templates sync` syncs ALL from Meta.
		// `templates sync <template_id>` syncs only that one.
		if len(args) >= 2 && args[1] != "" {
			return runReq("POST", "/app/templates/"+url.PathEscape(args[1])+"/sync", nil)
		}
		return runReq("POST", "/app/templates/sync", nil)

	case "create", "add", "new":
		return cmdTemplateCreate(args[1:])

	case "upload-media", "media-handle":
		return cmdTemplateUploadMedia(args[1:])

	case "delete", "remove", "rm":
		return cmdTemplateDelete(args[1:])

	default:
		return fmt.Errorf("unknown templates subcommand: %s\nrun: splashify templates", sub)
	}
}

// cmdTemplate is the singular form — show one template by id (list-and-filter
// since the backend has no per-id GET).
func cmdTemplate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify template <template_id>")
	}
	id := args[0]

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/templates", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success   bool             `json:"success"`
		Templates []map[string]any `json:"templates"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, t := range list.Templates {
		if tid, _ := t["template_id"].(string); tid == id {
			encoded, _ := json.MarshalIndent(t, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("template %s not found", id)
}

// ─── templates create ────────────────────────────────────────────────────────

// waTemplateCategories is the set the app's create form exposes. Meta accepts
// these three categories on the /message_templates endpoint.
var waTemplateCategories = map[string]bool{
	"MARKETING": true, "UTILITY": true, "AUTHENTICATION": true,
}

// cmdTemplateCreate POSTs a new WhatsApp template. Required:
//
//	--name        snake_case name (Meta accepts lowercase, max 512 chars)
//	--language    BCP-47 code, e.g. en, en_US, hi
//	--category    MARKETING | UTILITY | AUTHENTICATION
//
// Components ride one of:
//
//	--text         body text — emits a single BODY component. Simplest path.
//	--components <json>      explicit Meta components array (any shape).
//	--file <path>            a JSON file containing either an array (components)
//	                         or a full object {name, language, category, components}.
//
// For media headers (IMAGE / VIDEO / DOCUMENT), the flow is two-step:
//
//	HANDLE=$(splashify templates upload-media ./brochure.pdf | jq -r '.handle')
//	splashify templates create --file ./brochure-tpl.json     # references HANDLE
//
// — keeps the CLI simple and lets you reuse a handle across multiple
// templates without re-uploading.
func cmdTemplateCreate(args []string) error {
	fs := flag.NewFlagSet("templates create", flag.ContinueOnError)
	name := fs.String("name", "", "template name (required, lowercase recommended)")
	language := fs.String("language", "", "BCP-47 language code, e.g. en, en_US (required)")
	category := fs.String("category", "", "MARKETING | UTILITY | AUTHENTICATION (required)")
	text := fs.String("text", "", "body text — convenience for a single BODY component")
	components := fs.String("components", "", `explicit Meta components array, e.g. '[{"type":"BODY","text":"…"}]'`)
	file := fs.String("file", "", `JSON file with either a components array or full payload {name,language,category,components}`)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// --file: optionally carries the whole payload; the user can leave the
	// other flags blank and have everything come from the file.
	var fileBody map[string]any
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("read --file: %w", err)
		}
		// Try as a full payload object first.
		if err := json.Unmarshal(raw, &fileBody); err == nil && fileBody["components"] != nil {
			// good: full payload
		} else {
			// Try as a bare components array.
			var arr []any
			if err := json.Unmarshal(raw, &arr); err != nil {
				return fmt.Errorf("--file must be a JSON object {name,language,category,components} or a components array: %w", err)
			}
			fileBody = map[string]any{"components": arr}
		}
	}

	// Resolve effective fields (CLI flags > file).
	finalName := *name
	if finalName == "" && fileBody != nil {
		finalName, _ = fileBody["name"].(string)
	}
	finalLanguage := *language
	if finalLanguage == "" && fileBody != nil {
		finalLanguage, _ = fileBody["language"].(string)
	}
	finalCategory := strings.ToUpper(strings.TrimSpace(*category))
	if finalCategory == "" && fileBody != nil {
		if v, _ := fileBody["category"].(string); v != "" {
			finalCategory = strings.ToUpper(v)
		}
	}

	if finalName == "" || finalLanguage == "" || finalCategory == "" {
		return fmt.Errorf("usage: splashify templates create --name <name> --language <code> --category <MARKETING|UTILITY|AUTHENTICATION> --text \"…\"\n   or: splashify templates create --components '[…]' (+ --name --language --category)\n   or: splashify templates create --file <path-to-json>")
	}
	if !waTemplateCategories[finalCategory] {
		return fmt.Errorf("--category must be MARKETING, UTILITY, or AUTHENTICATION — got %q", *category)
	}

	// Resolve components — precedence: --components > --text > --file.
	var comps any
	switch {
	case *components != "":
		if err := json.Unmarshal([]byte(*components), &comps); err != nil {
			return fmt.Errorf("--components must be valid JSON array: %w", err)
		}
	case *text != "":
		comps = []map[string]any{
			{"type": "BODY", "text": *text},
		}
	case fileBody != nil:
		comps = fileBody["components"]
	}
	if comps == nil {
		return fmt.Errorf("must supply --text, --components, or a --file with a components array")
	}

	body := map[string]any{
		"name":       finalName,
		"language":   finalLanguage,
		"category":   finalCategory,
		"components": comps,
	}
	return runReq("POST", "/app/templates", body)
}

// ─── templates upload-media ──────────────────────────────────────────────────

// cmdTemplateUploadMedia uploads a single file to Meta via the backend and
// returns the resulting media handle. The handle goes into the HEADER
// component of a template (format: IMAGE | VIDEO | DOCUMENT) as
// `example.header_handle` — see the docs for an example.
func cmdTemplateUploadMedia(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify templates upload-media <path-to-file>")
	}
	filePath := args[0]
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not accessible: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; pass a single file", filePath)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	fmt.Fprintf(os.Stderr, "Uploading %s (%d bytes) for template media handle…\n", filepath.Base(filePath), info.Size())
	raw, err := api.uploadFile("/app/templates/media-handle", "file", filePath)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}

// ─── templates delete ────────────────────────────────────────────────────────

func cmdTemplateDelete(args []string) error {
	fs := flag.NewFlagSet("templates delete", flag.ContinueOnError)
	name := fs.String("name", "", "optional template name — used by Meta for accurate cleanup")
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		positional = append(positional, a)
	}
	if len(positional) < 1 {
		return fmt.Errorf("usage: splashify templates delete <template_id> [--name <name>]")
	}
	if err := fs.Parse(args[len(positional):]); err != nil {
		return err
	}
	path := "/app/templates/" + url.PathEscape(positional[0])
	if *name != "" {
		path += "?name=" + url.QueryEscape(*name)
	}
	return runReq("DELETE", path, nil)
}

// ─── splashify rcs … (RCS templates) ─────────────────────────────────────────

// cmdRCS dispatches the RCS-specific subtree. RCS has its own templates
// surface, separate from WhatsApp — namespacing under `rcs templates` /
// `rcs template <id>` keeps the two from colliding. `send` is the
// free-form RCS message path (the /messages page's RCS composer); the
// template send uses `send-template`.
func cmdRCS(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify rcs <send|send-template|templates|template> …")
	}
	switch args[0] {
	case "templates":
		return cmdRCSTemplates(args[1:])
	case "template":
		return cmdRCSTemplate(args[1:])
	case "send", "send-text":
		return cmdRCSSendText(args[1:])
	case "send-template":
		return cmdRCSSendTemplate(args[1:])
	default:
		return fmt.Errorf("unknown rcs subcommand: %s\nrun: splashify rcs", args[0])
	}
}

// cmdRCSSendText mirrors api.sendRCSText from /messages — the RCS
// composer's plain "Send" button. The backend bills via the RCS pricing
// table (separate from WA), so this is a paid send.
func cmdRCSSendText(args []string) error {
	fs := flag.NewFlagSet("rcs send", flag.ContinueOnError)
	to := fs.String("to", "", "recipient phone with country code (+91…)")
	text := fs.String("text", "", "message text")
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" || *text == "" {
		return fmt.Errorf(`usage: splashify rcs send --to "+91…" --text "…"`)
	}
	if !*yes {
		if err := preflightSend("RCS messages", true); err != nil {
			return err
		}
	}
	return runReq("POST", "/app/rcs/messages/send-text", map[string]any{
		"to": *to, "text": *text,
	})
}

// cmdRCSSendTemplate mirrors api.sendRCSTemplate from the
// RCSTemplateSendDialog on /messages. Required: --to + --template-id.
func cmdRCSSendTemplate(args []string) error {
	fs := flag.NewFlagSet("rcs send-template", flag.ContinueOnError)
	to := fs.String("to", "", "recipient phone with country code (+91…)")
	tplID := fs.String("template-id", "", "RCS template_id (UUID) — see `splashify rcs templates`")
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" || *tplID == "" {
		return fmt.Errorf(`usage: splashify rcs send-template --to "+91…" --template-id <uuid>`)
	}
	if !*yes {
		if err := preflightSend("RCS template messages", true); err != nil {
			return err
		}
	}
	return runReq("POST", "/app/rcs/messages/send-template", map[string]any{
		"to": *to, "template_id": *tplID,
	})
}

// ─── rcs templates (plural) ──────────────────────────────────────────────────

func cmdRCSTemplates(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/rcs/templates", nil)

	case "create", "add", "new":
		return cmdRCSTemplateCreate(args[1:])

	case "upload-media":
		return cmdRCSTemplateUploadMedia(args[1:])

	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify rcs templates delete <template_id>")
		}
		return runReq("DELETE", "/app/rcs/templates/"+args[1], nil)

	default:
		return fmt.Errorf("unknown rcs templates subcommand: %s\nrun: splashify rcs templates", sub)
	}
}

// ─── rcs template (singular) ─────────────────────────────────────────────────

func cmdRCSTemplate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify rcs template <template_id> [check-status]")
	}
	id := args[0]

	// `rcs template <id> check-status` — poll Meta/JioCX for the latest
	// approval state.
	if len(args) >= 2 && (args[1] == "check-status" || args[1] == "status") {
		return runReq("GET", "/app/rcs/templates/"+id+"/check-status", nil)
	}

	// Default: show one template by filtering the list (no per-id GET on
	// the backend, same pattern as other singular commands).
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/rcs/templates", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success   bool             `json:"success"`
		Templates []map[string]any `json:"templates"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, t := range list.Templates {
		if tid, _ := t["template_id"].(string); tid == id {
			encoded, _ := json.MarshalIndent(t, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("rcs template %s not found", id)
}

// ─── rcs templates create ────────────────────────────────────────────────────

// rcsTemplateTypes mirrors the backend's validation set
// (`basic` | `rich_card` | `carousel`).
var rcsTemplateTypes = map[string]bool{
	"basic": true, "rich_card": true, "carousel": true,
}

// rcsMediaHeights are the allowed media slot heights for rich cards. Sent
// alongside the file when uploading to /app/rcs/templates/upload-media.
var rcsMediaHeights = map[string]bool{
	"SHORT": true, "MEDIUM": true, "TALL": true,
}

// cmdRCSTemplateCreate POSTs a new RCS template. The backend stores
// `template_data` as an opaque JSON string; we accept any of:
//
//	--text "…"            convenience for type=basic — emits {"type":"text","text":"…"}
//	--data '<json>'       inline RML JSON (one of basic / rich_card / carousel shapes)
//	--file <path>         path to a JSON file with the same shape
//
// Required: --name, --type basic|rich_card|carousel.
func cmdRCSTemplateCreate(args []string) error {
	fs := flag.NewFlagSet("rcs templates create", flag.ContinueOnError)
	name := fs.String("name", "", "template name (required)")
	ttype := fs.String("type", "", "basic | rich_card | carousel (required)")
	text := fs.String("text", "", "body text — convenience for --type basic")
	data := fs.String("data", "", `RML JSON template_data, e.g. '{"type":"text","text":"…"}'`)
	file := fs.String("file", "", "path to a JSON file containing the template_data shape")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	tt := strings.ToLower(strings.TrimSpace(*ttype))
	if !rcsTemplateTypes[tt] {
		return fmt.Errorf("--type must be basic | rich_card | carousel — got %q", *ttype)
	}

	// Resolve the JSON payload — precedence: --data > --file > --text.
	var rawJSON string
	switch {
	case *data != "":
		rawJSON = *data
	case *file != "":
		b, err := os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("read --file: %w", err)
		}
		rawJSON = string(b)
	case *text != "":
		if tt != "basic" {
			return fmt.Errorf("--text is only valid for --type basic (use --data or --file for rich_card and carousel)")
		}
		b, _ := json.Marshal(map[string]any{"type": "text", "text": *text})
		rawJSON = string(b)
	default:
		return fmt.Errorf("must supply --text (for basic), --data '<json>', or --file <path>")
	}

	// Validate it parses — saves a round-trip when the JSON is broken.
	var probe any
	if err := json.Unmarshal([]byte(rawJSON), &probe); err != nil {
		return fmt.Errorf("template_data must be valid JSON: %w", err)
	}

	body := map[string]any{
		"name":          strings.TrimSpace(*name),
		"template_type": tt,
		"template_data": rawJSON, // backend wants this as a string
	}
	return runReq("POST", "/app/rcs/templates", body)
}

// ─── rcs templates upload-media ──────────────────────────────────────────────

// cmdRCSTemplateUploadMedia uploads a file to RCS template media storage and
// returns the URL/handle to plug into a rich_card's `file_url` (and
// optionally `thumbnail_url`). The backend requires a `media_height` form
// field describing which media slot the file belongs in.
func cmdRCSTemplateUploadMedia(args []string) error {
	fs := flag.NewFlagSet("rcs templates upload-media", flag.ContinueOnError)
	height := fs.String("height", "MEDIUM", "media slot height: SHORT | MEDIUM | TALL")

	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		positional = append(positional, a)
	}
	if len(positional) < 1 {
		return fmt.Errorf("usage: splashify rcs templates upload-media <path-to-file> [--height SHORT|MEDIUM|TALL]")
	}
	if err := fs.Parse(args[len(positional):]); err != nil {
		return err
	}

	h := strings.ToUpper(strings.TrimSpace(*height))
	if !rcsMediaHeights[h] {
		return fmt.Errorf("--height must be SHORT | MEDIUM | TALL — got %q", *height)
	}

	filePath := positional[0]
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not accessible: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; pass a single file", filePath)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	fmt.Fprintf(os.Stderr, "Uploading %s (%d bytes) for RCS template media (height=%s)…\n",
		filepath.Base(filePath), info.Size(), h)
	raw, err := api.uploadFileWithFields("/app/rcs/templates/upload-media", "file", filePath,
		map[string]string{"media_height": h})
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}
