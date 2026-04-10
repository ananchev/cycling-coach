package reporting_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"cycling-coach/internal/reporting"
	"cycling-coach/internal/storage"
)

func TestClaudeProvider_Generate_Success(t *testing.T) {
	respPayload := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": `{"summary":"Good week.","narrative":"# Narrative\n\nDetails."}`},
		},
	}
	client := &http.Client{Transport: reportingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}

		body, _ := json.Marshal(respPayload)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}

	p := reporting.NewClaudeProviderForTest("test-key", "https://example.test", client)

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	input := &reporting.ReportInput{
		Type:           storage.ReportTypeWeeklyReport,
		WeekStart:      weekStart,
		WeekEnd:        weekStart.Add(7 * 24 * time.Hour),
		AthleteProfile: "FTP: 251W",
	}

	out, err := p.Generate(context.Background(), input)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if out.Summary != "Good week." {
		t.Errorf("unexpected summary: %q", out.Summary)
	}
	if !strings.Contains(out.Narrative, "Narrative") {
		t.Errorf("unexpected narrative: %q", out.Narrative)
	}
}

func TestClaudeProvider_Generate_RequestBody(t *testing.T) {
	var gotBody map[string]any

	client := &http.Client{Transport: reportingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respPayload := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"summary":"s","narrative":"n"}`},
			},
		}
		body, _ := json.Marshal(respPayload)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}

	p := reporting.NewClaudeProviderForTest("test-key", "https://example.test", client)

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	input := &reporting.ReportInput{
		Type:           storage.ReportTypeWeeklyReport,
		WeekStart:      weekStart,
		WeekEnd:        weekStart.Add(7 * 24 * time.Hour),
		AthleteProfile: "my profile",
	}

	if _, err := p.Generate(context.Background(), input); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if gotBody["model"] == nil {
		t.Error("request body missing 'model'")
	}
	if gotBody["system"] != "my profile" {
		t.Errorf("expected system='my profile', got %v", gotBody["system"])
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Error("request body missing 'messages'")
	}
}

func TestClaudeProvider_Generate_APIError(t *testing.T) {
	client := &http.Client{Transport: reportingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("rate limited\n")),
		}, nil
	})}

	p := reporting.NewClaudeProviderForTest("test-key", "https://example.test", client)

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	input := &reporting.ReportInput{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: weekStart,
		WeekEnd:   weekStart.Add(7 * 24 * time.Hour),
	}

	_, err := p.Generate(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 in error, got: %v", err)
	}
}

func TestClaudeProvider_Generate_CodeFenceStripped(t *testing.T) {
	fencedJSON := "```json\n{\"summary\":\"Fence summary.\",\"narrative\":\"Fence narrative.\"}\n```"

	client := &http.Client{Transport: reportingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		respPayload := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fencedJSON},
			},
		}
		body, _ := json.Marshal(respPayload)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}

	p := reporting.NewClaudeProviderForTest("test-key", "https://example.test", client)

	weekStart := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	input := &reporting.ReportInput{
		Type:      storage.ReportTypeWeeklyReport,
		WeekStart: weekStart,
		WeekEnd:   weekStart.Add(7 * 24 * time.Hour),
	}

	out, err := p.Generate(context.Background(), input)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Summary != "Fence summary." {
		t.Errorf("unexpected summary: %q", out.Summary)
	}
}

type reportingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f reportingRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
