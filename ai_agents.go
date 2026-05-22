package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file implements `splashify ai-agents` (plural) and `splashify ai-agent`
// (singular) — the CLI mirror of the app's /ai-agents page + per-agent detail
// page. It covers the full lifecycle:
//
//	list / get / create / update / delete
//	set-default / unset-default
//	knowledge list / upload / delete
//
// Backed by /api/v1/app/ai-agents/*.

// agentChannelTypes is the backend-validated set. Instagram bot agents need
// the `instagram_automation_enabled` plan flag + a connected IG account;
// the backend returns 403 with code `ig_bot_blocked` if either is missing.
var agentChannelTypes = map[string]bool{"whatsapp": true, "instagram": true}

// agentKnowledgeExtensions are the accepted file extensions for knowledge
// uploads (enforced server-side; the CLI mirrors the validation client-side
// for a friendlier error before the round-trip).
var agentKnowledgeExtensions = map[string]bool{
	".pdf": true, ".docx": true, ".md": true, ".txt": true,
}

// ─── splashify ai-agents (plural) ────────────────────────────────────────────

func cmdAIAgents(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/ai-agents", nil)
	default:
		return fmt.Errorf("unknown ai-agents subcommand: %s\nrun: splashify ai-agents", sub)
	}
}

// ─── splashify ai-agent <id> (singular) ─────────────────────────────────────

func cmdAIAgent(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify ai-agent <agent_id|create|update|delete|set-default|unset-default|knowledge> …")
	}
	switch args[0] {
	case "create", "add", "new":
		return cmdAIAgentCreate(args[1:])
	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent update <agent_id> [--name …] [--role …] …")
		}
		return cmdAIAgentUpdate(args[1], args[2:])
	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent delete <agent_id>")
		}
		return runReq("DELETE", "/app/ai-agents/"+args[1], nil)
	case "set-default":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent set-default <agent_id>")
		}
		return runReq("POST", "/app/ai-agents/"+args[1]+"/set-default", nil)
	case "unset-default":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent unset-default <agent_id>")
		}
		return runReq("POST", "/app/ai-agents/"+args[1]+"/unset-default", nil)
	case "knowledge", "kb":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent knowledge <agent_id> [list|upload|delete] …")
		}
		return cmdAIAgentKnowledge(args[1], args[2:])
	}

	// Default: treat the first arg as agent_id and GET it.
	return runReq("GET", "/app/ai-agents/"+args[0], nil)
}

// ─── ai-agent create ────────────────────────────────────────────────────────

// cmdAIAgentCreate POSTs a new agent. The backend defaults --channel to
// "whatsapp" when empty; we set the default here so the CLI flag dump is
// accurate without an extra round-trip.
func cmdAIAgentCreate(args []string) error {
	fs := flag.NewFlagSet("ai-agent create", flag.ContinueOnError)
	name := fs.String("name", "", "agent name (required)")
	agentType := fs.String("agent-type", "", `agent_type, e.g. "support" / "sales" (required)`)
	channel := fs.String("channel", "whatsapp", "channel: whatsapp | instagram")
	industry := fs.String("industry", "", "industry tag, e.g. retail, finance")
	useCase := fs.String("use-case", "", "use case, e.g. lead_qualification")
	role := fs.String("role", "", "role description")
	goal := fs.String("goal", "", "single-sentence goal")
	instructions := fs.String("instructions", "", "system-prompt instructions")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*name) == "" || strings.TrimSpace(*agentType) == "" {
		return fmt.Errorf("usage: splashify ai-agent create --name \"Support bot\" --agent-type support [--channel whatsapp|instagram] [--industry …] [--use-case …] [--role …] [--goal …] [--instructions …]")
	}
	ch := strings.ToLower(strings.TrimSpace(*channel))
	if !agentChannelTypes[ch] {
		return fmt.Errorf("--channel must be whatsapp or instagram — got %q", *channel)
	}

	body := map[string]any{
		"name":             *name,
		"agent_type":       *agentType,
		"channel_type":     ch,
		"industry":         *industry,
		"use_case":         *useCase,
		"role_description": *role,
		"goal":             *goal,
		"instructions":     *instructions,
	}
	return runReq("POST", "/app/ai-agents", body)
}

// ─── ai-agent update ────────────────────────────────────────────────────────

