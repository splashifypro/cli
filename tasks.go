package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strings"
)

// This file holds the "do work" commands — sending messages, managing
// contacts, broadcasts, templates, analytics — plus the generic `api`
// passthrough. Every command authenticates with the stored oc_live_ token
// and hits the same /api/v1/app/* endpoints OpenClaw uses.

// splitTags converts a comma-separated tag list ("vip,lead, hot ") into a
// trimmed []string suitable for the backend's `[]string` json:"tags" field.
// Empty entries are dropped.
func splitTags(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// runReq is the shared path: load config, call the backend, pretty-print JSON.
func runReq(method, path string, body any) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw(method, path, body)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}

// withQuery appends a query string to a path, skipping empty values.
func withQuery(path string, params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// ─── message ─────────────────────────────────────────────────────────────────

func cmdMessage(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify message <send|template|media>")
	}
	switch args[0] {
	case "send":
		fs := flag.NewFlagSet("message send", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone, with country code (+91…)")
		text := fs.String("text", "", "message text")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" || *text == "" {
			return fmt.Errorf("usage: splashify message send --to +91… --text \"…\"")
		}
		return runReq("POST", "/app/messages/send-text", map[string]any{
			"to": *to, "text": *text,
		})

	case "template":
		fs := flag.NewFlagSet("message template", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone")
		name := fs.String("name", "", "approved template name")
		lang := fs.String("lang", "en", "template language code")
		vars := fs.String("vars", "", `JSON array of body variables, e.g. ["John","ORD1"]`)
		raw := fs.String("template", "", `(advanced) full Meta template JSON; overrides --name/--lang/--vars`)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" {
			return fmt.Errorf("usage: splashify message template --to +91… --name <template> [--lang en] [--vars '[…]']")
		}

		// The backend expects a full Meta-Cloud-API "template" object:
		//   { "name": "...", "language": {"code":"en"},
		//     "components": [ {"type":"body","parameters":[{"type":"text","text":"..."}]} ] }
		// Build it from --name/--lang/--vars, or accept a pre-formed one via --template.
		var templateObj map[string]any
		if *raw != "" {
			if err := json.Unmarshal([]byte(*raw), &templateObj); err != nil {
				return fmt.Errorf("--template must be valid JSON: %w", err)
			}
		} else {
			if *name == "" {
				return fmt.Errorf("--name is required (or use --template with a pre-formed JSON)")
			}
			templateObj = map[string]any{
				"name":     *name,
				"language": map[string]any{"code": *lang},
			}
			if *vars != "" {
				var values []any
				if err := json.Unmarshal([]byte(*vars), &values); err != nil {
					return fmt.Errorf("--vars must be a JSON array, e.g. '[\"Alice\",\"ORD1\"]': %w", err)
				}
				params := make([]map[string]any, 0, len(values))
				for _, v := range values {
					params = append(params, map[string]any{
						"type": "text",
						"text": fmt.Sprintf("%v", v),
					})
				}
				templateObj["components"] = []map[string]any{
					{"type": "body", "parameters": params},
				}
			}
		}
		return runReq("POST", "/app/messages/send-template", map[string]any{
			"to": *to, "template": templateObj,
		})

	case "media":
		fs := flag.NewFlagSet("message media", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone")
		mtype := fs.String("type", "", "media type: image, document, video, audio")
		murl := fs.String("url", "", "public URL of the media file")
		caption := fs.String("caption", "", "optional caption")
		filename := fs.String("filename", "", "filename hint (documents only)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" || *mtype == "" || *murl == "" {
			return fmt.Errorf("usage: splashify message media --to +91… --type image --url <url> [--caption …]")
		}
		body := map[string]any{"to": *to, "media_type": *mtype, "media_url": *murl}
		if *caption != "" {
			body["caption"] = *caption
		}
		if *filename != "" {
			body["filename"] = *filename
		}
		return runReq("POST", "/app/messages/send-media", body)

	default:
		return fmt.Errorf("unknown message subcommand: %s", args[0])
	}
}

// ─── conversations ───────────────────────────────────────────────────────────

func cmdConversations(args []string) error {
	fs := flag.NewFlagSet("conversations", flag.ContinueOnError)
	page := fs.String("page", "1", "page number")
	size := fs.String("page-size", "20", "items per page")
	status := fs.String("status", "", "filter: open, resolved")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/messages/conversations", map[string]string{
		"page": *page, "page_size": *size, "status": *status,
	}), nil)
}

