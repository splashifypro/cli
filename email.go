package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ─── email — full email-marketing surface ────────────────────────────────────
//
// Mirrors five pages:
//
//	/email                       splashify email                  (dashboard stats)
//	/settings/email-domain       splashify email domains|domain   (DKIM/SPF/DMARC verification)
//	/email/templates             splashify email templates|template
//	/email/audience              splashify email audience …       (contacts + segments)
//	/email/campaigns             splashify email campaigns|campaign
//
// `template_json` and campaign body shapes are complex enough that the CLI
// accepts a `--file <path>` (and `--data '{…}'` for inline JSON) instead of
// trying to model the visual editor through flags. Build the JSON once with
// the web app, then `--file` it from scripts.

func cmdEmail(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/email/dashboard/stats", nil)
	}
	switch args[0] {
	case "stats", "dashboard":
		return runReq("GET", "/app/email/dashboard/stats", nil)

	case "domains":
		return cmdEmailDomains(args[1:])
	case "domain":
		return cmdEmailDomain(args[1:])

	case "templates":
		return cmdEmailTemplatesList(args[1:])
	case "template":
		return cmdEmailTemplate(args[1:])

	case "audience":
		return cmdEmailAudience(args[1:])

	case "campaigns":
		return cmdEmailCampaignsList(args[1:])
	case "campaign":
		return cmdEmailCampaign(args[1:])

	default:
		return fmt.Errorf("unknown email subcommand: %s\nrun: splashify email", args[0])
	}
}

// ─── domains ─────────────────────────────────────────────────────────────────

func cmdEmailDomains(_ []string) error {
	return runReq("GET", "/app/email/domains", nil)
}

func cmdEmailDomain(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify email domain <domain|add|verify|delete> …")
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email domain add <domain>")
		}
		return runReq("POST", "/app/email/domains", map[string]any{"domain": args[1]})

	case "verify":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email domain verify <domain>")
		}
		return runReq("POST", "/app/email/domains/"+url.PathEscape(args[1])+"/verify", nil)

	case "delete", "rm", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email domain delete <domain>")
		}
		return runReq("DELETE", "/app/email/domains/"+url.PathEscape(args[1]), nil)

	default:
		// Treat first arg as the domain itself — `splashify email domain example.com`.
		return runReq("GET", "/app/email/domains/"+url.PathEscape(args[0]), nil)
	}
}

// ─── templates ───────────────────────────────────────────────────────────────

// emailTemplateRecord is what GET returns; the seed for read-modify-write
// on `template update` since PUT requires the full body.
type emailTemplateRecord struct {
	TemplateID   string          `json:"template_id"`
	Name         string          `json:"name"`
	Subject      string          `json:"subject"`
	TemplateJSON json.RawMessage `json:"template_json"`
}

func cmdEmailTemplatesList(_ []string) error {
	return runReq("GET", "/app/email/templates", nil)
}

func cmdEmailTemplate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify email template <id|create|update|delete|preview> …")
	}
	switch args[0] {
	case "create":
		return cmdEmailTemplateCreate(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email template update <id> [--name] [--subject] [--file <json>|--data '{...}']")
		}
		return cmdEmailTemplateUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email template delete <id>")
		}
		return runReq("DELETE", "/app/email/templates/"+args[1], nil)
	case "preview":
		return cmdEmailTemplatePreview(args[1:])
	default:
		// Treat as template_id.
		return runReq("GET", "/app/email/templates/"+args[0], nil)
	}
}

func cmdEmailTemplateCreate(args []string) error {
	fs := flag.NewFlagSet("email template create", flag.ContinueOnError)
	name := fs.String("name", "", "template name (required)")
	subject := fs.String("subject", "", "email subject line (required)")
	file := fs.String("file", "", "path to template_json file (required unless --data)")
	data := fs.String("data", "", "inline JSON for template_json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *subject == "" {
		return fmt.Errorf(`usage: splashify email template create --name "Welcome" --subject "Welcome to …" --file ./template.json`)
	}
	tj, err := loadTemplateJSON(*file, *data)
	if err != nil {
		return err
	}
	return runReq("POST", "/app/email/templates", map[string]any{
		"name":          *name,
		"subject":       *subject,
		"template_json": tj,
	})
}

