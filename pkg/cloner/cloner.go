package cloner

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

// Validate validates command-line arguments and configuration before execution.
func (o *CloneOptions) Validate() error {
	if len(o.Args) == 0 {
		return fmt.Errorf("resource type and name are required")
	}

	validDryRuns := map[string]bool{"none": true, "client": true, "server": true}
	if !validDryRuns[o.DryRunStrategy] {
		return fmt.Errorf("invalid dry-run strategy %q (must be 'none', 'client', or 'server')", o.DryRunStrategy)
	}

	validOutputs := map[string]bool{"": true, "yaml": true, "json": true}
	if !validOutputs[o.OutputFormat] {
		return fmt.Errorf("invalid output format %q (must be 'yaml' or 'json')", o.OutputFormat)
	}

	return nil
}

// Run executes the complete cloning workflow.
func (o *CloneOptions) Run(ctx context.Context) error {
	// 1. Resolve source namespace
	sourceNamespace, _, err := o.ConfigFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil || sourceNamespace == "" {
		sourceNamespace = "default"
	}

	// 2. Build resource visitor with full kubectl compatibility (shortnames, kinds, plurals, CRDs)
	builder := resource.NewBuilder(o.ConfigFlags).
		Unstructured().
		NamespaceParam(sourceNamespace).DefaultNamespace().
		ResourceTypeOrNameArgs(true, o.Args...).
		SingleResourceType().
		Latest()

	result := builder.Do()
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to resolve source resource: %w", err)
	}

	infos, err := result.Infos()
	if err != nil {
		return fmt.Errorf("failed to retrieve source resource info: %w", err)
	}
	if len(infos) == 0 {
		return fmt.Errorf("no resources found matching %v", o.Args)
	}
	if len(infos) > 1 {
		return fmt.Errorf("multiple resources found, please specify a single resource to clone")
	}

	info := infos[0]
	mapping := info.Mapping
	sourceName := info.Name
	sourceNamespace = info.Namespace
	isNamespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace

	// 3. Determine target namespace and target resource name
	targetName := sourceName
	if o.ToName != "" {
		targetName = o.ToName
	}

	targetNamespace := o.TargetNamespace
	if isNamespaced {
		if targetNamespace == "" {
			targetNamespace = o.resolveTargetDefaultNamespace()
		}
	} else {
		if targetNamespace != "" {
			fmt.Fprintf(o.ErrOut, "warning: resource %s is cluster-scoped; ignoring --to-namespace\n", mapping.Resource.Resource)
			targetNamespace = ""
		}
	}

	// 4. Validate source vs target duplication
	isSameCluster := isContextAndConfigSame(o)
	if isSameCluster && targetName == sourceName {
		if !isNamespaced || (isNamespaced && targetNamespace == sourceNamespace) {
			if !o.Overwrite {
				return fmt.Errorf("source and target are identical (%s/%s). Specify --to-name, --to-namespace, or --to-context to clone",
					mapping.Resource.Resource, sourceName)
			}
		}
	}

	// 5. Source object
	sourceObj, ok := info.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("failed to convert object to unstructured")
	}

	// 6. Sanitize resource for target destination
	opts := SanitizerOptions{
		ClearNodePort:     o.ClearNodePort,
		PreservePVCVolume: o.PreservePVCVolume,
		KeepAnnotations:   o.KeepAnnotations,
	}
	clonedObj, err := Sanitize(sourceObj, targetNamespace, targetName, isNamespaced, opts)
	if err != nil {
		return fmt.Errorf("failed to sanitize resource: %w", err)
	}

	// 7. Client-side Dry-Run Handling
	if o.DryRunStrategy == "client" {
		return o.printResult(clonedObj, mapping.Resource.Resource, sourceNamespace, targetNamespace, sourceName, targetName, true)
	}

	// 8. Find target RESTMapping on target cluster
	targetMapping, err := o.TargetMapper.RESTMapping(mapping.GroupVersionKind.GroupKind(), mapping.GroupVersionKind.Version)
	if err != nil {
		// Fallback to source mapping if target mapping lookup fails
		targetMapping = mapping
	}

	var targetResourceClient dynamic.ResourceInterface
	if isNamespaced {
		targetResourceClient = o.TargetDynamic.Resource(targetMapping.Resource).Namespace(targetNamespace)
	} else {
		targetResourceClient = o.TargetDynamic.Resource(targetMapping.Resource)
	}

	// 9. Execute Create or Update on Target
	createOpts := metav1.CreateOptions{}
	if o.DryRunStrategy == "server" {
		createOpts.DryRun = []string{metav1.DryRunAll}
	}

	createdObj, err := targetResourceClient.Create(ctx, clonedObj, createOpts)
	if err != nil {
		if errors.IsAlreadyExists(err) && o.Overwrite {
			// Resource already exists and --overwrite specified; update existing
			existing, getErr := targetResourceClient.Get(ctx, targetName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("resource exists but failed to fetch for overwrite: %w", getErr)
			}
			clonedObj.SetResourceVersion(existing.GetResourceVersion())

			updateOpts := metav1.UpdateOptions{}
			if o.DryRunStrategy == "server" {
				updateOpts.DryRun = []string{metav1.DryRunAll}
			}

			createdObj, err = targetResourceClient.Update(ctx, clonedObj, updateOpts)
			if err != nil {
				return fmt.Errorf("failed to overwrite target resource: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create target resource: %w", err)
		}
	}

	return o.printResult(createdObj, targetMapping.Resource.Resource, sourceNamespace, targetNamespace, sourceName, targetName, false)
}