func cmdConversation(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify conversation <id> [resolve]")
	}
	id := args[0]
	if len(args) > 1 && args[1] == "resolve" {
		return runReq("POST", "/app/messages/conversations/"+id+"/resolve", nil)
	}
	return runReq("GET", "/app/messages/conversations/"+id, nil)
}

func cmdUnread(_ []string) error {
	return runReq("GET", "/app/messages/unread-count", nil)
}

// ─── contacts ────────────────────────────────────────────────────────────────

func cmdContacts(args []string) error {
	fs := flag.NewFlagSet("contacts", flag.ContinueOnError)
	page := fs.String("page", "1", "page number")
	size := fs.String("page-size", "20", "items per page")
	search := fs.String("search", "", "search by name or phone")
	tag := fs.String("tag", "", "filter by tag")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/contacts", map[string]string{
		"page": *page, "page_size": *size, "search": *search, "tag": *tag,
	}), nil)
}

func cmdContact(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify contact <id|create|delete|tag|block|unblock> …")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("contact create", flag.ContinueOnError)
		phone := fs.String("phone", "", "phone with country code (sent as phone_number)")
		name := fs.String("name", "", "display name (sent as display_name)")
		email := fs.String("email", "", "contact email")
		tags := fs.String("tags", "", "comma-separated tags, e.g. vip,lead")
		notes := fs.String("notes", "", "free-text notes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *phone == "" {
			return fmt.Errorf("usage: splashify contact create --phone +91… [--name …] [--email …] [--tags vip,lead]")
		}
		body := map[string]any{"phone_number": *phone}
		if *name != "" {
			body["display_name"] = *name
		}
		if *email != "" {
			body["email"] = *email
		}
		if *notes != "" {
			body["notes"] = *notes
		}
		if *tags != "" {
			body["tags"] = splitTags(*tags)
		}
		return runReq("POST", "/app/contacts", body)

	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify contact delete <id>")
		}
		return runReq("DELETE", "/app/contacts/"+args[1], nil)

	case "tag":
		fs := flag.NewFlagSet("contact tag", flag.ContinueOnError)
		tags := fs.String("tags", "", "comma-separated tags, e.g. vip,lead")
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify contact tag <id> --tags vip,lead")
		}
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *tags == "" {
			return fmt.Errorf("--tags is required")
		}
		return runReq("POST", "/app/contacts/"+args[1]+"/tags", map[string]any{
			"tags": splitTags(*tags),
		})

	case "block":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify contact block <id>")
		}
		return runReq("POST", "/app/contacts/"+args[1]+"/block", nil)

	case "unblock":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify contact unblock <id>")
		}
		return runReq("POST", "/app/contacts/"+args[1]+"/unblock", nil)

	default:
		// Treat the argument as a contact ID.
		return runReq("GET", "/app/contacts/"+args[0], nil)
	}
}

// ─── broadcasts ──────────────────────────────────────────────────────────────

func cmdBroadcasts(_ []string) error {
	return runReq("GET", "/app/broadcasts", nil)
}

func cmdBroadcast(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify broadcast <id|stats|create> …")
	}
	switch args[0] {
	case "stats":
		return runReq("GET", "/app/broadcasts/stats", nil)

	case "create":
		fs := flag.NewFlagSet("broadcast create", flag.ContinueOnError)
		name := fs.String("name", "", "campaign name")
		tmpl := fs.String("template", "", "approved template name")
		audType := fs.String("audience-type", "", "segment, tag, or all")
		audID := fs.String("audience-id", "", "segment ID or tag name")
		schedule := fs.String("schedule", "", "ISO 8601 time; empty = send now")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || *tmpl == "" || *audType == "" {
			return fmt.Errorf("usage: splashify broadcast create --name … --template … --audience-type <segment|tag|all> [--audience-id …] [--schedule …]")
		}
		body := map[string]any{"name": *name, "template_name": *tmpl, "audience_type": *audType}
		if *audID != "" {
			body["audience_id"] = *audID
		}
		if *schedule != "" {
			body["schedule_at"] = *schedule
		}
		return runReq("POST", "/app/broadcasts", body)

	default:
		return runReq("GET", "/app/broadcasts/"+args[0], nil)
	}
}

// ─── waba — WhatsApp Business Account: view + update ─────────────────────────

