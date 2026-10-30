package rsync

import (
	"reflect"
	"testing"
)

func TestBuildServerSenderCommand(t *testing.T) {
	opts := Options{
		Compress: true,
		DryRun:   false,
	}

	cmd := BuildServerSenderCommand("/data/logs", opts)
	expected := []string{"rsync", "--server", "--sender", "-logDtprze.iLsfxC", ".", "/data/logs"}

	if !reflect.DeepEqual(cmd, expected) {
		t.Errorf("expected %v, got %v", expected, cmd)
	}
}

func TestBuildServerReceiverCommand(t *testing.T) {
	opts := Options{
		Delete:   true,
		Compress: true,
		DryRun:   false,
	}

	cmd := BuildServerReceiverCommand("/data/dest", opts)
	expected := []string{"rsync", "--server", "--delete", "-logDtprze.iLsfxC", ".", "/data/dest"}

	if !reflect.DeepEqual(cmd, expected) {
		t.Errorf("expected %v, got %v", expected, cmd)
	}
}
