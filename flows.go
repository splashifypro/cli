package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify flows` and `splashify flow <id>` — the CLI
// mirror of the app's /flows page. It covers:
//
//	flows           list every flow + response_count (same as the page)
//	flows sync      pull fresh data from Meta (same as the "Sync from Meta" button)
//	flows create-url   print the URL to create a Flow in Meta's Flow Builder
//	                   (same as the "Create in Meta" button)
//	flow <id>       show one flow (list-and-filter — no per-id GET on the backend)
//	flow <id> responses [--page] [--limit]   list submissions for this flow
//	flow <id> deprecate                       mark the flow deprecated
//	flow response <response_id>               show one submission by id
//
// Flow creation itself is **not** something the CLI can do — Meta requires
// a graphical builder. The CLI provides the URL via `flows create-url`; you
// open it in a browser, build the flow, then `flows sync` pulls it down.

// ─── splashify flows ─────────────────────────────────────────────────────────

func cmdFlows(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/flows", nil)

	case "sync", "refresh":
		return runReq("POST", "/app/flows/sync", nil)

	case "create-url", "create", "new", "create-link":
		return runReq("GET", "/app/flows/create-url", nil)

	default:
		return fmt.Errorf("unknown flows subcommand: %s\nrun: splashify flows", sub)
	}
}

// ─── splashify flow <id> ─────────────────────────────────────────────────────

func cmdFlow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify flow <flow_id> [responses|deprecate] …\n       splashify flow response <response_id>")
	}

	// `flow response <response_id>` — show one submission by id, regardless
	// of which flow it belongs to. Lifted out of the per-flow tree because
	// the backend endpoint takes only the response_id.
	if args[0] == "response" || args[0] == "responses" {
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("usage: splashify flow response <response_id>")
		}
		return runReq("GET", "/app/flows/responses/"+args[1], nil)
	}

	flowID := args[0]

	// Bare `flow <id>` — show one flow by filtering the list. The backend
	// has no per-id GET, so we use the same fallback as `member <id>`,
	// `canned <id>`, etc.
	if len(args) == 1 {
		return cmdFlowGet(flowID)
	}

	switch args[1] {
	case "responses", "submissions":
		return cmdFlowResponses(flowID, args[2:])

	case "response":
		if len(args) < 3 || args[2] == "" {
			return fmt.Errorf("usage: splashify flow <flow_id> response <response_id>")
		}
		// Same backend endpoint as the top-level form — the flow_id
		// argument here is purely user-facing convenience.
		return runReq("GET", "/app/flows/responses/"+args[2], nil)

	case "deprecate":
		return runReq("POST", "/app/flows/"+flowID+"/deprecate", nil)

	default:
		return fmt.Errorf("unknown flow action: %s\nrun: splashify flow %s", args[1], flowID)
	}
}

// cmdFlowGet fetches the full flows list and prints the one matching flowID.
func cmdFlowGet(flowID string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/flows", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success bool             `json:"success"`
		Flows   []map[string]any `json:"flows"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, f := range list.Flows {
		if fid, _ := f["flow_id"].(string); fid == flowID {
			encoded, _ := json.MarshalIndent(f, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("flow %s not found", flowID)
}

// cmdFlowResponses paginates the submissions for a given flow_id.
func cmdFlowResponses(flowID string, args []string) error {
	fs := flag.NewFlagSet("flow responses", flag.ContinueOnError)
	page := fs.String("page", "1", "page number (1-indexed)")
	limit := fs.String("limit", "20", "items per page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(flowID) == "" {
		return fmt.Errorf("flow_id is required")
	}
	path := withQuery("/app/flows/"+flowID+"/responses", map[string]string{
		"page": *page, "limit": *limit,
	})
	return runReq("GET", path, nil)
}
