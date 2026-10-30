package util

import (
	"fmt"
	"io"
	"time"
)

// PrintBanner prints a formatted progress notification.
func PrintBanner(w io.Writer, msg string) {
	fmt.Fprintf(w, "==> %s\n", msg)
}

// TrackDuration tracks and prints elapsed execution time.
func TrackDuration(w io.Writer, operation string) func() {
	start := time.Now()
	return func() {
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Fprintf(w, "==> %s completed in %s\n", operation, elapsed)
	}
}