// cmdAIAgentUpdate sends a sparse PUT — the backend accepts pointers for
// every editable field, so omitting a flag leaves that field unchanged.
// This is different from the canned/waba/attribute pattern (which use a
// PUT-everything semantics).
func cmdAIAgentUpdate(agentID string, args []string) error {
	fs := flag.NewFlagSet("ai-agent update", flag.ContinueOnError)
	name := fs.String("name", "", "new name")
	role := fs.String("role", "", "new role_description")
	goal := fs.String("goal", "", "new goal")
	instructions := fs.String("instructions", "", "new instructions")
	status := fs.String("status", "", "new status, e.g. active | paused")
	tone := fs.String("tone", "", "tone, e.g. friendly | formal")
	assignTo := fs.String("assign-to", "", "team_member_id to escalate handoffs to")
	aiProvider := fs.String("ai-provider", "", "anthropic | openai | gemini")
	aiModel := fs.String("ai-model", "", "model id, e.g. claude-sonnet-4-6")
	temperature := fs.Float64("temperature", 0, "LLM temperature 0.0–1.0")
	processing := fs.String("processing-msg", "", "true | false — enable processing-message bubble")
	processingText := fs.String("processing-msg-text", "", "text to send while the agent is thinking")
	imageRecognition := fs.String("image-recognition", "", "true | false — enable inbound-image OCR")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify ai-agent update <agent_id> [--name …] [--role …] [--goal …] [--instructions …] [--status …] [--tone …] [--assign-to …] [--ai-provider …] [--ai-model …] [--temperature 0.7] [--processing-msg true|false] [--processing-msg-text …] [--image-recognition true|false]")
	}

	body := map[string]any{}
	if passed["name"] {
		body["name"] = *name
	}
	if passed["role"] {
		body["role_description"] = *role
	}
	if passed["goal"] {
		body["goal"] = *goal
	}
	if passed["instructions"] {
		body["instructions"] = *instructions
	}
	if passed["status"] {
		body["status"] = *status
	}
	if passed["tone"] {
		body["tone"] = *tone
	}
	if passed["assign-to"] {
		body["assign_to"] = *assignTo
	}
	if passed["ai-provider"] {
		body["ai_provider"] = *aiProvider
	}
	if passed["ai-model"] {
		body["ai_model"] = *aiModel
	}
	if passed["temperature"] {
		body["llm_temperature"] = *temperature
	}
	if passed["processing-msg"] {
		b, err := parseBool(*processing)
		if err != nil {
			return fmt.Errorf("--processing-msg must be true or false")
		}
		body["processing_message_enabled"] = b
	}
	if passed["processing-msg-text"] {
		body["processing_message"] = *processingText
	}
	if passed["image-recognition"] {
		b, err := parseBool(*imageRecognition)
		if err != nil {
			return fmt.Errorf("--image-recognition must be true or false")
		}
		body["image_recognition_enabled"] = b
	}

	return runReq("PUT", "/app/ai-agents/"+agentID, body)
}

// ─── ai-agent knowledge ─────────────────────────────────────────────────────

// cmdAIAgentKnowledge dispatches the knowledge-base sub-tree. Three actions:
//
//	list                       GET    /knowledge
//	upload <file>              POST   /knowledge (multipart, field "file")
//	delete <file_id>           DELETE /knowledge/:file_id
func cmdAIAgentKnowledge(agentID string, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/ai-agents/"+agentID+"/knowledge", nil)

	case "upload", "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent knowledge <agent_id> upload <path-to-file>")
		}
		return cmdAIAgentKnowledgeUpload(agentID, args[1])

	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ai-agent knowledge <agent_id> delete <file_id>")
		}
		return runReq("DELETE", "/app/ai-agents/"+agentID+"/knowledge/"+args[1], nil)

	default:
		return fmt.Errorf("unknown knowledge subcommand: %s\nrun: splashify ai-agent knowledge <agent_id>", sub)
	}
}

// cmdAIAgentKnowledgeUpload pushes one file to the agent's knowledge base.
// Accepted extensions are checked client-side; the backend enforces a
// 15MB size cap.
func cmdAIAgentKnowledgeUpload(agentID, filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not accessible: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; pass a single file", filePath)
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	if !agentKnowledgeExtensions[ext] {
		return fmt.Errorf("knowledge files must be one of .pdf, .docx, .md, .txt — got %q", ext)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	fmt.Fprintf(os.Stderr, "Uploading %s (%d bytes) to agent %s knowledge base…\n",
		filepath.Base(filePath), info.Size(), agentID)
	raw, err := api.uploadFile("/app/ai-agents/"+agentID+"/knowledge", "file", filePath)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}
