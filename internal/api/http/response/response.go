// Package response provides standardised JSON response helpers for the agent
// REST API. Every response — success or error — uses the same envelope format
// so clients can handle them uniformly.
package response

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/plusclouds/ubuntu-agent/internal/config"
)

// Response is the top-level JSON envelope returned by every API endpoint.
type Response struct {
	// Success is true for 2xx responses and false for error responses.
	Success bool `json:"success"`
	// Data holds the response payload on success. Omitted on error.
	Data interface{} `json:"data,omitempty"`
	// Error holds error detail. Omitted on success.
	Error *APIError `json:"error,omitempty"`
	// Meta holds agent metadata attached to every response.
	Meta *Meta `json:"meta,omitempty"`
}

// APIError describes a structured error returned by the API.
type APIError struct {
	// Code is a machine-readable error identifier (e.g. "NOT_FOUND").
	Code string `json:"code"`
	// Message is a human-readable description of the error.
	Message string `json:"message"`
}

// Meta carries request-scoped metadata in every response.
type Meta struct {
	// Version is the running agent version string.
	Version string `json:"version"`
	// AgentID is the VM identifier for the agent instance.
	AgentID string `json:"agent_id"`
	// Timestamp is the Unix timestamp (seconds) when the response was generated.
	Timestamp int64 `json:"timestamp"`
}

// agentID is populated at startup via SetAgentID.
var agentID string

// SetAgentID sets the agent identifier included in every response's Meta field.
// Call this once during agent initialisation with the VM ID from the ISO.
func SetAgentID(id string) {
	agentID = id
}

// newMeta builds a Meta value for the current moment.
func newMeta() *Meta {
	return &Meta{
		Version:   config.AgentVersion,
		AgentID:   agentID,
		Timestamp: time.Now().Unix(),
	}
}

// JSON writes a JSON-encoded response with the given HTTP status code.
// data is placed in the Response.Data field; Success is set based on the
// status code (true for 2xx, false otherwise).
func JSON(w http.ResponseWriter, status int, data interface{}) {
	resp := Response{
		Success: status >= 200 && status < 300,
		Data:    data,
		Meta:    newMeta(),
	}
	writeJSON(w, status, resp)
}

// Error writes a structured JSON error response.
func Error(w http.ResponseWriter, status int, code, message string) {
	resp := Response{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
		Meta: newMeta(),
	}
	writeJSON(w, status, resp)
}

// Success writes a 200 OK JSON response with data as the payload.
func Success(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusOK, data)
}

// writeJSON marshals v to JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// At this point headers are already sent; we can only log.
		// In production the zap logger would be used, but this package
		// deliberately avoids a logger dependency to stay simple.
		_ = err
	}
}
