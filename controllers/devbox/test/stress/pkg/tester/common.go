package tester

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	"github.com/openebs/lvm-localpv/pkg/lvm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DevboxCreateSpec specification for creating Devbox
type DevboxCreateSpec struct {
	Name         string            // Devbox name
	Namespace    string            // Namespace
	Labels       map[string]string // Labels
	Image        string            // Image
	CPU          string            // CPU resource
	Memory       string            // Memory resource
	StorageLimit string            // Storage limit
}

// DevboxCommonHelper common utility helper for Devbox
// All shared methods across testers are placed here
type DevboxCommonHelper struct {
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	restConfig *rest.Config
}

// NewDevboxCommonHelper creates a new common helper utility
func NewDevboxCommonHelper(k8sClient kubernetes.Interface, ctrlClient client.Client, restConfig *rest.Config) *DevboxCommonHelper {
	return &DevboxCommonHelper{
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		restConfig: restConfig,
	}
}

// ==================== Devbox Creation Methods ====================

// GenerateDevbox generates a Devbox object (without creating it)
func (h *DevboxCommonHelper) GenerateDevbox(spec DevboxCreateSpec) *devboxv1alpha2.Devbox {
	return &devboxv1alpha2.Devbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: devboxv1alpha2.DevboxSpec{
			State: devboxv1alpha2.DevboxStateRunning,
			Image: spec.Image,
			Resource: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(spec.CPU),
				corev1.ResourceMemory: resource.MustParse(spec.Memory),
			},
			Config: devboxv1alpha2.Config{
				User:       "devbox",
				WorkingDir: "/home/devbox/project",
			},
			StorageLimit:     spec.StorageLimit,
			RuntimeClassName: "devbox-runtime",
			NetworkSpec: devboxv1alpha2.NetworkSpec{
				Type: devboxv1alpha2.NetworkTypeNodePort,
			},
			// add Tolerations, allow scheduling to nodes with devbox.sealos.io/node taint
			Tolerations: []corev1.Toleration{
				{
					Key:      "devbox.sealos.io/node",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			// add Affinity, force scheduling to nodes with devbox.sealos.io/node label
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "devbox.sealos.io/node",
										Operator: corev1.NodeSelectorOpExists,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// CreateDevbox creates a Devbox
func (h *DevboxCommonHelper) CreateDevbox(ctx context.Context, spec DevboxCreateSpec) error {
	devbox := h.GenerateDevbox(spec)
	if err := h.ctrlClient.Create(ctx, devbox); err != nil {
		return fmt.Errorf("failed to create Devbox: %w", err)
	}
	return nil
}

// ==================== Resource Check Methods ====================

// IsPodRunning checks if the Pod is running and ready
func (h *DevboxCommonHelper) IsPodRunning(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	podList := &corev1.PodList{}
	err := h.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
	if err != nil {
		return false
	}

	if len(podList.Items) == 0 {
		return false
	}

	pod := podList.Items[0]

	// 检查 Pod 状态
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	// 检查所有容器是否就绪
	for _, container := range pod.Status.ContainerStatuses {
		if !container.Ready {
			return false
		}
	}

	return true
}

// IsServiceCreated checks if the Service is created
func (h *DevboxCommonHelper) IsServiceCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	serviceList, err := h.k8sClient.CoreV1().Services(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	return err == nil && len(serviceList.Items) > 0
}

// IsSecretCreated checks if the Secret is created
func (h *DevboxCommonHelper) IsSecretCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	secretList, err := h.k8sClient.CoreV1().Secrets(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	return err == nil && len(secretList.Items) > 0
}

// IsLVMCreated checks if the LVM logical volume is created
func (h *DevboxCommonHelper) IsLVMCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	lvs, err := lvm.ListLVMLogicalVolume()
	if err != nil {
		return false
	}
	for _, lv := range lvs {
		if strings.Contains(lv.Name, devbox.Status.ContentID) {
			return true
		}
	}
	return false
}

// ==================== Resource Retrieval Methods ====================

// GetDevboxPods gets the Pods associated with a Devbox
func (h *DevboxCommonHelper) GetDevboxPods(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Pod, error) {
	podList, err := h.k8sClient.CoreV1().Pods(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// GetDevboxServices gets the Services associated with a Devbox
func (h *DevboxCommonHelper) GetDevboxServices(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Service, error) {
	serviceList, err := h.k8sClient.CoreV1().Services(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

// GetDevboxSecrets gets the Secrets associated with a Devbox
func (h *DevboxCommonHelper) GetDevboxSecrets(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Secret, error) {
	secretList, err := h.k8sClient.CoreV1().Secrets(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return secretList.Items, nil
}

// ==================== Wait Methods ====================

// WaitForDevboxState waits for Devbox to reach the specified state
func (h *DevboxCommonHelper) WaitForDevboxState(ctx context.Context, namespace, name string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("等待超时")
			}

			devbox := &devboxv1alpha2.Devbox{}
			if err := h.ctrlClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, devbox); err != nil {
				continue
			}

			if devbox.Status.State == targetState {
				return nil
			}
		}
	}
}

// WaitForDevboxRunningWithResources waits for Devbox to be running with all resources ready
func (h *DevboxCommonHelper) WaitForDevboxRunningWithResources(ctx context.Context, namespace, name string, timeout time.Duration) (*devboxv1alpha2.Devbox, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("等待超时")
			}

			devbox := &devboxv1alpha2.Devbox{}
			if err := h.ctrlClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, devbox); err != nil {
				continue
			}

			// check state
			if devbox.Status.State != devboxv1alpha2.DevboxStateRunning {
				continue
			}

			// check Pod
			if !h.IsPodRunning(ctx, *devbox) {
				continue
			}

			// check Service
			if !h.IsServiceCreated(ctx, *devbox) {
				continue
			}

			// check Secret
			if !h.IsSecretCreated(ctx, *devbox) {
				continue
			}

			return devbox, nil
		}
	}
}

