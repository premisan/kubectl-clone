package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/premisan/kubectl-clone/pkg/cmd"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	// Kubernetes plugin completion support (KEP-2580):
	// When invoked as 'kubectl_complete-<plugin>', kubectl passes the user's command line arguments directly.
	// We inject Cobra's hidden '__complete' command so Cobra's dynamic completion engine runs.
	binName := filepath.Base(os.Args[0])
	if strings.HasPrefix(binName, "kubectl_complete-") {
		newArgs := []string{os.Args[0], "__complete"}
		if len(os.Args) > 1 {
			newArgs = append(newArgs, os.Args[1:]...)
		}
		os.Args = newArgs
	}

	flags := pflag.NewFlagSet("kubectl-clone", pflag.ExitOnError)
	pflag.CommandLine = flags

	streams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	rootCmd := cmd.NewCmdClone(streams)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
