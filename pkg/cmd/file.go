package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/premisan/kubectl-clone/pkg/k8s"
	"github.com/premisan/kubectl-clone/pkg/rsync"
	"github.com/premisan/kubectl-clone/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	fileExample = `  # 1. Pod -> Local (seamlessly works on Distroless pods like coredns/etcd via Debug Container)
  kubectl clone file my-pod:/var/log/nginx/ ./local-logs/

  # 2. Local -> Pod (incremental diff sync with --delete)
  kubectl clone file ./dist/ my-pod:/usr/share/nginx/html/ --delete

  # 3. Pod -> Pod across namespaces / clusters
  kubectl clone file \
    --src-context cluster-dev --src-namespace dev pod-a:/app/data/ \
    --dest-context cluster-prod --dest-namespace prod pod-b:/app/data/

  # 4. Force or disable ephemeral debug container injection
  kubectl clone file coredns-xxxx:/etc/coredns/Corefile ./Corefile --use-debug=true`
)

// FileOptions contains flags and clients for 'kubectl clone file'.
type FileOptions struct {
	SrcContext    string
	DestContext   string
	SrcNamespace  string
	DestNamespace string
	Container     string
	Delete        bool
	Compress      bool
	DryRun        bool
	UseDebug      bool
	DebugImage    string

	genericclioptions.IOStreams
}

// NewCmdCloneFile creates the 'file' subcommand.
func NewCmdCloneFile(streams genericclioptions.IOStreams) *cobra.Command {
	o := &FileOptions{
		Compress:   true,
		UseDebug:   true, // Actively leverage debug containers by default for universal compatibility
		DebugImage: k8s.DefaultDebugImage,
		IOStreams:  streams,
	}

	cmd := &cobra.Command{
		Use:          "file <source> <destination> [flags]",
		Aliases:      []string{"f", "cp", "sync"},
		Short:        "Sync files and directories between Pod and Local, or Pod and Pod",
		Long:         `Incremental and recursive file copying using rsync over Kubernetes exec streams. Actively utilizes Ephemeral Debug Containers to access Distroless/Scratch pods without shells or utils.`,
		Example:      fileExample,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd.Context(), args[0], args[1])
		},
	}

	cmd.Flags().StringVar(&o.SrcContext, "src-context", "", "Source kubeconfig context")
	cmd.Flags().StringVar(&o.DestContext, "dest-context", "", "Destination kubeconfig context")
	cmd.Flags().StringVarP(&o.SrcNamespace, "src-namespace", "n", "", "Source namespace (defaults to active context namespace)")
	cmd.Flags().StringVar(&o.DestNamespace, "dest-namespace", "", "Destination namespace (defaults to active context namespace)")
	cmd.Flags().StringVarP(&o.Container, "container", "c", "", "Target container name inside pod")
	cmd.Flags().BoolVar(&o.Delete, "delete", false, "Delete extraneous files from destination")
	cmd.Flags().BoolVarP(&o.Compress, "compress", "z", true, "Compress file data during transfer")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Perform a trial run with no changes made")
	cmd.Flags().BoolVar(&o.UseDebug, "use-debug", true, "Actively use ephemeral debug containers to support Distroless/Scratch pods (defaults to true)")
	cmd.Flags().StringVar(&o.DebugImage, "debug-image", k8s.DefaultDebugImage, "Container image for ephemeral debug containers")

	_ = cmd.RegisterFlagCompletionFunc("src-context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("dest-context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

type location struct {
	IsPod   bool
	PodName string
	Path    string
}

func parseLocation(loc string) location {
	idx := strings.Index(loc, ":")
	if idx != -1 {
		return location{
			IsPod:   true,
			PodName: loc[:idx],
			Path:    loc[idx+1:],
		}
	}
	return location{
		IsPod: false,
		Path:  loc,
	}
}

// Run executes the file sync operation.
func (o *FileOptions) Run(ctx context.Context, srcArg, destArg string) error {
	srcLoc := parseLocation(srcArg)
	destLoc := parseLocation(destArg)

	if !srcLoc.IsPod && !destLoc.IsPod {
		return fmt.Errorf("at least one of source or destination must be a pod (e.g. 'pod-name:/path')")
	}

	rsyncOpts := rsync.Options{
		Delete:   o.Delete,
		Compress: o.Compress,
		DryRun:   o.DryRun,
		Archive:  true,
		Verbose:  true,
	}

	// Case 1: Pod -> Pod
	if srcLoc.IsPod && destLoc.IsPod {
		return o.runPodToPod(ctx, srcLoc, destLoc, rsyncOpts)
	}

	// Case 2: Pod <-> Local
	return o.runPodAndLocal(ctx, srcLoc, destLoc, rsyncOpts)
}

// preparePodEndpoint prepares either a direct container or an ephemeral debug container for rsync.
func (o *FileOptions) preparePodEndpoint(ctx context.Context, bundle *k8s.ClientBundle, loc location) (containerName, resolvedPath string, err error) {
	if !o.UseDebug {
		// Standard container mode
		if err := rsync.EnsureRsyncInstalled(ctx, bundle, loc.PodName, o.Container); err != nil {
			return "", "", err
		}
		return o.Container, loc.Path, nil
	}

	// Debug container mode (Distroless / Scratch friendly)
	util.PrintBanner(o.Out, fmt.Sprintf("Preparing ephemeral debug container on pod %s (targeted at %s)...", loc.PodName, o.Container))
	debugContainer, err := k8s.EnsureDebugContainer(ctx, bundle, loc.PodName, o.Container, o.DebugImage)
	if err != nil {
		util.PrintBanner(o.ErrOut, fmt.Sprintf("Warning: Ephemeral container attachment failed (%v). Falling back to direct container mode.", err))
		if rsyncErr := rsync.EnsureRsyncInstalled(ctx, bundle, loc.PodName, o.Container); rsyncErr != nil {
			return "", "", rsyncErr
		}
		return o.Container, loc.Path, nil
	}

	// Ensure rsync is installed in the debug container
	if err := rsync.EnsureRsyncInstalled(ctx, bundle, loc.PodName, debugContainer); err != nil {
		return "", "", fmt.Errorf("failed to prepare rsync in debug container: %w", err)
	}

	// Find PID of target container process to access its filesystem via /proc/<PID>/root
	targetPID, err := k8s.FindTargetPID(ctx, bundle, loc.PodName, debugContainer)
	if err != nil {
		targetPID = 1
	}

	// Map path to /proc/<PID>/root/<original_path>
	cleanPath := loc.Path
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}
	resolvedPath = path.Join(fmt.Sprintf("/proc/%d/root", targetPID), cleanPath)

	// Preserve trailing slash if present
	if strings.HasSuffix(loc.Path, "/") && !strings.HasSuffix(resolvedPath, "/") {
		resolvedPath += "/"
	}

	return debugContainer, resolvedPath, nil
}

func (o *FileOptions) runPodToPod(ctx context.Context, src, dest location, opts rsync.Options) error {
	util.PrintBanner(o.Out, fmt.Sprintf("Connecting to source pod %s and destination pod %s...", src.PodName, dest.PodName))
	done := util.TrackDuration(o.Out, "File synchronization")
	defer done()

	srcBundle, err := k8s.NewClientBundle("", o.SrcContext, o.SrcNamespace)
	if err != nil {
		return fmt.Errorf("failed to create source cluster client: %w", err)
	}

	destBundle, err := k8s.NewClientBundle("", o.DestContext, o.DestNamespace)
	if err != nil {
		return fmt.Errorf("failed to create destination cluster client: %w", err)
	}

	srcContainer, srcResolvedPath, err := o.preparePodEndpoint(ctx, srcBundle, src)
	if err != nil {
		return err
	}

	destContainer, destResolvedPath, err := o.preparePodEndpoint(ctx, destBundle, dest)
	if err != nil {
		return err
	}

	srcCmd := rsync.BuildServerSenderCommand(srcResolvedPath, opts)
	destCmd := rsync.BuildServerReceiverCommand(destResolvedPath, opts)

	util.PrintBanner(o.Out, fmt.Sprintf("Streaming rsync data: %s:%s -> %s:%s", src.PodName, srcResolvedPath, dest.PodName, destResolvedPath))
	return rsync.RunDualExecBridge(ctx, srcBundle, src.PodName, srcContainer, srcCmd, destBundle, dest.PodName, destContainer, destCmd, o.ErrOut)
}

func (o *FileOptions) runPodAndLocal(ctx context.Context, src, dest location, opts rsync.Options) error {
	// Verify local rsync binary is available
	_, err := exec.LookPath("rsync")
	if err != nil {
		return fmt.Errorf("local 'rsync' command not found in PATH: %w", err)
	}

	// Check kubectl command availability
	_, err = exec.LookPath("kubectl")
	if err != nil {
		return fmt.Errorf("local 'kubectl' command not found in PATH: %w", err)
	}

	done := util.TrackDuration(o.Out, "Local-Pod file transfer")
	defer done()

	var podBundle *k8s.ClientBundle
	var podLoc location
	var targetContext, targetNamespace string

	if src.IsPod {
		podLoc = src
		targetContext = o.SrcContext
		targetNamespace = o.SrcNamespace
	} else {
		podLoc = dest
		targetContext = o.DestContext
		targetNamespace = o.DestNamespace
	}

	podBundle, err = k8s.NewClientBundle("", targetContext, targetNamespace)
	if err != nil {
		return err
	}

	activeContainer, resolvedPodPath, err := o.preparePodEndpoint(ctx, podBundle, podLoc)
	if err != nil {
		return err
	}

	// Build rsync command with -e "kubectl exec -i"
	args := []string{"-av"}
	if opts.Compress {
		args = append(args, "-z")
	}
	if opts.Delete {
		args = append(args, "--delete")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	rsh := "kubectl"
	if targetContext != "" {
		rsh += fmt.Sprintf(" --context=%s", targetContext)
	}
	if targetNamespace != "" {
		rsh += fmt.Sprintf(" -n %s", targetNamespace)
	}
	if activeContainer != "" {
		rsh += fmt.Sprintf(" -c %s", activeContainer)
	}
	rsh += " exec -i"

	args = append(args, "-e", rsh)

	var srcPath, destPath string
	if src.IsPod {
		srcPath = fmt.Sprintf("%s:%s", src.PodName, resolvedPodPath)
		destPath = dest.Path
	} else {
		srcPath = src.Path
		destPath = fmt.Sprintf("%s:%s", dest.PodName, resolvedPodPath)
	}

	args = append(args, "--blocking-io", srcPath, destPath)

	util.PrintBanner(o.Out, fmt.Sprintf("Running rsync: %s -> %s", srcPath, destPath))
	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmd.Stdout = o.Out
	cmd.Stderr = o.ErrOut
	cmd.Stdin = os.Stdin

	return cmd.Run()
}
