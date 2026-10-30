package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	DefaultRsyncImage = "alpine:latest"
	MigrationLabelKey = "app.kubernetes.io/managed-by"
	MigrationLabelVal = "kubectl-clone"
)

// FindActivePodMountingPVC searches for any running pod currently mounting the given PVC,
// returning the pod name and the node name where it is scheduled.
func FindActivePodMountingPVC(ctx context.Context, bundle *ClientBundle, pvcName string) (podName, nodeName string, err error) {
	podList, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", "", err
	}

	for _, pod := range podList.Items {
		// Ignore terminating pods or non-running pods
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		// Ignore our own migration workers
		if pod.Labels[MigrationLabelKey] == MigrationLabelVal {
			continue
		}

		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvcName {
				return pod.Name, pod.Spec.NodeName, nil
			}
		}
	}

	return "", "", nil
}

// SpawnMigrationPod creates an ephemeral migration pod mounting a single PVC.
// If nodeName is specified, the pod is co-located on that node (e.g. for live RWO PVC replication).
func SpawnMigrationPod(ctx context.Context, bundle *ClientBundle, podName, pvcName, mountPath, image, nodeName string, readOnly bool) (*corev1.Pod, error) {
	if image == "" {
		image = DefaultRsyncImage
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: bundle.Namespace,
			Labels: map[string]string{
				MigrationLabelKey: MigrationLabelVal,
				"kubectl-clone/role": "migration-worker",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "rsync-worker",
					Image:   image,
					Command: []string{"sh", "-c", "trap 'exit 0' SIGTERM; while true; do sleep 3600; done"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pvc-volume",
							MountPath: mountPath,
							ReadOnly:  readOnly,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "pvc-volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	created, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create migration pod %s in namespace %s: %w", podName, bundle.Namespace, err)
	}

	return created, nil
}

// SpawnDirectDualPVCPod creates a single pod mounting both source and destination PVCs for high-speed local copying.
// If nodeName is specified, the pod is scheduled on that specific node to co-locate with live source workloads.
func SpawnDirectDualPVCPod(ctx context.Context, bundle *ClientBundle, podName, srcPVC, destPVC, image, nodeName string) (*corev1.Pod, error) {
	if image == "" {
		image = DefaultRsyncImage
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: bundle.Namespace,
			Labels: map[string]string{
				MigrationLabelKey: MigrationLabelVal,
				"kubectl-clone/role": "direct-worker",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "direct-worker",
					Image:   image,
					Command: []string{"sh", "-c", "trap 'exit 0' SIGTERM; while true; do sleep 3600; done"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "src-vol",
							MountPath: "/src",
							ReadOnly:  true,
						},
						{
							Name:      "dest-vol",
							MountPath: "/dest",
							ReadOnly:  false,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "src-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: srcPVC,
						},
					},
				},
				{
					Name: "dest-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: destPVC,
						},
					},
				},
			},
		},
	}

	created, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create direct worker pod %s: %w", podName, err)
	}

	return created, nil
}

// WaitForPodRunning blocks until the pod enters the Running phase or timeout occurs.
func WaitForPodRunning(ctx context.Context, bundle *ClientBundle, podName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := bundle.Clientset.CoreV1().Pods(bundle.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodUnknown:
			return false, fmt.Errorf("pod entered %s phase", pod.Status.Phase)
		default:
			return false, nil
		}
	})
}

// DeletePod deletes the specified ephemeral pod.
func DeletePod(ctx context.Context, bundle *ClientBundle, podName string) error {
	gracePeriod := int64(0)
	deletePolicy := metav1.DeletePropagationForeground
	return bundle.Clientset.CoreV1().Pods(bundle.Namespace).Delete(ctx, podName, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &deletePolicy,
	})
}
