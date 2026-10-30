package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTreeWithMetadata(t *testing.T) {
	entries := []fileEntry{
		{relPath: "etc", permissions: "drwxr-xr-x", sizeStr: "4.0K", isDir: true},
		{relPath: "etc/coredns", permissions: "drwxr-xr-x", sizeStr: "4.0K", isDir: true},
		{relPath: "etc/coredns/Corefile", permissions: "-rw-r--r--", sizeStr: "1.2K", isDir: false},
		{relPath: "var", permissions: "drwxr-xr-x", sizeStr: "4.0K", isDir: true},
		{relPath: "var/log", permissions: "drwxr-xr-x", sizeStr: "4.0K", isDir: true},
		{relPath: "var/log/app.log", permissions: "-rw-r--r--", sizeStr: "500B", isDir: false},
	}

	var buf bytes.Buffer
	renderTreeWithMetadata(&buf, entries, true, true, false, "/root")

	output := buf.String()

	if !strings.Contains(output, "Corefile") {
		t.Errorf("expected tree to contain Corefile, got:\n%s", output)
	}
	if !strings.Contains(output, "[-rw-r--r--   1.2K]") {
		t.Errorf("expected tree to contain permissions and size, got:\n%s", output)
	}
	if !strings.Contains(output, "[drwxr-xr-x   4.0K]") {
		t.Errorf("expected tree to contain directory metadata, got:\n%s", output)
	}
}

func TestFormatHumanSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{500, "500B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1073741824, "1.0G"},
	}

	for _, tc := range tests {
		actual := formatHumanSize(tc.bytes)
		if actual != tc.expected {
			t.Errorf("for %d bytes, expected %s, got %s", tc.bytes, tc.expected, actual)
		}
	}
}
