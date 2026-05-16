package internal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── NewAppClient construction tests ───────────────────────────────────────────

func TestNewAppClient_NoCert_Succeeds(t *testing.T) {
	cfg := &Config{AppBaseURL: "https://example.com"}
	c, err := NewAppClient(cfg)
	if err != nil {
		t.Fatalf("NewAppClient (no cert): %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil AppClient")
	}
}

func TestNewAppClient_MissingCertFile_ReturnsError(t *testing.T) {
	cfg := &Config{
		AppBaseURL:    "https://example.com",
		AppClientCert: "/nonexistent/client.crt",
		AppClientKey:  "/nonexistent/client.key",
	}
	_, err := NewAppClient(cfg)
	if err == nil {
		t.Fatal("expected error for missing cert file, got nil")
	}
}

func TestNewAppClient_ValidCertKey_Succeeds(t *testing.T) {
	certFile, keyFile := writeTempCertKey(t)
	cfg := &Config{
		AppBaseURL:    "https://example.com",
		AppClientCert: certFile,
		AppClientKey:  keyFile,
	}
	c, err := NewAppClient(cfg)
	if err != nil {
		t.Fatalf("NewAppClient (with cert): %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil AppClient")
	}
}

// ── Get — success path ────────────────────────────────────────────────────────

func TestAppClient_Get_200_ReturnsBody(t *testing.T) {
	want := `{"markdown":"hello"}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp/v1/profile" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, want)
	}))
	defer srv.Close()

	c := newAppClientWithHTTPClient(srv.URL, srv.Client())
	body, err := c.Get(t.Context(), "/api/mcp/v1/profile")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != want {
		t.Errorf("body = %q, want %q", string(body), want)
	}
}

// ── Get — error mapping ───────────────────────────────────────────────────────

func TestAppClient_Get_404_ReturnsAppError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	c := newAppClientWithHTTPClient(srv.URL, srv.Client())
	_, err := c.Get(t.Context(), "/api/mcp/v1/workouts/99999")

	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if appErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", appErr.StatusCode)
	}
}

func TestAppClient_Get_500_ReturnsAppError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"app unavailable"}`)
	}))
	defer srv.Close()

	c := newAppClientWithHTTPClient(srv.URL, srv.Client())
	_, err := c.Get(t.Context(), "/api/mcp/v1/profile")

	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if appErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", appErr.StatusCode)
	}
}

func TestAppClient_Get_NetworkError_ReturnsError(t *testing.T) {
	// Port 0 is never bound — connection refused immediately.
	c := newAppClientWithHTTPClient("http://127.0.0.1:0", &http.Client{Timeout: time.Second})
	_, err := c.Get(t.Context(), "/api/mcp/v1/profile")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── MapAppError — tool-level error mapping ────────────────────────────────────

func TestMapAppError_404_MapsToNotFound(t *testing.T) {
	err := &AppError{StatusCode: http.StatusNotFound, Body: `{"error":"not found"}`}
	result, _, toolErr := MapAppError(err)
	if toolErr != nil {
		t.Fatalf("MapAppError returned Go error: %v", toolErr)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

func TestMapAppError_5xx_MapsToAppUnavailable(t *testing.T) {
	for _, code := range []int{500, 502, 503} {
		t.Run(fmt.Sprintf("HTTP%d", code), func(t *testing.T) {
			err := &AppError{StatusCode: code, Body: `{"error":"app unavailable"}`}
			result, _, toolErr := MapAppError(err)
			if toolErr != nil {
				t.Fatalf("MapAppError returned Go error: %v", toolErr)
			}
			if !result.IsError {
				t.Error("expected IsError=true")
			}
		})
	}
}

func TestMapAppError_NetworkError_MapsToAppUnavailable(t *testing.T) {
	c := newAppClientWithHTTPClient("http://127.0.0.1:0", &http.Client{Timeout: time.Second})
	_, netErr := c.Get(t.Context(), "/api/mcp/v1/profile")
	if netErr == nil {
		t.Skip("expected network error, got nil — skipping")
	}

	result, _, toolErr := MapAppError(netErr)
	if toolErr != nil {
		t.Fatalf("MapAppError returned Go error: %v", toolErr)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

// ── End-to-end: TLS server → MapAppError ─────────────────────────────────────

func TestAppClient_404_EndToEnd_MapsToNotFoundToolError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	c := newAppClientWithHTTPClient(srv.URL, srv.Client())
	_, err := c.Get(t.Context(), "/api/mcp/v1/reports/99999")

	result, _, toolErr := MapAppError(err)
	if toolErr != nil {
		t.Fatalf("MapAppError returned Go error: %v", toolErr)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

func TestAppClient_5xx_EndToEnd_MapsToAppUnavailableToolError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"app unavailable"}`)
	}))
	defer srv.Close()

	c := newAppClientWithHTTPClient(srv.URL, srv.Client())
	_, err := c.Get(t.Context(), "/api/mcp/v1/profile")

	result, _, toolErr := MapAppError(err)
	if toolErr != nil {
		t.Fatalf("MapAppError returned Go error: %v", toolErr)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// writeTempCertKey generates a real self-signed ECDSA certificate and key,
// writes them to temp files, and returns their paths. The cert is used only
// to exercise the NewAppClient cert-loading path, not to establish actual TLS.
func writeTempCertKey(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-client"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "client.crt")
	keyFile = filepath.Join(dir, "client.key")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}
