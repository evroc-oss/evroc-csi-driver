// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/stretchr/testify/require"
)

const (
	testClientID     = "test-client"
	testUsername     = "testuser"
	testPassword     = "testpass"
	testAccessToken  = "test-access-token"
	testRefreshToken = "test-refresh-token"
	testTokenExpiry  = 1 // Token expiry in seconds for testing auto-refresh
)

// mockOAuth2Server creates a test OAuth2 server that handles token requests.
type mockOAuth2Server struct {
	*httptest.Server
	tokenHandler func(grantType, username, password string) (accessToken, refreshToken string, expiresIn int, statusCode int)
}

func newMockOAuth2Server() *mockOAuth2Server {
	mock := &mockOAuth2Server{}

	// Default handler returns valid tokens
	mock.tokenHandler = func(grantType, username, password string) (string, string, int, int) {
		if grantType == "password" && username == testUsername && password == testPassword {
			return testAccessToken, testRefreshToken, 3600, http.StatusOK
		}
		return "", "", 0, http.StatusUnauthorized
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/protocol/openid-connect/token" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}

		grantType := r.FormValue("grant_type")
		username := r.FormValue("username")
		password := r.FormValue("password")

		accessToken, refreshToken, expiresIn, statusCode := mock.tokenHandler(grantType, username, password)

		if statusCode != http.StatusOK {
			http.Error(w, "unauthorized", statusCode)
			return
		}

		response := map[string]interface{}{
			"access_token":  accessToken,
			"token_type":    "Bearer",
			"expires_in":    expiresIn,
			"refresh_token": refreshToken,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
	}))

	return mock
}

// newHangingServer creates a server that responds successfully N times, then hangs forever.
func newHangingServer(successCount int, expiresIn int) (*httptest.Server, *int) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount <= successCount {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "failed to parse form", http.StatusBadRequest)
				return
			}
			response := map[string]interface{}{
				"access_token":  testAccessToken,
				"token_type":    "Bearer",
				"expires_in":    expiresIn,
				"refresh_token": testRefreshToken,
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(response); err != nil {
				http.Error(w, "failed to encode response", http.StatusInternalServerError)
			}
			return
		}
		time.Sleep(10 * time.Second)
	}))
	return server, &requestCount
}

// newServerWithAutoRefresh creates a server that supports token refresh.
func newServerWithAutoRefresh(initialExpiresIn int) (*mockOAuth2Server, *bool) {
	refreshCalled := false
	server := newMockOAuth2Server()
	server.tokenHandler = func(grantType, username, password string) (string, string, int, int) {
		switch grantType {
		case "password":
			return "initial-token", testRefreshToken, initialExpiresIn, http.StatusOK
		case "refresh_token":
			refreshCalled = true
			return "refreshed-token", "new-refresh-token", 3600, http.StatusOK
		default:
			return "", "", 0, http.StatusBadRequest
		}
	}
	return server, &refreshCalled
}

// newServerThatFailsAfterN creates a server that succeeds N times then fails forever.
func newServerThatFailsAfterN(successCount int, initialExpiresIn int) (*mockOAuth2Server, *int) {
	requestCount := 0
	server := newMockOAuth2Server()
	server.tokenHandler = func(grantType, username, password string) (string, string, int, int) {
		requestCount++
		if requestCount <= successCount {
			if requestCount == 1 {
				return "initial-token", "initial-refresh-token", initialExpiresIn, http.StatusOK
			}
			return "reauth-token", "new-refresh-token", 3600, http.StatusOK
		}
		return "", "", 0, http.StatusUnauthorized
	}
	return server, &requestCount
}

// newServerWithRefreshFailure creates a server for testing refresh failure with successful re-auth.
func newServerWithRefreshFailure(initialExpiresIn int) (*mockOAuth2Server, *int) {
	requestCount := 0
	server := newMockOAuth2Server()
	server.tokenHandler = func(grantType, username, password string) (string, string, int, int) {
		requestCount++
		if requestCount == 1 {
			return "initial-token", "initial-refresh-token", initialExpiresIn, http.StatusOK
		}
		if requestCount == 2 {
			return "", "", 0, http.StatusUnauthorized
		}
		return "reauth-token", "new-refresh-token", 3600, http.StatusOK
	}
	return server, &requestCount
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func testConfig(issuerURL string) *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			IssuerURL: issuerURL,
			ClientID:  testClientID,
			Username:  testUsername,
			Password:  testPassword,
		},
	}
}

func TestNewClient(t *testing.T) {
	server := newMockOAuth2Server()
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)
	require.NotNil(t, client)

	// Verify token was obtained
	require.True(t, client.IsTokenValid())

	// Verify access token
	accessToken, err := client.GetAccessToken(ctx)
	require.NoError(t, err)
	require.Equal(t, testAccessToken, accessToken)
}

