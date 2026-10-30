package k8s

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	DefaultDebugImage = "alpine:latest"
)

// EnsureDebugContainer attaches or reuses an ephemeral debug container with process sharing targeted at targetContainer.
func EnsureDebugContainer(ctx context.Context, bundle *ClientBundle, podName, targetContainer, debugImage string) (string, error) {
	if debugImage == "" {
		debugImage = DefaultDebugImage
	}

	pod, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// 1. Check if an active debug container already exists
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if strings.HasPrefix(status.Name, "clone-debug-") && status.State.Running != nil {
			return status.Name, nil
		}
	}

	// If targetContainer not specified, default to first regular container
	if targetContainer == "" && len(pod.Spec.Containers) > 0 {
		targetContainer = pod.Spec.Containers[0].Name
	}

	// 2. Add new Ephemeral Container targeted at the target container
	debugName := fmt.Sprintf("clone-debug-%04d", rand.Intn(10000))
	ephemeralSpec := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:            debugName,
			Image:           debugImage,
			Command:         []string{"sh", "-c", "trap 'exit 0' SIGTERM; while true; do sleep 3600; done"},
			ImagePullPolicy: corev1.PullIfNotPresent,
			Stdin:           true,
			TTY:             false,
		},
		TargetContainerName: targetContainer,
	}

	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephemeralSpec)

	_, err = bundle.Clientset.CoreV1().Pods(bundle.Namespace).UpdateEphemeralContainers(ctx, podName, pod, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach ephemeral debug container to pod %s: %w", podName, err)
	}

	// 3. Wait for the ephemeral container to enter Running phase
	err = WaitForEphemeralContainerRunning(ctx, bundle, podName, debugName, 45*time.Second)
	if err != nil {
		return "", fmt.Errorf("debug container failed to start: %w", err)
	}

	return debugName, nil
}

// WaitForEphemeralContainerRunning polls until the specified ephemeral container is running.
func WaitForEphemeralContainerRunning(ctx context.Context, bundle *ClientBundle, podName, containerName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name == containerName {
				if status.State.Running != nil {
					return true, nil
				}
				if status.State.Terminated != nil {
					return false, fmt.Errorf("debug container terminated unexpectedly: %s", status.State.Terminated.Message)
				}
			}
		}
		return false, nil
	})
}

// FindTargetPID locates the process ID of the target container from within the debug container.
func FindTargetPID(ctx context.Context, bundle *ClientBundle, podName, debugContainerName string) (int, error) {
	var stdout, stderr bytes.Buffer
	// Command to list processes: exclude sleep, sh, ps
	cmd := []string{"sh", "-c", "ps -o pid,comm | grep -v -E 'PID|sleep|sh|ps' | awk 'NR==1{print $1}'"}
	err := Exec(ctx, bundle, podName, debugContainerName, cmd, nil, &stdout, &stderr)
	if err == nil {
		pidStr := strings.TrimSpace(stdout.String())
		if pid, parseErr := strconv.Atoi(pidStr); parseErr == nil && pid > 0 {
			return pid, nil
		}
	}

	// Fallback to PID 1 (standard main process of a container)
	return 1, nil
}
