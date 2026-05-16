// smoketest validates connectivity across the full MCP server trust chain:
//
//  1. App connectivity  — AppClient (mTLS) → cycling-coach /api/mcp/v1/* endpoints
//  2. OAuth in-process  — OAuthServer token issuance and Bearer validation
//  3. MCP server e2e   — running MCP server with a ROPC-issued token (opt-in)
//
// See cmd/smoketest/README.md for configuration and usage.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"mcp-server/internal"
)

const callTimeout = 15 * time.Second

func main() {
	cfg, err := internal.LoadConfig()
	if err != nil {
		fatalf("config: %v\n  (set MCP_APP_BASE_URL; add MCP_APP_CLIENT_CERT/KEY for mTLS)", err)
	}

	st := &smokeTest{cfg: cfg}
	st.run()
}

// ── runner ────────────────────────────────────────────────────────────────────

type smokeTest struct {
	cfg    *internal.Config
	passed int
	failed int
	skip   int
}

func (s *smokeTest) run() {
	bar := strings.Repeat("─", 60)
	fmt.Println(bar)
	fmt.Println("  cycling-coach MCP — smoke test")
	fmt.Println(bar)

	s.runAppSection()
	s.runOAuthSection()

	if mcpURL := os.Getenv("MCP_SERVER_URL"); mcpURL != "" {
		s.runMCPSection(strings.TrimRight(mcpURL, "/"))
	} else {
		fmt.Printf("\n  [MCP SERVER]  skipped — set MCP_SERVER_URL to enable\n")
	}

	fmt.Println("\n" + bar)
	total := s.passed + s.failed + s.skip
	fmt.Printf("  %d/%d checks passed", s.passed, total)
	if s.skip > 0 {
		fmt.Printf("  (%d skipped)", s.skip)
	}
	fmt.Println()
	fmt.Println(bar)

	if s.failed > 0 {
		os.Exit(1)
	}
}

// ── Section 1: App connectivity ───────────────────────────────────────────────

func (s *smokeTest) runAppSection() {
	fmt.Printf("\n  [APP CONNECTIVITY]  %s\n\n", s.cfg.AppBaseURL)

	appClient, err := internal.NewAppClient(s.cfg)
	if err != nil {
		s.record(false, "create AppClient", 0, err.Error(), 0)
		return
	}

	// Endpoints with no prerequisite
	s.appGet(appClient, "/api/mcp/v1/profile")
	s.appGet(appClient, "/api/mcp/v1/zone-config")
	s.appGet(appClient, "/api/mcp/v1/block-context?block=current")
	s.appGet(appClient, "/api/mcp/v1/progress")

	// Workout list → detail
	wBody := s.appGet(appClient, "/api/mcp/v1/workouts?limit=5")
	if wID := firstID(wBody, "items"); wID != "" {
		s.appGet(appClient, "/api/mcp/v1/workouts/"+wID)
	} else {
		s.skipCheck("GET /api/mcp/v1/workouts/{id}", "no workouts returned")
	}

	// Notes and body metrics
	s.appGet(appClient, "/api/mcp/v1/notes?limit=5")
	s.appGet(appClient, "/api/mcp/v1/body-metrics?limit=5")

	// Report list → detail
	rBody := s.appGet(appClient, "/api/mcp/v1/reports?limit=5")
	if rID := firstID(rBody, "items"); rID != "" {
		s.appGet(appClient, "/api/mcp/v1/reports/"+rID)
	} else {
		s.skipCheck("GET /api/mcp/v1/reports/{id}", "no reports returned")
	}
}

// appGet calls path on the app via AppClient and records the result.
// Returns the raw body on success (used to extract IDs for follow-up calls).
func (s *smokeTest) appGet(client *internal.AppClient, path string) []byte {
	label := "GET " + path
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	body, err := client.Get(ctx, path)
	dur := time.Since(start)

	if err != nil {
		var appErr *internal.AppError
		if errors.As(err, &appErr) {
			s.record(false, label, appErr.StatusCode, truncate(appErr.Body, 80), dur)
		} else {
			s.record(false, label, 0, err.Error(), dur)
		}
		return nil
	}

	detail := fmt.Sprintf("%s", bodyHint(body))
	s.record(true, label, http.StatusOK, detail, dur)
	return body
}

