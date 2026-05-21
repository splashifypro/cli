package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// ─── activity — read-only mirror of /activity-logs ───────────────────────────
//
// One backend endpoint:
//
//	GET /api/v1/app/activity-logs?action=&entity_type=&entity_id=&actor_id=&limit=
//
// The page itself is owner-only — the backend enforces that, so a non-owner
// oc_live_ token gets a 403 here (the CLI surfaces it as-is).
//
//	splashify activity                          latest 100 logs
//	splashify activity --limit 200              custom limit
//	splashify activity --action login           filter by action
//	splashify activity --entity contact         filter by entity type
//	splashify activity --entity contact --entity-id <id>
//	splashify activity --actor <actor_user_id>
//	splashify activity --search "john"          client-side filter on actor/description
//
// Known actions:        login, logout, created, updated, deleted, assigned,
//                       unassigned, blocked, unblocked, resolved, reopened.
// Known entity types:   auth, contact, conversation, template, message, tag.
//
// `--search` mirrors the page's search box (client-side substring match across
// actor_name, actor_email, description, entity_name). The backend has no
// equivalent query param.

type activityLogEntry struct {
	LogID       string `json:"log_id"`
	Action      string `json:"action"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	EntityName  string `json:"entity_name"`
	ActorID     string `json:"actor_id"`
	ActorName   string `json:"actor_name"`
	ActorEmail  string `json:"actor_email"`
	ActorRole   string `json:"actor_role"`
	Description string `json:"description"`
	Metadata    string `json:"metadata"`
	CreatedAt   string `json:"created_at"`
}

type activityListResponse struct {
	Success bool               `json:"success"`
	Logs    []activityLogEntry `json:"logs"`
}

func cmdActivity(args []string) error {
	fs := flag.NewFlagSet("activity", flag.ContinueOnError)
	action := fs.String("action", "", "filter by action (login, created, updated, deleted, …)")
	entity := fs.String("entity", "", "filter by entity type (auth, contact, conversation, template, message, tag)")
	entityID := fs.String("entity-id", "", "filter to a specific entity")
	actor := fs.String("actor", "", "filter by actor user_id")
	limit := fs.String("limit", "", "max records (server default if empty)")
	search := fs.String("search", "", "client-side substring filter on actor/description")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := withQuery("/app/activity-logs", map[string]string{
		"action":      *action,
		"entity_type": *entity,
		"entity_id":   *entityID,
		"actor_id":    *actor,
		"limit":       *limit,
	})

	// No client-side search → just stream the backend response through.
	if *search == "" {
		return runReq("GET", path, nil)
	}

	// With --search we need the parsed records so we can filter.
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", path, nil)
	if err != nil {
		return err
	}
	var list activityListResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		// Couldn't parse — emit the raw body so users still see something.
		printJSON(raw)
		return nil
	}
	needle := strings.ToLower(*search)
	matched := make([]activityLogEntry, 0, len(list.Logs))
	for _, l := range list.Logs {
		if strings.Contains(strings.ToLower(l.ActorName), needle) ||
			strings.Contains(strings.ToLower(l.ActorEmail), needle) ||
			strings.Contains(strings.ToLower(l.Description), needle) ||
			strings.Contains(strings.ToLower(l.EntityName), needle) {
			matched = append(matched, l)
		}
	}
	out := map[string]any{"success": true, "logs": matched, "count": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}
