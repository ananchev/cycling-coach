package internal

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	accessTokenTTL = time.Hour
	authCodeTTL    = 5 * time.Minute
)

// OAuthServer is a minimal embedded OAuth 2.1 authorization server.
//
// Supported flows:
//   - Authorization Code + PKCE (S256) — for claude.ai and remote MCP clients.
//   - Resource Owner Password Credentials — for CLI / smoke-test use.
//
// When OAuthSigningKey is empty, RequireBearer passes all requests through
// without validation (local-dev passthrough).
type OAuthServer struct {
	publicURL  string
	username   string
	password   string
	signingKey []byte

	mu    sync.Mutex
	codes map[string]*pendingCode
}

type pendingCode struct {
	redirectURI string
	challenge   string // base64url(SHA256(verifier))
	expiry      time.Time
}

// NewOAuthServer creates an OAuthServer from cfg.
func NewOAuthServer(cfg *Config) *OAuthServer {
	return &OAuthServer{
		publicURL:  strings.TrimRight(cfg.PublicURL, "/"),
		username:   cfg.OAuthUser,
		password:   cfg.OAuthPassword,
		signingKey: []byte(cfg.OAuthSigningKey),
		codes:      make(map[string]*pendingCode),
	}
}

// ── RFC 8414 metadata ────────────────────────────────────────────────────────

// Metadata serves GET /.well-known/oauth-authorization-server.
func (o *OAuthServer) Metadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"issuer":                                 o.publicURL,
		"authorization_endpoint":                 o.publicURL + "/oauth/authorize",
		"token_endpoint":                         o.publicURL + "/oauth/token",
		"response_types_supported":               []string{"code"},
		"grant_types_supported":                  []string{"authorization_code"},
		"code_challenge_methods_supported":        []string{"S256"},
		"token_endpoint_auth_methods_supported":   []string{"none"},
	})
}

// ── Authorization Code flow ──────────────────────────────────────────────────

