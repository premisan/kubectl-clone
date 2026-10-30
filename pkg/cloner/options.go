package cloner

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// CloneOptions contains all user configurations and clients for the clone operation.
type CloneOptions struct {
	// Source configuration
	ConfigFlags *genericclioptions.ConfigFlags

	// Target flags
	TargetContext    string
	TargetNamespace  string
	TargetKubeconfig string
	ToName           string

	// Operation flags
	Overwrite         bool
	DryRunStrategy    string // "none", "client", "server"
	OutputFormat      string // "", "yaml", "json"
	ClearNodePort     bool
	PreservePVCVolume bool
	KeepAnnotations   bool

	// Positional arguments
	Args []string

	// IOStreams
	genericclioptions.IOStreams

	// Cached runtime clients
	TargetDynamic dynamic.Interface
	TargetMapper  meta.RESTMapper
	SourceRestCfg *rest.Config
	TargetRestCfg *rest.Config
}

// NewCloneOptions creates a default CloneOptions instance.
func NewCloneOptions(streams genericclioptions.IOStreams) *CloneOptions {
	return &CloneOptions{
		ConfigFlags:       genericclioptions.NewConfigFlags(true),
		DryRunStrategy:    "none",
		ClearNodePort:     true,
		PreservePVCVolume: false,
		IOStreams:         streams,
	}
}

// Complete initializes clients, configs, and REST mappers for source and target.
func (o *CloneOptions) Complete(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("resource type and name must be specified (e.g., 'kubectl clone cm coredns')")
	}
	o.Args = args

	// 1. Build Source RESTConfig
	var err error
	o.SourceRestCfg, err = o.ConfigFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build source kubeconfig: %w", err)
	}

	// 2. Build Target RESTConfig and clients
	o.TargetRestCfg, err = o.buildTargetRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build target kubeconfig: %w", err)
	}

	o.TargetDynamic, err = dynamic.NewForConfig(o.TargetRestCfg)
	if err != nil {
		return fmt.Errorf("failed to create target dynamic client: %w", err)
	}

	targetDiscoClient, err := discovery.NewDiscoveryClientForConfig(o.TargetRestCfg)
	if err != nil {
		return fmt.Errorf("failed to create target discovery client: %w", err)
	}
	cachedDiscoClient := memory.NewMemCacheClient(targetDiscoClient)
	targetDeferredMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoClient)
	o.TargetMapper = restmapper.NewShortcutExpander(targetDeferredMapper, targetDiscoClient, nil)

	return nil
}

// buildTargetRESTConfig creates a RESTConfig for the target cluster.
func (o *CloneOptions) buildTargetRESTConfig() (*rest.Config, error) {
	// If no custom target kubeconfig/context specified, clone source config
	if o.TargetKubeconfig == "" && o.TargetContext == "" {
		return rest.CopyConfig(o.SourceRestCfg), nil
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
	return clientConfig.ClientConfig()
}