// cmdWaba surfaces everything the app's /dashboard page shows about the user's
// connected WhatsApp Business Account, plus the writes that page supports
// (profile fields, Meta sync, phone registration, deletion request).
//
//	splashify waba                                show full status (default)
//	splashify waba setup-status                   high-level setup checklist
//	splashify waba sync                           pull fresh data from Meta
//	splashify waba update --about "..." ...       update business profile
//	splashify waba register-phone                 (re)register phone with Meta
//	splashify waba oba-status                     Official Business Account status
//	splashify waba oba-apply                      apply for Official Business Account
//	splashify waba request-deletion               request WABA deletion
func cmdWaba(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status", "info", "":
		return runReq("GET", "/app/dashboard/whatsapp-status", nil)

	case "setup-status":
		return runReq("GET", "/app/dashboard/setup-status", nil)

	case "sync":
		return runReq("POST", "/app/dashboard/sync-meta", nil)

	case "register-phone":
		return runReq("POST", "/app/dashboard/register-phone", nil)

	case "oba-status":
		return runReq("GET", "/app/dashboard/oba/status", nil)

	case "oba-apply":
		return runReq("POST", "/app/dashboard/oba/apply", nil)

	case "request-deletion":
		return runReq("POST", "/app/waba/request-deletion", nil)

	case "update":
		// PUT /app/dashboard/whatsapp-profile accepts a free-form Meta
		// Cloud API "whatsapp_business_profile" body and forwards it as-is.
		// We expose the common fields as named flags; --data wins for the
		// rest (e.g. profile_picture_handle).
		fs := flag.NewFlagSet("waba update", flag.ContinueOnError)
		about := fs.String("about", "", "short bio (Meta limit ~139 chars)")
		desc := fs.String("description", "", "longer business description")
		address := fs.String("address", "", "physical address")
		email := fs.String("email", "", "contact email")
		vertical := fs.String("vertical", "", "business category code, e.g. RETAIL")
		websites := fs.String("websites", "", "comma-separated URLs (max 2 per Meta)")
		raw := fs.String("data", "", `(advanced) raw JSON body merged on top of the named fields`)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		body := map[string]any{}
		if *about != "" {
			body["about"] = *about
		}
		if *desc != "" {
			body["description"] = *desc
		}
		if *address != "" {
			body["address"] = *address
		}
		if *email != "" {
			body["email"] = *email
		}
		if *vertical != "" {
			body["vertical"] = *vertical
		}
		if *websites != "" {
			body["websites"] = splitTags(*websites) // same split semantics: comma-separated → []string
		}
		if *raw != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(*raw), &extra); err != nil {
				return fmt.Errorf("--data must be a JSON object: %w", err)
			}
			for k, v := range extra {
				body[k] = v
			}
		}
		if len(body) == 0 {
			return fmt.Errorf(`usage: splashify waba update [--about "..."] [--description "..."] [--address "..."] [--email "..."] [--vertical RETAIL] [--websites https://...,https://...] [--data '{"...":"..."}']`)
		}
		return runReq("PUT", "/app/dashboard/whatsapp-profile", body)

	default:
		return fmt.Errorf("unknown waba subcommand: %s\nrun: splashify waba", sub)
	}
}

// ─── templates / analytics / wallet ──────────────────────────────────────────

func cmdTemplates(_ []string) error {
	return runReq("GET", "/app/templates", nil)
}

func cmdAnalytics(args []string) error {
	path := "/app/analytics/summary"
	if len(args) > 0 && args[0] == "trends" {
		path = "/app/analytics/trends"
	}
	return runReq("GET", path, nil)
}

func cmdWallet(args []string) error {
	if len(args) > 0 && args[0] == "transactions" {
		return runReq("GET", "/app/wallet/transactions", nil)
	}
	return runReq("GET", "/app/wallet/info", nil)
}

// ─── generic api passthrough ─────────────────────────────────────────────────

func cmdAPI(args []string) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	data := fs.String("data", "", "JSON request body for POST/PUT/PATCH")
	// Method and path are positional; flags may follow them.
	var positionals []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		positionals = append(positionals, a)
	}
	if len(positionals) < 2 {
		return fmt.Errorf("usage: splashify api <GET|POST|PUT|PATCH|DELETE> <path> [--data '{…}']\n" +
			"  e.g. splashify api GET /app/contacts?page=2\n" +
			"       splashify api POST /app/messages/send-text --data '{\"phone\":\"+91…\",\"message\":\"hi\"}'")
	}
	if err := fs.Parse(args[len(positionals):]); err != nil {
		return err
	}

	method := strings.ToUpper(positionals[0])
	path := positionals[1]
	// Accept paths with or without the /api/v1 prefix.
	path = strings.TrimPrefix(path, "/api/v1")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var body any
	if *data != "" {
		var parsed any
		if err := json.Unmarshal([]byte(*data), &parsed); err != nil {
			return fmt.Errorf("--data must be valid JSON: %w", err)
		}
		body = parsed
	}
	return runReq(method, path, body)
}