// WaitForCommitComplete waits for commit to complete
func (h *DevboxCommonHelper) WaitForCommitComplete(ctx context.Context, namespace, name string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	return h.WaitForDevboxState(ctx, namespace, name, targetState, timeout)
}

// WaitForReleaseComplete waits for release to complete
func (h *DevboxCommonHelper) WaitForReleaseComplete(ctx context.Context, namespace, name string, timeout time.Duration) (devboxv1alpha2.DevBoxReleasePhase, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("等待超时")
			}

			release := &devboxv1alpha2.DevBoxRelease{}
			if err := h.ctrlClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, release); err != nil {
				return "", err
			}

			switch release.Status.Phase {
			case devboxv1alpha2.DevBoxReleasePhaseSuccess:
				return devboxv1alpha2.DevBoxReleasePhaseSuccess, nil
			case devboxv1alpha2.DevBoxReleasePhaseFailed:
				return devboxv1alpha2.DevBoxReleasePhaseFailed, nil
			}
		}
	}
}

// ==================== Data Write and Verification Methods ====================

// WriteTestDataToDevbox writes test data to Devbox container
// directory: data directory name (relative to /home/devbox/project/)
// dataSize: data size, e.g. "100M"
// fileCount: number of files
func (h *DevboxCommonHelper) WriteTestDataToDevbox(ctx context.Context, devbox devboxv1alpha2.Devbox, directory string, dataSize string, fileCount int) error {
	// Find Pod
	podList := &corev1.PodList{}
	if err := h.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name}); err != nil {
		return fmt.Errorf("failed to find Pod: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no Pod found")
	}

	pod := podList.Items[0]
	sizeValue := strings.TrimSuffix(dataSize, "M")

	// Build write command
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`
mkdir -p /home/devbox/project/%s
cd /home/devbox/project/%s
echo "Starting to write test data to %s..."

# Calculate available space (leave some buffer)
available_space=$(df /home/devbox/project/%s | tail -1 | awk '{print $4}')
echo "Available space: $available_space KB"

# Try to write files until storage limit is reached
file_count=0
for i in $(seq 1 %d); do
    if dd if=/dev/urandom of=file_$i.bin bs=1M count=%s 2>/dev/null; then
        file_count=$((file_count + 1))
    else
        echo "Failed to create file (storage limit)"
        break
    fi
done

echo "Data write completed: created $file_count files"
echo "Directory size:"
du -sh .
`, directory, directory, directory, directory, fileCount, sizeValue),
	}

	containerName := "devbox"
	if len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	return h.ExecCommandInPod(ctx, pod.Namespace, pod.Name, containerName, cmd)
}

// VerifyTestDataInDevbox verifies test data in Devbox container
// directory: data directory name (relative to /home/devbox/project/)
func (h *DevboxCommonHelper) VerifyTestDataInDevbox(ctx context.Context, devbox devboxv1alpha2.Devbox, directory string) error {
	// Find Pod
	podList := &corev1.PodList{}
	if err := h.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name}); err != nil {
		return fmt.Errorf("failed to find Pod: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no Pod found")
	}

	pod := podList.Items[0]

	// Build verification command
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`
echo "Checking test data..."
if [ -d "/home/devbox/project/%s" ]; then
    cd /home/devbox/project/%s
    file_count=$(ls -1 file_*.bin 2>/dev/null | wc -l)
    total_size=$(du -sh . 2>/dev/null | cut -f1)
    echo "Found $file_count test files, total size: $total_size"
    
    if [ "$file_count" -gt 0 ]; then
        echo "Data verification successful: found $file_count files, total size $total_size"
        if [ "$file_count" -gt 10 ]; then
            echo "... and $((file_count - 10)) more files"
        fi
        exit 0
    else
        echo "Data verification failed: no test files"
        ls -lah
        exit 1
    fi
else
    echo "Data verification failed: directory does not exist /home/devbox/project/%s"
    exit 1
fi
`, directory, directory, directory),
	}

	containerName := "devbox"
	if len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	return h.ExecCommandInPod(ctx, pod.Namespace, pod.Name, containerName, cmd)
}

// ==================== Pod Command Execution ====================

