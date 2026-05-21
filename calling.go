package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ─── calling — full surface of /calling and /calling/<id> ────────────────────
//
// Two top-level commands match the broadcast pattern:
//
//	splashify calling …                  list-side, settings, templates, sends, analytics
//	splashify call <call_id> …           per-call actions
//
// Sends + initiates run the same preflight as broadcasts (subscription
// active + non-zero wallet) via preflightSend(). The WebRTC live-audio
// endpoints (/app/whatsapp-calling/*) are intentionally NOT exposed —
// they require a real WebRTC peer with SDP/ICE, which isn't something a
// shell can drive. The CLI handles the REST-friendly parts: backend-side
// dial via /app/calling/initiate, template messages, call history,
// analytics, permissions, settings.

func cmdCalling(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/calling", nil)
	}
	switch args[0] {
	case "overview":
		return runReq("GET", "/app/calling", nil)

	case "settings":
		return cmdCallingSettings(args[1:])

	case "analytics":
		return cmdCallingAnalytics(args[1:])

	case "calls":
		return cmdCallingCalls(args[1:])

	case "permission-status":
		return cmdCallingPermissionStatus(args[1:])

	case "permissions":
		return cmdCallingPermissions(args[1:])

	case "templates":
		return runReq("GET", "/app/calling/templates", nil)

	case "template":
		return cmdCallingTemplate(args[1:])

	case "send":
		return cmdCallingSend(args[1:])

	default:
		return fmt.Errorf("unknown calling subcommand: %s\nrun: splashify calling", args[0])
	}
}

// cmdCall covers per-call actions.
//
//	splashify call <call_id>                          show one call
//	splashify call initiate --to +91…                 backend-side dial
//	splashify call upload-recording <call_id> <file>  multipart upload
func cmdCall(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify call <call_id|initiate|upload-recording> …")
	}
	switch args[0] {
	case "initiate":
		return cmdCallInitiate(args[1:])
	case "upload-recording":
		if len(args) < 3 {
			return fmt.Errorf("usage: splashify call upload-recording <call_id> <path-to-file>")
		}
		return cmdCallUploadRecording(args[1], args[2])
	default:
		// Treat as call_id.
		return runReq("GET", "/app/calling/calls/"+args[0], nil)
	}
}

// ─── settings ────────────────────────────────────────────────────────────────

func cmdCallingSettings(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/calling/settings", nil)
	}
	switch args[0] {
	case "update":
		fs := flag.NewFlagSet("calling settings update", flag.ContinueOnError)
		data := fs.String("data", "", `raw JSON for the "calling" sub-object (required)`)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *data == "" {
			return fmt.Errorf(`usage: splashify calling settings update --data '{"calling_enabled":true,"business_hours_enabled":true,…}'

The JSON is wrapped by the CLI as {"calling": <your_json>} and PUT to
/app/calling/settings — matching the page's updateCallingSettings.`)
		}
		var calling any
		if err := json.Unmarshal([]byte(*data), &calling); err != nil {
			return fmt.Errorf("--data must be valid JSON: %w", err)
		}
		return runReq("PUT", "/app/calling/settings", map[string]any{"calling": calling})
	default:
		return fmt.Errorf("usage: splashify calling settings [update --data '{…}']")
	}
}

// ─── analytics ───────────────────────────────────────────────────────────────

func cmdCallingAnalytics(args []string) error {
	fs := flag.NewFlagSet("calling analytics", flag.ContinueOnError)
	start := fs.String("start-time", "", "ISO 8601 start")
	end := fs.String("end-time", "", "ISO 8601 end")
	granularity := fs.String("granularity", "", "HALF_HOUR | DAILY | WEEKLY | MONTHLY")
	dimensions := fs.String("dimensions", "", "DIRECTION | STATUS")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/calling/analytics", map[string]string{
		"start_time":  *start,
		"end_time":    *end,
		"granularity": *granularity,
		"dimensions":  *dimensions,
	}), nil)
}

// ─── calls list / detail / initiate / upload-recording ───────────────────────

func cmdCallingCalls(args []string) error {
	fs := flag.NewFlagSet("calling calls", flag.ContinueOnError)
	search := fs.String("search", "", "search by phone / contact")
	status := fs.String("status", "", "filter by status (e.g. completed, missed, failed)")
	agent := fs.String("agent", "", "filter by agent")
	page := fs.String("page", "", "page number")
	limit := fs.String("limit", "", "items per page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/calling/calls", map[string]string{
		"search": *search, "status": *status, "agent": *agent,
		"page": *page, "limit": *limit,
	}), nil)
}

