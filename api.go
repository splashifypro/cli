package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiClient is a thin HTTP client for the partnersapi backend, authenticated
// with an `oc_live_` access token.
type apiClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newAPIClient(baseURL, token string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// apiError carries a non-2xx response for friendly CLI messages.
type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string {
	msg := e.body
	// Surface the backend's "message" field if the body is JSON.
	var parsed map[string]any
	if json.Unmarshal([]byte(e.body), &parsed) == nil {
		if m, ok := parsed["message"].(string); ok && m != "" {
			msg = m
		}
	}
	return fmt.Sprintf("HTTP %d: %s", e.status, msg)
}

// do performs a request against /api/v1 and decodes the JSON response.
func (c *apiClient) do(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+"/api/v1"+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{status: resp.StatusCode, body: string(raw)}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// callRaw performs a request against /api/v1 and returns the raw JSON body.
// It is the basis for every task command and the generic `api` passthrough.
func (c *apiClient) callRaw(method, path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// printJSON writes a JSON value to stdout, indented for readability.
func printJSON(raw json.RawMessage) {
	if len(raw) == 0 {
		fmt.Println("(empty response)")
		return
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not valid JSON — print as-is.
		fmt.Println(string(raw))
		return
	}
	fmt.Println(buf.String())
}

// ─── Typed endpoints used by the CLI ─────────────────────────────────────────

type meResponse struct {
	Success bool   `json:"success"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	User    struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
}

// me validates the token and returns the account identity. It tolerates the
// two response shapes /app/me may use (flat or nested under "user").
func (c *apiClient) me() (email, name string, err error) {
	var r meResponse
	if err = c.do(http.MethodGet, "/app/me", nil, &r); err != nil {
		return "", "", err
	}
	email, name = r.Email, r.Name
	if email == "" {
		email = r.User.Email
	}
	if name == "" {
		name = r.User.Name
	}
	return email, name, nil
}

type accessToken struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at"`
	ExpiresAt   string `json:"expires_at"`
	Revoked     bool   `json:"revoked"`
}

func (c *apiClient) listTokens() ([]accessToken, error) {
	var r struct {
		Success bool          `json:"success"`
		Tokens  []accessToken `json:"tokens"`
	}
	if err := c.do(http.MethodGet, "/app/developer/access-tokens", nil, &r); err != nil {
		return nil, err
	}
	return r.Tokens, nil
}

func (c *apiClient) createToken(name string, expiresInDays int) (id, token string, err error) {
	var r struct {
		Success bool   `json:"success"`
		ID      string `json:"id"`
		Token   string `json:"token"`
	}
	body := map[string]any{"name": name, "expires_in_days": expiresInDays}
	if err = c.do(http.MethodPost, "/app/developer/access-tokens", body, &r); err != nil {
		return "", "", err
	}
	return r.ID, r.Token, nil
}

func (c *apiClient) revokeToken(id string) error {
	return c.do(http.MethodDelete, "/app/developer/access-tokens/"+id, nil, nil)
}

// eligibilityResponse mirrors GET /app/developer/cli-eligibility. The backend
// returns 200 OK in every case; the CLI inspects `Eligible` and `Reason`
// rather than parsing HTTP status codes.
type eligibilityResponse struct {
	Success       bool   `json:"success"`
	Eligible      bool   `json:"eligible"`
	Reason        string `json:"reason"`
	WabaConnected bool   `json:"waba_connected"`
	AccountEmail  string `json:"account_email"`
	PlanActive    bool   `json:"plan_active"`
	Message       string `json:"message"`
}

// cliEligibility asks the backend whether this account is allowed to use the
// CLI. Returns the parsed payload so callers can branch on the reason.
func (c *apiClient) cliEligibility() (eligibilityResponse, error) {
	var r eligibilityResponse
	if err := c.do(http.MethodGet, "/app/developer/cli-eligibility", nil, &r); err != nil {
		return r, err
	}
	return r, nil
}
