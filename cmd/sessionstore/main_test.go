package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunWritesAndReadsBackSession(t *testing.T) {
	var buf bytes.Buffer
	if err := run(&buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "created id=") {
		t.Errorf("output missing creation line: %q", out)
	}
	if !strings.Contains(out, `payload="hello session"`) {
		t.Errorf("output missing read-back payload: %q", out)
	}
}
