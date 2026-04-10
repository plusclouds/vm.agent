package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
	"github.com/plusclouds/ubuntu-agent/internal/config"
)

// envelope is used to decode the standard response wrapper.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Meta *struct {
		Version   string `json:"version"`
		AgentID   string `json:"agent_id"`
		Timestamp int64  `json:"timestamp"`
	} `json:"meta,omitempty"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return env
}

func TestSuccess_StatusAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Success(rec, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}
}

func TestSuccess_Body(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Success(rec, map[string]string{"key": "val"})

	env := decode(t, rec)
	if !env.Success {
		t.Error("expected success=true")
	}
	if env.Error != nil {
		t.Error("expected no error field on success")
	}
	if env.Meta == nil {
		t.Fatal("expected meta field to be present")
	}
	if env.Meta.Version != config.AgentVersion {
		t.Errorf("meta.version: got %q, want %q", env.Meta.Version, config.AgentVersion)
	}
	if env.Meta.Timestamp == 0 {
		t.Error("meta.timestamp should be non-zero")
	}
}

func TestError_StatusAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, http.StatusNotFound, "NOT_FOUND", "resource missing")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	env := decode(t, rec)
	if env.Success {
		t.Error("expected success=false for error response")
	}
	if env.Error == nil {
		t.Fatal("expected error field to be present")
	}
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("error.code: got %q, want NOT_FOUND", env.Error.Code)
	}
	if env.Error.Message != "resource missing" {
		t.Errorf("error.message: got %q, want 'resource missing'", env.Error.Message)
	}
	if env.Data != nil {
		t.Error("expected no data field on error response")
	}
}

func TestJSON_2xxSetsSuccessTrue(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusCreated, "payload")

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	env := decode(t, rec)
	if !env.Success {
		t.Error("expected success=true for 2xx status")
	}
}

func TestJSON_4xxSetsSuccessFalse(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusBadRequest, "bad input")

	env := decode(t, rec)
	if env.Success {
		t.Error("expected success=false for 4xx status")
	}
}

func TestSetAgentID(t *testing.T) {
	response.SetAgentID("vm-abc-123")
	rec := httptest.NewRecorder()
	response.Success(rec, nil)

	env := decode(t, rec)
	if env.Meta == nil {
		t.Fatal("expected meta")
	}
	if env.Meta.AgentID != "vm-abc-123" {
		t.Errorf("meta.agent_id: got %q, want vm-abc-123", env.Meta.AgentID)
	}

	// reset so other tests are not affected
	response.SetAgentID("")
}