func cmdCallInitiate(args []string) error {
	fs := flag.NewFlagSet("call initiate", flag.ContinueOnError)
	to := fs.String("to", "", "destination phone with country code (required)")
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return fmt.Errorf(`usage: splashify call initiate --to "+91…"`)
	}
	if !*yes {
		if err := preflightSend("calls", true); err != nil {
			return err
		}
	}
	return runReq("POST", "/app/calling/initiate", map[string]any{"to": *to})
}

func cmdCallUploadRecording(callID, filePath string) error {
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
	if err := preflightSend("call recordings", false); err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	fmt.Fprintf(os.Stderr, "Uploading %s (%d bytes)…\n", filePath, info.Size())
	raw, err := api.uploadFile("/app/calling/calls/"+url.PathEscape(callID)+"/recording", "recording", filePath)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}

// ─── permissions ─────────────────────────────────────────────────────────────

func cmdCallingPermissionStatus(args []string) error {
	fs := flag.NewFlagSet("calling permission-status", flag.ContinueOnError)
	phone := fs.String("phone", "", "phone with country code (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *phone == "" {
		return fmt.Errorf(`usage: splashify calling permission-status --phone "+91…"`)
	}
	return runReq("GET", "/app/calling/permission-status?phone="+url.QueryEscape(*phone), nil)
}

func cmdCallingPermissions(args []string) error {
	fs := flag.NewFlagSet("calling permissions", flag.ContinueOnError)
	search := fs.String("search", "", "search filter")
	status := fs.String("status", "", "status filter")
	agent := fs.String("agent", "", "agent filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/calling/permissions", map[string]string{
		"search": *search, "status": *status, "agent": *agent,
	}), nil)
}

// ─── templates ───────────────────────────────────────────────────────────────

func cmdCallingTemplate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify calling template <status|create-call-button|create-permission|set-default> …")
	}
	switch args[0] {
	case "status":
		fs := flag.NewFlagSet("calling template status", flag.ContinueOnError)
		name := fs.String("name", "", "template name (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return fmt.Errorf(`usage: splashify calling template status --name "<template_name>"`)
		}
		return runReq("GET", "/app/calling/templates/status?name="+url.QueryEscape(*name), nil)

	case "create-call-button":
		fs := flag.NewFlagSet("calling template create-call-button", flag.ContinueOnError)
		name := fs.String("name", "", "template_name (required)")
		body := fs.String("body-text", "", "template body text (required)")
		buttonLabel := fs.String("button-label", "", "button label, e.g. 'Call us' (required)")
		phone := fs.String("phone-number", "", "the phone the button dials (required)")
		lang := fs.String("language", "en", "language_code (default: en)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || *body == "" || *buttonLabel == "" || *phone == "" {
			return fmt.Errorf(`usage: splashify calling template create-call-button --name <tpl> --body-text "…" --button-label "Call us" --phone-number "+91…" [--language en]`)
		}
		return runReq("POST", "/app/calling/templates/call-button", map[string]any{
			"template_name": *name,
			"body_text":     *body,
			"button_label":  *buttonLabel,
			"phone_number":  *phone,
			"language_code": *lang,
		})

	case "create-permission":
		fs := flag.NewFlagSet("calling template create-permission", flag.ContinueOnError)
		name := fs.String("name", "", "template_name (required)")
		body := fs.String("body-text", "", "template body text (required)")
		lang := fs.String("language", "en", "language_code (default: en)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || *body == "" {
			return fmt.Errorf(`usage: splashify calling template create-permission --name <tpl> --body-text "…" [--language en]`)
		}
		return runReq("POST", "/app/calling/templates/permission", map[string]any{
			"template_name": *name,
			"body_text":     *body,
			"language_code": *lang,
		})

	case "set-default":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify calling template set-default <template_id>")
		}
		return runReq("POST", "/app/calling/templates/set-default", map[string]any{
			"template_id": args[1],
		})

	default:
		return fmt.Errorf("unknown calling template subcommand: %s", args[0])
	}
}

// ─── send (template messages that drive calls) ───────────────────────────────