// ── Section 2: OAuth in-process ───────────────────────────────────────────────

func (s *smokeTest) runOAuthSection() {
	fmt.Printf("\n  [OAUTH — in-process]\n\n")

	if s.cfg.OAuthSigningKey == "" {
		fmt.Printf("      skipped — set MCP_OAUTH_SIGNING_KEY to enable\n")
		s.skip += 4
		return
	}

	oauthSrv := internal.NewOAuthServer(s.cfg)

	// 1. Issue token via ROPC
	tok, ok := s.oauthROPC(oauthSrv)
	if !ok {
		return
	}

	// 2. Verify token passes RequireBearer
	s.oauthBearerCheck(oauthSrv, tok)

	// 3. Verify missing token is rejected
	s.oauthBearerMissingCheck(oauthSrv)

	// 4. Verify expired token is rejected
	s.oauthExpiredCheck(oauthSrv)
}

func (s *smokeTest) oauthROPC(oauthSrv *internal.OAuthServer) (string, bool) {
	label := "POST /oauth/token (ROPC)"
	start := time.Now()

	tok, err := oauthSrv.IssueROPC(s.cfg.OAuthUser, s.cfg.OAuthPassword)
	dur := time.Since(start)
	if err != nil {
		s.record(false, label, 0, err.Error(), dur)
		return "", false
	}
	s.record(true, label, http.StatusOK, "JWT issued", dur)
	return tok, true
}

func (s *smokeTest) oauthBearerCheck(oauthSrv *internal.OAuthServer, tok string) {
	label := "RequireBearer — valid token passes"
	start := time.Now()
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	req, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := &captureWriter{}
	oauthSrv.RequireBearer(inner).ServeHTTP(rr, req)
	dur := time.Since(start)

	if reached && rr.status == http.StatusOK {
		s.record(true, label, rr.status, "", dur)
	} else {
		s.record(false, label, rr.status, "inner handler not reached", dur)
	}
}

func (s *smokeTest) oauthBearerMissingCheck(oauthSrv *internal.OAuthServer) {
	label := "RequireBearer — missing token → 401"
	start := time.Now()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	rr := &captureWriter{}
	oauthSrv.RequireBearer(inner).ServeHTTP(rr, req)
	dur := time.Since(start)

	ok := rr.status == http.StatusUnauthorized
	s.record(ok, label, rr.status, "", dur)
}

func (s *smokeTest) oauthExpiredCheck(oauthSrv *internal.OAuthServer) {
	label := "RequireBearer — expired token → 401"
	start := time.Now()
	expiredTok := internal.BuildExpiredJWT(oauthSrv)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+expiredTok)
	rr := &captureWriter{}
	oauthSrv.RequireBearer(inner).ServeHTTP(rr, req)
	dur := time.Since(start)

	ok := rr.status == http.StatusUnauthorized
	s.record(ok, label, rr.status, "", dur)
}

// ── Section 3: MCP server end-to-end ─────────────────────────────────────────

func (s *smokeTest) runMCPSection(mcpURL string) {
	fmt.Printf("\n  [MCP SERVER]  %s\n\n", mcpURL)

	hc := &http.Client{Timeout: callTimeout}

	// 1. OAuth metadata
	s.httpCheck(hc, "GET /.well-known/oauth-authorization-server",
		"GET", mcpURL+"/.well-known/oauth-authorization-server", nil, "", http.StatusOK)

	if s.cfg.OAuthSigningKey == "" {
		fmt.Printf("      (token checks skipped — MCP_OAUTH_SIGNING_KEY not set)\n")
		s.skip += 3
		return
	}

	// 2. ROPC token from the running server
	label := "POST /oauth/token (ROPC → running server)"
	tok, status, err := mcpROPC(hc, mcpURL, s.cfg.OAuthUser, s.cfg.OAuthPassword)
	if err != nil || tok == "" {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = "no access_token in response"
		}
		s.record(false, label, status, msg, 0)
	} else {
		s.record(true, label, status, "JWT issued", 0)
	}

	// 3. No token → 401
	s.httpCheck(hc, "POST /mcp — no token → 401",
		"POST", mcpURL+"/mcp", nil, "", http.StatusUnauthorized)

	// 4. Valid token → not 401
	if tok != "" {
		s.httpCheckNot(hc, "POST /mcp — valid token → not 401",
			"POST", mcpURL+"/mcp", map[string]string{"Authorization": "Bearer " + tok},
			`{}`, http.StatusUnauthorized)
	}
}

