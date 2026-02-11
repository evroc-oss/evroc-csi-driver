// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"golang.org/x/oauth2"
)

const (
	// Authentication retry configuration
	authMaxRetries      = 3
	authRetryBackoff    = 2 * time.Second
	authRequestTimeout  = 15 * time.Second
	tokenValidityBuffer = 30 * time.Second

	// HTTP client timeouts for dual-stack networking
	dialTimeout            = 10 * time.Second
	dialKeepAlive          = 30 * time.Second
	dialFallbackDelay      = 300 * time.Millisecond
	tlsHandshakeTimeout    = 10 * time.Second
	idleConnTimeout        = 90 * time.Second
	expectContinueTimeout  = 1 * time.Second
)

// Client handles OIDC authentication and token management.
type Client struct {
	tokenSource  oauth2.TokenSource
	logger       *slog.Logger
	oauth2Config *oauth2.Config
	token        *oauth2.Token
	httpClient   *http.Client // HTTP client with TLS config for re-authentication
	username     string
	password     string
	tokenMu      sync.RWMutex
}

// NewClient creates a new authentication client from the centralized configuration.
func NewClient(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is nil")
	}

	// Build token URL from issuer URL
	tokenURL := cfg.Auth.IssuerURL + "/protocol/openid-connect/token"

	// Create OAuth2 configuration
	oauth2Cfg := &oauth2.Config{
		ClientID: cfg.Auth.ClientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenURL,
		},
	}

	// Configure HTTP client with dual-stack support and fast fallback
	// This handles environments transitioning to IPv6 where DNS returns IPv6
	// but connectivity may not be available yet
	dialer := &net.Dialer{
		Timeout:       dialTimeout,
		KeepAlive:     dialKeepAlive,
		DualStack:     true, // Enable both IPv4 and IPv6
		FallbackDelay: dialFallbackDelay,
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
	}

	// Apply insecure TLS if requested
	if cfg.Evroc.InsecureTLS {
		logger.Warn("Insecure TLS mode enabled for authentication - certificate verification is disabled")
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   authRequestTimeout,
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)

	client := &Client{
		logger:       logger,
		oauth2Config: oauth2Cfg,
		username:     cfg.Auth.Username,
		password:     cfg.Auth.Password,
		httpClient:   httpClient,
	}

	// Perform initial authentication with retries
	// This handles transient network issues during startup
	if err := client.authenticateWithRetry(ctx, authMaxRetries); err != nil {
		logger.Warn("Initial authentication failed after retries", "error", err)
		return nil, fmt.Errorf("initial authentication failed: %w", err)
	}

	logger.Info("Authentication client initialized",
		"issuerURL", cfg.Auth.IssuerURL,
		"clientID", cfg.Auth.ClientID,
		"username", cfg.Auth.Username)

	return client, nil
}

// authenticateWithRetry attempts authentication with exponential backoff retry logic.
// This handles transient network issues, IPv4/IPv6 fallback delays, and slow auth services.
func (c *Client) authenticateWithRetry(ctx context.Context, maxAttempts int) error {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(attempt-1) * authRetryBackoff
			c.logger.Info("Retrying authentication after failure",
				"attempt", attempt,
				"maxAttempts", maxAttempts,
				"backoff", backoff)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			}
		}

		err := c.authenticate(ctx)
		if err == nil {
			if attempt > 1 {
				c.logger.Info("Authentication succeeded after retry", "attempt", attempt)
			}
			return nil
		}

		lastErr = err
		c.logger.Warn("Authentication attempt failed",
			"attempt", attempt,
			"maxAttempts", maxAttempts,
			"error", err)
	}

	return fmt.Errorf("authentication failed after %d attempts: %w", maxAttempts, lastErr)
}

