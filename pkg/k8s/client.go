package k8s

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientBundle wraps all common Kubernetes clients and context info for a cluster.
type ClientBundle struct {
	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	RESTConfig    *rest.Config
	RESTMapper    meta.RESTMapper
	Namespace     string
	ContextName   string
}

// NewClientBundle creates a ClientBundle for a specific kubeconfig path, context, and namespace.
func NewClientBundle(kubeconfigPath, contextName, namespaceOverride string) (*ClientBundle, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	if namespaceOverride != "" {
		overrides.Context.Namespace = namespaceOverride
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}

	// Determine resolved namespace
	resolvedNamespace, _, err := clientConfig.Namespace()
	if err != nil || resolvedNamespace == "" {
		resolvedNamespace = "default"
	}
	if namespaceOverride != "" {
		resolvedNamespace = namespaceOverride
	}

	// Determine resolved context
	rawConfig, err := clientConfig.RawConfig()
	resolvedContext := contextName
	if resolvedContext == "" && err == nil {
		resolvedContext = rawConfig.CurrentContext
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	discoClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	cachedDisco := memory.NewMemCacheClient(discoClient)
	deferredMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisco)
	shortcutMapper := restmapper.NewShortcutExpander(deferredMapper, discoClient, nil)

	return &ClientBundle{
		Clientset:     clientset,
		DynamicClient: dynClient,
		RESTConfig:    restConfig,
		RESTMapper:    shortcutMapper,
		Namespace:     resolvedNamespace,
		ContextName:   resolvedContext,
	}, nil
}
