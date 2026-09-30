package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKnownCredentialsInNativeHistory(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "private-key-value")
	r := New("provider-secret-value")
	out := r.JSON(json.RawMessage(`{"content":[{"text":"private-key-value provider-secret-value"}]}`))
	if !json.Valid(out) || strings.Contains(string(out), "secret-value") || strings.Contains(string(out), "private-key-value") {
		t.Fatal(string(out))
	}
}
