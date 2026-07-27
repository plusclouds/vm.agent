package dispatcher

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactedParams_RedactsPasswordForSetPasswordOp(t *testing.T) {
	raw := []byte(`{"username":"alice","password":"s3cret"}`)
	got := redactedParams("identity.set_password", raw)

	if strings.Contains(string(got), "s3cret") {
		t.Fatalf("redactedParams() leaked the password: %s", got)
	}

	var m map[string]string
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("redactedParams() output is not valid JSON: %v", err)
	}
	if m["password"] != "***" {
		t.Errorf("password = %q, want ***", m["password"])
	}
	if m["username"] != "alice" {
		t.Errorf("username = %q, want alice (should be preserved)", m["username"])
	}
}

func TestRedactedParams_PassesThroughOtherOperationsUnchanged(t *testing.T) {
	raw := []byte(`{"name":"nginx"}`)
	got := redactedParams("services.start", raw)
	if string(got) != string(raw) {
		t.Errorf("redactedParams() modified params for a non-sensitive op: %s", got)
	}
}

func TestRedactedParams_NoPasswordField_Unaffected(t *testing.T) {
	raw := []byte(`{"username":"alice"}`)
	got := redactedParams("identity.set_password", raw)
	if string(got) != string(raw) {
		t.Errorf("redactedParams() = %s, want unchanged %s", got, raw)
	}
}

func TestRedactedParams_MalformedJSON_DoesNotPanicOrLeak(t *testing.T) {
	raw := []byte(`not json, contains s3cret anyway`)
	got := redactedParams("identity.set_password", raw)
	if strings.Contains(string(got), "s3cret") {
		t.Fatalf("redactedParams() leaked content from malformed input: %s", got)
	}
}
