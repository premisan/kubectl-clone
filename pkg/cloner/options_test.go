package cloner

import (
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestCloneOptionsValidate(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()

	tests := []struct {
		name      string
		opts      *CloneOptions
		expectErr bool
	}{
		{
			name: "Valid options",
			opts: func() *CloneOptions {
				o := NewCloneOptions(streams)
				o.Args = []string{"secret", "my-secret"}
				o.DryRunStrategy = "none"
				return o
			}(),
			expectErr: false,
		},
		{
			name: "Missing resource args",
			opts: func() *CloneOptions {
				o := NewCloneOptions(streams)
				o.Args = []string{}
				return o
			}(),
			expectErr: true,
		},
		{
			name: "Invalid dry run",
			opts: func() *CloneOptions {
				o := NewCloneOptions(streams)
				o.Args = []string{"secret", "my-secret"}
				o.DryRunStrategy = "invalid"
				return o
			}(),
			expectErr: true,
		},
		{
			name: "Invalid output format",
			opts: func() *CloneOptions {
				o := NewCloneOptions(streams)
				o.Args = []string{"secret", "my-secret"}
				o.OutputFormat = "xml"
				return o
			}(),
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if (err != nil) != tc.expectErr {
				t.Fatalf("expected error: %v, got: %v", tc.expectErr, err)
			}
		})
	}
}

func TestResolveTargetDefaultNamespace(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()
	o := NewCloneOptions(streams)

	// Explicit target namespace specified
	o.TargetNamespace = "custom-target"
	if ns := o.resolveTargetDefaultNamespace(); ns != "custom-target" {
		t.Errorf("expected custom-target, got %s", ns)
	}

	// No explicit target namespace, fallback to default when kubeconfig is empty
	o.TargetNamespace = ""
	if ns := o.resolveTargetDefaultNamespace(); ns != "default" {
		t.Errorf("expected default, got %s", ns)
	}
}
