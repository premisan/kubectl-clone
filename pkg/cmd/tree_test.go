package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTree(t *testing.T) {
	paths := []string{
		"etc",
		"etc/coredns",
		"etc/coredns/Corefile",
		"var",
		"var/log",
		"var/log/app.log",
	}

	var buf bytes.Buffer
	renderTree(&buf, paths)

	output := buf.String()

	if !strings.Contains(output, "├── etc") && !strings.Contains(output, "└── etc") {
		t.Errorf("expected tree to contain etc, got:\n%s", output)
	}
	if !strings.Contains(output, "Corefile") {
		t.Errorf("expected tree to contain Corefile, got:\n%s", output)
	}
	if !strings.Contains(output, "app.log") {
		t.Errorf("expected tree to contain app.log, got:\n%s", output)
	}
}