// authenticate obtains a new OAuth2 token using the Resource Owner Password Credentials Grant.
func (c *Client) authenticate(ctx context.Context) error {
	c.logger.Debug("Authenticating with OIDC provider",
		"username", c.username,
		"tokenURL", c.oauth2Config.Endpoint.TokenURL)

	// Use the stored HTTP client (with TLS config) if available
	if c.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	}

	// Use Resource Owner Password Credentials Grant
	token, err := c.oauth2Config.PasswordCredentialsToken(ctx, c.username, c.password)
	if err != nil {
		c.logger.Warn("Authentication failed", "error", err)
		return fmt.Errorf("failed to obtain token: %w", err)
	}

	c.tokenMu.Lock()
	c.token = token
	// Token source will use ctx for refresh operations, preserving HTTP client config
	c.tokenSource = c.oauth2Config.TokenSource(ctx, token)
	c.tokenMu.Unlock()

	c.logger.Debug("Authentication successful",
		"expiresIn", time.Until(token.Expiry).Round(time.Second))

	return nil
}

// GetToken returns the current OAuth2 token, refreshing it if necessary.
func (c *Client) GetToken(ctx context.Context) (*oauth2.Token, error) {
	c.tokenMu.RLock()
	tokenSource := c.tokenSource
	currentToken := c.token
	c.tokenMu.RUnlock()

	if tokenSource == nil {
		c.logger.Warn("Token source not initialized")
		return nil, fmt.Errorf("token source not initialized")
	}

	// TokenSource.Token() will automatically refresh if expired
	token, err := tokenSource.Token()
	if err != nil {
		c.logger.Warn("Token refresh failed, attempting re-authentication", "error", err)

		// If refresh fails, try to re-authenticate
		if authErr := c.authenticate(ctx); authErr != nil {
			c.logger.Warn("Re-authentication failed", "error", authErr)
			return nil, fmt.Errorf("re-authentication failed: %w", authErr)
		}

		// Return the newly authenticated token
		c.tokenMu.RLock()
		defer c.tokenMu.RUnlock()
		c.logger.Info("Re-authenticated successfully", "expiresIn", time.Until(c.token.Expiry).Round(time.Second))
		return c.token, nil
	}

	// Update our cached token
	c.tokenMu.Lock()
	wasRefreshed := currentToken != nil && token.AccessToken != currentToken.AccessToken
	c.token = token
	c.tokenMu.Unlock()

	if wasRefreshed {
		c.logger.Info("Token refreshed", "expiresIn", time.Until(token.Expiry).Round(time.Second))
	}

	return token, nil
}

// GetHTTPClient returns an HTTP client that automatically handles OAuth2 authentication.
// The client will automatically refresh tokens when they expire.
func (c *Client) GetHTTPClient(ctx context.Context) (*http.Client, error) {
	c.tokenMu.RLock()
	tokenSource := c.tokenSource
	c.tokenMu.RUnlock()

	if tokenSource == nil {
		c.logger.Warn("Cannot create HTTP client: token source not initialized")
		return nil, fmt.Errorf("token source not initialized")
	}

	// Use custom HTTP client (e.g., with insecure TLS) if configured
	if c.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	}

	// Create HTTP client with OAuth2 transport that handles auto-refresh
	return c.oauth2Config.Client(ctx, c.token), nil
}

// GetAccessToken returns the current access token string, refreshing if necessary.
func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// IsTokenValid checks if the current token is valid and not expired.
func (c *Client) IsTokenValid() bool {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()

	if c.token == nil {
		return false
	}

	// Check if token is expired (with buffer to prevent race conditions)
	return c.token.Valid() && time.Until(c.token.Expiry) > tokenValidityBuffer
}

// ForceRefresh forces a token refresh even if the current token is still valid.
// This can be useful for testing or recovering from authentication issues.
func (c *Client) ForceRefresh(ctx context.Context) error {
	c.logger.Info("Forcing token refresh")
	if err := c.authenticate(ctx); err != nil {
		return err
	}
	c.logger.Info("Token force refresh completed")
	return nil
}
