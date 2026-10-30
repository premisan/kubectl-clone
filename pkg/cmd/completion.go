package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var defaultResourceTypes = []string{
	"configmaps", "secrets", "deployments", "services", "pods",
	"persistentvolumeclaims", "serviceaccounts", "ingresses",
	"statefulsets", "daemonsets", "jobs", "cronjobs",
	"clusterroles", "clusterrolebindings", "roles", "rolebindings",
	"cm", "deploy", "svc", "po", "pvc", "sa", "ing", "sts", "ds", "cr", "crb",
}

// completeResourceTypes provides shell autocompletion for Kubernetes resource types.
func completeResourceTypes(configFlags *genericclioptions.ConfigFlags, toComplete string) []string {
	results := make([]string, 0)

	// Try fetching live resource list from cluster with timeout
	disco, err := configFlags.ToDiscoveryClient()
	if err == nil {
		_, apiResourceLists, err := disco.ServerGroupsAndResources()
		if err == nil && len(apiResourceLists) > 0 {
			seen := make(map[string]bool)
			for _, rl := range apiResourceLists {
				for _, res := range rl.APIResources {
					if !strings.Contains(res.Name, "/") { // skip subresources
						if !seen[res.Name] {
							seen[res.Name] = true
							results = append(results, res.Name)
						}
						for _, sn := range res.ShortNames {
							if !seen[sn] {
								seen[sn] = true
								results = append(results, sn)
							}
						}
					}
				}
			}
			if len(results) > 0 {
				return filterPrefix(results, toComplete)
			}
		}
	}

	// Fallback to standard resource types
	return filterPrefix(defaultResourceTypes, toComplete)
}

// completeResourceNames provides autocompletion for resource names of a given type.
func completeResourceNames(configFlags *genericclioptions.ConfigFlags, resourceType, toComplete string) []string {
	restCfg, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil
	}

	mapper, err := configFlags.ToRESTMapper()
	if err != nil {
		return nil
	}

	gvr, err := mapper.ResourceFor(schema.GroupVersionResource{Resource: resourceType})
	if err != nil {
		return nil
	}

	ns, _, _ := configFlags.ToRawKubeConfigLoader().Namespace()
	if ns == "" {
		ns = "default"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	list, err := dynClient.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{Limit: 100})
	if err != nil {
		// Try cluster-scoped list
		list, err = dynClient.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 100})
		if err != nil {
			return nil
		}
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}

	return filterPrefix(names, toComplete)
}

// completeNamespaces provides autocompletion for namespaces.
func completeNamespaces(configFlags *genericclioptions.ConfigFlags, toComplete string) []string {
	restCfg, err := configFlags.ToRESTConfig()
	if err != nil {
		return []string{"default", "kube-system", "kube-public"}
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return []string{"default", "kube-system", "kube-public"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	list, err := dynClient.Resource(nsGVR).List(ctx, metav1.ListOptions{Limit: 100})
	if err != nil {
		return []string{"default", "kube-system", "kube-public"}
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}
	return filterPrefix(names, toComplete)
}

// completeContexts provides autocompletion for kubeconfig contexts.
func completeContexts(toComplete string) []string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{}).RawConfig()
	if err != nil {
		return nil
	}

	contexts := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		contexts = append(contexts, name)
	}
	return filterPrefix(contexts, toComplete)
}

func filterPrefix(items []string, prefix string) []string {
	if prefix == "" {
		return items
	}
	matches := make([]string, 0)
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			matches = append(matches, item)
		}
	}
	return matches
}

// newCmdCompletion creates the 'completion' command to output shell script.
func newCmdCompletion(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `To load completions:

Bash:
  $ source <(kubectl clone completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ kubectl clone completion bash > /etc/bash_completion.d/kubectl-clone
  # macOS:
  $ kubectl clone completion bash > $(brew --prefix)/etc/bash_completion.d/kubectl-clone

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session:
  $ kubectl clone completion zsh > "${fpath[1]}/_kubectl-clone"

Fish:
  $ kubectl clone completion fish | source

  # To load completions for each session:
  $ kubectl clone completion fish > ~/.config/fish/completions/kubectl-clone.fish

PowerShell:
  PS> kubectl clone completion powershell | Out-String | Invoke-Expression
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return rootCmd.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			}
			return nil
		},
	}
	return cmd
}
