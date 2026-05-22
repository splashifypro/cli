package main

import (
	"encoding/json"
	"flag"
	"fmt"
)

// This file implements `splashify integrations` — the CLI mirror of
// /integrations (and the per-slug pages it links to). The backend's
// integration model is split into three sub-surfaces, all wrapped here:
//
//	accounts   — third-party OAuth connections (Pipedream-style)
//	configs    — per-slug behavior (which template fires on which event)
//	logs       — webhook events received from the third party
//
// Backed by /api/v1/app/integrations/*.

// ─── splashify integrations ──────────────────────────────────────────────────

func cmdIntegrations(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		// Default view: configs (the per-slug behavior shown on /integrations).
		return runReq("GET", "/app/integrations/configs", nil)

	case "configs", "list-configs":
		return runReq("GET", "/app/integrations/configs", nil)

	case "config":
		return cmdIntegrationsConfig(args[1:])

	case "accounts", "connections":
		return runReq("GET", "/app/integrations/accounts", nil)

	case "account":
		return cmdIntegrationsAccount(args[1:])

	case "token", "connect-token":
		// Mints a short-lived token the third party uses to redirect-back.
		// No body — the backend stamps the user from the JWT/oc_live_ token.
		return runReq("POST", "/app/integrations/token", nil)

	case "logs":
		return cmdIntegrationsLogs(args[1:])

	case "log":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify integrations log <log_id>")
		}
		return runReq("GET", "/app/integrations/logs/"+args[1], nil)

	default:
		return fmt.Errorf("unknown integrations subcommand: %s\nrun: splashify integrations", sub)
	}
}

// ─── integrations config ─────────────────────────────────────────────────────

func cmdIntegrationsConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify integrations config <slug> [save|delete] …")
	}
	slug := args[0]
	if len(args) == 1 {
		// Bare `config <slug>` — show that one config by filtering the list
		// (no per-slug GET endpoint).
		return cmdIntegrationsConfigGet(slug)
	}
	switch args[1] {
	case "save", "set", "update", "edit":
		return cmdIntegrationsConfigSave(slug, args[2:])
	case "delete", "remove", "rm":
		return runReq("DELETE", "/app/integrations/configs/"+slug, nil)
	default:
		return fmt.Errorf("unknown config action: %s\nrun: splashify integrations config %s", args[1], slug)
	}
}

func cmdIntegrationsConfigGet(slug string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/integrations/configs", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success bool             `json:"success"`
		Configs []map[string]any `json:"configs"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, c := range list.Configs {
		if s, _ := c["slug"].(string); s == slug {
			encoded, _ := json.MarshalIndent(c, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("integration config %s not found", slug)
}

// cmdIntegrationsConfigSave PUTs the per-slug config. The page exposes a
// large form here — every field is optional, but the backend stores the
// full body, so we collect what the user passes and submit it as-is.
//
// Two fields hold nested JSON the form builds graphically. The CLI accepts
// them as JSON strings via --config and --vars; for the deeply-nested
// event_automations map we expose --events as a JSON object.
func cmdIntegrationsConfigSave(slug string, args []string) error {
	fs := flag.NewFlagSet("integrations config save", flag.ContinueOnError)
	enabled := fs.String("enabled", "", "true | false")
	configJSON := fs.String("config", "", `provider-specific config map JSON, e.g. '{"api_key":"…","webhook_secret":"…"}'`)
	templateID := fs.String("template", "", "fallback template_id used when no event-specific automation matches")
	templateName := fs.String("template-name", "", "template name (display only)")
	templateLang := fs.String("template-language", "", "BCP-47 language for the fallback template")
	varsJSON := fs.String("vars", "", `variable-mapping JSON, e.g. '{"{{1}}":"contact.first_name"}'`)
	phoneField := fs.String("phone-field", "", "incoming-payload field path that holds the phone number")
	eventsJSON := fs.String("events", "", `event automations JSON map: '{"signup":{"template_id":"…","variable_mappings":{…},"phone_field":"…"}}'`)
	if err := fs.Parse(args); err != nil {
		return err
	}

	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify integrations config %s save [--enabled true|false] [--template <id>] [--template-name …] [--template-language …] [--config '{…}'] [--vars '{…}'] [--phone-field …] [--events '{…}']", slug)
	}

	body := map[string]any{}
	if passed["enabled"] {
		b, err := parseBool(*enabled)
		if err != nil {
			return fmt.Errorf("--enabled must be true or false")
		}
		body["enabled"] = b
	}
	if passed["template"] {
		body["template_id"] = *templateID
	}
	if passed["template-name"] {
		body["template_name"] = *templateName
	}
	if passed["template-language"] {
		body["template_language"] = *templateLang
	}
	if passed["phone-field"] {
		body["phone_field"] = *phoneField
	}
	if passed["config"] {
		var m map[string]string
		if err := json.Unmarshal([]byte(*configJSON), &m); err != nil {
			return fmt.Errorf("--config must be a JSON object of string:string — %w", err)
		}
		body["config"] = m
	}
	if passed["vars"] {
		var m map[string]string
		if err := json.Unmarshal([]byte(*varsJSON), &m); err != nil {
			return fmt.Errorf("--vars must be a JSON object of string:string — %w", err)
		}
		body["variable_mappings"] = m
	}
	if passed["events"] {
		var m map[string]any
		if err := json.Unmarshal([]byte(*eventsJSON), &m); err != nil {
			return fmt.Errorf("--events must be a JSON object — %w", err)
		}
		body["event_automations"] = m
	}

	return runReq("PUT", "/app/integrations/configs/"+slug, body)
}

// ─── integrations account ───────────────────────────────────────────────────

func cmdIntegrationsAccount(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify integrations account <account_id> [disconnect]")
	}
	accountID := args[0]
	if len(args) == 1 {
		// No per-id GET on the backend — list-and-filter.
		cfg, err := requireConfig()
		if err != nil {
			return err
		}
		raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/integrations/accounts", nil)
		if err != nil {
			return err
		}
		var list struct {
			Success  bool             `json:"success"`
			Accounts []map[string]any `json:"accounts"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			printJSON(raw)
			return nil
		}
		for _, a := range list.Accounts {
			if id, _ := a["account_id"].(string); id == accountID {
				encoded, _ := json.MarshalIndent(a, "", "  ")
				fmt.Println(string(encoded))
				return nil
			}
		}
		return fmt.Errorf("integrations account %s not found", accountID)
	}
	switch args[1] {
	case "disconnect", "delete", "remove", "rm":
		return runReq("DELETE", "/app/integrations/accounts/"+accountID, nil)
	default:
		return fmt.Errorf("unknown account action: %s", args[1])
	}
}

// ─── integrations logs ──────────────────────────────────────────────────────

func cmdIntegrationsLogs(args []string) error {
	fs := flag.NewFlagSet("integrations logs", flag.ContinueOnError)
	limit := fs.String("limit", "", "max rows (server default if empty)")
	slug := fs.String("slug", "", "filter by integration slug")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := withQuery("/app/integrations/logs", map[string]string{
		"limit": *limit, "slug": *slug,
	})
	return runReq("GET", path, nil)
}
