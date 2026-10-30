package rsync

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/premisan/kubectl-clone/pkg/k8s"
)

// RunDualExecBridge sets up a zero-copy, bi-directional memory pipe between two pods' rsync processes.
func RunDualExecBridge(
	ctx context.Context,
	srcBundle *k8s.ClientBundle, srcPod, srcContainer string, srcCmd []string,
	destBundle *k8s.ClientBundle, destPod, destContainer string, destCmd []string,
	stderr io.Writer,
) error {
	// Pipe 1: src stdout -> dest stdin
	srcOutReader, srcOutWriter := io.Pipe()
	// Pipe 2: dest stdout -> src stdin
	destOutReader, destOutWriter := io.Pipe()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var srcErr, destErr error

	// 1. Launch Source rsync sender stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer srcOutWriter.Close() // notify dest stdin of EOF
		err := k8s.Exec(ctx, srcBundle, srcPod, srcContainer, srcCmd, destOutReader, srcOutWriter, stderr)
		if err != nil {
			srcErr = fmt.Errorf("source rsync failed: %w", err)
			cancel()
		}
	}()

	// 2. Launch Destination rsync receiver stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer destOutWriter.Close() // notify src stdin of EOF
		err := k8s.Exec(ctx, destBundle, destPod, destContainer, destCmd, srcOutReader, destOutWriter, stderr)
		if err != nil {
			destErr = fmt.Errorf("destination rsync failed: %w", err)
			cancel()
		}
	}()

	wg.Wait()

	if srcErr != nil {
		return srcErr
	}
	if destErr != nil {
		return destErr
	}

	return nil
}
