package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	rootExample = `  # 1. Resource Object YAML Cloning (resource, r)
  kubectl clone resource secret my-secret --to-namespace prod
  kubectl clone r configmap coredns -n kube-system --to-name coredns-backup

  # 2. File & Directory Syncing (file, f)
  kubectl clone file my-pod:/var/log/nginx/ ./local-logs/
  kubectl clone f ./dist/ my-pod:/usr/share/nginx/html/ --delete

  # 3. PersistentVolumeClaim Volume Migration (pvc, p)
  kubectl clone pvc source-pvc dest-pvc --src-namespace dev --dest-namespace prod
  kubectl clone p source-pvc dest-pvc --src-context staging --dest-context prod --delete`
)

// NewCmdClone creates the root 'kubectl clone' command with subcommands.
func NewCmdClone(streams genericclioptions.IOStreams) *cobra.Command {
	resourceCmd := NewCmdCloneResource(streams)
	fileCmd := NewCmdCloneFile(streams)
	pvcCmd := NewCmdClonePVC(streams)

	cmd := &cobra.Command{
		Use:   "clone [resource|file|pvc] [flags]",
		Short: "Clone Kubernetes resources, files, and PVC data across namespaces and clusters",
		Long: `kubectl-clone is a multi-purpose cloning and migration engine for Kubernetes.

Available Subcommands:
  - resource (r) : Clone Kubernetes objects (Secret, ConfigMap, Deployment, CRD) with metadata sanitization
  - file     (f) : Incremental and recursive file synchronization between Pods and Local machines
  - pvc      (p) : Migrate and mirror PersistentVolumeClaim volume data across namespaces and clusters`,
		Example:      rootExample,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Register explicit subcommands only (no fallback)
	cmd.AddCommand(resourceCmd)
	cmd.AddCommand(fileCmd)
	cmd.AddCommand(pvcCmd)
	cmd.AddCommand(newCmdCompletion(cmd))

	return cmd
}
