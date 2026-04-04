package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const claudeAPIVersion = "2023-06-01"

// ClaudeProvider calls the Anthropic Messages API to generate coaching content.
type ClaudeProvider struct {
	apiKey  string
	model   string
	baseURL string // injectable for tests; production value is https://api.anthropic.com
	client  *http.Client
}

// NewClaudeProvider creates a ClaudeProvider using the given API key and model.
// model defaults to claude-sonnet-4-20250514 when empty.
func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &ClaudeProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		client:  &http.Client{},
	}
}

// NewClaudeProviderForTest creates a ClaudeProvider that targets a custom base URL.
// Intended for use in tests only.
func NewClaudeProviderForTest(apiKey, baseURL string, client *http.Client) *ClaudeProvider {
	return &ClaudeProvider{apiKey: apiKey, model: "claude-sonnet-4-20250514", baseURL: baseURL, client: client}
}

// Generate calls the Claude API with the athlete profile as the system prompt
// and the assembled ride/notes data as the user message.
// It expects Claude to respond with a JSON object {"summary":"...","narrative":"..."}.
func (p *ClaudeProvider) Generate(ctx context.Context, input *ReportInput) (*ReportOutput, error) {
	prompt := BuildPrompt(input)

	reqBody := claudeRequest{
		Model:     p.model,
		MaxTokens: 4096,
		System:    input.AthleteProfile,
		Messages: []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: API returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var apiResp claudeResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: unmarshal response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: empty content in response")
	}

	text := apiResp.Content[0].Text
	out, err := parseCoachingJSON(text)
	if err != nil {
		return nil, fmt.Errorf("reporting.ClaudeProvider.Generate: parse coaching JSON: %w", err)
	}
	return out, nil
}

// CallRaw calls the Claude API with the given system and user prompts and returns
// the raw text response. Unlike Generate it does not expect or parse JSON —
// used for free-form generation such as profile evolution.
func (p *ClaudeProvider) CallRaw(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := claudeRequest{
		Model:     p.model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  []claudeMessage{{Role: "user", Content: userPrompt}},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: do: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: API returned %d: %s", resp.StatusCode, string(respBytes))
	}
	var apiResp claudeResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: unmarshal: %w", err)
	}
	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("reporting.ClaudeProvider.CallRaw: empty content in response")
	}
	return strings.TrimSpace(apiResp.Content[0].Text), nil
}

// parseCoachingJSON extracts {"summary":"...","narrative":"..."} from the Claude text response.
// It tolerates the JSON being wrapped in a markdown code fence.
func parseCoachingJSON(text string) (*ReportOutput, error) {
	text = strings.TrimSpace(text)
	// Strip optional ```json ... ``` fence.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx != -1 {
			text = text[idx+1:]
		}
		if idx := strings.LastIndex(text, "```"); idx != -1 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}

	var out ReportOutput
	if err := json.NewDecoder(strings.NewReader(text)).Decode(&out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &out, nil
}

// ---- request / response DTOs ----

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []claudeContent `json:"content"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