// cmdEmailTemplateUpdate is read-modify-write because the backend's PUT
// requires {name, subject, template_json}. Load current, overlay passed
// flags, send full body.
func cmdEmailTemplateUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("email template update", flag.ContinueOnError)
	name := fs.String("name", "", "new name")
	subject := fs.String("subject", "", "new subject")
	file := fs.String("file", "", "path to new template_json file")
	data := fs.String("data", "", "inline JSON for new template_json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify email template update <id> [--name …] [--subject …] [--file <json> | --data '{…}']")
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	raw, err := api.callRaw("GET", "/app/email/templates/"+id, nil)
	if err != nil {
		return fmt.Errorf("load current template: %w", err)
	}
	// The single-template GET wraps the row under a `template` key in some
	// builds; tolerate both shapes.
	var wrapper struct {
		Template emailTemplateRecord `json:"template"`
	}
	current := emailTemplateRecord{}
	if err := json.Unmarshal(raw, &current); err != nil || current.TemplateID == "" {
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return fmt.Errorf("decode template: %w", err)
		}
		current = wrapper.Template
	}

	body := map[string]any{
		"name":          current.Name,
		"subject":       current.Subject,
		"template_json": current.TemplateJSON,
	}
	if passed["name"] {
		body["name"] = *name
	}
	if passed["subject"] {
		body["subject"] = *subject
	}
	if passed["file"] || passed["data"] {
		tj, err := loadTemplateJSON(*file, *data)
		if err != nil {
			return err
		}
		body["template_json"] = tj
	}
	return runReq("PUT", "/app/email/templates/"+id, body)
}

func cmdEmailTemplatePreview(args []string) error {
	fs := flag.NewFlagSet("email template preview", flag.ContinueOnError)
	file := fs.String("file", "", "path to template_json file")
	data := fs.String("data", "", "inline JSON for template_json")
	vars := fs.String("vars", "", `inline JSON object for variables, e.g. '{"name":"Alice"}'`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	tj, err := loadTemplateJSON(*file, *data)
	if err != nil {
		return err
	}
	body := map[string]any{"template_json": tj}
	if *vars != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(*vars), &parsed); err != nil {
			return fmt.Errorf("--vars must be a JSON object: %w", err)
		}
		body["variables"] = parsed
	}
	return runReq("POST", "/app/email/templates/preview", body)
}

// loadTemplateJSON reads template_json from --file (path) or --data (inline)
// and returns the parsed object. Exactly one of file/data must be provided.
func loadTemplateJSON(file, data string) (any, error) {
	if file == "" && data == "" {
		return nil, fmt.Errorf("template_json required — pass --file <path> or --data '{…}'")
	}
	if file != "" && data != "" {
		return nil, fmt.Errorf("only one of --file or --data may be set")
	}
	var raw []byte
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		raw = b
	} else {
		raw = []byte(data)
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("template_json must be valid JSON: %w", err)
	}
	return parsed, nil
}

// ─── audience (contacts + segments) ──────────────────────────────────────────

func cmdEmailAudience(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/email/audience/stats", nil)
	}
	switch args[0] {
	case "stats":
		return runReq("GET", "/app/email/audience/stats", nil)
	case "contacts":
		return cmdEmailContactsList(args[1:])
	case "contact":
		return cmdEmailContact(args[1:])
	case "segments":
		return runReq("GET", "/app/email/audience/segments", nil)
	case "segment":
		return cmdEmailSegment(args[1:])
	default:
		return fmt.Errorf("unknown email audience subcommand: %s", args[0])
	}
}

func cmdEmailContactsList(args []string) error {
	fs := flag.NewFlagSet("email audience contacts", flag.ContinueOnError)
	status := fs.String("status", "", "filter by status (e.g. subscribed, unsubscribed)")
	search := fs.String("search", "", "search by email")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/email/audience/contacts", map[string]string{
		"status": *status, "search": *search,
	}), nil)
}