// ExecCommandInPod executes a command in a Pod
func (h *DevboxCommonHelper) ExecCommandInPod(ctx context.Context, namespace, podName, containerName string, cmd []string) error {
	return h.ExecCommandInPodWithRetry(ctx, namespace, podName, containerName, cmd, 3, 5*time.Second)
}

// ExecCommandInPodWithRetry executes a command in a Pod with retry mechanism
// maxRetries: maximum number of retry attempts (0 means no retry)
// retryDelay: delay between retries
func (h *DevboxCommonHelper) ExecCommandInPodWithRetry(ctx context.Context, namespace, podName, containerName string, cmd []string, maxRetries int, retryDelay time.Duration) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d after error: %v", attempt, maxRetries, lastErr)
			time.Sleep(retryDelay)
		}

		req := h.k8sClient.CoreV1().RESTClient().Post().
			Resource("pods").
			Name(podName).
			Namespace(namespace).
			SubResource("exec")

		req.VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

		var stdout, stderr bytes.Buffer

		exec, err := remotecommand.NewSPDYExecutor(h.restConfig, "POST", req.URL())
		if err != nil {
			lastErr = fmt.Errorf("create executor failed: %w", err)
			if isNetworkError(err) {
				continue // Retry on network errors
			}
			return lastErr
		}

		err = exec.Stream(remotecommand.StreamOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		})

		if err != nil {
			lastErr = fmt.Errorf("execute command failed: %w, stderr: %s", err, stderr.String())
			if isNetworkError(err) {
				continue // Retry on network errors
			}
			return lastErr
		}

		// Success
		log.Printf("command output:\n%s", stdout.String())
		if stderr.Len() > 0 {
			log.Printf("command error output:\n%s", stderr.String())
		}

		if attempt > 0 {
			log.Printf("✓ Command succeeded after %d retries", attempt)
		}

		return nil
	}

	return fmt.Errorf("command failed after %d attempts: %w", maxRetries+1, lastErr)
}

// isNetworkError checks if an error is a network-related error that should be retried
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Check for common network errors
	networkErrorPatterns := []string{
		"connection timed out",
		"connection refused",
		"connect: connection timed out",
		"dial tcp",
		"i/o timeout",
		"TLS handshake timeout",
		"EOF",
		"broken pipe",
		"connection reset by peer",
	}

	for _, pattern := range networkErrorPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// ==================== Image Management Methods ====================

// RemoveCurrentBaseImage removes the current base image of the Devbox from the local node
func (h *DevboxCommonHelper) RemoveCurrentBaseImage(ctx context.Context, namespace, name string) error {
	devbox := &devboxv1alpha2.Devbox{}
	if err := h.ctrlClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 devbox 失败: %w", err)
	}

	contentID := devbox.Status.ContentID
	if contentID == "" {
		return fmt.Errorf("devbox %s 未找到 contentID", name)
	}

	if devbox.Status.CommitRecords == nil {
		return fmt.Errorf("devbox %s 的 commitRecords 为空", name)
	}

	record, ok := devbox.Status.CommitRecords[contentID]
	if !ok || record == nil {
		return fmt.Errorf("devbox %s 未找到 contentID %s 对应的记录", name, contentID)
	}

	currentBaseImage := record.BaseImage
	if currentBaseImage == "" {
		return fmt.Errorf("devbox %s 的基础镜像为空", name)
	}

	// 当前周期的 baseImage == 上一次 CommitImage，需要根据该 CommitImage 找到更早一层的 baseImage
	var imageToRemove string
	for _, rec := range devbox.Status.CommitRecords {
		if rec == nil || rec.CommitImage == "" {
			continue
		}
		if rec.CommitImage == currentBaseImage {
			imageToRemove = rec.BaseImage
			break
		}
	}

	if imageToRemove == "" {
		log.Printf("未找到匹配当前基础镜像 %s 的上一层记录，跳过删除", currentBaseImage)
		return nil
	}

	log.Printf("准备删除上一轮基础镜像: %s (当前基础镜像: %s)", imageToRemove, currentBaseImage)
	namespaces := []string{"k8s.io", "sealos.io"}
	for _, ns := range namespaces {
		log.Printf("尝试从 namespace %s 删除镜像: %s", ns, imageToRemove)
		if err := runCtrImageRemove(ctx, ns, imageToRemove); err != nil {
			// k8s.io 删除失败直接返回，sealos.io 失败仅记录日志
			if ns == "k8s.io" {
				return err
			}
			log.Printf("从 namespace %s 删除镜像失败（忽略）: %v", ns, err)
		}
	}

	log.Printf("镜像删除流程完成，等待 6 秒确保清理完成")
	time.Sleep(6 * time.Second)
	return nil
}

func runCtrImageRemove(ctx context.Context, namespace, image string) error {
	cmd := exec.CommandContext(ctx, "ctr", "-n", namespace, "images", "rm", image)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg != "" {
			return fmt.Errorf("删除镜像 %s 失败: %w, stderr: %s", image, err, errMsg)
		}
		return fmt.Errorf("删除镜像 %s 失败: %w", image, err)
	}

	if out := stdout.String(); out != "" {
		log.Printf("ctr 输出: %s", out)
	}
	return nil
}
