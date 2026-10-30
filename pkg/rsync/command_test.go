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

func TestBuildServerWithOptions(t *testing.T) {
	opts := Options{
		Delete:    true,
		Checksum:  true,
		Compress:  true,
		Partial:   true,
		Bwlimit:   "5m",
		Excludes:  []string{"*.log", "cache/"},
		Includes:  []string{"*.txt"},
		ExtraArgs: []string{"--stats"},
	}

	sender := BuildServerSenderCommand("/src", opts)
	expectedSender := []string{
		"rsync", "--server", "--sender",
		"--exclude=*.log", "--exclude=cache/",
		"--include=*.txt",
		"--bwlimit=5m",
		"--partial",
		"--stats",
		"-logDtprcze.iLsfxC", ".", "/src",
	}

	if !reflect.DeepEqual(sender, expectedSender) {
		t.Errorf("expected sender %v, got %v", expectedSender, sender)
	}

	receiver := BuildServerReceiverCommand("/dest", opts)
	expectedReceiver := []string{
		"rsync", "--server", "--delete",
		"--exclude=*.log", "--exclude=cache/",
		"--include=*.txt",
		"--bwlimit=5m",
		"--partial",
		"--stats",
		"-logDtprcze.iLsfxC", ".", "/dest",
	}

	if !reflect.DeepEqual(receiver, expectedReceiver) {
		t.Errorf("expected receiver %v, got %v", expectedReceiver, receiver)
	}
}
