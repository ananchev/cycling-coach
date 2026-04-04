package wahoo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"golang.org/x/oauth2"

	"cycling-coach/internal/config"
	"cycling-coach/internal/storage"
)

var wahooEndpoint = oauth2.Endpoint{
	AuthURL:  "https://api.wahooligan.com/oauth/authorize",
	TokenURL: "https://api.wahooligan.com/oauth/token",
}

// newOAuth2Config constructs the oauth2.Config for the Wahoo Cloud API.
func newOAuth2Config(cfg *config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.WahooClientID,
		ClientSecret: cfg.WahooClientSecret,
		RedirectURL:  cfg.WahooRedirectURI,
		Scopes:       []string{"user_read", "workouts_read", "offline_data"},
		Endpoint:     wahooEndpoint,
	}
}

// dbTokenSource implements oauth2.TokenSource backed by SQLite.
// It returns the stored token when valid, refreshing and persisting it when expired.
type dbTokenSource struct {
	db   *sql.DB
	conf *oauth2.Config
}

func (s *dbTokenSource) Token() (*oauth2.Token, error) {
	stored, err := storage.GetToken(s.db)
	if err != nil {
		return nil, fmt.Errorf("wahoo.dbTokenSource: get stored token: %w", err)
	}

	tok := &oauth2.Token{
		AccessToken:  stored.AccessToken,
		RefreshToken: stored.RefreshToken,
		Expiry:       stored.ExpiresAt,
	}

	if tok.Valid() {
		return tok, nil
	}

	// Token is expired — refresh via the oauth2 library.
	slog.Info("wahoo: access token expired, refreshing")
	newTok, err := s.conf.TokenSource(context.Background(), tok).Token()
	if err != nil {
		return nil, fmt.Errorf("wahoo.dbTokenSource: refresh: %w", err)
	}

	if err := storage.SaveToken(s.db, &storage.WahooToken{
		AccessToken:  newTok.AccessToken,
		RefreshToken: newTok.RefreshToken,
		ExpiresAt:    newTok.Expiry,
	}); err != nil {
		return nil, fmt.Errorf("wahoo.dbTokenSource: save refreshed token: %w", err)
	}
	slog.Info("wahoo: access token refreshed and saved")
	return newTok, nil
}

// AuthHandler handles the Wahoo OAuth2 web flow.
type AuthHandler struct {
	db   *sql.DB
	conf *oauth2.Config
	mu   sync.Mutex
	// pendingState holds the state parameter between authorize and callback.
	// A single in-memory string is safe for this single-user system.
	pendingState string
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(db *sql.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		db:   db,
		conf: newOAuth2Config(cfg),
	}
}

// Authorize handles GET /wahoo/authorize.
// It generates a state token, stores it, and redirects to the Wahoo consent page.
func (a *AuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	a.mu.Lock()
	a.pendingState = state
	a.mu.Unlock()

	url := a.conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	slog.Info("wahoo: redirecting to authorization", "url", url)
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback handles GET /wahoo/callback.
// It verifies the state, exchanges the authorization code for tokens, and persists them.
func (a *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	expected := a.pendingState
	a.mu.Unlock()

	if got := r.URL.Query().Get("state"); got != expected || expected == "" {
		slog.Warn("wahoo: invalid state on callback", "got", r.URL.Query().Get("state"))
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	tok, err := a.conf.Exchange(r.Context(), code)
	if err != nil {
		slog.Error("wahoo: token exchange failed", "err", err)
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := storage.SaveToken(a.db, &storage.WahooToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.Expiry,
	}); err != nil {
		slog.Error("wahoo: save token failed", "err", err)
		http.Error(w, "failed to save token", http.StatusInternalServerError)
		return
	}

	// Clear pending state after successful exchange.
	a.mu.Lock()
	a.pendingState = ""
	a.mu.Unlock()

	slog.Info("wahoo: OAuth2 complete, token saved")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintln(w, `<!DOCTYPE html><html><body>
<h2>Wahoo connected ✓</h2>
<p>Authentication successful. You can close this window and return to your app.</p>
</body></html>`)
}

func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("wahoo.generateState: rand.Read: %v", err))
	}
	return hex.EncodeToString(b)
}
