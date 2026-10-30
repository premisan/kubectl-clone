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
// It also copies volume mounts from the target container and sets SYS_PTRACE permissions.
func EnsureDebugContainer(ctx context.Context, bundle *ClientBundle, podName, targetContainer, debugImage string) (string, error) {
	if debugImage == "" {
		debugImage = DefaultDebugImage
	}

	pod, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// If targetContainer not specified, default to first regular container
	if targetContainer == "" && len(pod.Spec.Containers) > 0 {
		targetContainer = pod.Spec.Containers[0].Name
	}

	// 1. Check if a properly configured debug container already exists (has volume mounts and is running)
	for _, ec := range pod.Spec.EphemeralContainers {
		if strings.HasPrefix(ec.Name, "clone-debug-") && len(ec.VolumeMounts) > 0 {
			for _, status := range pod.Status.EphemeralContainerStatuses {
				if status.Name == ec.Name && status.State.Running != nil {
					return ec.Name, nil
				}
			}
		}
	}

	// 2. Extract volume mounts from the target container to directly mount PVCs, ConfigMaps, and Secrets
	var volumeMounts []corev1.VolumeMount
	for _, c := range pod.Spec.Containers {
		if c.Name == targetContainer {
			volumeMounts = append(volumeMounts, c.VolumeMounts...)
			break
		}
	}

	// 3. Configure root and SYS_PTRACE capabilities to avoid kernel permission denied errors
	rootUID := int64(0)
	allowPrivilegeEscalation := true
	secContext := &corev1.SecurityContext{
		RunAsUser:                &rootUID,
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		Capabilities: &corev1.Capabilities{
			Add: []corev1.Capability{"SYS_PTRACE", "SYS_ADMIN", "DAC_OVERRIDE"},
		},
	}

	debugName := fmt.Sprintf("clone-debug-%04d", rand.Intn(10000))
	ephemeralSpec := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:            debugName,
			Image:           debugImage,
			Command:         []string{"sh", "-c", "trap 'exit 0' SIGTERM; while true; do sleep 3600; done"},
			ImagePullPolicy: corev1.PullIfNotPresent,
			Stdin:           true,
			TTY:             false,
			VolumeMounts:    volumeMounts,
			SecurityContext: secContext,
		},
		TargetContainerName: targetContainer,
	}

	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephemeralSpec)

	_, err = bundle.Clientset.CoreV1().Pods(bundle.Namespace).UpdateEphemeralContainers(ctx, podName, pod, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach ephemeral debug container to pod %s: %w", podName, err)
	}

	// 4. Wait for the ephemeral container to enter Running phase
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

// ResolveDebugPath checks if the target path is directly mounted in the debug container (e.g. volumes),
// or if it should be accessed via /proc/<PID>/root/<path>.
func ResolveDebugPath(ctx context.Context, bundle *ClientBundle, podName, debugContainer, targetPath string) string {
	cleanPath := targetPath
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	// Check if cleanPath exists directly in the debug container (via copied VolumeMounts)
	checkCmd := []string{"test", "-e", cleanPath}
	if err := Exec(ctx, bundle, podName, debugContainer, checkCmd, nil, nil, nil); err == nil {
		// Directly mounted volume found!
		if strings.HasSuffix(targetPath, "/") && !strings.HasSuffix(cleanPath, "/") {
			cleanPath += "/"
		}
		return cleanPath
	}

	// Otherwise access via target container process root
	targetPID, err := FindTargetPID(ctx, bundle, podName, debugContainer)
	if err != nil {
		targetPID = 1
	}

	resolved := fmt.Sprintf("/proc/%d/root%s", targetPID, cleanPath)
	if strings.HasSuffix(targetPath, "/") && !strings.HasSuffix(resolved, "/") {
		resolved += "/"
	}
	return resolved
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

	// Fallback to PID 1
	return 1, nil
}