var authFormTmpl = template.Must(template.New("auth").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Authorize — Cycling Coach MCP</title>
<style>
body{font-family:system-ui,sans-serif;max-width:420px;margin:80px auto;padding:0 20px}
h1{font-size:1.4rem;margin-bottom:.25rem}
p.sub{color:#555;font-size:.9rem;margin-bottom:1.5rem}
label{display:block;margin:.75rem 0 .25rem;font-weight:500}
input[type=password]{width:100%;padding:.5rem;font-size:1rem;box-sizing:border-box;border:1px solid #ccc;border-radius:4px}
button{margin-top:1rem;padding:.6rem 1.4rem;font-size:1rem;background:#111;color:#fff;border:none;border-radius:4px;cursor:pointer}
.err{color:#c00;font-size:.9rem;margin-top:.5rem}
</style>
</head>
<body>
<h1>Authorize Access</h1>
<p class="sub">A client is requesting read-only access to your cycling data.</p>
<form method="POST">
<input type="hidden" name="redirect_uri"   value="{{.RedirectURI}}">
<input type="hidden" name="state"          value="{{.State}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<label for="pw">Password</label>
<input type="password" id="pw" name="password" autofocus required>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<button type="submit">Authorize</button>
</form>
</body>
</html>`))

type authFormData struct {
	RedirectURI   string
	State         string
	CodeChallenge string
	Error         string
}

// AuthorizeForm handles GET /oauth/authorize — renders the password form.
func (o *OAuthServer) AuthorizeForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		oauthError(w, "unsupported_response_type", "only response_type=code is supported", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge_method") != "S256" {
		oauthError(w, "invalid_request", "code_challenge_method must be S256", http.StatusBadRequest)
		return
	}
	if q.Get("redirect_uri") == "" || q.Get("code_challenge") == "" {
		oauthError(w, "invalid_request", "redirect_uri and code_challenge are required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	authFormTmpl.Execute(w, authFormData{ //nolint:errcheck
		RedirectURI:   q.Get("redirect_uri"),
		State:         q.Get("state"),
		CodeChallenge: q.Get("code_challenge"),
	})
}

// AuthorizeSubmit handles POST /oauth/authorize — validates the password,
// issues an authorization code, and redirects to redirect_uri.
func (o *OAuthServer) AuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	data := authFormData{
		RedirectURI:   r.FormValue("redirect_uri"),
		State:         r.FormValue("state"),
		CodeChallenge: r.FormValue("code_challenge"),
	}
	if r.FormValue("password") != o.password {
		data.Error = "Incorrect password."
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		authFormTmpl.Execute(w, data) //nolint:errcheck
		return
	}
	code, err := randomToken(32)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	o.mu.Lock()
	o.codes[code] = &pendingCode{
		redirectURI: data.RedirectURI,
		challenge:   data.CodeChallenge,
		expiry:      time.Now().Add(authCodeTTL),
	}
	o.mu.Unlock()

	target, err := url.Parse(data.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := target.Query()
	q.Set("code", code)
	if data.State != "" {
		q.Set("state", data.State)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ── Token endpoint ───────────────────────────────────────────────────────────

// Token handles POST /oauth/token.
// grant_type=authorization_code: validates code + PKCE verifier, issues JWT.
// grant_type=password:           validates username + password, issues JWT.
func (o *OAuthServer) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, "invalid_request", "cannot parse form", http.StatusBadRequest)
		return
	}
	switch r.FormValue("grant_type") {
	case "authorization_code":
		o.tokenAuthCode(w, r)
	case "password":
		o.tokenPassword(w, r)
	default:
		oauthError(w, "unsupported_grant_type", "supported: authorization_code, password", http.StatusBadRequest)
	}
}

func (o *OAuthServer) tokenAuthCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	verifier := r.FormValue("code_verifier")
	redirectURI := r.FormValue("redirect_uri")

	if code == "" || verifier == "" {
		oauthError(w, "invalid_request", "code and code_verifier are required", http.StatusBadRequest)
		return
	}
	o.mu.Lock()
	pending, ok := o.codes[code]
	if ok {
		delete(o.codes, code) // single-use
	}
	o.mu.Unlock()

	if !ok || time.Now().After(pending.expiry) {
		oauthError(w, "invalid_grant", "code not found or expired", http.StatusUnauthorized)
		return
	}
	if redirectURI != "" && redirectURI != pending.redirectURI {
		oauthError(w, "invalid_grant", "redirect_uri mismatch", http.StatusUnauthorized)
		return
	}
	if !verifyS256(verifier, pending.challenge) {
		oauthError(w, "invalid_grant", "PKCE verification failed", http.StatusUnauthorized)
		return
	}
	tok, err := o.issueJWT("athlete")
	if err != nil {
		oauthError(w, "server_error", "token generation failed", http.StatusInternalServerError)
		return
	}
	writeToken(w, tok)
}

func (o *OAuthServer) tokenPassword(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("username") != o.username || r.FormValue("password") != o.password {
		oauthError(w, "invalid_grant", "invalid username or password", http.StatusUnauthorized)
		return
	}
	tok, err := o.issueJWT("athlete")
	if err != nil {
		oauthError(w, "server_error", "token generation failed", http.StatusInternalServerError)
		return
	}
	writeToken(w, tok)
}

// ── RequireBearer middleware ─────────────────────────────────────────────────

// RequireBearer validates the Bearer JWT on every MCP request.
// When OAuthSigningKey is empty, all requests pass through (local-dev mode).
func (o *OAuthServer) RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(o.signingKey) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="cycling-coach-mcp"`)
			http.Error(w, `{"error":"missing_token"}`, http.StatusUnauthorized)
			return
		}
		if err := o.verifyJWT(token); err != nil {
			slog.Debug("bearer token rejected", "err", err)
			w.Header().Set("WWW-Authenticate", `Bearer realm="cycling-coach-mcp" error="invalid_token"`)
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── JWT (HS256, no external dependency) ─────────────────────────────────────

func (o *OAuthServer) issueJWT(subject string) (string, error) {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"sub": subject,
		"iss": o.publicURL,
		"iat": now.Unix(),
		"exp": now.Add(accessTokenTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	claims := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := hdr + "." + claims
	mac := hmac.New(sha256.New, o.signingKey)
	mac.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + sig, nil
}

func (o *OAuthServer) verifyJWT(token string) error {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return fmt.Errorf("malformed token")
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, o.signingKey)
	mac.Write([]byte(unsigned))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return fmt.Errorf("invalid signature")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("malformed payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return fmt.Errorf("malformed claims: %w", err)
	}
	exp, ok := claims["exp"].(float64)
	if !ok || time.Now().Unix() > int64(exp) {
		return fmt.Errorf("token expired")
	}
	return nil
}

// ── Helpers for external callers (smoke test) ────────────────────────────────

// IssueROPC validates username+password and returns a signed JWT.
// Used by the smoke test binary to obtain a token without an HTTP round-trip.
func (o *OAuthServer) IssueROPC(username, password string) (string, error) {
	if username != o.username || password != o.password {
		return "", fmt.Errorf("invalid credentials")
	}
	return o.issueJWT("athlete")
}

// BuildExpiredJWT returns a structurally valid JWT signed with o's key but with
// an exp claim set one hour in the past. Used by the smoke test to verify that
// RequireBearer correctly rejects expired tokens.
func BuildExpiredJWT(o *OAuthServer) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{
		"sub": "athlete",
		"iss": o.publicURL,
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	claims := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := hdr + "." + claims
	mac := hmac.New(sha256.New, o.signingKey)
	mac.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + sig
}

// ── Package-level helpers ────────────────────────────────────────────────────

// verifyS256 checks that SHA256(verifier) == base64url_decode(challenge).
func verifyS256(verifier, challenge string) bool {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:]) == challenge
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func writeToken(w http.ResponseWriter, tok string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"access_token": tok,
		"token_type":   "Bearer",
		"expires_in":   int(accessTokenTTL.Seconds()),
	})
}

func oauthError(w http.ResponseWriter, code, desc string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error":             code,
		"error_description": desc,
	})
}
