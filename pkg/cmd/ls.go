package cmd

import (
	"context"
	"fmt"

	"github.com/premisan/kubectl-clone/pkg/k8s"
	"github.com/premisan/kubectl-clone/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	lsExample = `  # 1. Standard long listing of pod root directory
  kubectl clone ls my-pod

  # 2. List all files including hidden files (-a) and human readable sizes
  kubectl clone ls coredns-xxxx:/etc/coredns -n kube-system -a

  # 3. Sort by modification time, newest first (-t)
  kubectl clone ls my-pod:/var/log -t

  # 4. Sort by file size, largest first (-S) in reverse order (-r)
  kubectl clone ls my-pod:/data -S -r

  # 5. Recursive listing (-R)
  kubectl clone ls my-pod:/etc -R

  # 6. Use shorthand 'l'
  kubectl clone l etcd-desktop-control-plane:/etc/kubernetes/pki -n kube-system -t`
)

// LsOptions contains flags and configuration for 'kubectl clone ls'.
type LsOptions struct {
	Namespace     string
	Context       string
	Container     string
	UseDebug      bool
	DebugImage    string

	// Linux ls options
	All           bool
	AlmostAll     bool
	Long          bool
	HumanReadable bool
	SortTime      bool
	SortSize      bool
	Reverse       bool
	Recursive     bool
	OnePerLine    bool

	genericclioptions.IOStreams
}

// NewCmdCloneLs creates the 'ls' subcommand.
func NewCmdCloneLs(streams genericclioptions.IOStreams) *cobra.Command {
	o := &LsOptions{
		UseDebug:      true,
		DebugImage:    k8s.DefaultDebugImage,
		Long:          true,
		HumanReadable: true,
		IOStreams:     streams,
	}

	cmd := &cobra.Command{
		Use:          "ls <pod>:[path] [flags]",
		Aliases:      []string{"l", "list"},
		Short:        "List files and directories inside a pod (supports Linux ls flags)",
		Long:         `List files, permissions, and directory structures inside containers with standard Linux ls flags. Uses Ephemeral Debug Containers to inspect Distroless/Scratch pods without native shells or 'ls' binaries.`,
		Example:      lsExample,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd.Context(), args[0])
		},
	}

	// k8s flags
	cmd.Flags().StringVar(&o.Context, "context", "", "The kubeconfig context to use")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "If present, the namespace scope for this request")
	cmd.Flags().StringVarP(&o.Container, "container", "c", "", "Container name in pod")
	cmd.Flags().BoolVar(&o.UseDebug, "use-debug", true, "Use ephemeral debug container to support Distroless/Scratch pods (defaults to true)")
	cmd.Flags().StringVar(&o.DebugImage, "debug-image", k8s.DefaultDebugImage, "Container image for ephemeral debug containers")

	// Standard Linux ls flags
	cmd.Flags().BoolVarP(&o.All, "all", "a", false, "Do not ignore entries starting with .")
	cmd.Flags().BoolVarP(&o.AlmostAll, "almost-all", "A", false, "Do not list implied . and ..")
	cmd.Flags().BoolVarP(&o.Long, "long", "l", true, "Use a long listing format")
	cmd.Flags().BoolVar(&o.HumanReadable, "human-readable", true, "Print human readable sizes (e.g., 1K 234M 2G)")
	cmd.Flags().BoolVarP(&o.SortTime, "sort-time", "t", false, "Sort by modification time, newest first")
	cmd.Flags().BoolVarP(&o.SortSize, "sort-size", "S", false, "Sort by file size, largest first")
	cmd.Flags().BoolVarP(&o.Reverse, "reverse", "r", false, "Reverse order while sorting")
	cmd.Flags().BoolVarP(&o.Recursive, "recursive", "R", false, "List subdirectories recursively")
	cmd.Flags().BoolVarP(&o.OnePerLine, "one", "1", false, "List one file per line")

	_ = cmd.RegisterFlagCompletionFunc("context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// Run executes the 'ls' inspection.
func (o *LsOptions) Run(ctx context.Context, targetArg string) error {
	loc := parseLocation(targetArg)
	podName := loc.PodName
	targetPath := loc.Path

	if !loc.IsPod {
		podName = targetArg
		targetPath = "/"
	}
	if targetPath == "" {
		targetPath = "/"
	}

	bundle, err := k8s.NewClientBundle("", o.Context, o.Namespace)
	if err != nil {
		return fmt.Errorf("failed to init cluster client: %w", err)
	}

	activeContainer := o.Container
	resolvedPath := targetPath

	if o.UseDebug {
		util.PrintBanner(o.Out, fmt.Sprintf("Inspecting pod %s via ephemeral debug container...", podName))
		debugContainer, err := k8s.EnsureDebugContainer(ctx, bundle, podName, o.Container, o.DebugImage)
		if err != nil {
			util.PrintBanner(o.ErrOut, fmt.Sprintf("Warning: Debug container attachment failed (%v). Falling back to direct container mode.", err))
		} else {
			activeContainer = debugContainer
			resolvedPath = k8s.ResolveDebugPath(ctx, bundle, podName, debugContainer, targetPath)
		}
	}

	// Build ls command line with user-selected flags
	lsArgs := []string{"ls"}
	flags := "-"
	if o.Long {
		flags += "l"
	}
	if o.HumanReadable {
		flags += "h"
	}
	if o.All {
		flags += "a"
	} else if o.AlmostAll {
		flags += "A"
	}
	if o.SortTime {
		flags += "t"
	}
	if o.SortSize {
		flags += "S"
	}
	if o.Reverse {
		flags += "r"
	}
	if o.Recursive {
		flags += "R"
	}
	if o.OnePerLine {
		flags += "1"
	}

	if flags != "-" {
		lsArgs = append(lsArgs, flags)
	}
	lsArgs = append(lsArgs, resolvedPath)

	fmt.Fprintf(o.Out, "Listing: %s:%s\n\n", podName, targetPath)
	return k8s.Exec(ctx, bundle, podName, activeContainer, lsArgs, nil, o.Out, o.ErrOut)
}