// Each "send" command costs money (template message + later the call itself
// when the user taps the button), so they preflight subscription + balance.
func cmdCallingSend(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify calling send <call-button|permission|template|permission-template> …")
	}
	switch args[0] {
	case "call-button":
		return cmdCallingSendCallButton(args[1:])
	case "permission":
		return cmdCallingSendPermission(args[1:])
	case "template":
		return cmdCallingSendTemplate(args[1:])
	case "permission-template":
		return cmdCallingSendPermissionTemplate(args[1:])
	default:
		return fmt.Errorf("unknown send subcommand: %s", args[0])
	}
}

func cmdCallingSendCallButton(args []string) error {
	fs := flag.NewFlagSet("calling send call-button", flag.ContinueOnError)
	to := fs.String("to", "", "recipient phone (required)")
	body := fs.String("body-text", "", "message body (required)")
	buttonLabel := fs.String("button-label", "", "button label (required)")
	phone := fs.String("phone-number", "", "the phone the button dials (required)")
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" || *body == "" || *buttonLabel == "" || *phone == "" {
		return fmt.Errorf(`usage: splashify calling send call-button --to "+91…" --body-text "…" --button-label "Call us" --phone-number "+91…"`)
	}
	if !*yes {
		if err := preflightSend("call-button messages", true); err != nil {
			return err
		}
	}
	return runReq("POST", "/app/calling/send-call-button", map[string]any{
		"to":            *to,
		"body_text":     *body,
		"button_label":  *buttonLabel,
		"phone_number":  *phone,
	})
}

func cmdCallingSendPermission(args []string) error {
	fs := flag.NewFlagSet("calling send permission", flag.ContinueOnError)
	to := fs.String("to", "", "recipient phone (required)")
	body := fs.String("body-text", "", "interactive body text (default mode)")
	mode := fs.String("type", "", "interactive | template (default: interactive when --body-text is set)")
	template := fs.String("template", "", `template JSON when --type=template, e.g. '{"name":"…","language":{"code":"en"}}'`)
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return fmt.Errorf(`usage: splashify calling send permission --to "+91…" [--body-text "…"] | --type template --template '{…}'`)
	}
	if !*yes {
		if err := preflightSend("permission messages", true); err != nil {
			return err
		}
	}
	body0 := map[string]any{"to": *to}
	if *mode != "" {
		body0["type"] = *mode
	}
	if *body != "" {
		body0["body_text"] = *body
	}
	if *template != "" {
		var t any
		if err := json.Unmarshal([]byte(*template), &t); err != nil {
			return fmt.Errorf("--template must be valid JSON: %w", err)
		}
		body0["template"] = t
	}
	return runReq("POST", "/app/calling/send-permission", body0)
}

func cmdCallingSendTemplate(args []string) error {
	fs := flag.NewFlagSet("calling send template", flag.ContinueOnError)
	to := fs.String("to", "", "recipient phone (required)")
	name := fs.String("name", "", "template_name (required)")
	lang := fs.String("language", "en", "language_code (default: en)")
	vars := fs.String("vars", "", `JSON array of variables, e.g. '["John","ORD-1024"]'`)
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" || *name == "" {
		return fmt.Errorf(`usage: splashify calling send template --to "+91…" --name <template> [--language en] [--vars '["a","b"]']`)
	}
	if !*yes {
		if err := preflightSend("template call messages", true); err != nil {
			return err
		}
	}
	body := map[string]any{
		"to":            *to,
		"template_name": *name,
		"language_code": *lang,
	}
	if *vars != "" {
		var parsed []string
		if err := json.Unmarshal([]byte(*vars), &parsed); err != nil {
			return fmt.Errorf(`--vars must be a JSON array of strings, e.g. '["John","ORD-1024"]': %w`, err)
		}
		body["variables"] = parsed
	}
	return runReq("POST", "/app/calling/send-template", body)
}

func cmdCallingSendPermissionTemplate(args []string) error {
	fs := flag.NewFlagSet("calling send permission-template", flag.ContinueOnError)
	to := fs.String("to", "", "recipient phone (required)")
	name := fs.String("name", "", "template_name (required)")
	lang := fs.String("language", "en", "language_code (default: en)")
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" || *name == "" {
		return fmt.Errorf(`usage: splashify calling send permission-template --to "+91…" --name <template> [--language en]`)
	}
	if !*yes {
		if err := preflightSend("permission template messages", true); err != nil {
			return err
		}
	}
	return runReq("POST", "/app/calling/send-permission-template", map[string]any{
		"to":            *to,
		"template_name": *name,
		"language_code": *lang,
	})
}

var _ = strings.TrimSpace // keep strings in the import set even if unused after edits
