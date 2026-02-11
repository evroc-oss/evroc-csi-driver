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

// DeletePod deletes a pod and waits for it to be fully removed
func (f *Framework) DeletePod(ctx context.Context, namespace, name string) error {
	// Force delete with zero grace period for faster cleanup
	gracePeriod := int64(0)
	err := f.ClientSet.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil && !errors.IsNotFound(err) {
		return wrapError(fmt.Sprintf("delete pod %s/%s", namespace, name), err)
	}

	if errors.IsNotFound(err) {
		return nil
	}

	f.Logf("Deleting pod %s/%s (force), waiting for removal...", namespace, name)

	// Wait for pod to be fully deleted (up to 30 seconds with force delete)
	deleteTimeout := 30 * time.Second
	pollInterval := 500 * time.Millisecond // Poll more frequently for faster detection
	err = wait.PollUntilContextTimeout(ctx, pollInterval, deleteTimeout, true, func(ctx context.Context) (bool, error) {
		_, err := f.ClientSet.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("pod %s/%s not deleted after %v: %w", namespace, name, deleteTimeout, err)
	}

	f.Logf("Pod %s/%s fully deleted", namespace, name)
	return nil
}

// GetPod gets a pod by name
func (f *Framework) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	return f.ClientSet.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}

// WaitForPodReady waits for a pod to be ready
func (f *Framework) WaitForPodReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	f.Logf("Waiting for pod %s/%s to be ready (timeout: %v)", namespace, name, timeout)

	return wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := f.GetPod(ctx, namespace, name)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		return isPodReady(pod), nil
	})
}

// WaitForPodDeleted waits for a pod to be deleted
func (f *Framework) WaitForPodDeleted(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := f.GetPod(ctx, namespace, name)
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
