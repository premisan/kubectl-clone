package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/premisan/kubectl-clone/pkg/k8s"
	"github.com/premisan/kubectl-clone/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	treeExample = `  # 1. View tree structure of a pod from root /
  kubectl clone tree my-pod

  # 2. View tree with file permissions and human-readable sizes (-p -h)
  kubectl clone tree coredns-xxxx:/etc -n kube-system -p -h

  # 3. View directories only with depth limit of 2 (-d -L 2)
  kubectl clone tree my-pod:/var -d -L 2

  # 4. Include hidden files (-a) and ignore patterns (-I "*.tmp")
  kubectl clone tree my-pod:/app -a -I "*.tmp"

  # 5. Use shorthand 't'
  kubectl clone t my-pod:/var/log -L 2`
)

// TreeOptions contains flags and configuration for 'kubectl clone tree'.
type TreeOptions struct {
	Namespace     string
	Context       string
	Container     string
	UseDebug      bool
	DebugImage    string

	// Linux tree flags
	MaxDepth      int    // -L, --depth
	DirsOnly      bool   // -d, --dirs-only
	All           bool   // -a, --all (include hidden files)
	Permissions   bool   // -p, --permissions (show file permissions)
	ShowSize      bool   // -s, --size (show file sizes)
	HumanReadable bool   // -h, --human-readable (show sizes in human format)
	IgnorePattern string // -I, --ignore (ignore pattern)
	FullPath      bool   // --full-path

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
		Short:        "Display directory tree hierarchy inside a pod (supports Linux tree flags)",
		Long:         `Visualizes file and directory hierarchy inside containers with standard Linux tree flags (-p, -h, -a, -d, -L, -I). Ephemeral Debug Containers allow deep inspection into Distroless/Scratch pods without native 'tree' or 'sh' utilities.`,
		Example:      treeExample,
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

	// Standard Linux tree flags
	cmd.Flags().IntVarP(&o.MaxDepth, "depth", "L", 3, "Max display depth of the directory tree")
	cmd.Flags().BoolVarP(&o.DirsOnly, "dirs-only", "d", false, "List directories only")
	cmd.Flags().BoolVarP(&o.All, "all", "a", false, "All files are listed (including hidden files)")
	cmd.Flags().BoolVarP(&o.Permissions, "permissions", "p", false, "Print the file type and permissions for each file")
	cmd.Flags().BoolVarP(&o.ShowSize, "size", "s", false, "Print the size in bytes of each file")
	cmd.Flags().BoolVar(&o.HumanReadable, "human-readable", false, "Print the size in a more human readable way")
	cmd.Flags().StringVarP(&o.IgnorePattern, "ignore", "I", "", "Do not list files that match the given wildcard pattern")
	cmd.Flags().BoolVar(&o.FullPath, "full-path", false, "Print the full path prefix for each file")

	_ = cmd.RegisterFlagCompletionFunc("context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

type fileEntry struct {
	relPath     string
	permissions string
	sizeStr     string
	isDir       bool
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

	// Prepare find command
	findCmd := []string{"find", resolvedPath, "-maxdepth", fmt.Sprintf("%d", o.MaxDepth)}
	if o.DirsOnly {
		findCmd = append(findCmd, "-type", "d")
	}

	// If metadata (permissions/size) requested, append exec ls -ld
	needMetadata := o.Permissions || o.ShowSize || o.HumanReadable
	if needMetadata {
		findCmd = append(findCmd, "-exec", "ls", "-ld", "{}", "+")
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

	prefixToStrip := resolvedPath
	if strings.HasSuffix(prefixToStrip, "/") && len(prefixToStrip) > 1 {
		prefixToStrip = prefixToStrip[:len(prefixToStrip)-1]
	}

	entries := make([]fileEntry, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var fullPath, perms, sz string
		var isDir bool

		if needMetadata {
			// Parse `ls -ld` output: e.g. "drwxr-xr-x 2 root root 4096 Oct 30 12:00 /etc/coredns"
			fields := strings.Fields(line)
			if len(fields) >= 9 {
				perms = fields[0]
				isDir = strings.HasPrefix(perms, "d")
				rawSizeBytes, _ := strconv.ParseInt(fields[4], 10, 64)
				if o.HumanReadable {
					sz = formatHumanSize(rawSizeBytes)
				} else {
					sz = fields[4]
				}
				fullPath = strings.Join(fields[8:], " ")
			} else {
				fullPath = line
			}
		} else {
			fullPath = line
		}

		if fullPath == resolvedPath {
			continue
		}

		rel := strings.TrimPrefix(fullPath, prefixToStrip)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}

		// Filter out hidden files if --all is not enabled
		if !o.All {
			hidden := false
			for _, part := range strings.Split(rel, "/") {
				if strings.HasPrefix(part, ".") {
					hidden = true
					break
				}
			}
			if hidden {
				continue
			}
		}

		// Filter out ignore pattern
		if o.IgnorePattern != "" {
			base := path.Base(rel)
			if matched, _ := filepath.Match(o.IgnorePattern, base); matched {
				continue
			}
		}

		entries = append(entries, fileEntry{
			relPath:     rel,
			permissions: perms,
			sizeStr:     sz,
			isDir:       isDir,
		})
	}

	fmt.Fprintf(o.Out, "%s:%s\n", podName, targetPath)
	renderTreeWithMetadata(o.Out, entries, o.Permissions, o.ShowSize || o.HumanReadable, o.FullPath, targetPath)

	return nil
}

type treeNode struct {
	name        string
	fullPath    string
	permissions string
	sizeStr     string
	children    map[string]*treeNode
}

func renderTreeWithMetadata(w io.Writer, entries []fileEntry, showPerms, showSize, fullPath bool, basePath string) {
	root := &treeNode{name: "", children: make(map[string]*treeNode)}

	for _, entry := range entries {
		parts := strings.Split(entry.relPath, "/")
		curr := root
		for i, part := range parts {
			if _, exists := curr.children[part]; !exists {
				curr.children[part] = &treeNode{
					name:     part,
					children: make(map[string]*treeNode),
				}
			}
			curr = curr.children[part]
			if i == len(parts)-1 {
				curr.permissions = entry.permissions
				curr.sizeStr = entry.sizeStr
				if fullPath {
					curr.fullPath = path.Join(basePath, entry.relPath)
				}
			}
		}
	}

	var printChildren func(n *treeNode, prefix string)
	printChildren = func(n *treeNode, prefix string) {
		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for i, k := range keys {
			child := n.children[k]
			isLast := i == len(keys)-1
			connector := "├── "
			newPrefix := prefix + "│   "
			if isLast {
				connector = "└── "
				newPrefix = prefix + "    "
			}

			metaPrefix := ""
			if showPerms && showSize && child.permissions != "" {
				metaPrefix = fmt.Sprintf("[%s %6s] ", child.permissions, child.sizeStr)
			} else if showPerms && child.permissions != "" {
				metaPrefix = fmt.Sprintf("[%s] ", child.permissions)
			} else if showSize && child.sizeStr != "" {
				metaPrefix = fmt.Sprintf("[%6s] ", child.sizeStr)
			}

			displayName := child.name
			if fullPath && child.fullPath != "" {
				displayName = child.fullPath
			}

			fmt.Fprintf(w, "%s%s%s%s\n", prefix, connector, metaPrefix, displayName)
			printChildren(child, newPrefix)
		}
	}

	printChildren(root, "")
}

func formatHumanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(bytes)/float64(div), "KMGTPE"[exp])
}
