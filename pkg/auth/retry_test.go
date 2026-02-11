// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func newRetryTestServer(failCount int) (*httptest.Server, *int) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount <= failCount {
			http.Error(w, "auth failed", http.StatusUnauthorized)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "test-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "test-refresh-token",
		})
	}))
	return server, &attemptCount
}

func newRetryTestClient(serverURL string) *Client {
	return &Client{
		logger: testLogger(),
		oauth2Config: &oauth2.Config{
			ClientID: testClientID,
			Endpoint: oauth2.Endpoint{
				TokenURL: serverURL + "/protocol/openid-connect/token",
			},
		},
		username:   testUsername,
		password:   testPassword,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestAuthenticateWithRetry_SuccessAfterRetries(t *testing.T) {
	server, _ := newRetryTestServer(2)
	defer server.Close()

	client := newRetryTestClient(server.URL)
	ctx := context.Background()

	start := time.Now()
	err := client.authenticateWithRetry(ctx, authMaxRetries+2)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, client.token)
	require.GreaterOrEqual(t, elapsed, authRetryBackoff-100*time.Millisecond)
}

func TestAuthenticateWithRetry_FailureAfterAllRetries(t *testing.T) {
	// Calculate expected backoff for authMaxRetries attempts
	totalBackoff := time.Duration(0)
	for attempt := 2; attempt <= authMaxRetries; attempt++ {
		totalBackoff += time.Duration(attempt-1) * authRetryBackoff
	}

	server, _ := newRetryTestServer(999)
	defer server.Close()

	client := newRetryTestClient(server.URL)
	ctx := context.Background()

	start := time.Now()
	err := client.authenticateWithRetry(ctx, authMaxRetries)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, client.token)
	require.GreaterOrEqual(t, elapsed, totalBackoff-200*time.Millisecond)
}

func TestAuthenticateWithRetry_ContextCancellation(t *testing.T) {
	server, _ := newRetryTestServer(999)
	defer server.Close()

	client := newRetryTestClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := client.authenticateWithRetry(ctx, authMaxRetries+2)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, authRetryBackoff)
}
