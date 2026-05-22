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
		return fmt.Errorf("usage: splashify message <send|template|media|location|reaction|contact|typing>")
	}
	switch args[0] {
	case "send":
		fs := flag.NewFlagSet("message send", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone, with country code (+91…)")
		text := fs.String("text", "", "message text")
		// `--context-message-id` carries the wa_message_id you're replying
		// to so the recipient's inbox shows the quoted bubble — same effect
		// as tapping "Reply" on a message in the /messages page.
		contextID := fs.String("context-message-id", "", "wa_message_id you're replying to (optional)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" || *text == "" {
			return fmt.Errorf("usage: splashify message send --to +91… --text \"…\" [--context-message-id <wa_id>]")
		}
		body := map[string]any{"to": *to, "text": *text}
		if *contextID != "" {
			body["context_message_id"] = *contextID
		}
		return runReq("POST", "/app/messages/send-text", body)

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
		// `--voice` flips the audio render to a WhatsApp voice-note bubble
		// (single-tap play, no track scrubber). Only meaningful for
		// --type audio; backend ignores it for other types.
		voice := fs.String("voice", "", "true to render an audio file as a voice note (audio only)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" || *mtype == "" || *murl == "" {
			return fmt.Errorf("usage: splashify message media --to +91… --type image --url <url> [--caption …] [--voice true]")
		}
		body := map[string]any{"to": *to, "media_type": *mtype, "media_url": *murl}
		if *caption != "" {
			body["caption"] = *caption
		}
		if *filename != "" {
			body["filename"] = *filename
		}
		if *voice != "" {
			b, err := parseBool(*voice)
			if err != nil {
				return fmt.Errorf("--voice must be true or false")
			}
			body["voice"] = b
		}
		return runReq("POST", "/app/messages/send-media", body)

	case "location":
		// Mirrors api.sendLocationMessage on the page — drops a pin in the
		// recipient's WhatsApp thread. lat/lng are required; name+address
		// show as the label above the map.
		fs := flag.NewFlagSet("message location", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone")
		lat := fs.Float64("lat", 0, "latitude (required)")
		lng := fs.Float64("lng", 0, "longitude (required)")
		name := fs.String("name", "", "optional place name shown above the pin")
		address := fs.String("address", "", "optional address shown under the place name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		passed := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
		if *to == "" || !passed["lat"] || !passed["lng"] {
			return fmt.Errorf("usage: splashify message location --to +91… --lat 12.97 --lng 77.59 [--name …] [--address …]")
		}
		body := map[string]any{"to": *to, "latitude": *lat, "longitude": *lng}
		if *name != "" {
			body["name"] = *name
		}
		if *address != "" {
			body["address"] = *address
		}
		return runReq("POST", "/app/messages/send-location", body)

	case "reaction", "react":
		// Mirrors api.sendReactionMessage — attaches an emoji reaction to an
		// existing wa_message_id. Pass --emoji "" to remove the reaction.
		fs := flag.NewFlagSet("message reaction", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone")
		msgID := fs.String("message-id", "", "wa_message_id you're reacting to (required)")
		emoji := fs.String("emoji", "", `emoji, e.g. "👍" — pass "" to clear`)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		passed := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
		if *to == "" || *msgID == "" || !passed["emoji"] {
			return fmt.Errorf(`usage: splashify message reaction --to +91… --message-id <wa_id> --emoji "👍"`)
		}
		return runReq("POST", "/app/messages/send-reaction", map[string]any{
			"to": *to, "message_id": *msgID, "emoji": *emoji,
		})

	case "contact", "contact-card", "vcard":
		// Mirrors api.sendContactMessage — Meta's contacts-type message.
		// Accepts the canonical contacts[] array via --contacts (inline
		// JSON) or --file (path to a JSON file). The contacts array must
		// match Meta's shape:
		//   [{"name":{"formatted_name":"Jane Doe","first_name":"Jane"},
		//     "phones":[{"phone":"+91…","type":"WORK","wa_id":"91…"}]}]
		fs := flag.NewFlagSet("message contact", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone")
		raw := fs.String("contacts", "", "inline JSON: array of Meta contact objects")
		file := fs.String("file", "", "path to a JSON file containing the contacts array")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" {
			return fmt.Errorf(`usage: splashify message contact --to +91… --contacts '[…]'
       splashify message contact --to +91… --file ./contact.json`)
		}
		var contactsJSON string
		switch {
		case *raw != "":
			contactsJSON = *raw
		case *file != "":
			b, err := os.ReadFile(*file)
			if err != nil {
				return fmt.Errorf("read --file: %w", err)
			}
			contactsJSON = string(b)
		default:
			return fmt.Errorf("must pass --contacts '[…]' or --file <path>")
		}
		var contacts []map[string]any
		if err := json.Unmarshal([]byte(contactsJSON), &contacts); err != nil {
			// Allow a single contact object — wrap it in an array.
			var one map[string]any
			if err2 := json.Unmarshal([]byte(contactsJSON), &one); err2 == nil {
				contacts = []map[string]any{one}
			} else {
				return fmt.Errorf("contacts must be a JSON array (or object): %w", err)
			}
		}
		if len(contacts) == 0 {
			return fmt.Errorf("at least one contact is required")
		}
		return runReq("POST", "/app/messages/send-contact", map[string]any{
			"to": *to, "contacts": contacts,
		})

	case "typing", "typing-indicator":
		// Mirrors api.sendTypingIndicator — surfaces a "…" bubble on the
		// recipient's side. Cheap (no wallet cost) but each call expires
		// quickly; debounce on the caller's side.
		fs := flag.NewFlagSet("message typing", flag.ContinueOnError)
		to := fs.String("to", "", "recipient phone (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *to == "" {
			return fmt.Errorf("usage: splashify message typing --to +91…")
		}
		return runReq("POST", "/app/messages/typing-indicator", map[string]any{"to": *to})

	default:
		return fmt.Errorf("unknown message subcommand: %s\nrun: splashify message", args[0])
	}
}

// ─── conversations ───────────────────────────────────────────────────────────

func cmdConversations(args []string) error {
	fs := flag.NewFlagSet("conversations", flag.ContinueOnError)
	page := fs.String("page", "", "page number")
	// --page-size kept as an alias of --limit so existing v0.1.x scripts
	// keep working. Both feed the backend's `limit` parameter.
	size := fs.String("page-size", "", "items per page (alias of --limit)")
	limit := fs.String("limit", "", "items per page")
	status := fs.String("status", "", "filter: open, resolved")
	channel := fs.String("channel", "", "whatsapp | rcs | instagram (matches the inbox tab)")
	search := fs.String("search", "", "search by contact name / phone / last message body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	effLimit := *limit
	if effLimit == "" {
		effLimit = *size
	}
	return runReq("GET", withQuery("/app/messages/conversations", map[string]string{
		"page":    *page,
		"limit":   effLimit,
		"status":  *status,
		"channel": *channel,
		"search":  *search,
	}), nil)
}

func cmdConversation(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify conversation <id> [resolve|reopen|assign --to <member_id>]")
	}
	id := args[0]
	if len(args) > 1 {
		switch args[1] {
		case "resolve", "close":
			return runReq("POST", "/app/messages/conversations/"+id+"/resolve", nil)
		case "reopen", "open":
			return runReq("POST", "/app/messages/conversations/"+id+"/reopen", nil)
		case "assign":
			// `--to` may be empty — unassigns the conversation. Matches the
			// /messages page's "Unassign" affordance.
			fs := flag.NewFlagSet("conversation assign", flag.ContinueOnError)
			to := fs.String("to", "", "member_id / user_id to assign to (empty = unassign)")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			passed := map[string]bool{}
			fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
			if !passed["to"] {
				return fmt.Errorf("usage: splashify conversation <id> assign --to <member_id>  (pass --to '' to unassign)")
			}
			return runReq("POST", "/app/messages/conversations/"+id+"/assign", map[string]any{
				"assigned_to": *to,
			})
		default:
			return fmt.Errorf("unknown conversation action: %s\nrun: splashify conversation", args[1])
		}
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
		return fmt.Errorf("usage: splashify contact <id|create|update|delete|tag|untag|block|unblock> …")
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

	case "untag", "remove-tags":
		// Mirrors api.removeContactTags — the backend's DELETE accepts the
		// tags as a comma-separated query param, not a body. Same shape as
		// the page's handleRemoveTag.
		fs := flag.NewFlagSet("contact untag", flag.ContinueOnError)
		tags := fs.String("tags", "", "comma-separated tags to remove, e.g. vip,lead")
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify contact untag <id> --tags vip,lead")
		}
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *tags == "" {
			return fmt.Errorf("--tags is required")
		}
		// Re-encode to handle commas + spaces consistently.
		parts := splitTags(*tags)
		return runReq("DELETE",
			"/app/contacts/"+args[1]+"/tags?tags="+url.QueryEscape(strings.Join(parts, ",")),
			nil)

	case "update", "edit", "patch":
		// Mirrors api.updateContact — sparse PUT, only the flags the user
		// explicitly passed get sent. Covers the /messages page surfaces
		// for notes (handleSaveNotes), opt-out toggle (handleOptToggle),
		// and the contact sidebar's display-name / email / website / tags
		// edits. --data accepts a raw JSON object that overlays last for
		// any fields the named flags don't cover (e.g. column_data).
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify contact update <id> [--name …] [--email …] [--website …] [--notes …] [--opted-out true|false] [--tags vip,lead] [--data '{…}']")
		}
		fs := flag.NewFlagSet("contact update", flag.ContinueOnError)
		name := fs.String("name", "", "display name (display_name)")
		email := fs.String("email", "", "email")
		website := fs.String("website", "", "website URL (website_url)")
		notes := fs.String("notes", "", "free-text notes")
		optedOut := fs.String("opted-out", "", "true|false — marketing opt-out flag (has_opted_out)")
		tags := fs.String("tags", "", "comma-separated tags (replaces the tag set)")
		raw := fs.String("data", "", `raw JSON merged on top of the named fields, e.g. '{"column_data":"{…}"}'`)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		passed := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
		if len(passed) == 0 {
			return fmt.Errorf("usage: splashify contact update <id> [--name …] [--email …] [--website …] [--notes …] [--opted-out true|false] [--tags vip,lead] [--data '{…}']")
		}
		body := map[string]any{}
		if passed["name"] {
			body["display_name"] = *name
		}
		if passed["email"] {
			body["email"] = *email
		}
		if passed["website"] {
			body["website_url"] = *website
		}
		if passed["notes"] {
			body["notes"] = *notes
		}
		if passed["opted-out"] {
			b, err := parseBool(*optedOut)
			if err != nil {
				return fmt.Errorf("--opted-out must be true or false")
			}
			body["has_opted_out"] = b
		}
		if passed["tags"] {
			body["tags"] = splitTags(*tags)
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
		return runReq("PUT", "/app/contacts/"+args[1], body)

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

// Broadcast commands live in broadcast.go since v0.1.23 — old stubs here
// used wrong field names (audience_type/audience_id, schedule_at) and have
// been removed.

// ─── account — read-only mirror of /settings/account-details ─────────────────

// cmdAccount surfaces the data the app's "Account Details" page shows. It is
// intentionally read-only — there is no `update`, no `accept`, no `invite`,
// no `switch`. To mutate any of this state, use the web app.
//
//	splashify account                       consolidated view (default)
//	splashify account info                  /app/me — profile + plan
//	splashify account orgs                  /app/organizations
//	splashify account invitations           /app/org/invitations  (received)
//	splashify account sent-invitations      /app/org/sent-invitations
//	splashify account wallet                /app/wallet/info
func cmdAccount(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "info", "me", "profile":
		return runReq("GET", "/app/me", nil)
	case "orgs", "organizations":
		return runReq("GET", "/app/organizations", nil)
	case "invitations", "invites":
		return runReq("GET", "/app/org/invitations", nil)
	case "sent-invitations", "sent-invites":
		return runReq("GET", "/app/org/sent-invitations", nil)
	case "wallet":
		return runReq("GET", "/app/wallet/info", nil)
	case "", "details", "overview":
		return cmdAccountOverview()
	default:
		return fmt.Errorf("unknown account subcommand: %s\nrun: splashify account", sub)
	}
}

// cmdAccountOverview fetches every section the /settings/account-details page
// reads and prints them as one consolidated JSON object. Each section is
// fetched independently — a section that 404s or errors is reported as
// `{"error":"..."}` rather than failing the whole command.
func cmdAccountOverview() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	fetch := func(path string) json.RawMessage {
		out, err := api.callRaw("GET", path, nil)
		if err != nil {
			return json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		return out
	}

	combined := map[string]json.RawMessage{
		"user":                 fetch("/app/me"),
		"wallet":               fetch("/app/wallet/info"),
		"whatsapp":             fetch("/app/dashboard/whatsapp-status"),
		"organizations":        fetch("/app/organizations"),
		"invitations_received": fetch("/app/org/invitations"),
		"invitations_sent":     fetch("/app/org/sent-invitations"),
	}

	out, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// ─── attributes — CRUD + visibility + reorder on /settings/attributes ────────

// attributeRecord mirrors a single row in /app/attributes. Used as the
// read-modify-write seed for `attribute update`. Pointers on the optional
// booleans let us tell "absent" from "false" — important because the
// backend's PUT requires the full body.
type attributeRecord struct {
	AttributeID  string   `json:"attribute_id"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	IsVisible    *bool    `json:"is_visible,omitempty"`
	IsRequired   *bool    `json:"is_required,omitempty"`
	DisplayOrder int      `json:"display_order,omitempty"`
	Options      []string `json:"options,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
	HelpText     string   `json:"help_text,omitempty"`
}

type attributesListResponse struct {
	Success    bool              `json:"success"`
	Attributes []attributeRecord `json:"attributes"`
}

// cmdAttributes lists every attribute (custom contact column).
//
//	splashify attributes               full list
//	splashify attributes --search co   client-side substring filter on label
func cmdAttributes(args []string) error {
	fs := flag.NewFlagSet("attributes", flag.ContinueOnError)
	search := fs.String("search", "", "client-side substring filter on label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *search == "" {
		return runReq("GET", "/app/attributes", nil)
	}
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/attributes", nil)
	if err != nil {
		return err
	}
	var list attributesListResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	needle := strings.ToLower(*search)
	matched := make([]attributeRecord, 0, len(list.Attributes))
	for _, a := range list.Attributes {
		if strings.Contains(strings.ToLower(a.Label), needle) {
			matched = append(matched, a)
		}
	}
	out := map[string]any{"success": true, "attributes": matched, "count": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

// cmdAttribute handles per-entity reads, CRUD writes, the visibility
// toggle, and the up/down reorder action.
//
//	splashify attribute <id>                          show one
//	splashify attribute create --label "Company" --type TEXT [flags…]
//	splashify attribute update <id> [flags…]          PUT (read-modify-write)
//	splashify attribute delete <id>                   soft-delete
//	splashify attribute toggle-visibility <id>        flip is_visible
//	splashify attribute reorder <id> up | down        move in display order
//
// Types: TEXT, NUMBER, EMAIL, PHONE, DATE, SELECT, MULTISELECT, URL, CHECKBOX
//
// SELECT / MULTISELECT require --options (comma-separated). Validation
// happens server-side; the CLI passes whatever you provide.
func cmdAttribute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify attribute <id|create|update|delete|toggle-visibility|reorder> …")
	}

	switch args[0] {
	case "create":
		return cmdAttributeCreate(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify attribute update <id> [--label …] [--type …] [--visible …] [--required …] [--options …] [--default …] [--help …]")
		}
		return cmdAttributeUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify attribute delete <id>")
		}
		return runReq("DELETE", "/app/attributes/"+args[1], nil)
	case "toggle-visibility", "toggle":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify attribute toggle-visibility <id>")
		}
		return runReq("POST", "/app/attributes/"+args[1]+"/toggle-visibility", nil)
	case "reorder", "move":
		if len(args) < 3 {
			return fmt.Errorf("usage: splashify attribute reorder <id> up|down")
		}
		dir := strings.ToLower(args[2])
		if dir != "up" && dir != "down" {
			return fmt.Errorf("direction must be 'up' or 'down', got %q", args[2])
		}
		return runReq("POST", "/app/attributes/reorder", map[string]any{
			"attribute_id": args[1],
			"direction":    dir,
		})
	}

	// Default: treat the first arg as an attribute_id. The backend has no
	// per-id GET, so we fetch the list and filter — clean fallback that
	// behaves the way users expect.
	id := args[0]
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/attributes", nil)
	if err != nil {
		return err
	}
	var list attributesListResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("decode attributes list: %w", err)
	}
	for _, a := range list.Attributes {
		if a.AttributeID == id {
			encoded, _ := json.MarshalIndent(a, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("attribute %s not found", id)
}

// cmdAttributeCreate POSTs a new attribute. --label and --type are
// required; the rest mirror the dialog in /settings/attributes. --options
// is comma-separated; --visible / --required accept true/false.
func cmdAttributeCreate(args []string) error {
	fs := flag.NewFlagSet("attribute create", flag.ContinueOnError)
	label := fs.String("label", "", "attribute label (required, max 100 chars; spaces become underscores)")
	atype := fs.String("type", "", "TEXT|NUMBER|EMAIL|PHONE|DATE|SELECT|MULTISELECT|URL|CHECKBOX (required)")
	options := fs.String("options", "", "comma-separated options (required for SELECT / MULTISELECT)")
	visible := fs.String("visible", "", "true|false (default: backend chooses, usually true)")
	required := fs.String("required", "", "true|false (default: false)")
	defaultVal := fs.String("default", "", "default value for new contacts")
	helpText := fs.String("help", "", "help text shown to users")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *label == "" || *atype == "" {
		return fmt.Errorf(`usage: splashify attribute create --label "Company" --type TEXT [--options "a,b,c"] [--visible true|false] [--required true|false] [--default …] [--help …]`)
	}
	// Replicate the page's UX: spaces in labels become underscores. Lets
	// users type "Job Title" and get "Job_Title".
	cleanLabel := strings.ReplaceAll(*label, " ", "_")

	body := map[string]any{
		"label": cleanLabel,
		"type":  strings.ToUpper(*atype),
	}
	if *options != "" {
		body["options"] = splitTags(*options)
	}
	if *defaultVal != "" {
		body["default_value"] = *defaultVal
	}
	if *helpText != "" {
		body["help_text"] = *helpText
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if passed["visible"] {
		b, err := parseBool(*visible)
		if err != nil {
			return fmt.Errorf("--visible must be true or false")
		}
		body["is_visible"] = b
	}
	if passed["required"] {
		b, err := parseBool(*required)
		if err != nil {
			return fmt.Errorf("--required must be true or false")
		}
		body["is_required"] = b
	}
	return runReq("POST", "/app/attributes", body)
}

// cmdAttributeUpdate is read-modify-write — the backend's PUT requires
// label + type + the full set of optional columns. Load the current row,
// overlay the flags the user explicitly passed, send the full body.
func cmdAttributeUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("attribute update", flag.ContinueOnError)
	label := fs.String("label", "", "new label")
	atype := fs.String("type", "", "new type")
	options := fs.String("options", "", "comma-separated options")
	visible := fs.String("visible", "", "true|false")
	required := fs.String("required", "", "true|false")
	defaultVal := fs.String("default", "", "default value")
	helpText := fs.String("help", "", "help text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify attribute update <id> [--label …] [--type …] [--visible …] [--required …] [--options …] [--default …] [--help …]")
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	// Seed the body from the current attribute.
	raw, err := api.callRaw("GET", "/app/attributes", nil)
	if err != nil {
		return err
	}
	var list attributesListResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("decode attributes list: %w", err)
	}
	var current *attributeRecord
	for i := range list.Attributes {
		if list.Attributes[i].AttributeID == id {
			current = &list.Attributes[i]
			break
		}
	}
	if current == nil {
		return fmt.Errorf("attribute %s not found", id)
	}

	body := map[string]any{
		"label": current.Label,
		"type":  current.Type,
	}
	if current.IsVisible != nil {
		body["is_visible"] = *current.IsVisible
	}
	if current.IsRequired != nil {
		body["is_required"] = *current.IsRequired
	}
	if len(current.Options) > 0 {
		body["options"] = current.Options
	}
	if current.DefaultValue != "" {
		body["default_value"] = current.DefaultValue
	}
	if current.HelpText != "" {
		body["help_text"] = current.HelpText
	}

	// Overlay the user's flags.
	if passed["label"] {
		body["label"] = strings.ReplaceAll(*label, " ", "_")
	}
	if passed["type"] {
		body["type"] = strings.ToUpper(*atype)
	}
	if passed["options"] {
		body["options"] = splitTags(*options)
	}
	if passed["default"] {
		body["default_value"] = *defaultVal
	}
	if passed["help"] {
		body["help_text"] = *helpText
	}
	if passed["visible"] {
		b, err := parseBool(*visible)
		if err != nil {
			return fmt.Errorf("--visible must be true or false")
		}
		body["is_visible"] = b
	}
	if passed["required"] {
		b, err := parseBool(*required)
		if err != nil {
			return fmt.Errorf("--required must be true or false")
		}
		body["is_required"] = b
	}

	return runReq("PUT", "/app/attributes/"+id, body)
}

// ─── segments — CRUD + introspection on /settings/segments ───────────────────

// cmdSegments lists segments with pagination + server-side search.
//
//	splashify segments                              first page
//	splashify segments --page 2 --limit 50          paginate
//	splashify segments --search vip                 server-side name search
//	splashify segments stats                        overall stats
func cmdSegments(args []string) error {
	// `segments stats` is a small convenience for the top-level endpoint.
	if len(args) > 0 && args[0] == "stats" {
		return runReq("GET", "/app/segments/stats", nil)
	}
	fs := flag.NewFlagSet("segments", flag.ContinueOnError)
	search := fs.String("search", "", "server-side name search")
	page := fs.String("page", "", "page number")
	limit := fs.String("limit", "", "items per page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := withQuery("/app/segments", map[string]string{
		"search": *search, "page": *page, "limit": *limit,
	})
	return runReq("GET", path, nil)
}

// cmdSegment is the singular form — per-entity actions and CRUD writes.
//
//	splashify segment <id>                          GET one segment
//	splashify segment <id> contacts [--page --limit]
//	splashify segment <id> count                    current member count
//	splashify segment <id> refresh                  recompute count
//
//	splashify segment create   --name "VIP" --filters '{"conditions":[…],"logic":"and"}' \
//	                           [--description "…"] [--dynamic true|false] [--active true|false]
//	splashify segment update <id> [--name …] [--description …] [--filters …] \
//	                              [--dynamic …] [--active …]
//	splashify segment delete <id>
//
// Filter DSL (shipped as-is to the backend's POST /app/segments):
//
//	{
//	  "conditions": [
//	    {"field": "tags",        "operator": "includes", "value": "VIP"},
//	    {"field": "hasOptedOut", "operator": "equals",   "value": false}
//	  ],
//	  "logic": "and"
//	}
//
// Fields: displayName, phoneNumber, email, tags, hasOptedOut, isBlocked,
// createdAt, updatedAt. Operators: equals, notEquals, contains, notContains,
// startsWith, endsWith, isEmpty, isNotEmpty, includes, notIncludes (the
// last two are the array form used by `tags`).
func cmdSegment(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify segment <id|create|update|delete|stats> …")
	}

	switch args[0] {
	case "create":
		return cmdSegmentCreate(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify segment update <id> [--name …] [--description …] [--filters …] [--active true|false] [--dynamic true|false]")
		}
		return cmdSegmentUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify segment delete <id>")
		}
		return runReq("DELETE", "/app/segments/"+args[1], nil)
	case "stats":
		return runReq("GET", "/app/segments/stats", nil)
	}

	// Treat the first arg as a segment id; optional sub-action follows.
	id := args[0]
	if len(args) == 1 {
		return runReq("GET", "/app/segments/"+id, nil)
	}
	switch args[1] {
	case "contacts":
		fs := flag.NewFlagSet("segment contacts", flag.ContinueOnError)
		page := fs.String("page", "", "page number")
		limit := fs.String("limit", "", "items per page")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		path := withQuery("/app/segments/"+id+"/contacts", map[string]string{
			"page": *page, "limit": *limit,
		})
		return runReq("GET", path, nil)
	case "count":
		return runReq("GET", "/app/segments/"+id+"/count", nil)
	case "refresh", "recompute":
		return runReq("POST", "/app/segments/"+id+"/refresh", nil)
	default:
		return fmt.Errorf("unknown segment action: %s\nrun: splashify segment", args[1])
	}
}

// cmdSegmentCreate parses the create flags, validates the filter JSON, and
// POSTs. --filters is required; everything else is optional. --dynamic /
// --active accept "true"/"false" strings so the flag.Visit check below can
// tell "explicitly set" from "left at default".
func cmdSegmentCreate(args []string) error {
	fs := flag.NewFlagSet("segment create", flag.ContinueOnError)
	name := fs.String("name", "", "segment name (required)")
	desc := fs.String("description", "", "optional description")
	filters := fs.String("filters", "", `filter DSL JSON, e.g. '{"conditions":[…],"logic":"and"}' (required)`)
	dynamic := fs.String("dynamic", "", "true|false (default: backend chooses)")
	active := fs.String("active", "", "true|false (default: backend chooses)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *filters == "" {
		return fmt.Errorf(`usage: splashify segment create --name "VIP" --filters '{"conditions":[…],"logic":"and"}' [--description …] [--dynamic true|false] [--active true|false]`)
	}

	var filterObj map[string]any
	if err := json.Unmarshal([]byte(*filters), &filterObj); err != nil {
		return fmt.Errorf("--filters must be valid JSON: %w", err)
	}

	body := map[string]any{
		"name":    *name,
		"filters": filterObj,
	}
	if *desc != "" {
		body["description"] = *desc
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if passed["dynamic"] {
		b, err := parseBool(*dynamic)
		if err != nil {
			return fmt.Errorf("--dynamic must be true or false")
		}
		body["is_dynamic"] = b
	}
	if passed["active"] {
		b, err := parseBool(*active)
		if err != nil {
			return fmt.Errorf("--active must be true or false")
		}
		body["is_active"] = b
	}

	return runReq("POST", "/app/segments", body)
}

// cmdSegmentUpdate PATCHes only the fields the user explicitly passed. The
// backend's PATCH accepts an arbitrary subset, so partial updates are safe
// without read-modify-write.
func cmdSegmentUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("segment update", flag.ContinueOnError)
	name := fs.String("name", "", "new name")
	desc := fs.String("description", "", "new description")
	filters := fs.String("filters", "", "new filters JSON")
	dynamic := fs.String("dynamic", "", "true|false")
	active := fs.String("active", "", "true|false")
	if err := fs.Parse(args); err != nil {
		return err
	}

	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf(`usage: splashify segment update <id> [--name …] [--description …] [--filters '{…}'] [--dynamic true|false] [--active true|false]`)
	}

	body := map[string]any{}
	if passed["name"] {
		body["name"] = *name
	}
	if passed["description"] {
		body["description"] = *desc
	}
	if passed["filters"] {
		var filterObj map[string]any
		if err := json.Unmarshal([]byte(*filters), &filterObj); err != nil {
			return fmt.Errorf("--filters must be valid JSON: %w", err)
		}
		body["filters"] = filterObj
	}
	if passed["dynamic"] {
		b, err := parseBool(*dynamic)
		if err != nil {
			return fmt.Errorf("--dynamic must be true or false")
		}
		body["is_dynamic"] = b
	}
	if passed["active"] {
		b, err := parseBool(*active)
		if err != nil {
			return fmt.Errorf("--active must be true or false")
		}
		body["is_active"] = b
	}

	return runReq("PATCH", "/app/segments/"+id, body)
}

// parseBool accepts the canonical truthy/falsy strings users will type
// at the shell: true/false, yes/no, 1/0, on/off (case-insensitive).
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1", "on", "y", "t":
		return true, nil
	case "false", "no", "0", "off", "n", "f":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean: %q", s)
}

