package rsync

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/premisan/kubectl-clone/pkg/k8s"
)

// EnsureRsyncInstalled checks if rsync is available in the target pod/container, and attempts auto-installation if missing.
func EnsureRsyncInstalled(ctx context.Context, bundle *k8s.ClientBundle, podName, containerName string) error {
	var stdout, stderr bytes.Buffer
	err := k8s.Exec(ctx, bundle, podName, containerName, []string{"which", "rsync"}, nil, &stdout, &stderr)
	if err == nil && strings.TrimSpace(stdout.String()) != "" {
		// rsync is already present
		return nil
	}

	// Try Alpine package manager
	var installErr bytes.Buffer
	apkErr := k8s.Exec(ctx, bundle, podName, containerName, []string{"apk", "add", "--no-cache", "rsync"}, nil, nil, &installErr)
	if apkErr == nil {
		return nil
	}

	// Try Debian/Ubuntu package manager
	installErr.Reset()
	aptErr := k8s.Exec(ctx, bundle, podName, containerName, []string{"sh", "-c", "apt-get update -qq && apt-get install -y -qq rsync"}, nil, nil, &installErr)
	if aptErr == nil {
		return nil
	}

	return fmt.Errorf("rsync is not installed in pod %s/%s and automatic installation failed. Please ensure rsync is installed or use an image with rsync included",
		bundle.Namespace, podName)
}
