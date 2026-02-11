package framework

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// DeletePVC deletes a PersistentVolumeClaim and waits for it to be fully removed
func (f *Framework) DeletePVC(ctx context.Context, namespace, name string) error {
	err := f.ClientSet.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return wrapError(fmt.Sprintf("delete PVC %s/%s", namespace, name), err)
	}

	if errors.IsNotFound(err) {
		return nil
	}

	f.Logf("Deleting PVC %s/%s, waiting for removal...", namespace, name)

	// Wait for PVC to be fully deleted (up to 90 seconds - cleanup is best-effort anyway)
	deleteTimeout := 90 * time.Second
	pollInterval := 500 * time.Millisecond // Poll more frequently for faster detection
	err = wait.PollUntilContextTimeout(ctx, pollInterval, deleteTimeout, true, func(ctx context.Context) (bool, error) {
		_, err := f.ClientSet.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("PVC %s/%s not deleted after %v: %w", namespace, name, deleteTimeout, err)
	}

	f.Logf("PVC %s/%s fully deleted", namespace, name)
	return nil
}

// GetPVC gets a PVC by name
func (f *Framework) GetPVC(ctx context.Context, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	return f.ClientSet.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
}


