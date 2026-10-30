package cmd

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/premisan/kubectl-clone/pkg/k8s"
	"github.com/premisan/kubectl-clone/pkg/rsync"
	"github.com/premisan/kubectl-clone/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	pvcExample = `  # 1. Clone PVC in the same namespace (auto-uses Direct strategy)
  kubectl clone pvc source-pvc dest-pvc

  # 2. Clone PVC across different namespaces
  kubectl clone pvc source-pvc dest-pvc \
    --src-namespace dev \
    --dest-namespace prod

  # 3. Clone PVC across different clusters without LoadBalancer or public IP!
  kubectl clone pvc source-pvc dest-pvc \
    --src-context eks-staging --src-namespace dev \
    --dest-context gke-prod --dest-namespace prod \
    --strategy stdio-bridge \
    --delete -z`
)

// PVCOptions contains flags and configuration for 'kubectl clone pvc'.
type PVCOptions struct {
	SrcContext    string
	DestContext   string
	SrcNamespace  string
	DestNamespace string
	SrcPath       string
	DestPath      string
	Strategy      string // auto, direct, stdio-bridge
	Delete        bool
	Compress      bool
	DryRun        bool
	Image         string

	genericclioptions.IOStreams
}

// NewCmdClonePVC creates the 'pvc' subcommand.
func NewCmdClonePVC(streams genericclioptions.IOStreams) *cobra.Command {
	o := &PVCOptions{
		SrcPath:   "/",
		DestPath:  "/",
		Strategy:  "auto",
		Compress:  true,
		Image:     k8s.DefaultRsyncImage,
		IOStreams: streams,
	}

	cmd := &cobra.Command{
		Use:          "pvc <source-pvc> <dest-pvc> [flags]",
		Aliases:      []string{"p", "volume", "pv"},
		Short:        "Clone and synchronize data between PersistentVolumeClaims",
		Long:         `Seamlessly sync PVC data across namespaces or different Kubernetes clusters without requiring public IPs, Ingress, or LoadBalancers using Dual-Exec Stdio Bridge.`,
		Example:      pvcExample,
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
	cmd.Flags().StringVar(&o.SrcPath, "src-path", "/", "Subpath within the source PVC to clone")
	cmd.Flags().StringVar(&o.DestPath, "dest-path", "/", "Subpath within destination PVC")
	cmd.Flags().StringVar(&o.Strategy, "strategy", "auto", "Replication strategy: 'auto', 'direct', or 'stdio-bridge'")
	cmd.Flags().BoolVar(&o.Delete, "delete", false, "Delete extraneous files in destination PVC (full mirroring)")
	cmd.Flags().BoolVarP(&o.Compress, "compress", "z", true, "Compress data stream during transfer")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Trial run with no changes made")
	cmd.Flags().StringVar(&o.Image, "image", k8s.DefaultRsyncImage, "Container image for ephemeral migration pods")

	_ = cmd.RegisterFlagCompletionFunc("strategy", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "direct", "stdio-bridge"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("src-context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("dest-context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// Run executes the PVC migration workflow.
func (o *PVCOptions) Run(ctx context.Context, srcPVC, destPVC string) error {
	srcBundle, err := k8s.NewClientBundle("", o.SrcContext, o.SrcNamespace)
	if err != nil {
		return fmt.Errorf("failed to init source cluster: %w", err)
	}

	destBundle, err := k8s.NewClientBundle("", o.DestContext, o.DestNamespace)
	if err != nil {
		return fmt.Errorf("failed to init destination cluster: %w", err)
	}

	// Strategy selection
	strategy := o.Strategy
	if strategy == "auto" {
		isSameCluster := (o.SrcContext == o.DestContext)
		isSameNamespace := (srcBundle.Namespace == destBundle.Namespace)
		if isSameCluster && isSameNamespace {
			strategy = "direct"
		} else {
			strategy = "stdio-bridge"
		}
	}

	rsyncOpts := rsync.Options{
		Delete:   o.Delete,
		Compress: o.Compress,
		DryRun:   o.DryRun,
		Archive:  true,
		Verbose:  true,
	}

	switch strategy {
	case "direct":
		return o.runDirect(ctx, srcBundle, srcPVC, destPVC, rsyncOpts)
	case "stdio-bridge":
		return o.runStdioBridge(ctx, srcBundle, destBundle, srcPVC, destPVC, rsyncOpts)
	default:
		return fmt.Errorf("unknown replication strategy %q (must be 'auto', 'direct', or 'stdio-bridge')", strategy)
	}
}

// runDirect spawns a single pod mounting both PVCs and performs local rsync.
func (o *PVCOptions) runDirect(ctx context.Context, bundle *k8s.ClientBundle, srcPVC, destPVC string, opts rsync.Options) error {
	util.PrintBanner(o.Out, "Using Direct strategy: mounting both PVCs in a single worker pod for local transfer")
	done := util.TrackDuration(o.Out, "Direct PVC synchronization")
	defer done()

	podName := fmt.Sprintf("kubectl-clone-direct-%d", rand.Intn(100000))
	_, err := k8s.SpawnDirectDualPVCPod(ctx, bundle, podName, srcPVC, destPVC, o.Image)
	if err != nil {
		return err
	}
	defer func() {
		util.PrintBanner(o.Out, fmt.Sprintf("Cleaning up worker pod %s...", podName))
		_ = k8s.DeletePod(context.Background(), bundle, podName)
	}()

	util.PrintBanner(o.Out, fmt.Sprintf("Waiting for worker pod %s to be ready...", podName))
	if err := k8s.WaitForPodRunning(ctx, bundle, podName, 60*time.Second); err != nil {
		return fmt.Errorf("worker pod failed to start: %w", err)
	}

	if err := rsync.EnsureRsyncInstalled(ctx, bundle, podName, "direct-worker"); err != nil {
		return err
	}

	rsyncCmd := []string{"rsync", "-av"}
	if opts.Delete {
		rsyncCmd = append(rsyncCmd, "--delete")
	}
	if opts.DryRun {
		rsyncCmd = append(rsyncCmd, "--dry-run")
	}
	rsyncCmd = append(rsyncCmd, "/src/", "/dest/")

	util.PrintBanner(o.Out, "Starting local high-speed copy /src/ -> /dest/...")
	return k8s.Exec(ctx, bundle, podName, "direct-worker", rsyncCmd, nil, o.Out, o.ErrOut)
}

// runStdioBridge spawns a pod on each cluster and bridges their rsync streams over memory pipe.
func (o *PVCOptions) runStdioBridge(ctx context.Context, srcBundle, destBundle *k8s.ClientBundle, srcPVC, destPVC string, opts rsync.Options) error {
	util.PrintBanner(o.Out, "Using Stdio-Bridge strategy: streaming bi-directional rsync over k8s API exec stream")
	done := util.TrackDuration(o.Out, "Cross-Cluster/Namespace PVC synchronization")
	defer done()

	randId := rand.Intn(100000)
	srcPodName := fmt.Sprintf("kubectl-clone-src-%d", randId)
	destPodName := fmt.Sprintf("kubectl-clone-dest-%d", randId)

	// 1. Spawn source migration pod
	util.PrintBanner(o.Out, fmt.Sprintf("Spawning source pod %s in %s...", srcPodName, srcBundle.Namespace))
	_, err := k8s.SpawnMigrationPod(ctx, srcBundle, srcPodName, srcPVC, "/data", o.Image)
	if err != nil {
		return err
	}
	defer func() {
		util.PrintBanner(o.Out, fmt.Sprintf("Cleaning up source pod %s...", srcPodName))
		_ = k8s.DeletePod(context.Background(), srcBundle, srcPodName)
	}()

	// 2. Spawn destination migration pod
	util.PrintBanner(o.Out, fmt.Sprintf("Spawning destination pod %s in %s...", destPodName, destBundle.Namespace))
	_, err = k8s.SpawnMigrationPod(ctx, destBundle, destPodName, destPVC, "/data", o.Image)
	if err != nil {
		return err
	}
	defer func() {
		util.PrintBanner(o.Out, fmt.Sprintf("Cleaning up destination pod %s...", destPodName))
		_ = k8s.DeletePod(context.Background(), destBundle, destPodName)
	}()

	// 3. Wait for both pods to be ready
	util.PrintBanner(o.Out, "Waiting for worker pods to enter Running phase...")
	if err := k8s.WaitForPodRunning(ctx, srcBundle, srcPodName, 90*time.Second); err != nil {
		return fmt.Errorf("source worker pod failed to start: %w", err)
	}
	if err := k8s.WaitForPodRunning(ctx, destBundle, destPodName, 90*time.Second); err != nil {
		return fmt.Errorf("destination worker pod failed to start: %w", err)
	}

	// 4. Ensure rsync in both pods
	if err := rsync.EnsureRsyncInstalled(ctx, srcBundle, srcPodName, "rsync-worker"); err != nil {
		return err
	}
	if err := rsync.EnsureRsyncInstalled(ctx, destBundle, destPodName, "rsync-worker"); err != nil {
		return err
	}

	// 5. Build rsync commands
	srcCmd := rsync.BuildServerSenderCommand("/data/", opts)
	destCmd := rsync.BuildServerReceiverCommand("/data/", opts)

	util.PrintBanner(o.Out, "Establishing Dual-Exec Stdio pipeline across clusters...")
	return rsync.RunDualExecBridge(ctx, srcBundle, srcPodName, "rsync-worker", srcCmd, destBundle, destPodName, "rsync-worker", destCmd, o.ErrOut)
}
