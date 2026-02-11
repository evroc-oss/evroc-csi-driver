package framework

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// kubectl wrappers for YAML manifests and pod exec commands

// Kubectl provides a clean interface for kubectl operations
func (f *Framework) Kubectl(ctx context.Context, args ...string) (string, error) {
	// Prepend kubeconfig flag
	fullArgs := append([]string{"--kubeconfig", f.KubeconfigPath, "--insecure-skip-tls-verify"}, args...)

	cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("kubectl %s failed: %w\nStderr: %s", strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// KubectlWithInput runs kubectl with stdin input (for apply -f -)
func (f *Framework) KubectlWithInput(ctx context.Context, input string, args ...string) (string, error) {
	fullArgs := append([]string{"--kubeconfig", f.KubeconfigPath, "--insecure-skip-tls-verify"}, args...)

	cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("kubectl %s failed: %w\nStderr: %s", strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// Exec executes a command in a pod
func (f *Framework) Exec(ctx context.Context, namespace, podName string, command ...string) (string, error) {
	args := []string{"exec", "-n", namespace, podName, "--"}
	args = append(args, command...)
	return f.Kubectl(ctx, args...)
}

// Apply applies a YAML manifest (string or from file)
func (f *Framework) Apply(ctx context.Context, manifest string) error {
	_, err := f.KubectlWithInput(ctx, manifest, "apply", "-f", "-")
	return err
}
