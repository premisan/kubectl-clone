package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/premisan/kubectl-clone/pkg/k8s"
	"github.com/premisan/kubectl-clone/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	treeExample = `  # 1. View tree structure of a pod from root /
  kubectl clone tree my-pod

  # 2. View tree of /etc in coredns with depth limit of 2
  kubectl clone tree coredns-xxxx:/etc -n kube-system -L 2

  # 3. View directories only
  kubectl clone tree my-pod:/app -d

  # 4. Use shorthand 't'
  kubectl clone t my-pod:/var/log`
)

// TreeOptions contains flags and configuration for 'kubectl clone tree'.
type TreeOptions struct {
	Namespace  string
	Context    string
	Container  string
	UseDebug   bool
	DebugImage string
	MaxDepth   int
	DirsOnly   bool

	genericclioptions.IOStreams
}

// NewCmdCloneTree creates the 'tree' subcommand.
func NewCmdCloneTree(streams genericclioptions.IOStreams) *cobra.Command {
	o := &TreeOptions{
		UseDebug:   true,
		DebugImage: k8s.DefaultDebugImage,
		MaxDepth:   3,
		IOStreams:  streams,
	}

	cmd := &cobra.Command{
		Use:          "tree <pod>:[path] [flags]",
		Aliases:      []string{"t"},
		Short:        "Display directory tree hierarchy inside a pod (supports Distroless pods)",
		Long:         `Visualizes file and directory hierarchy inside containers in a tree structure. Ephemeral Debug Containers allow deep inspection into Distroless/Scratch pods without 'tree' or 'sh' utilities.`,
		Example:      treeExample,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd.Context(), args[0])
		},
	}

	cmd.Flags().StringVar(&o.Context, "context", "", "The kubeconfig context to use")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "If present, the namespace scope for this request")
	cmd.Flags().StringVarP(&o.Container, "container", "c", "", "Container name in pod")
	cmd.Flags().IntVarP(&o.MaxDepth, "depth", "L", 3, "Max display depth of the directory tree")
	cmd.Flags().BoolVarP(&o.DirsOnly, "dirs-only", "d", false, "List directories only")
	cmd.Flags().BoolVar(&o.UseDebug, "use-debug", true, "Use ephemeral debug container to support Distroless/Scratch pods (defaults to true)")
	cmd.Flags().StringVar(&o.DebugImage, "debug-image", k8s.DefaultDebugImage, "Container image for ephemeral debug containers")

	_ = cmd.RegisterFlagCompletionFunc("context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// Run executes the directory tree inspection.
func (o *TreeOptions) Run(ctx context.Context, targetArg string) error {
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

	// Build find command to list entries up to MaxDepth
	findCmd := []string{"find", resolvedPath, "-maxdepth", fmt.Sprintf("%d", o.MaxDepth)}
	if o.DirsOnly {
		findCmd = append(findCmd, "-type", "d")
	}

	var stdout, stderr bytes.Buffer
	err = k8s.Exec(ctx, bundle, podName, activeContainer, findCmd, nil, &stdout, &stderr)
	if err != nil {
		return fmt.Errorf("failed to traverse directory tree: %w", err)
	}

	rawLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(rawLines) == 0 || (len(rawLines) == 1 && rawLines[0] == "") {
		fmt.Fprintf(o.Out, "%s:%s is empty\n", podName, targetPath)
		return nil
	}

	// Clean paths by stripping the /proc/<PID>/root prefix for user display
	prefixToStrip := resolvedPath
	if strings.HasSuffix(prefixToStrip, "/") && len(prefixToStrip) > 1 {
		prefixToStrip = prefixToStrip[:len(prefixToStrip)-1]
	}

	relativePaths := make([]string, 0, len(rawLines))
	for _, l := range rawLines {
		if l == "" || l == resolvedPath {
			continue
		}
		rel := strings.TrimPrefix(l, prefixToStrip)
		rel = strings.TrimPrefix(rel, "/")
		if rel != "" {
			relativePaths = append(relativePaths, rel)
		}
	}

	sort.Strings(relativePaths)

	// Render Ascii Tree
	fmt.Fprintf(o.Out, "%s:%s\n", podName, targetPath)
	renderTree(o.Out, relativePaths)

	return nil
}

func renderTree(w io.Writer, paths []string) {
	type node struct {
		name     string
		children map[string]*node
	}

	root := &node{name: "", children: make(map[string]*node)}

	for _, p := range paths {
		parts := strings.Split(p, "/")
		curr := root
		for _, part := range parts {
			if _, exists := curr.children[part]; !exists {
				curr.children[part] = &node{name: part, children: make(map[string]*node)}
			}
			curr = curr.children[part]
		}
	}

	var printChildren func(n *node, prefix string)
	printChildren = func(n *node, prefix string) {
		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for i, k := range keys {
			isLast := i == len(keys)-1
			connector := "├── "
			newPrefix := prefix + "│   "
			if isLast {
				connector = "└── "
				newPrefix = prefix + "    "
			}

			fmt.Fprintf(w, "%s%s%s\n", prefix, connector, k)
			printChildren(n.children[k], newPrefix)
		}
	}

	printChildren(root, "")
}
