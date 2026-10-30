package cmd

import (
	"testing"
)

func TestParseLocation(t *testing.T) {
	tests := []struct {
		input       string
		expectedPod bool
		expectedName string
		expectedPath string
	}{
		{
			input:        "my-pod:/var/log",
			expectedPod:  true,
			expectedName: "my-pod",
			expectedPath: "/var/log",
		},
		{
			input:        "./local-dir/",
			expectedPod:  false,
			expectedName: "",
			expectedPath: "./local-dir/",
		},
		{
			input:        "/tmp/file.txt",
			expectedPod:  false,
			expectedName: "",
			expectedPath: "/tmp/file.txt",
		},
		{
			input:        "nginx-pod:app/config.json",
			expectedPod:  true,
			expectedName: "nginx-pod",
			expectedPath: "app/config.json",
		},
	}

	for _, tc := range tests {
		loc := parseLocation(tc.input)
		if loc.IsPod != tc.expectedPod {
			t.Errorf("for %q, expected isPod %v, got %v", tc.input, tc.expectedPod, loc.IsPod)
		}
		if loc.PodName != tc.expectedName {
			t.Errorf("for %q, expected podName %q, got %q", tc.input, tc.expectedName, loc.PodName)
		}
		if loc.Path != tc.expectedPath {
			t.Errorf("for %q, expected path %q, got %q", tc.input, tc.expectedPath, loc.Path)
		}
	}
}