func (o *CloneOptions) printResult(obj *unstructured.Unstructured, resourceKind, srcNs, tgtNs, srcName, tgtName string, isDryRun bool) error {
	if o.OutputFormat == "yaml" {
		data, err := yaml.Marshal(obj.Object)
		if err != nil {
			return err
		}
		fmt.Fprintln(o.Out, string(data))
		return nil
	} else if o.OutputFormat == "json" {
		data, err := json.MarshalIndent(obj.Object, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(o.Out, string(data))
		return nil
	}

	dryRunPrefix := ""
	if isDryRun || o.DryRunStrategy == "server" {
		dryRunPrefix = fmt.Sprintf(" (dry-run: %s)", o.DryRunStrategy)
	}

	var destination string
	if tgtNs != "" {
		if o.TargetContext != "" {
			destination = fmt.Sprintf("namespace %q in cluster context %q", tgtNs, o.TargetContext)
		} else {
			destination = fmt.Sprintf("namespace %q", tgtNs)
		}
	} else {
		if o.TargetContext != "" {
			destination = fmt.Sprintf("cluster context %q", o.TargetContext)
		} else {
			destination = "current cluster"
		}
	}

	fmt.Fprintf(o.Out, "%s/%s cloned successfully to %s as %q%s\n",
		resourceKind, srcName, destination, tgtName, dryRunPrefix)
	return nil
}

func isContextAndConfigSame(o *CloneOptions) bool {
	// Check kubeconfig path
	srcKubeconfig := ""
	if o.ConfigFlags.KubeConfig != nil {
		srcKubeconfig = *o.ConfigFlags.KubeConfig
	}
	if o.TargetKubeconfig != "" && o.TargetKubeconfig != srcKubeconfig {
		return false
	}

	// Check context
	srcContext := ""
	if o.ConfigFlags.Context != nil {
		srcContext = *o.ConfigFlags.Context
	}
	if o.TargetContext != "" && o.TargetContext != srcContext {
		return false
	}

	return true
}

// resolveTargetDefaultNamespace returns the active default namespace of the destination cluster/context.
func (o *CloneOptions) resolveTargetDefaultNamespace() string {
	if o.TargetNamespace != "" {
		return o.TargetNamespace
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if o.TargetKubeconfig != "" {
		loadingRules.ExplicitPath = o.TargetKubeconfig
	} else if o.ConfigFlags.KubeConfig != nil && *o.ConfigFlags.KubeConfig != "" {
		loadingRules.ExplicitPath = *o.ConfigFlags.KubeConfig
	}

	overrides := &clientcmd.ConfigOverrides{}
	if o.TargetContext != "" {
		overrides.CurrentContext = o.TargetContext
	} else if o.ConfigFlags.Context != nil && *o.ConfigFlags.Context != "" {
		overrides.CurrentContext = *o.ConfigFlags.Context
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return "default"
	}

	targetCtxName := overrides.CurrentContext
	if targetCtxName == "" {
		targetCtxName = rawConfig.CurrentContext
	}

	if ctx, exists := rawConfig.Contexts[targetCtxName]; exists && ctx != nil && ctx.Namespace != "" {
		return ctx.Namespace
	}

	return "default"
}