func cmdEmailContact(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify email audience contact <id|add|update|delete> …")
	}
	switch args[0] {
	case "add":
		return cmdEmailContactAdd(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email audience contact update <id> [--status …] [--metadata '{…}']")
		}
		return cmdEmailContactUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email audience contact delete <id>")
		}
		return runReq("DELETE", "/app/email/audience/contacts/"+args[1], nil)
	default:
		return runReq("GET", "/app/email/audience/contacts/"+args[0], nil)
	}
}

func cmdEmailContactAdd(args []string) error {
	fs := flag.NewFlagSet("email audience contact add", flag.ContinueOnError)
	emails := fs.String("emails", "", "comma-separated emails, e.g. a@x.com,b@x.com (required)")
	metadata := fs.String("metadata", "", `inline JSON object for metadata, e.g. '{"plan":"pro"}'`)
	segments := fs.String("segments", "", "comma-separated segment IDs to add the contacts to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *emails == "" {
		return fmt.Errorf(`usage: splashify email audience contact add --emails "a@x.com,b@x.com" [--metadata '{…}'] [--segments id1,id2]`)
	}
	body := map[string]any{"emails": splitTags(*emails)}
	if *metadata != "" {
		var md map[string]string
		if err := json.Unmarshal([]byte(*metadata), &md); err != nil {
			return fmt.Errorf("--metadata must be a JSON object of string keys/values: %w", err)
		}
		body["metadata"] = md
	}
	if *segments != "" {
		body["segment_ids"] = splitTags(*segments)
	}
	return runReq("POST", "/app/email/audience/contacts", body)
}

func cmdEmailContactUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("email audience contact update", flag.ContinueOnError)
	status := fs.String("status", "", "new status (e.g. subscribed, unsubscribed)")
	metadata := fs.String("metadata", "", "inline JSON object for metadata")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify email audience contact update <id> [--status …] [--metadata '{…}']")
	}
	body := map[string]any{}
	if passed["status"] {
		body["status"] = *status
	}
	if passed["metadata"] {
		var md map[string]string
		if err := json.Unmarshal([]byte(*metadata), &md); err != nil {
			return fmt.Errorf("--metadata must be a JSON object: %w", err)
		}
		body["metadata"] = md
	}
	return runReq("PUT", "/app/email/audience/contacts/"+id, body)
}

