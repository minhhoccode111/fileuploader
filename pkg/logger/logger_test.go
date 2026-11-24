package logger

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWithRequestIDTagsEveryLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := newWithWriter(&buf, "info")
	l.WithRequestID("abc123").Info("hello")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("Unmarshal() error = %v, want nil", err)
	}

	if line["request_id"] != "abc123" {
		t.Errorf("request_id = %v, want abc123", line["request_id"])
	}

	if line["message"] != "hello" {
		t.Errorf("message = %v, want hello", line["message"])
	}
}

func TestWithRequestIDLeavesTheParentAlone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := newWithWriter(&buf, "info")
	l.WithRequestID("abc123")
	l.Info("plain")

	if bytes.Contains(buf.Bytes(), []byte("request_id")) {
		t.Error("parent logger gained a request_id field")
	}
}
