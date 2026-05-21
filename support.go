package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// ─── support — full mirror of /support and /support/<ticket_id> ──────────────
//
// Support is free for every account — no subscription / balance preflight
// here. Endpoints (all under /api/v1/app/tickets):
//
//	POST   /tickets                       create
//	GET    /tickets                       list
//	GET    /tickets/:ticket_id            detail (with replies)
//	POST   /tickets/:ticket_id/replies    add reply
//	POST   /tickets/:ticket_id/close      close
//
// Surface:
//
//	splashify support                                list tickets (default)
//	splashify support list [--status …] [--search …] client-side filter
//	splashify tickets                                alias for `support`
//
//	splashify ticket <id>                            detail
//	splashify ticket create --title "…" --description "…" \
//	                        --category bug|billing|feature_request|account|general \
//	                        [--priority low|medium|high|urgent]
//	splashify ticket <id> reply "<message>"          add a reply
//	splashify ticket <id> close                      close the ticket
//
// Categories (from the page): bug, billing, feature_request, account, general
// Priorities:                 low, medium, high, urgent
// Statuses (read-only):       open, in_progress, waiting_for_user, resolved, closed

func cmdSupport(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/tickets", nil)
	}
	switch args[0] {
	case "list", "ls":
		return cmdSupportList(args[1:])
	default:
		return fmt.Errorf("usage: splashify support [list --status … --search …]")
	}
}

// cmdSupportList mirrors the page's filter/search box — the backend's
// list endpoint doesn't accept query params, so filtering is client-side.
func cmdSupportList(args []string) error {
	fs := flag.NewFlagSet("support list", flag.ContinueOnError)
	status := fs.String("status", "", "open | in_progress | waiting_for_user | resolved | closed")
	search := fs.String("search", "", "client-side substring filter on title")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *status == "" && *search == "" {
		return runReq("GET", "/app/tickets", nil)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/tickets", nil)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool             `json:"success"`
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		printJSON(raw)
		return nil
	}
	needle := strings.ToLower(*search)
	matched := make([]map[string]any, 0, len(resp.Tickets))
	for _, t := range resp.Tickets {
		if *status != "" {
			st, _ := t["status"].(string)
			if !strings.EqualFold(st, *status) {
				continue
			}
		}
		if needle != "" {
			title, _ := t["title"].(string)
			if !strings.Contains(strings.ToLower(title), needle) {
				continue
			}
		}
		matched = append(matched, t)
	}
	out := map[string]any{"success": true, "tickets": matched, "count": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

// cmdTicket dispatches per-ticket actions and the `create` write.
//
//	splashify ticket create … flags …
//	splashify ticket <id>                         GET detail
//	splashify ticket <id> reply "<message>"       POST reply
//	splashify ticket <id> close                   POST close
func cmdTicket(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify ticket <id|create> …")
	}
	if args[0] == "create" {
		return cmdTicketCreate(args[1:])
	}

	id := args[0]
	if len(args) == 1 {
		return runReq("GET", "/app/tickets/"+id, nil)
	}

	switch args[1] {
	case "reply":
		if len(args) < 3 {
			return fmt.Errorf(`usage: splashify ticket <id> reply "<message>"`)
		}
		// Join the remaining args so users can type a reply without quotes:
		//   splashify ticket abc reply Thanks for the update
		msg := strings.Join(args[2:], " ")
		return runReq("POST", "/app/tickets/"+id+"/replies",
			map[string]any{"message": msg})

	case "close":
		return runReq("POST", "/app/tickets/"+id+"/close", nil)

	default:
		return fmt.Errorf(`unknown ticket action: %s
usage: splashify ticket <id> [reply "<message>" | close]`, args[1])
	}
}

func cmdTicketCreate(args []string) error {
	fs := flag.NewFlagSet("ticket create", flag.ContinueOnError)
	title := fs.String("title", "", "subject line (required)")
	desc := fs.String("description", "", "ticket body (required)")
	category := fs.String("category", "", "bug | billing | feature_request | account | general (required)")
	priority := fs.String("priority", "medium", "low | medium | high | urgent (default: medium)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *title == "" || *desc == "" || *category == "" {
		return fmt.Errorf(`usage: splashify ticket create \
  --title "Brief one-line subject" \
  --description "Full detail…" \
  --category bug|billing|feature_request|account|general \
  [--priority low|medium|high|urgent]`)
	}
	switch *category {
	case "bug", "billing", "feature_request", "account", "general":
	default:
		return fmt.Errorf(`--category must be one of: bug, billing, feature_request, account, general`)
	}
	switch *priority {
	case "low", "medium", "high", "urgent":
	default:
		return fmt.Errorf(`--priority must be one of: low, medium, high, urgent`)
	}
	return runReq("POST", "/app/tickets", map[string]any{
		"title":       *title,
		"description": *desc,
		"category":    *category,
		"priority":    *priority,
	})
}
