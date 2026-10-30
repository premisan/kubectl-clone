package cmd

import (
	"github.com/premisan/kubectl-clone/pkg/cloner"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	resourceExample = `  # Clone a secret to another namespace
  kubectl clone resource secret my-secret --to-namespace prod

  # Clone a deployment with a new name in the same namespace
  kubectl clone resource deployment web-app --to-name web-app-v2

  # Clone a ConfigMap across clusters (to another kubeconfig context)
  kubectl clone resource configmap app-config --to-context cluster-staging --to-namespace default

  # Clone a cluster-scoped ClusterRole with a new name
  kubectl clone resource clusterrole admin-template --to-name custom-admin`
)

// NewCmdCloneResource creates the 'resource' subcommand for cloning Kubernetes API objects.
func NewCmdCloneResource(streams genericclioptions.IOStreams) *cobra.Command {
	o := cloner.NewCloneOptions(streams)

	cmd := &cobra.Command{
		Use:          "resource <TYPE> <NAME> [flags]",
		Aliases:      []string{"r", "res", "obj", "object"},
		Short:        "Clone a Kubernetes resource across namespaces, clusters, or names",
		Long:         `Clone any Kubernetes resource (including CRDs) to another namespace, cluster context, or with a new name while automatically sanitizing server-managed metadata and status fields.`,
		Example:      resourceExample,
		SilenceUsage: true,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeResourceTypes(o.ConfigFlags, toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			if len(args) == 1 {
				return completeResourceNames(o.ConfigFlags, args[0], toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if err := o.Complete(args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	// Target destination flags
	cmd.Flags().StringVar(&o.TargetNamespace, "to-namespace", o.TargetNamespace, "The destination namespace to clone the resource into (defaults to current context's default namespace)")
	cmd.Flags().StringVar(&o.TargetContext, "to-context", o.TargetContext, "The destination kubeconfig context to clone the resource into (defaults to current context)")
	cmd.Flags().StringVar(&o.TargetKubeconfig, "to-kubeconfig", o.TargetKubeconfig, "Path to the destination kubeconfig file (defaults to source kubeconfig)")
	cmd.Flags().StringVar(&o.ToName, "to-name", o.ToName, "The destination name for the cloned resource (defaults to source resource name)")

	// Backward compatibility alias for --new-name
	cmd.Flags().StringVar(&o.ToName, "new-name", o.ToName, "Deprecated: use --to-name")
	_ = cmd.Flags().MarkDeprecated("new-name", "please use --to-name instead")

	// Execution flags
	cmd.Flags().BoolVar(&o.Overwrite, "overwrite", o.Overwrite, "Overwrite the target resource if it already exists")
	cmd.Flags().StringVar(&o.DryRunStrategy, "dry-run", o.DryRunStrategy, "Must be 'none', 'client', or 'server'")
	cmd.Flags().StringVarP(&o.OutputFormat, "output", "o", o.OutputFormat, "Output format. One of: yaml|json")

	// Sanitization tuning flags
	cmd.Flags().BoolVar(&o.ClearNodePort, "clear-nodeport", true, "Clear spec.ports[*].nodePort on Services to prevent port collision")
	cmd.Flags().BoolVar(&o.PreservePVCVolume, "preserve-pvc-volume", false, "Preserve spec.volumeName on PersistentVolumeClaims (default removes it to unbind PV)")
	cmd.Flags().BoolVar(&o.KeepAnnotations, "keep-annotations", false, "Preserve server-managed system annotations")

	// Bind standard kubectl source flags (--context, --namespace, --kubeconfig, etc.)
	o.ConfigFlags.AddFlags(cmd.Flags())

	// Dynamic shell completion for flags
	_ = cmd.RegisterFlagCompletionFunc("to-namespace", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeNamespaces(o.ConfigFlags, toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("namespace", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeNamespaces(o.ConfigFlags, toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("to-context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("context", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeContexts(toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("dry-run", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"none", "client", "server"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "json"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}
