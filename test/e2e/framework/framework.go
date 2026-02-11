package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultPollInterval = 200 * time.Millisecond
)

// Framework provides utilities for e2e testing
type Framework struct {
	T              *testing.T
	ClientSet      kubernetes.Interface
	Namespace      string
	KubeconfigPath string
	CleanupFuncs   []func() error
}

// NewFramework creates a new test framework
func NewFramework(t *testing.T, namespace string) *Framework {
	config, kubeconfigPath := loadKubeConfig(t)

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create kubernetes client: %v", err)
	}

	// Create namespace if it doesn't exist
	if namespace != "" && namespace != "default" {
		createNamespace(t, clientSet, namespace)
	}

	return &Framework{
		T:              t,
		ClientSet:      clientSet,
		Namespace:      namespace,
		KubeconfigPath: kubeconfigPath,
		CleanupFuncs:   []func() error{},
	}
}

// loadKubeConfig loads kubeconfig for the test cluster
func loadKubeConfig(t *testing.T) (*rest.Config, string) {
	// Try KUBECONFIG env var first
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		// Fall back to default location
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Failed to get home directory: %v", err)
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		t.Fatalf("Failed to load kubeconfig from %s: %v", kubeconfigPath, err)
	}

	// Skip TLS verification for test clusters
	config.Insecure = true
	config.CAData = nil
	config.CAFile = ""

	// Increase rate limits for test scenarios
	// Default is 5 QPS and 10 burst which is too low for polling tests
	config.QPS = 50
	config.Burst = 100

	return config, kubeconfigPath
}

// createNamespace creates a namespace if it doesn't exist
func createNamespace(t *testing.T, clientSet kubernetes.Interface, namespace string) {
	ctx := context.Background()

	_, err := clientSet.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		t.Logf("Namespace %s already exists", namespace)
		return
	}

	if !errors.IsNotFound(err) {
		t.Fatalf("Failed to check namespace: %v", err)
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err = clientSet.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace %s: %v", namespace, err)
	}

	t.Logf("Created namespace %s", namespace)
}

// AddCleanup adds a cleanup function to be called when the test completes
func (f *Framework) AddCleanup(fn func() error) {
	f.CleanupFuncs = append(f.CleanupFuncs, fn)
}

// Cleanup runs all registered cleanup functions
// Uses a fresh context with timeout to avoid issues with cancelled test contexts
func (f *Framework) Cleanup() {
	f.T.Log("Running cleanup...")
	for i := len(f.CleanupFuncs) - 1; i >= 0; i-- {
		if err := f.CleanupFuncs[i](); err != nil {
			f.T.Logf("Cleanup error: %v", err)
		}
	}
	f.T.Log("Cleanup completed")
}

// wrapError wraps an error with context about the operation
func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// AddPodCleanup adds a cleanup function to delete a pod
// Uses a fresh background context to avoid issues with cancelled test contexts
// Best-effort: logs warning if deletion times out but doesn't fail
func (f *Framework) AddPodCleanup(namespace, name string) {
	f.AddCleanup(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		if err := f.DeletePod(ctx, namespace, name); err != nil {
			f.T.Logf("Warning: Pod cleanup timed out (non-fatal): %v", err)
			// Return nil to make cleanup best-effort - don't fail test on cleanup timeout
			return nil
		}
		return nil
	})
}

// AddPVCCleanup adds a cleanup function to delete a PVC
// Uses a fresh background context to avoid issues with cancelled test contexts
// Best-effort: logs warning if deletion times out but doesn't fail
func (f *Framework) AddPVCCleanup(namespace, name string) {
	f.AddCleanup(func() error {
		// 2-minute timeout for PVC cleanup (best-effort, so won't block test)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := f.DeletePVC(ctx, namespace, name); err != nil {
			f.T.Logf("Warning: PVC cleanup timed out (non-fatal): %v", err)
			// Return nil to make cleanup best-effort - don't fail test on cleanup timeout
			return nil
		}
		return nil
	})
}

// Logf logs a formatted message
func (f *Framework) Logf(format string, args ...interface{}) {
	f.T.Logf(format, args...)
}

// GetNodes gets all nodes in the cluster
func (f *Framework) GetNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList, err := f.ClientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

// GetNode gets a specific node by name
func (f *Framework) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	return f.ClientSet.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
}
