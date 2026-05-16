package internal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const appClientTimeout = 30 * time.Second

// AppClient is the single outbound HTTP client for the cycling-coach app's
// /api/mcp/v1/* endpoints.
//
// In production it presents a client certificate for mTLS authentication at
// the Cloudflare edge. The app's server certificate is verified against the
// system root CA — no custom CA bundle is needed because the app is
// Cloudflare-fronted and carries a publicly trusted certificate.
type AppClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string // sent as "Authorization: Bearer <apiKey>" when non-empty
}

// NewAppClient builds an AppClient from the provided Config.
//
// When AppClientCert and AppClientKey are both set the client presents the
// certificate on every TLS handshake (mTLS). When either is empty a plain
// HTTPS client is created — suitable for local development where no mTLS edge
// is present.
func NewAppClient(cfg *Config) (*AppClient, error) {
	var transport http.RoundTripper
	if cfg.AppClientCert != "" || cfg.AppClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.AppClientCert, cfg.AppClientKey)
		if err != nil {
			return nil, fmt.Errorf("appclient: load client cert: %w", err)
		}
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				// MinVersion guards against downgrade attacks.
				MinVersion: tls.VersionTLS12,
				// Server cert is verified against the system root CA.
				// InsecureSkipVerify is intentionally NOT set.
			},
		}
	}
	hc := &http.Client{Timeout: appClientTimeout}
	if transport != nil {
		hc.Transport = transport
	}
	return &AppClient{
		baseURL:    strings.TrimRight(cfg.AppBaseURL, "/"),
		httpClient: hc,
		apiKey:     cfg.AppAPIKey,
	}, nil
}

// newAppClientWithHTTPClient creates an AppClient with an injected http.Client.
// Used in tests to supply an httptest server client that trusts the test cert.
func newAppClientWithHTTPClient(baseURL string, hc *http.Client) *AppClient {
	return &AppClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: hc,
	}
}

// AppError is returned by Get when the app responds with a non-2xx status code.
type AppError struct {
	StatusCode int
	Body       string
}

func (e *AppError) Error() string {
	return fmt.Sprintf("app returned HTTP %d: %s", e.StatusCode, e.Body)
}

// Get performs GET <baseURL><path> and returns the response body on success.
// Non-2xx responses are returned as *AppError. Network or context errors are
// wrapped and returned as-is.
func (c *AppClient) Get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("appclient: build request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("appclient: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("appclient: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &AppError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return body, nil
}

// MapAppError converts a Get error into a structured MCP tool error result.
//
// Mapping:
//   - *AppError with 404   → not-found tool error
//   - *AppError with other → app-unavailable tool error (status code included)
//   - network/context error → app-unavailable tool error
func MapAppError(err error) (*mcp.CallToolResult, any, error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		if appErr.StatusCode == http.StatusNotFound {
			return NotFoundError("resource")
		}
		return AppUnavailableError(fmt.Errorf("HTTP %d", appErr.StatusCode))
	}
	return AppUnavailableError(err)
}