func cmdEmailSegment(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify email audience segment <id|create|update|delete|add-contacts|remove-contacts> …")
	}
	switch args[0] {
	case "create":
		return cmdEmailSegmentCreate(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email audience segment update <id> [--name …] [--description …]")
		}
		return cmdEmailSegmentUpdate(args[1], args[2:])
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email audience segment delete <id>")
		}
		return runReq("DELETE", "/app/email/audience/segments/"+args[1], nil)
	}

	// Treat the first arg as a segment_id, optional action follows.
	id := args[0]
	if len(args) == 1 {
		// No per-id GET in the backend — list-and-filter.
		cfg, err := requireConfig()
		if err != nil {
			return err
		}
		raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/email/audience/segments", nil)
		if err != nil {
			return err
		}
		var resp struct {
			Segments []map[string]any `json:"segments"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			printJSON(raw)
			return nil
		}
		for _, s := range resp.Segments {
			if sid, _ := s["segment_id"].(string); sid == id {
				out, _ := json.MarshalIndent(s, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			if sid, _ := s["id"].(string); sid == id {
				out, _ := json.MarshalIndent(s, "", "  ")
				fmt.Println(string(out))
				return nil
			}
		}
		return fmt.Errorf("segment %s not found", id)
	}

	switch args[1] {
	case "add-contacts":
		if len(args) < 3 {
			return fmt.Errorf("usage: splashify email audience segment <id> add-contacts <id1>,<id2>,…")
		}
		ids := splitTags(strings.Join(args[2:], ","))
		return runReq("POST", "/app/email/audience/segments/"+id+"/contacts",
			map[string]any{"contact_ids": ids})
	case "remove-contacts":
		if len(args) < 3 {
			return fmt.Errorf("usage: splashify email audience segment <id> remove-contacts <id1>,<id2>,…")
		}
		ids := splitTags(strings.Join(args[2:], ","))
		return runReq("DELETE", "/app/email/audience/segments/"+id+"/contacts",
			map[string]any{"contact_ids": ids})
	default:
		return fmt.Errorf("unknown segment action: %s", args[1])
	}
}

func cmdEmailSegmentCreate(args []string) error {
	fs := flag.NewFlagSet("email audience segment create", flag.ContinueOnError)
	name := fs.String("name", "", "segment name (required)")
	desc := fs.String("description", "", "optional description")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf(`usage: splashify email audience segment create --name "VIP" [--description "…"]`)
	}
	body := map[string]any{"name": *name}
	if *desc != "" {
		body["description"] = *desc
	}
	return runReq("POST", "/app/email/audience/segments", body)
}

func cmdEmailSegmentUpdate(id string, args []string) error {
	fs := flag.NewFlagSet("email audience segment update", flag.ContinueOnError)
	name := fs.String("name", "", "new name")
	desc := fs.String("description", "", "new description")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify email audience segment update <id> [--name …] [--description …]")
	}
	body := map[string]any{}
	if passed["name"] {
		body["name"] = *name
	}
	if passed["description"] {
		body["description"] = *desc
	}
	return runReq("PUT", "/app/email/audience/segments/"+id, body)
}

// ─── campaigns ───────────────────────────────────────────────────────────────

func cmdEmailCampaignsList(_ []string) error {
	return runReq("GET", "/app/email/campaigns", nil)
}

func cmdEmailCampaign(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify email campaign <id|create|send|cancel> …")
	}
	switch args[0] {
	case "create":
		return cmdEmailCampaignCreate(args[1:])
	case "send":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email campaign send <id>")
		}
		return runReq("POST", "/app/email/campaigns/"+args[1]+"/send", nil)
	case "cancel":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify email campaign cancel <id>")
		}
		return runReq("POST", "/app/email/campaigns/"+args[1]+"/cancel", nil)
	default:
		return runReq("GET", "/app/email/campaigns/"+args[0], nil)
	}
}

func cmdEmailCampaignCreate(args []string) error {
	fs := flag.NewFlagSet("email campaign create", flag.ContinueOnError)
	name := fs.String("name", "", "campaign name (required)")
	templateID := fs.String("template-id", "", "template_id (required)")
	fromName := fs.String("from-name", "", "sender display name (required)")
	fromEmail := fs.String("from-email", "", "sender address — must be on a verified domain (required)")
	replyTo := fs.String("reply-to", "", "optional Reply-To address")
	segmentIDs := fs.String("segment-ids", "", "comma-separated segment IDs")
	contactIDs := fs.String("contact-ids", "", "comma-separated contact IDs")
	scheduledAt := fs.String("scheduled-at", "", "ISO 8601 timestamp; omit to send immediately on `campaign send`")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *templateID == "" || *fromName == "" || *fromEmail == "" {
		return fmt.Errorf(`usage: splashify email campaign create \
  --name "May launch" --template-id <id> \
  --from-name "Acme" --from-email "hi@your-verified-domain.com" \
  [--reply-to "support@…"] \
  [--segment-ids id1,id2 | --contact-ids id1,id2] \
  [--scheduled-at 2026-06-01T10:00:00Z]`)
	}
	if *segmentIDs == "" && *contactIDs == "" {
		return fmt.Errorf("at least one of --segment-ids or --contact-ids is required")
	}
	body := map[string]any{
		"name":        *name,
		"template_id": *templateID,
		"from_name":   *fromName,
		"from_email":  *fromEmail,
	}
	if *replyTo != "" {
		body["reply_to"] = *replyTo
	}
	if *segmentIDs != "" {
		body["segment_ids"] = splitTags(*segmentIDs)
	}
	if *contactIDs != "" {
		body["contact_ids"] = splitTags(*contactIDs)
	}
	if *scheduledAt != "" {
		body["scheduled_at"] = *scheduledAt
	}
	return runReq("POST", "/app/email/campaigns", body)
}
