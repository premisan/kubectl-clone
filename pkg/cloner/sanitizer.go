package cloner

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SanitizerOptions configures how resources are cleaned during cloning.
type SanitizerOptions struct {
	// ClearNodePort removes nodePort from Service specs to avoid port collision.
	ClearNodePort bool
	// PreservePVCVolume keeps spec.volumeName on PersistentVolumeClaims (default is false).
	PreservePVCVolume bool
	// KeepAnnotations prevents deleting system annotations if true.
	KeepAnnotations bool
}

// DefaultSanitizerOptions returns standard sanitization options.
func DefaultSanitizerOptions() SanitizerOptions {
	return SanitizerOptions{
		ClearNodePort:     true,
		PreservePVCVolume: false,
		KeepAnnotations:   false,
	}
}

// System annotations to strip during cloning
var systemAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"deployment.kubernetes.io/revision",
	"pv.kubernetes.io/bind-completed",
	"pv.kubernetes.io/bound-by-controller",
	"volume.beta.kubernetes.io/storage-provisioner",
	"volume.kubernetes.io/selected-node",
	"volume.kubernetes.io/storage-provisioner",
	"control-plane.alpha.kubernetes.io/leader",
}

// Sanitize cleans up an unstructured Kubernetes resource so it can be safely created elsewhere.
func Sanitize(obj *unstructured.Unstructured, targetNamespace string, targetName string, isNamespaced bool, opts SanitizerOptions) (*unstructured.Unstructured, error) {
	cloned := obj.DeepCopy()

	// 1. Remove status field completely
	unstructured.RemoveNestedField(cloned.Object, "status")

	// 2. Adjust target Name and Namespace
	if targetName != "" {
		cloned.SetName(targetName)
	}
	if isNamespaced {
		cloned.SetNamespace(targetNamespace)
	} else {
		cloned.SetNamespace("")
	}

	// 3. Remove server-generated metadata
	metadata, ok := cloned.Object["metadata"].(map[string]interface{})
	if ok {
		delete(metadata, "uid")
		delete(metadata, "resourceVersion")
		delete(metadata, "generation")
		delete(metadata, "creationTimestamp")
		delete(metadata, "managedFields")
		delete(metadata, "ownerReferences")
		delete(metadata, "selfLink")
		delete(metadata, "generateName")
	}

	// 4. Sanitize annotations
	if !opts.KeepAnnotations {
		annotations := cloned.GetAnnotations()
		if annotations != nil {
			for _, key := range systemAnnotations {
				delete(annotations, key)
			}
			if len(annotations) == 0 {
				cloned.SetAnnotations(nil)
			} else {
				cloned.SetAnnotations(annotations)
			}
		}
	}

	// 5. Resource-specific adjustments
	kind := cloned.GetKind()
	apiVersion := cloned.GetAPIVersion()

	// Services: remove clusterIP (unless Headless "None") and clusterIPs
	if (apiVersion == "v1" || apiVersion == "core/v1") && kind == "Service" {
		sanitizeService(cloned, opts)
	}

	// PVC: remove volumeName
	if (apiVersion == "v1" || apiVersion == "core/v1") && kind == "PersistentVolumeClaim" {
		sanitizePVC(cloned, opts)
	}

	// ServiceAccount: remove auto-generated secret references
	if (apiVersion == "v1" || apiVersion == "core/v1") && kind == "ServiceAccount" {
		unstructured.RemoveNestedField(cloned.Object, "secrets")
	}

	// Pod: clear nodeName and hostIP if cloning standalone Pod
	if (apiVersion == "v1" || apiVersion == "core/v1") && kind == "Pod" {
		unstructured.RemoveNestedField(cloned.Object, "spec", "nodeName")
	}

	return cloned, nil
}

func sanitizeService(obj *unstructured.Unstructured, opts SanitizerOptions) {
	clusterIP, found, _ := unstructured.NestedString(obj.Object, "spec", "clusterIP")
	if found && strings.ToLower(clusterIP) != "none" {
		unstructured.RemoveNestedField(obj.Object, "spec", "clusterIP")
	}
	unstructured.RemoveNestedField(obj.Object, "spec", "clusterIPs")
	unstructured.RemoveNestedField(obj.Object, "spec", "healthCheckNodePort")

	if opts.ClearNodePort {
		ports, found, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
		if found {
			for _, p := range ports {
				if portMap, ok := p.(map[string]interface{}); ok {
					delete(portMap, "nodePort")
				}
			}
			_ = unstructured.SetNestedSlice(obj.Object, ports, "spec", "ports")
		}
	}
}

func sanitizePVC(obj *unstructured.Unstructured, opts SanitizerOptions) {
	if !opts.PreservePVCVolume {
		unstructured.RemoveNestedField(obj.Object, "spec", "volumeName")
	}
}
