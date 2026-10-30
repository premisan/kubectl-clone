package cloner

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSanitizeService(t *testing.T) {
	svc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":              "my-svc",
				"namespace":         "source-ns",
				"resourceVersion":   "12345",
				"uid":               "uuid-1234",
				"creationTimestamp": "2026-01-01T00:00:00Z",
				"annotations": map[string]interface{}{
					"custom.company.io/flag":                     "true",
					"kubectl.kubernetes.io/last-applied-configuration": "{}",
				},
			},
			"spec": map[string]interface{}{
				"clusterIP":  "10.96.0.1",
				"clusterIPs": []interface{}{"10.96.0.1"},
				"ports": []interface{}{
					map[string]interface{}{
						"port":     int64(80),
						"nodePort": int64(30080),
					},
				},
			},
			"status": map[string]interface{}{
				"loadBalancer": map[string]interface{}{},
			},
		},
	}

	opts := DefaultSanitizerOptions()
	opts.ClearNodePort = true

	cloned, err := Sanitize(svc, "target-ns", "new-svc", true, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cloned.GetName() != "new-svc" {
		t.Errorf("expected name new-svc, got %s", cloned.GetName())
	}
	if cloned.GetNamespace() != "target-ns" {
		t.Errorf("expected namespace target-ns, got %s", cloned.GetNamespace())
	}
	if cloned.GetResourceVersion() != "" {
		t.Errorf("resourceVersion should be empty, got %s", cloned.GetResourceVersion())
	}
	if cloned.GetUID() != "" {
		t.Errorf("uid should be empty, got %s", cloned.GetUID())
	}

	// Status should be removed
	if _, found, _ := unstructured.NestedMap(cloned.Object, "status"); found {
		t.Errorf("status field should be deleted")
	}

	// clusterIP and clusterIPs should be removed
	if _, found, _ := unstructured.NestedString(cloned.Object, "spec", "clusterIP"); found {
		t.Errorf("spec.clusterIP should be removed")
	}
	if _, found, _ := unstructured.NestedSlice(cloned.Object, "spec", "clusterIPs"); found {
		t.Errorf("spec.clusterIPs should be removed")
	}

	// nodePort should be removed
	ports, _, _ := unstructured.NestedSlice(cloned.Object, "spec", "ports")
	p0 := ports[0].(map[string]interface{})
	if _, found := p0["nodePort"]; found {
		t.Errorf("nodePort should be removed from ports")
	}

	// System annotation removed, custom kept
	annos := cloned.GetAnnotations()
	if _, found := annos["kubectl.kubernetes.io/last-applied-configuration"]; found {
		t.Errorf("kubectl annotation should be removed")
	}
	if annos["custom.company.io/flag"] != "true" {
		t.Errorf("custom annotation should be preserved")
	}
}

func TestSanitizeClusterScopedResource(t *testing.T) {
	cr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name":            "admin-clone",
				"resourceVersion": "999",
			},
			"rules": []interface{}{},
		},
	}

	opts := DefaultSanitizerOptions()
	cloned, err := Sanitize(cr, "ignored-ns", "new-admin-role", false, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cloned.GetName() != "new-admin-role" {
		t.Errorf("expected new-admin-role, got %s", cloned.GetName())
	}
	if cloned.GetNamespace() != "" {
		t.Errorf("cluster-scoped resource namespace should be empty, got %s", cloned.GetNamespace())
	}
}
