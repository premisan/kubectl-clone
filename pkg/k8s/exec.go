package k8s

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// Exec executes a command in a pod/container and streams stdin/stdout/stderr.
func Exec(ctx context.Context, bundle *ClientBundle, podName, containerName string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	req := bundle.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(bundle.Namespace).
		SubResource("exec")

	opts := &corev1.PodExecOptions{
		Command:   cmd,
		Container: containerName,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
		TTY:       false,
	}

	req.VersionedParams(opts, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	}

	if err := exec.StreamWithContext(ctx, streamOpts); err != nil {
		return fmt.Errorf("exec stream failed in pod %s/%s: %w", bundle.Namespace, podName, err)
	}

	return nil
}
