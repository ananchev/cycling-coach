package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestOAuthServer returns an OAuthServer wired with fixed test credentials.
func newTestOAuthServer() *OAuthServer {
	return NewOAuthServer(&Config{
		AppBaseURL:      "https://app.example.com",
		PublicURL:       "https://mcp.example.com",
		OAuthUser:       "athlete",
		OAuthPassword:   "secret",
		OAuthSigningKey: "test-signing-key-32-bytes-minimum",
	})
}

// testPKCE returns a fixed PKCE verifier + S256 challenge pair.
func testPKCE() (verifier, challenge string) {
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

// buildExpiredJWT crafts a structurally valid JWT signed with key but with exp in the past.
func buildExpiredJWT(t *testing.T, key []byte, issuer string) string {
	t.Helper()
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{
		"sub": "athlete",
		"iss": issuer,
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"exp": time.Now().Add(-time.Hour).Unix(), // already expired
	})
	claims := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := hdr + "." + claims
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + sig
}

// ── Metadata ─────────────────────────────────────────────────────────────────

func TestOAuthServer_Metadata_ReturnsRequiredFields(t *testing.T) {
	srv := newTestOAuthServer()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rr := httptest.NewRecorder()
	srv.Metadata(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{
		"issuer", "authorization_endpoint", "token_endpoint",
		"response_types_supported", "code_challenge_methods_supported",
	} {
		if body[key] == nil {
			t.Errorf("missing field %q in metadata", key)
		}
	}
	if body["issuer"] != "https://mcp.example.com" {
		t.Errorf("issuer = %v, want https://mcp.example.com", body["issuer"])
	}
}

// ── AuthorizeForm (GET) ───────────────────────────────────────────────────────

func TestOAuthServer_AuthorizeForm_ValidParams_ReturnsForm(t *testing.T) {
	srv := newTestOAuthServer()
	_, challenge := testPKCE()
	req := httptest.NewRequest(http.MethodGet,
		"/oauth/authorize?response_type=code&redirect_uri=https://cb.example.com&code_challenge="+
			challenge+"&code_challenge_method=S256",
		nil)
	rr := httptest.NewRecorder()
	srv.AuthorizeForm(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<form method="POST">`) {
		t.Error("expected HTML form in response")
	}
	if !strings.Contains(body, challenge) {
		t.Error("expected code_challenge embedded in form")
	}
}

func TestOAuthServer_AuthorizeForm_WrongResponseType_Returns400(t *testing.T) {
	srv := newTestOAuthServer()
	req := httptest.NewRequest(http.MethodGet,
		"/oauth/authorize?response_type=token&redirect_uri=https://cb.example.com&code_challenge=x&code_challenge_method=S256",
		nil)
	rr := httptest.NewRecorder()
	srv.AuthorizeForm(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestOAuthServer_AuthorizeForm_MissingCodeChallenge_Returns400(t *testing.T) {
	srv := newTestOAuthServer()
	req := httptest.NewRequest(http.MethodGet,
		"/oauth/authorize?response_type=code&redirect_uri=https://cb.example.com&code_challenge_method=S256",
		nil)
	rr := httptest.NewRecorder()
	srv.AuthorizeForm(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestOAuthServer_AuthorizeForm_PlainChallengeMethod_Returns400(t *testing.T) {
	srv := newTestOAuthServer()
	req := httptest.NewRequest(http.MethodGet,
		"/oauth/authorize?response_type=code&redirect_uri=https://cb.example.com&code_challenge=x&code_challenge_method=plain",
		nil)
	rr := httptest.NewRecorder()
	srv.AuthorizeForm(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

// ── AuthorizeSubmit (POST) ────────────────────────────────────────────────────

func TestOAuthServer_AuthorizeSubmit_WrongPassword_ReRendersForm(t *testing.T) {
	srv := newTestOAuthServer()
	_, challenge := testPKCE()
	form := url.Values{
		"redirect_uri":   {"https://cb.example.com/callback"},
		"state":          {"st123"},
		"code_challenge": {challenge},
		"password":       {"wrongpassword"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.AuthorizeSubmit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-render)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Incorrect password") {
		t.Error("expected error message in re-rendered form")
	}
}

func TestOAuthServer_AuthorizeSubmit_CorrectPassword_RedirectsWithCode(t *testing.T) {
	srv := newTestOAuthServer()
	_, challenge := testPKCE()
	form := url.Values{
		"redirect_uri":   {"https://cb.example.com/callback"},
		"state":          {"st123"},
		"code_challenge": {challenge},
		"password":       {"secret"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.AuthorizeSubmit(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rr.Code, rr.Body.String())
	}
	parsed, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if parsed.Query().Get("code") == "" {
		t.Fatal("expected code in redirect Location")
	}
	if parsed.Query().Get("state") != "st123" {
		t.Errorf("state = %q, want st123", parsed.Query().Get("state"))
	}
}

// ── Token — Authorization Code flow ─────────────────────────────────────────

// obtainCode is a test helper that runs the full authorize POST and returns the code.
func obtainCode(t *testing.T, srv *OAuthServer, challenge, redirectURI string) string {
	t.Helper()
	form := url.Values{
		"redirect_uri":   {redirectURI},
		"state":          {"s"},
		"code_challenge": {challenge},
		"password":       {"secret"},
	}
	r := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.AuthorizeSubmit(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("authorize: status = %d: %s", w.Code, w.Body.String())
	}
	parsed, _ := url.Parse(w.Header().Get("Location"))
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatal("no code in redirect")
	}
	return code
}

func TestOAuthServer_Token_AuthCode_ValidFlow_IssuesToken(t *testing.T) {
	srv := newTestOAuthServer()
	verifier, challenge := testPKCE()
	code := obtainCode(t, srv, challenge, "https://cb.example.com/callback")

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {"https://cb.example.com/callback"},
	}
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Token(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if tok, _ := resp["access_token"].(string); tok == "" {
		t.Error("expected non-empty access_token")
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", resp["token_type"])
	}
}

func TestOAuthServer_Token_AuthCode_WrongVerifier_Returns401(t *testing.T) {
	srv := newTestOAuthServer()
	_, challenge := testPKCE()
	code := obtainCode(t, srv, challenge, "https://cb.example.com/callback")

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {"wrong-verifier-does-not-match-challenge"},
	}
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Token(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestOAuthServer_Token_AuthCode_ExpiredCode_Returns401(t *testing.T) {
	srv := newTestOAuthServer()

	// Inject an already-expired code directly.
	srv.mu.Lock()
	srv.codes["expired-code"] = &pendingCode{
		redirectURI: "https://cb.example.com/callback",
		challenge:   "some-challenge",
		expiry:      time.Now().Add(-time.Minute),
	}
	srv.mu.Unlock()

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"expired-code"},
		"code_verifier": {"any-verifier"},
	}
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Token(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestOAuthServer_Token_AuthCode_ReusedCode_Returns401(t *testing.T) {
	srv := newTestOAuthServer()
	verifier, challenge := testPKCE()
	code := obtainCode(t, srv, challenge, "https://cb.example.com/callback")

	exchange := func() int {
		tf := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"code_verifier": {verifier},
		}
		r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tf.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.Token(w, r)
		return w.Code
	}

	if got := exchange(); got != http.StatusOK {
		t.Fatalf("first exchange: status = %d, want 200", got)
	}
	if got := exchange(); got != http.StatusUnauthorized {
		t.Fatalf("second exchange: status = %d, want 401 (code is single-use)", got)
	}
}

// ── Token — ROPC (password grant) ───────────────────────────────────────────

func TestOAuthServer_Token_ROPC_ValidCredentials_IssuesToken(t *testing.T) {
	srv := newTestOAuthServer()
	form := url.Values{
		"grant_type": {"password"},
		"username":   {"athlete"},
		"password":   {"secret"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.Token(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	if tok, _ := resp["access_token"].(string); tok == "" {
		t.Error("expected access_token in response")
	}
}

func TestOAuthServer_Token_ROPC_InvalidPassword_Returns401(t *testing.T) {
	srv := newTestOAuthServer()
	form := url.Values{
		"grant_type": {"password"},
		"username":   {"athlete"},
		"password":   {"wrong"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.Token(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestOAuthServer_Token_UnsupportedGrantType_Returns400(t *testing.T) {
	srv := newTestOAuthServer()
	form := url.Values{"grant_type": {"client_credentials"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.Token(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if body["error"] != "unsupported_grant_type" {
		t.Errorf("error = %q, want unsupported_grant_type", body["error"])
	}
}

// ── RequireBearer middleware ──────────────────────────────────────────────────

func TestRequireBearer_NoSigningKey_PassesThrough(t *testing.T) {
	srv := NewOAuthServer(&Config{AppBaseURL: "https://app.example.com"})
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rr := httptest.NewRecorder()
	srv.RequireBearer(inner).ServeHTTP(rr, req)

	if !called {
		t.Error("expected inner handler to be called when signing key is absent")
	}
}

func TestRequireBearer_MissingToken_Returns401(t *testing.T) {
	srv := newTestOAuthServer()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rr := httptest.NewRecorder()
	srv.RequireBearer(inner).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestRequireBearer_InvalidToken_Returns401(t *testing.T) {
	srv := newTestOAuthServer()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	rr := httptest.NewRecorder()
	srv.RequireBearer(inner).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestRequireBearer_ValidToken_PassesThrough(t *testing.T) {
	srv := newTestOAuthServer()
	tok, err := srv.issueJWT("athlete")
	if err != nil {
		t.Fatalf("issueJWT: %v", err)
	}
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	srv.RequireBearer(inner).ServeHTTP(rr, req)

	if !called {
		t.Error("expected inner handler to be called with valid token")
	}
}

func TestRequireBearer_ExpiredToken_Returns401(t *testing.T) {
	srv := newTestOAuthServer()
	expiredTok := buildExpiredJWT(t, srv.signingKey, srv.publicURL)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+expiredTok)
	rr := httptest.NewRecorder()
	srv.RequireBearer(inner).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for expired token", rr.Code)
	}
}

// ── End-to-end: ROPC token → RequireBearer ───────────────────────────────────

func TestOAuth_ROPC_TokenThenBearer_EndToEnd(t *testing.T) {
	srv := newTestOAuthServer()

	// 1. Obtain token via ROPC.
	form := url.Values{
		"grant_type": {"password"},
		"username":   {"athlete"},
		"password":   {"secret"},
	}
	r1 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	r1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w1 := httptest.NewRecorder()
	srv.Token(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("token: status = %d", w1.Code)
	}
	var tokenResp map[string]any
	json.NewDecoder(w1.Body).Decode(&tokenResp)
	tok, _ := tokenResp["access_token"].(string)
	if tok == "" {
		t.Fatal("no access_token in response")
	}

	// 2. Use token as Bearer on a protected request.
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	r2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r2.Header.Set("Authorization", "Bearer "+tok)
	w2 := httptest.NewRecorder()
	srv.RequireBearer(inner).ServeHTTP(w2, r2)

	if !called {
		t.Error("expected inner handler to be called with ROPC-issued token")
	}
	if w2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w2.Code)
	}
}
