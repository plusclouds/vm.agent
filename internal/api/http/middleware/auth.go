// Package middleware provides Chi-compatible HTTP middleware for the agent API.
package middleware

import (
	"net/http"
	"strings"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
)

// Auth returns a middleware that validates the API key on every request.
// The key may be supplied in one of two ways:
//
//  1. Authorization: Bearer <key>
//  2. X-API-Key: <key>
//
// All endpoints — including /healthz and /metrics — require authentication
// since the agent may be exposed to a DMZ or untrusted network.
// If the API key is empty (agent not yet configured), all routes return 401.
func Auth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract the presented key.
			presented := extractKey(r)
			if presented == "" {
				response.Error(w, http.StatusUnauthorized, "MISSING_CREDENTIALS",
					"Authentication required. Provide a Bearer token or X-API-Key header.")
				return
			}

			// Reject if the agent has no key configured.
			if apiKey == "" {
				response.Error(w, http.StatusUnauthorized, "AGENT_NOT_CONFIGURED",
					"Agent API key is not configured. Ensure the config drive is mounted.")
				return
			}

			// Constant-time comparison to mitigate timing attacks.
			if !secureCompare(presented, apiKey) {
				response.Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS",
					"Invalid API key.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractKey pulls the API key from the request headers.
// It prefers the Authorization: Bearer header, then falls back to X-API-Key.
func extractKey(r *http.Request) string {
	// Try Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	// Try X-API-Key header.
	if key := r.Header.Get("X-API-Key"); key != "" {
		return strings.TrimSpace(key)
	}

	return ""
}

// secureCompare performs a constant-time string comparison to prevent
// timing-based side-channel attacks.
func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