// ── Recording helpers ─────────────────────────────────────────────────────────

func (s *smokeTest) record(ok bool, label string, status int, detail string, dur time.Duration) {
	symbol := "✓"
	if !ok {
		symbol = "✗"
		s.failed++
	} else {
		s.passed++
	}

	statusStr := "   "
	if status > 0 {
		statusStr = strconv.Itoa(status)
	}
	durStr := ""
	if dur > 0 {
		durStr = fmtDur(dur)
	}

	fmt.Printf("  %s  %-48s  %s  %s", symbol, label, statusStr, durStr)
	if detail != "" {
		fmt.Printf("  %s", detail)
	}
	fmt.Println()
}

func (s *smokeTest) skipCheck(label, reason string) {
	s.skip++
	fmt.Printf("  -  %-48s  skipped: %s\n", label, reason)
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (s *smokeTest) httpCheck(hc *http.Client, label, method, rawURL string,
	headers map[string]string, body string, wantStatus int) {
	start := time.Now()
	status, respBody, err := httpDo(hc, method, rawURL, headers, body)
	dur := time.Since(start)
	if err != nil {
		s.record(false, label, 0, err.Error(), dur)
		return
	}
	ok := status == wantStatus
	detail := ""
	if !ok {
		detail = truncate(respBody, 80)
	}
	s.record(ok, label, status, detail, dur)
}

func (s *smokeTest) httpCheckNot(hc *http.Client, label, method, rawURL string,
	headers map[string]string, body string, notStatus int) {
	start := time.Now()
	status, respBody, err := httpDo(hc, method, rawURL, headers, body)
	dur := time.Since(start)
	if err != nil {
		s.record(false, label, 0, err.Error(), dur)
		return
	}
	ok := status != notStatus
	detail := ""
	if !ok {
		detail = truncate(respBody, 80)
	}
	s.record(ok, label, status, detail, dur)
}

func httpDo(hc *http.Client, method, rawURL string, headers map[string]string, body string) (int, string, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return 0, "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func mcpROPC(hc *http.Client, mcpURL, user, password string) (string, int, error) {
	form := url.Values{
		"grant_type": {"password"},
		"username":   {user},
		"password":   {password},
	}
	req, err := http.NewRequest(http.MethodPost, mcpURL+"/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	tok, _ := body["access_token"].(string)
	return tok, resp.StatusCode, nil
}

// captureWriter records the status code written by an http.Handler under test.
type captureWriter struct {
	status int
	header http.Header
}

func (c *captureWriter) Header() http.Header {
	if c.header == nil {
		c.header = make(http.Header)
	}
	return c.header
}
func (c *captureWriter) Write(b []byte) (int, error) { return len(b), nil }
func (c *captureWriter) WriteHeader(code int)         { c.status = code }

// ── JSON / formatting helpers ─────────────────────────────────────────────────

// firstID extracts the id field from the first element of the named array in JSON.
// Used to chain list → detail calls without hard-coding IDs.
func firstID(body []byte, arrayKey string) string {
	if body == nil {
		return ""
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	raw, ok := envelope[arrayKey]
	if !ok {
		return ""
	}
	var items []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return ""
	}
	return strconv.FormatInt(items[0].ID, 10)
}

// bodyHint returns a short human-readable hint about a JSON response body.
func bodyHint(body []byte) string {
	if len(body) == 0 {
		return "(empty)"
	}
	// Try to count items in an array or a known list field.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, key := range []string{"items", "data"} {
			if raw, ok := envelope[key]; ok {
				var arr []json.RawMessage
				if err := json.Unmarshal(raw, &arr); err == nil {
					return fmt.Sprintf("%d items", len(arr))
				}
			}
		}
	}
	// Fallback: show first 80 chars.
	preview := strings.ReplaceAll(string(body), "\n", " ")
	return truncate(preview, 80)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fmtDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoketest: "+format+"\n", args...)
	os.Exit(1)
}