func TestNewClient_InvalidCredentials(t *testing.T) {
	server := newMockOAuth2Server()
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)
	cfg.Auth.Username = "baduser"
	cfg.Auth.Password = "badpass"

	_, err := NewClient(ctx, cfg, testLogger())
	require.Error(t, err)
}

func TestGetToken_AutoRefresh(t *testing.T) {
	server, refreshCalled := newServerWithAutoRefresh(testTokenExpiry)
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)

	time.Sleep(2 * testTokenExpiry * time.Second)

	accessToken, err := client.GetAccessToken(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, accessToken)
	require.True(t, *refreshCalled)
}

func TestGetHTTPClient(t *testing.T) {
	server := newMockOAuth2Server()
	defer server.Close()

	// Add a test endpoint that verifies auth header
	testEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		require.Equal(t, "Bearer "+testAccessToken, authHeader)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authenticated"))
	}))
	defer testEndpoint.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	authClient, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)

	// Get HTTP client with automatic auth
	httpClient, err := authClient.GetHTTPClient(ctx)
	require.NoError(t, err)

	// Make a request using the authenticated client
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testEndpoint.URL+"/api/test", nil)
	require.NoError(t, err)

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestForceRefresh(t *testing.T) {
	server, callCount := newServerThatFailsAfterN(2, 3600)
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)
	require.Equal(t, 1, *callCount, "expected 1 initial call")

	// Force refresh
	err = client.ForceRefresh(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, *callCount, "expected 2 calls after refresh")
}

func TestGetAccessToken_Timeout(t *testing.T) {
	// Server responds once, then hangs
	server, requestCount := newHangingServer(1, 3600)
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)
	require.Equal(t, 1, *requestCount)

	// This should succeed because token is cached (no network call)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	token, err := client.GetAccessToken(timeoutCtx)
	require.NoError(t, err)
	require.Equal(t, testAccessToken, token)
	require.Equal(t, 1, *requestCount, "token was cached")
}

func TestGetToken_RefreshTimeout(t *testing.T) {
	// Create a server that works for initial auth (1s token), then hangs on refresh
	server, requestCount := newHangingServer(1, 1)
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	// Initial client creation should succeed
	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)
	require.Equal(t, 1, *requestCount, "expected 1 request for initial auth")

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	// Create a context with short timeout for token refresh
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This should fail because refresh times out
	_, err = client.GetToken(timeoutCtx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "context deadline exceeded")
	require.Equal(t, 2, *requestCount, "expected 2 requests: initial auth + failed refresh")
}

func TestNewClient_ContextCanceled(t *testing.T) {
	server := newMockOAuth2Server()
	defer server.Close()

	// Create context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := testConfig(server.URL)

	// Should fail due to canceled context
	_, err := NewClient(ctx, cfg, testLogger())
	require.Error(t, err)
}

func TestGetToken_RefreshFailureWithReauth(t *testing.T) {
	server, requestCount := newServerWithRefreshFailure(1)
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)
	require.Equal(t, 1, *requestCount)

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	// Get token - should trigger refresh failure and re-auth
	token, err := client.GetToken(ctx)
	require.NoError(t, err)
	require.NotNil(t, token)
	require.Equal(t, "reauth-token", token.AccessToken)
	require.Equal(t, 3, *requestCount, "expected 3 requests: initial auth, failed refresh, successful re-auth")
}

func TestGetToken_RefreshAndReauthBothFail(t *testing.T) {
	server, requestCount := newServerThatFailsAfterN(1, 1)
	defer server.Close()

	ctx := context.Background()
	cfg := testConfig(server.URL)

	client, err := NewClient(ctx, cfg, testLogger())
	require.NoError(t, err)
	require.Equal(t, 1, *requestCount)

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	// Get token - should fail after re-auth also fails
	_, err = client.GetToken(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "re-authentication failed")
	require.GreaterOrEqual(t, *requestCount, 2, "expected at least 2 requests: initial auth and failed re-auth")
}

func TestGetToken_SlowServerWithTimeout(t *testing.T) {
	// Create server that responds slowly
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/protocol/openid-connect/token" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}

		// Delay response
		time.Sleep(200 * time.Millisecond)

		response := map[string]interface{}{
			"access_token":  testAccessToken,
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": testRefreshToken,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
	}))
	defer slowServer.Close()

	cfg := testConfig(slowServer.URL)

	t.Run("succeeds with sufficient timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		client, err := NewClient(ctx, cfg, testLogger())
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("fails with insufficient timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := NewClient(ctx, cfg, testLogger())
		require.Error(t, err)
		require.Contains(t, err.Error(), "context deadline exceeded")
	})
}
