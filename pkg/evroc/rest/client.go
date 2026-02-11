// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// HTTP client timeouts for dual-stack networking
	dialTimeout           = 10 * time.Second
	dialKeepAlive         = 30 * time.Second
	dialFallbackDelay     = 300 * time.Millisecond
	tlsHandshakeTimeout   = 10 * time.Second
	idleConnTimeout       = 90 * time.Second
	expectContinueTimeout = 1 * time.Second
)

// AuthClient is the interface for authentication.
type AuthClient interface {
	GetAccessToken(ctx context.Context) (string, error)
	IsTokenValid() bool
}

// HTTPError represents an HTTP error response with status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// Client provides access to the evroc REST API.
type Client struct {
	baseURL    *url.URL
	authClient AuthClient
	httpClient *http.Client
	logger     *slog.Logger
	cfg        *config.Config
	metrics    *metrics.Manager

	tokenMu sync.RWMutex // Protects currentToken

	projectID    string // Project ID for resource paths (e.g., "untitled-project")
	region       string // Region for resource paths (e.g., "se-sto")
	identifier   string // CSI driver identifier for resource labeling
	currentToken string // Cached access token
}

// NewClient creates a new REST API client.
func NewClient(ctx context.Context, authClient AuthClient, cfg *config.Config, logger *slog.Logger, metricsManager *metrics.Manager) (*Client, error) {
	baseURL, err := url.Parse(cfg.Evroc.RestURL)
	if err != nil {
		return nil, fmt.Errorf("parse REST URL: %w", err)
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
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.Evroc.InsecureTLS,
		},
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.CSI.RestAPITimeout,
	}

	if cfg.Evroc.InsecureTLS {
		logger.Warn("Insecure TLS mode enabled - certificate verification is disabled. This should NEVER be used in production!")
	}

	// Use Project as projectID (e.g., "untitled-project")
	projectID := cfg.Evroc.Project
	if projectID == "" {
		return nil, fmt.Errorf("evroc.project is required for REST API")
	}

	// Get region from infrastructure config
	region := cfg.Infrastructure.Region
	if region == "" {
		return nil, fmt.Errorf("infrastructure.region is required for REST API")
	}

	// Identifier defaults to projectID if not specified
	identifier := cfg.CSI.Identifier
	if identifier == "" {
		identifier = projectID
	}

	client := &Client{
		baseURL:    baseURL,
		authClient: authClient,
		httpClient: httpClient,
		logger:     logger,
		cfg:        cfg,
		projectID:  projectID,
		region:     region,
		identifier: identifier,
		metrics:    metricsManager,
	}

	// Initialize token cache
	if authClient != nil {
		startTime := time.Now()
		token, err := authClient.GetAccessToken(ctx)
		duration := time.Since(startTime).Seconds()

		if err != nil {
			metricsManager.RecordAuthOperationError("initial_auth", duration, classifyAuthError(err))
			logger.Error("Failed to get initial access token", "duration", duration, "error", err)
			return nil, fmt.Errorf("get initial access token: %w", err)
		}

		// Record successful initial authentication
		metricsManager.RecordAuthOperation("initial_auth", duration)
		logger.Info("Initial authentication successful", "duration", duration)
		client.currentToken = token
	}

	return client, nil
}

// refreshTokenIfNeeded checks if the current token is still valid and refreshes it if necessary.
func (c *Client) refreshTokenIfNeeded(ctx context.Context) error {
	// If no auth client, nothing to do
	if c.authClient == nil {
		return nil
	}

	// Check if current token is still valid
	if c.authClient.IsTokenValid() {
		return nil
	}

	c.logger.Info("Token expired or expiring soon, refreshing token")

	// Track authentication operation timing
	startTime := time.Now()

	// Get a fresh token
	token, err := c.authClient.GetAccessToken(ctx)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		c.metrics.RecordAuthOperationError("token_refresh", duration, classifyAuthError(err))
		c.logger.Error("Failed to refresh token", "duration", duration, "error", err)
		return status.Errorf(codes.Unauthenticated, "get fresh access token: %v", err)
	}

	// Record successful authentication
	c.metrics.RecordAuthOperation("token_refresh", duration)

	// Cache the new token
	c.tokenMu.Lock()
	c.currentToken = token
	c.tokenMu.Unlock()

	c.logger.Info("Token refreshed successfully", "duration", duration)
	return nil
}

// doRequest performs an HTTP request with automatic token refresh.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	fullURL := fmt.Sprintf("%s://%s%s", c.baseURL.Scheme, c.baseURL.Host, path)
	return c.doRequestWithFullURL(ctx, method, fullURL, body, result)
}

