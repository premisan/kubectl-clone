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
	lsExample = `  # 1. List root directory of a pod
  kubectl clone ls my-pod

  # 2. List specific directory inside a Distroless pod (e.g. coredns)
  kubectl clone ls -n kube-system coredns-589f44dc88-4z4s6:/etc/coredns

  # 3. Use shorthand 'l'
  kubectl clone l etcd-desktop-control-plane:/etc/kubernetes/pki -n kube-system`
)

// LsOptions contains flags and configuration for 'kubectl clone ls'.
type LsOptions struct {
	Namespace  string
	Context    string
	Container  string
	UseDebug   bool
	DebugImage string
	All        bool
	Human      bool

	genericclioptions.IOStreams
}

// NewCmdCloneLs creates the 'ls' subcommand.
func NewCmdCloneLs(streams genericclioptions.IOStreams) *cobra.Command {
	o := &LsOptions{
		UseDebug:   true,
		DebugImage: k8s.DefaultDebugImage,
		All:        true,
		Human:      true,
		IOStreams:  streams,
	}

	cmd := &cobra.Command{
		Use:          "ls <pod>:[path] [flags]",
		Aliases:      []string{"l", "list"},
		Short:        "List files and directories inside a pod (supports Distroless pods)",
		Long:         `List files, permissions, and directory structures inside containers. Uses Ephemeral Debug Containers to inspect Distroless/Scratch pods without native shells or 'ls' binaries.`,
		Example:      lsExample,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd.Context(), args[0])
		},
	}

	cmd.Flags().StringVar(&o.Context, "context", "", "The kubeconfig context to use")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "If present, the namespace scope for this request")
	cmd.Flags().StringVarP(&o.Container, "container", "c", "", "Container name in pod")
	cmd.Flags().BoolVar(&o.UseDebug, "use-debug", true, "Use ephemeral debug container to support Distroless/Scratch pods (defaults to true)")
	cmd.Flags().StringVar(&o.DebugImage, "debug-image", k8s.DefaultDebugImage, "Container image for ephemeral debug containers")

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

	// If no colon was provided (e.g. "kubectl clone ls my-pod"), treat entire arg as podName
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

	lsCmd := []string{"ls", "-la"}
	if o.Human {
		lsCmd = []string{"ls", "-lah"}
	}
	lsCmd = append(lsCmd, resolvedPath)

	fmt.Fprintf(o.Out, "Listing: %s:%s\n\n", podName, targetPath)
	return k8s.Exec(ctx, bundle, podName, activeContainer, lsCmd, nil, o.Out, o.ErrOut)
}