// ─── tags — CRUD on the tag library (/settings/tags) ─────────────────────────

// cmdTags lists every tag on the account.
//
//	splashify tags                 list all tags
//	splashify tags --search vip    client-side substring filter
func cmdTags(args []string) error {
	fs := flag.NewFlagSet("tags", flag.ContinueOnError)
	search := fs.String("search", "", "client-side substring filter on tag name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *search == "" {
		return runReq("GET", "/app/tags", nil)
	}

	// The backend doesn't accept a search query, so mirror the page's
	// behaviour and filter client-side.
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/tags", nil)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool             `json:"success"`
		Tags    []map[string]any `json:"tags"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		// Some deployments return the array under a different key — just
		// dump the raw response so users can see what they got.
		printJSON(raw)
		return nil
	}
	needle := strings.ToLower(*search)
	matched := make([]map[string]any, 0, len(resp.Tags))
	for _, t := range resp.Tags {
		name, _ := t["name"].(string)
		if strings.Contains(strings.ToLower(name), needle) {
			matched = append(matched, t)
		}
	}
	out := map[string]any{"success": true, "tags": matched, "count": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

// cmdTag covers the write side and the single-entity read.
//
//	splashify tag create "<name>"             POST /app/tags
//	splashify tag rename <id> "<new name>"    PUT /app/tags/:id
//	splashify tag delete <id>                 DELETE /app/tags/:id
//	splashify tag <id>                        GET /app/tags/:id (if backend supports it)
//
// On delete, every contact tagged with this tag is unmapped — same as the
// /settings/tags page warns. There is no soft-undo.
func cmdTag(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify tag <create|rename|delete|...> …\n       splashify tags  (list)")
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf(`usage: splashify tag create "<name>"`)
		}
		name := strings.Join(args[1:], " ")
		return runReq("POST", "/app/tags", map[string]any{"name": name})

	case "rename", "update", "edit":
		if len(args) < 3 {
			return fmt.Errorf(`usage: splashify tag rename <id> "<new-name>"`)
		}
		id := args[1]
		name := strings.Join(args[2:], " ")
		return runReq("PUT", "/app/tags/"+id, map[string]any{"name": name})

	case "delete", "rm", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify tag delete <id>")
		}
		return runReq("DELETE", "/app/tags/"+args[1], nil)

	default:
		// Treat the first arg as a tag id and try to GET it. Backends that
		// don't expose per-id GET will return 404 — surface it as-is.
		return runReq("GET", "/app/tags/"+args[0], nil)
	}
}

// ─── opt — Opt-out / Opt-in keyword management ───────────────────────────────

// optConfig mirrors one side of the opt-settings payload. The backend's PUT
// expects both opt_out and opt_in present, so every write does a
// read-modify-write to preserve the other side.
type optConfig struct {
	Keywords        []string `json:"keywords"`
	Response        string   `json:"response"`
	ResponseEnabled bool     `json:"response_enabled"`
}

type optSettings struct {
	Success bool      `json:"success"`
	OptOut  optConfig `json:"opt_out"`
	OptIn   optConfig `json:"opt_in"`
}

// cmdOpt drives /settings/opt-management — keyword lists, auto-response
// text, and the response on/off toggle for both opt-out and opt-in.
//
//	splashify opt                                full settings (default)
//	splashify opt out | in                       just one side
//	splashify opt out add STOP UNSUBSCRIBE       add keywords
//	splashify opt out remove STOP                remove keywords
//	splashify opt out response "<text>"          set auto-response text
//	splashify opt out response-on | response-off toggle auto-response
//	splashify opt in  …                          same actions on the in side
//
// Every write is read-modify-write — the backend's PUT requires both
// opt_out and opt_in in the body, so we load the current state, mutate
// the requested side, and submit the full payload.
func cmdOpt(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/opt-settings", nil)
	}

	side := args[0]
	switch side {
	case "status", "show":
		return runReq("GET", "/app/opt-settings", nil)
	case "out", "in":
		// fall through to action handling
	default:
		return fmt.Errorf("unknown opt subcommand: %s\nrun: splashify opt", side)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	current, err := loadOptSettings(api)
	if err != nil {
		return err
	}
	target := &current.OptOut
	if side == "in" {
		target = &current.OptIn
	}

	// `splashify opt out` / `splashify opt in` with no further args — print
	// just that side.
	if len(args) == 1 {
		raw, mErr := json.MarshalIndent(target, "", "  ")
		if mErr != nil {
			return fmt.Errorf("encode: %w", mErr)
		}
		fmt.Println(string(raw))
		return nil
	}

	action := args[1]
	rest := args[2:]

	switch action {
	case "add":
		if len(rest) == 0 {
			return fmt.Errorf("usage: splashify opt %s add <keyword> [<keyword> ...]", side)
		}
		for _, kw := range rest {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			already := false
			for _, existing := range target.Keywords {
				if strings.EqualFold(existing, kw) {
					already = true
					break
				}
			}
			if !already {
				target.Keywords = append(target.Keywords, kw)
			}
		}

	case "remove", "rm":
		if len(rest) == 0 {
			return fmt.Errorf("usage: splashify opt %s remove <keyword> [<keyword> ...]", side)
		}
		drop := map[string]bool{}
		for _, kw := range rest {
			drop[strings.ToLower(strings.TrimSpace(kw))] = true
		}
		kept := target.Keywords[:0]
		for _, kw := range target.Keywords {
			if !drop[strings.ToLower(kw)] {
				kept = append(kept, kw)
			}
		}
		target.Keywords = kept

	case "response":
		// Allow `splashify opt out response ""` to clear the text by passing
		// an explicit empty single-arg, or any args joined by space.
		if len(rest) == 0 {
			return fmt.Errorf(`usage: splashify opt %s response "<text>"`, side)
		}
		target.Response = strings.Join(rest, " ")

	case "response-on", "enable":
		target.ResponseEnabled = true

	case "response-off", "disable":
		target.ResponseEnabled = false

	default:
		return fmt.Errorf(`unknown action: %s
usage: splashify opt %s <add|remove|response|response-on|response-off> …`, action, side)
	}

	return saveOptSettings(api, current)
}

// loadOptSettings reads the current opt-settings into a parsed struct,
// normalising nil slices so the round-trip back to JSON doesn't emit `null`.
func loadOptSettings(api *apiClient) (*optSettings, error) {
	raw, err := api.callRaw("GET", "/app/opt-settings", nil)
	if err != nil {
		return nil, err
	}
	var s optSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode opt-settings: %w", err)
	}
	if s.OptOut.Keywords == nil {
		s.OptOut.Keywords = []string{}
	}
	if s.OptIn.Keywords == nil {
		s.OptIn.Keywords = []string{}
	}
	return &s, nil
}

// saveOptSettings PUTs the full opt-settings payload (both sides required by
// the backend) and prints the response.
func saveOptSettings(api *apiClient, s *optSettings) error {
	body := map[string]any{
		"opt_out": s.OptOut,
		"opt_in":  s.OptIn,
	}
	raw, err := api.callRaw("PUT", "/app/opt-settings", body)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}

// ─── media — list, upload, delete the user's media library ───────────────────

// cmdMedia surfaces /media — list/filter the user's uploaded files, upload
// new files from disk, see storage usage, and delete files by media_id.
//
//	splashify media                     list everything (default)
//	splashify media list [--type T]     filter by image | video | audio | document
//	splashify media storage             storage quota + usage
//	splashify media upload <path>       upload a file from disk
//	splashify media delete <media_id>   delete a media row by ID
//
// Accepted file types (per backend's classifyFileType):
//   images:    .jpg .jpeg .png
//   videos:    .mp4
//   audio:     .mp3 .ogg .webm .aac .amr .m4a
//   documents: .pdf .xlsx .xls .doc .docx .csv
func cmdMedia(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		fs := flag.NewFlagSet("media list", flag.ContinueOnError)
		ftype := fs.String("type", "", "filter: image, video, audio, document")
		// `splashify media` (no subcommand) has no further args; only "list" / "ls" can take flags.
		flagArgs := args
		if sub == "list" || sub == "ls" {
			flagArgs = args[1:]
		} else {
			flagArgs = []string{}
		}
		if err := fs.Parse(flagArgs); err != nil {
			return err
		}
		path := "/app/media"
		if *ftype != "" {
			path += "?type=" + url.QueryEscape(*ftype)
		}
		return runReq("GET", path, nil)

	case "storage", "quota":
		return runReq("GET", "/app/media/storage", nil)

	case "upload":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify media upload <path-to-file>")
		}
		return cmdMediaUpload(args[1])

	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify media delete <media_id>")
		}
		return runReq("DELETE", "/app/media/"+args[1], nil)

	default:
		return fmt.Errorf("unknown media subcommand: %s\nrun: splashify media", sub)
	}
}

// cmdMediaUpload posts a single file to /app/media/upload via multipart.
// Backend constraints (image/video/audio/doc + size caps + storage quota)
// are enforced server-side — the CLI just streams the file up and prints
// whatever JSON comes back.
func cmdMediaUpload(filePath string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not accessible: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; pass a single file", filePath)
	}

	api := newAPIClient(cfg.BaseURL, cfg.Token)
	fmt.Fprintf(os.Stderr, "Uploading %s (%d bytes)…\n", filepath.Base(filePath), info.Size())
	raw, err := api.uploadFile("/app/media/upload", "file", filePath)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}

// ─── subscription — read-only mirror of /settings/subscriptions ──────────────

// cmdSubscription surfaces the user's current plan + add-ons + plan list.
// Read-only: no upgrade, no payment initiation, no coupon validation —
// every mutation stays in the web app, with the CLI pointing users at
// /settings/subscriptions when an upgrade is needed.
//
//	splashify subscription                  consolidated view (default)
//	splashify subscription status           /app/plans/subscription
//	splashify subscription plans            /plans — available plans
//	splashify subscription addons           current add-ons only
func cmdSubscription(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status", "current":
		return runReq("GET", "/app/plans/subscription", nil)
	case "plans", "available":
		// /plans is at /api/v1/plans (not under /app), so callRaw still works.
		return runReq("GET", "/plans", nil)
	case "addons":
		// Reuse status — the addons live on the same payload, just project them.
		cfg, err := requireConfig()
		if err != nil {
			return err
		}
		raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/plans/subscription", nil)
		if err != nil {
			return err
		}
		var parsed struct {
			Addons json.RawMessage `json:"addons"`
		}
		_ = json.Unmarshal(raw, &parsed)
		if len(parsed.Addons) == 0 {
			parsed.Addons = json.RawMessage("[]")
		}
		printJSON(parsed.Addons)
		return nil
	case "", "details", "overview":
		return cmdSubscriptionOverview()
	default:
		return fmt.Errorf("unknown subscription subcommand: %s\nrun: splashify subscription", sub)
	}
}

// cmdSubscriptionOverview merges the subscription detail (current plan +
// add-ons) with the eligibility snapshot (so the user sees plan_status,
// trial_ends_at, and the upgrade URL in one place).
func cmdSubscriptionOverview() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	fetch := func(path string) json.RawMessage {
		out, err := api.callRaw("GET", path, nil)
		if err != nil {
			return json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		return out
	}

	combined := map[string]json.RawMessage{
		"subscription": fetch("/app/plans/subscription"),
		"eligibility":  fetch("/app/developer/cli-eligibility"),
		"available_plans": fetch("/plans"),
	}

	out, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// ─── billing — read-only mirror of /settings/billing + invoices/logs ─────────

// cmdBilling surfaces everything the app's "Billing" page reads — the GST
// billing profile, recent invoices, and billing logs. Strictly read-only: no
// profile update, no GSTIN validation, no certificate upload. Mutations
// remain in the web app.
//
//	splashify billing                       consolidated view (default)
//	splashify billing profile               /app/billing — GST profile + address
//	splashify billing invoices              /app/invoices — invoice list
//	splashify billing logs [--period all]   /app/expenses/billing-logs
func cmdBilling(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "profile":
		return runReq("GET", "/app/billing", nil)
	case "invoices":
		return runReq("GET", "/app/invoices", nil)
	case "logs":
		fs := flag.NewFlagSet("billing logs", flag.ContinueOnError)
		period := fs.String("period", "all", "time window: all, 30d, 90d, today, etc.")
		limit := fs.String("limit", "", "max entries (server default if empty)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := withQuery("/app/expenses/billing-logs", map[string]string{
			"period": *period, "limit": *limit,
		})
		return runReq("GET", path, nil)
	case "", "details", "overview":
		return cmdBillingOverview()
	default:
		return fmt.Errorf("unknown billing subcommand: %s\nrun: splashify billing", sub)
	}
}

// cmdBillingOverview fetches every billing-related read the dashboard touches
// and prints them as one consolidated JSON object. Sections that fail are
// reported per-section as `{"error":"..."}` so the rest of the view still
// renders. Subscription state lives on /app/me (plan_name, plan_status,
// plan_billing_cycle, plan_expires_at, trial_ends_at) so we include it here.
func cmdBillingOverview() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	fetch := func(path string) json.RawMessage {
		out, err := api.callRaw("GET", path, nil)
		if err != nil {
			return json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		return out
	}

	combined := map[string]json.RawMessage{
		"profile":      fetch("/app/billing"),
		"subscription": fetch("/app/me"),
		"wallet":       fetch("/app/wallet/info"),
		"invoices":     fetch("/app/invoices"),
		"logs":         fetch("/app/expenses/billing-logs?period=all"),
	}

	out, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	fmt.Println(string(out))
	return nil
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
		//
		// IMPORTANT — read-modify-write
		// The backend's UpdateWhatsAppProfile runs:
		//   UPDATE app_wabas SET profile_about=?, profile_description=?,
		//     profile_address=?, profile_email=?, profile_vertical=?,
		//     profile_websites=?, updated_at=? WHERE user_id=?
		// against every request body, binding nil for any field the caller
		// omitted. A partial update (e.g. `--about "..."` alone) therefore
		// nukes the other profile columns in the DB even though Meta itself
		// still has them. Until the backend ships a dynamic-UPDATE fix, we
		// preload the current profile from /app/dashboard/whatsapp-status
		// and overlay only the flags the user explicitly passed.
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

		// Record which flags the user actually passed (vs. left at default).
		// flag.Visit only walks explicitly-set flags, which is exactly the
		// distinction the read-modify-write needs.
		passed := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })

		if len(passed) == 0 && *raw == "" {
			return fmt.Errorf(`usage: splashify waba update [--about "..."] [--description "..."] [--address "..."] [--email "..."] [--vertical RETAIL] [--websites https://...,https://...] [--data '{"...":"..."}']`)
		}

		// Seed body with the current profile so omitted fields are preserved.
		// Best-effort: if the read fails, we still send what the user asked
		// for — same behaviour as before.
		cfg, err := requireConfig()
		if err != nil {
			return err
		}
		api := newAPIClient(cfg.BaseURL, cfg.Token)

		body := map[string]any{}
		if currentRaw, err := api.callRaw("GET", "/app/dashboard/whatsapp-status", nil); err == nil {
			var current struct {
				ProfileAbout       string   `json:"profile_about"`
				ProfileDescription string   `json:"profile_description"`
				ProfileAddress     string   `json:"profile_address"`
				ProfileEmail       string   `json:"profile_email"`
				ProfileVertical    string   `json:"profile_vertical"`
				ProfileWebsites    []string `json:"profile_websites"`
			}
			_ = json.Unmarshal(currentRaw, &current)
			body["about"] = current.ProfileAbout
			body["description"] = current.ProfileDescription
			body["address"] = current.ProfileAddress
			body["email"] = current.ProfileEmail
			body["vertical"] = current.ProfileVertical
			body["websites"] = current.ProfileWebsites
		} else {
			fmt.Fprintln(os.Stderr, "warning: could not preload current profile —", err)
		}

		// Overlay the explicitly-passed flags.
		if passed["about"] {
			body["about"] = *about
		}
		if passed["description"] {
			body["description"] = *desc
		}
		if passed["address"] {
			body["address"] = *address
		}
		if passed["email"] {
			body["email"] = *email
		}
		if passed["vertical"] {
			body["vertical"] = *vertical
		}
		if passed["websites"] {
			body["websites"] = splitTags(*websites)
		}

		// --data still wins on top, for advanced fields the named flags
		// don't cover (e.g. profile_picture_handle).
		if *raw != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(*raw), &extra); err != nil {
				return fmt.Errorf("--data must be a JSON object: %w", err)
			}
			for k, v := range extra {
				body[k] = v
			}
		}

		return runReq("PUT", "/app/dashboard/whatsapp-profile", body)

	default:
		return fmt.Errorf("unknown waba subcommand: %s\nrun: splashify waba", sub)
	}
}

// ─── analytics / wallet ──────────────────────────────────────────────────────
//
// (templates moved to cli/templates.go — that file owns `templates`, the
// singular `template <id>`, and the `rcs templates` / `rcs template <id>`
// subtrees.)

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