func (c *Client) doRequestWithFullURL(ctx context.Context, method, fullURL string, body interface{}, result interface{}) error {
	// Use cached access token
	c.tokenMu.RLock()
	token := c.currentToken
	c.tokenMu.RUnlock()

	// Marshal request body if provided
	var bodyReader io.Reader
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return status.Errorf(codes.Internal, "marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create HTTP request using the full URL string directly
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return status.Errorf(codes.Internal, "create request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Log request with body if present
	if len(bodyBytes) > 0 {
		c.logger.Info("REST API request",
			"method", method,
			"url", fullURL,
			"body", string(bodyBytes))
	} else {
		c.logger.Info("REST API request",
			"method", method,
			"url", fullURL)
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return status.Errorf(codes.Unavailable, "execute request: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("Failed to close response body", "error", closeErr)
		}
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return status.Errorf(codes.Internal, "read response body: %v", err)
	}

	// Handle error responses
	if resp.StatusCode >= 400 {
		c.logger.Info("REST API response",
			"status", resp.StatusCode,
			"contentLength", len(respBody),
			"body", string(respBody))
		var apiErr Error
		var msg string
		if err := json.Unmarshal(respBody, &apiErr); err != nil {
			msg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
		} else {
			msg = fmt.Sprintf("API error %d: %s - %s", resp.StatusCode, apiErr.Reason, apiErr.Debug)
		}

		// Convert HTTP status code to gRPC code
		code := httpStatusToGRPCCode(resp.StatusCode)
		return status.Errorf(code, "%s", msg)
	}

	// Log successful responses (no body)
	c.logger.Info("REST API response",
		"status", resp.StatusCode,
		"contentLength", len(respBody))

	// Unmarshal success response if result is provided
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return status.Errorf(codes.Internal, "unmarshal response: %v", err)
		}
	}

	return nil
}

// classifyAuthError classifies authentication errors for metrics.
func classifyAuthError(err error) string {
	if err == nil {
		return "unknown"
	}
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		return "timeout"
	} else if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "401") {
		return "unauthorized"
	} else if strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "403") {
		return "forbidden"
	} else if strings.Contains(errStr, "network") || strings.Contains(errStr, "connection") {
		return "network"
	}
	return "unknown"
}

// IsNotFound returns true if the error is a 404 Not Found or 403 Forbidden error.
// The API returns 403 for resources that don't exist to avoid leaking information.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "403") || strings.Contains(errStr, "404")
}

// IsConflict returns true if the error is a 409 Conflict error.
func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "409") || strings.Contains(errStr, "conflict") || strings.Contains(errStr, "already exists")
}

// IsAlreadyExists returns true if the error is a 409 Conflict error.
func IsAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "409")
}

// IsServerError returns true if the error is a 5xx server error.
func IsServerError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500 && httpErr.StatusCode < 600
	}
	return false
}

// buildResourcePath constructs a resource path with project and region.
// Format: /api/v1/projects/{projectId}/regions/{regionId}/{resource}
// Example: /compute/v1alpha1/projects/untitled-project/regions/se-sto/disks
func (c *Client) buildResourcePath(apiGroup, version, resource string) string {
	return fmt.Sprintf("/%s/%s/projects/%s/regions/%s/%s",
		apiGroup, version, c.projectID, c.region, resource)
}

// buildResourceItemPath constructs a path for a specific resource item.
// Format: /api/v1/projects/{projectId}/regions/{regionId}/{resource}/{itemId}
func (c *Client) buildResourceItemPath(apiGroup, version, resource, itemID string) string {
	return fmt.Sprintf("/%s/%s/projects/%s/regions/%s/%s/%s",
		apiGroup, version, c.projectID, c.region, resource, itemID)
}

// buildResourcePathWithQuery constructs a full URL with query parameters.
func (c *Client) buildResourcePathWithQuery(apiGroup, version, resource string, query url.Values) string {
	path := c.buildResourcePath(apiGroup, version, resource)
	fullURL := fmt.Sprintf("%s://%s%s", c.baseURL.Scheme, c.baseURL.Host, path)
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	return fullURL
}

// httpStatusToGRPCCode converts HTTP status codes to gRPC codes.
func httpStatusToGRPCCode(httpStatus int) codes.Code {
	switch httpStatus {
	case http.StatusBadRequest: // 400
		return codes.InvalidArgument
	case http.StatusUnauthorized: // 401
		return codes.Unauthenticated
	case http.StatusForbidden: // 403
		return codes.PermissionDenied
	case http.StatusNotFound: // 404
		return codes.NotFound
	case http.StatusMethodNotAllowed: // 405
		return codes.Unimplemented
	case http.StatusConflict: // 409
		return codes.AlreadyExists
	case http.StatusGone: // 410
		return codes.NotFound
	case http.StatusPreconditionFailed: // 412
		return codes.FailedPrecondition
	case http.StatusRequestEntityTooLarge: // 413
		return codes.OutOfRange
	case http.StatusTooManyRequests: // 429
		return codes.ResourceExhausted
	case http.StatusInternalServerError: // 500
		return codes.Internal
	case http.StatusNotImplemented: // 501
		return codes.Unimplemented
	case http.StatusBadGateway: // 502
		return codes.Unavailable
	case http.StatusServiceUnavailable: // 503
		return codes.Unavailable
	case http.StatusGatewayTimeout: // 504
		return codes.DeadlineExceeded
	default:
		if httpStatus >= 400 && httpStatus < 500 {
			return codes.InvalidArgument
		}
		return codes.Internal
	}
}
