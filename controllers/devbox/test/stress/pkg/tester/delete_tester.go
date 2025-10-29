package tester

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	"github.com/openebs/lvm-localpv/pkg/lvm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewDevboxDeleteTester creates a new delete tester
func NewDevboxDeleteTester(config *DeleteTestConfig) (*DevboxDeleteTester, error) {
	// 创建 Kubernetes 客户端
	var restConfig *rest.Config
	var err error

	// 尝试从 kubeconfig 文件或集群配置创建配置
	restConfig, err = clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		// 尝试从集群配置创建
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			// 尝试从默认配置创建
			restConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
			if err != nil {
				return nil, fmt.Errorf("failed to create kubeconfig: %w", err)
			}
		}
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	// 创建 controller-runtime 客户端
	scheme := runtime.NewScheme()
	if err := devboxv1alpha2.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add devbox scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %w", err)
	}

	ctrlClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create ctrl client: %w", err)
	}

	// 创建通用辅助工具
	helper := NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)

	return &DevboxDeleteTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunDeleteTest runs the delete test
func (t *DevboxDeleteTester) RunDeleteTest(ctx context.Context) (*DeleteTestResult, error) {
	log.Printf("========== 开始 Devbox 删除测试 ==========")
	log.Printf("命名空间: %s", t.config.Namespace)
	log.Printf("并发数: %d", t.config.ConcurrentCount)

	result := &DeleteTestResult{}
	startTime := time.Now()

	// 步骤 1: 扫描所有 Devbox
	log.Printf("\n=== 步骤 1: 扫描 Devbox ===")
	devboxes, err := t.scanDevboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("扫描 Devbox 失败: %w", err)
	}

	if len(devboxes) == 0 {
		log.Printf("未找到任何 Devbox，测试结束")
		return result, nil
	}

	log.Printf("找到 %d 个 Devbox", len(devboxes))
	for i, devbox := range devboxes {
		log.Printf("  [%d] %s (状态: %s)", i+1, devbox.Name, devbox.Status.State)
	}

	result.TotalDevboxes = len(devboxes)

	// 步骤 2: 并发删除 Devbox
	log.Printf("\n=== 步骤 2: 并发删除 Devbox (并发数: %d) ===", t.config.ConcurrentCount)
	deleteResult := t.deleteDevboxesConcurrently(ctx, devboxes)

	// 合并结果
	result.SuccessfulDeletes = deleteResult.SuccessfulDeletes
	result.FailedDeletes = deleteResult.FailedDeletes
	result.DeleteDetails = deleteResult.DeleteDetails
	result.ResourceCheckStats = deleteResult.ResourceCheckStats
	result.ErrorMessages = deleteResult.ErrorMessages

	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulDeletes > 0 {
		result.AverageDeleteTime = result.TotalTestTime / time.Duration(result.SuccessfulDeletes)
	}
	if result.TotalTestTime.Seconds() > 0 {
		result.MaxQPS = float64(result.SuccessfulDeletes) / result.TotalTestTime.Seconds()
	}

	// 打印统计信息
	log.Printf("\n========== 删除测试完成 ==========")
	log.Printf("总删除数: %d", result.TotalDevboxes)
	log.Printf("成功删除: %d", result.SuccessfulDeletes)
	log.Printf("失败删除: %d", result.FailedDeletes)
	log.Printf("平均删除时间: %v", result.AverageDeleteTime)
	log.Printf("总耗时: %v", result.TotalTestTime)
	log.Printf("删除 QPS: %.2f", result.MaxQPS)
	log.Printf("\n资源清理统计:")
	log.Printf("  Devbox 清理: %d/%d", result.ResourceCheckStats.DevboxCleaned, result.TotalDevboxes)
	log.Printf("  Pod 清理: %d/%d", result.ResourceCheckStats.PodCleaned, result.TotalDevboxes)
	log.Printf("  Service 清理: %d/%d", result.ResourceCheckStats.ServiceCleaned, result.TotalDevboxes)
	log.Printf("  Secret 清理: %d/%d", result.ResourceCheckStats.SecretCleaned, result.TotalDevboxes)
	log.Printf("  LVM 清理: %d/%d", result.ResourceCheckStats.LVMCleaned, result.TotalDevboxes)

	if len(result.ErrorMessages) > 0 {
		log.Printf("\n错误信息:")
		for i, msg := range result.ErrorMessages {
			log.Printf("  [%d] %s", i+1, msg)
		}
	}

	return result, nil
}

// scanDevboxes 扫描指定命名空间下的所有 Devbox
func (t *DevboxDeleteTester) scanDevboxes(ctx context.Context) ([]devboxv1alpha2.Devbox, error) {
	devboxList := &devboxv1alpha2.DevboxList{}
	err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace))
	if err != nil {
		return nil, fmt.Errorf("列出 Devbox 失败: %w", err)
	}

	return devboxList.Items, nil
}

// deleteDevboxesConcurrently 并发删除 Devbox
func (t *DevboxDeleteTester) deleteDevboxesConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox) *DeleteTestResult {
	result := &DeleteTestResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 创建信号量控制并发数
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(index int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("[%d/%d] 开始删除 Devbox: %s", index+1, len(devboxes), db.Name)

			// 删除并检查资源清理
			detail := t.deleteDevboxWithCheck(ctx, db)

			mu.Lock()
			defer mu.Unlock()

			result.DeleteDetails = append(result.DeleteDetails, detail)

			if detail.DeleteSuccess && detail.DevboxCleaned && detail.PodCleaned &&
				detail.ServiceCleaned && detail.SecretCleaned && detail.LVMCleaned {
				result.SuccessfulDeletes++
			} else {
				result.FailedDeletes++
				if detail.Error != "" {
					result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %s", db.Name, detail.Error))
				}
			}

			// 统计资源清理
			if detail.DevboxCleaned {
				result.ResourceCheckStats.DevboxCleaned++
			}
			if detail.PodCleaned {
				result.ResourceCheckStats.PodCleaned++
			}
			if detail.ServiceCleaned {
				result.ResourceCheckStats.ServiceCleaned++
			}
			if detail.SecretCleaned {
				result.ResourceCheckStats.SecretCleaned++
			}
			if detail.LVMCleaned {
				result.ResourceCheckStats.LVMCleaned++
			}

			log.Printf("[%d/%d] Devbox %s 删除完成，总耗时: %v", index+1, len(devboxes), db.Name, detail.TotalDuration)

		}(i, devbox)
	}

	wg.Wait()
	return result
}

// deleteDevboxWithCheck delete Devbox and check resource cleanup
func (t *DevboxDeleteTester) deleteDevboxWithCheck(ctx context.Context, devbox devboxv1alpha2.Devbox) DeleteTestDetail {
	detail := DeleteTestDetail{
		DevboxName: devbox.Name,
	}

	startTime := time.Now()

	// record ContentID (for LVM check)
	contentID := devbox.Status.ContentID

	// step 1: delete Devbox
	deleteStart := time.Now()
	err := t.ctrlClient.Delete(ctx, &devbox)
	detail.DeleteDuration = time.Since(deleteStart)

	if err != nil {
		detail.Error = fmt.Sprintf("delete Devbox failed: %v", err)
		log.Printf("[%s] delete failed: %v", devbox.Name, err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	detail.DeleteSuccess = true
	log.Printf("[%s] delete command sent, starting to monitor resource cleanup...", devbox.Name)

	// step 2: monitor resource cleanup
	cleanupStart := time.Now()
	cleanupCtx, cancel := context.WithTimeout(ctx, t.config.DeleteTimeout)
	defer cancel()

	ticker := time.NewTicker(t.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cleanupCtx.Done():
			// timeout, check final state
			detail.CleanupDuration = time.Since(cleanupStart)
			detail.TotalDuration = time.Since(startTime)
			t.checkFinalResourceState(ctx, devbox, contentID, &detail)
			if !detail.DevboxCleaned || !detail.PodCleaned || !detail.ServiceCleaned ||
				!detail.SecretCleaned || !detail.LVMCleaned {
				detail.Error = fmt.Sprintf("resource cleanup timeout: %s", detail.RemainingResource)
			}
			return detail

		case <-ticker.C:
			// check if resource cleanup is completed
			if t.checkResourceCleanup(ctx, devbox, contentID, &detail) {
				detail.CleanupDuration = time.Since(cleanupStart)
				detail.TotalDuration = time.Since(startTime)
				log.Printf("[%s] all resources cleaned up, duration: %v", devbox.Name, detail.CleanupDuration)
				return detail
			}
		}
	}
}

// checkResourceCleanup checks if resources are cleaned up
func (t *DevboxDeleteTester) checkResourceCleanup(ctx context.Context, devbox devboxv1alpha2.Devbox, contentID string, detail *DeleteTestDetail) bool {
	allCleaned := true

	// 1. check if Devbox is deleted
	if !detail.DevboxCleaned {
		devboxObj := &devboxv1alpha2.Devbox{}
		err := t.ctrlClient.Get(ctx, client.ObjectKey{
			Namespace: devbox.Namespace,
			Name:      devbox.Name,
		}, devboxObj)
		if errors.IsNotFound(err) {
			detail.DevboxCleaned = true
			log.Printf("[%s] ✓ Devbox deleted", devbox.Name)
		} else {
			allCleaned = false
		}
	}

	// 2. check if Pod is deleted
	if !detail.PodCleaned {
		if !t.helper.IsPodRunning(ctx, devbox) {
			// Pod does not exist or is not running
			podList := &corev1.PodList{}
			err := t.ctrlClient.List(ctx, podList,
				client.InNamespace(devbox.Namespace),
				client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
			if err == nil && len(podList.Items) == 0 {
				detail.PodCleaned = true
				log.Printf("[%s] ✓ Pod deleted", devbox.Name)
			} else {
				allCleaned = false
			}
		} else {
			allCleaned = false
		}
	}

	// 3. check if Service is deleted
	if !detail.ServiceCleaned {
		if !t.helper.IsServiceCreated(ctx, devbox) {
			detail.ServiceCleaned = true
			log.Printf("[%s] ✓ Service deleted", devbox.Name)
		} else {
			allCleaned = false
		}
	}

	// 4. check if Secret is deleted
	if !detail.SecretCleaned {
		if !t.helper.IsSecretCreated(ctx, devbox) {
			detail.SecretCleaned = true
			log.Printf("[%s] ✓ Secret deleted", devbox.Name)
		} else {
			allCleaned = false
		}
	}

	// 5. check if LVM is deleted
	if !detail.LVMCleaned {
		if contentID != "" {
			if !t.helper.IsLVMCreated(ctx, devbox) {
				detail.LVMCleaned = true
				log.Printf("[%s] ✓ LVM logical volume deleted", devbox.Name)
			} else {
				allCleaned = false
			}
		} else {
			// no ContentID, consider LVM cleaned
			detail.LVMCleaned = true
		}
	}

	return allCleaned
}

// checkFinalResourceState checks the final resource state
func (t *DevboxDeleteTester) checkFinalResourceState(ctx context.Context, devbox devboxv1alpha2.Devbox, contentID string, detail *DeleteTestDetail) {
	var remaining []string

	// 再次检查所有资源
	t.checkResourceCleanup(ctx, devbox, contentID, detail)

	// 收集残留资源信息
	if !detail.DevboxCleaned {
		remaining = append(remaining, "Devbox")
	}
	if !detail.PodCleaned {
		remaining = append(remaining, "Pod")
	}
	if !detail.ServiceCleaned {
		remaining = append(remaining, "Service")
	}
	if !detail.SecretCleaned {
		remaining = append(remaining, "Secret")
	}
	if !detail.LVMCleaned {
		remaining = append(remaining, "LVM")
	}

	if len(remaining) > 0 {
		detail.RemainingResource = strings.Join(remaining, ", ")
		log.Printf("[%s] ⚠ 残留资源: %s", devbox.Name, detail.RemainingResource)
	}
}

// GetDevboxLVName gets the LV name corresponding to a Devbox (helper method)
func (t *DevboxDeleteTester) GetDevboxLVName(contentID string) (string, error) {
	lvs, err := lvm.ListLVMLogicalVolume()
	if err != nil {
		return "", fmt.Errorf("获取 LVM 逻辑卷列表失败: %w", err)
	}

	for _, lv := range lvs {
		if strings.Contains(lv.Name, contentID) {
			return lv.Name, nil
		}
	}

	return "", fmt.Errorf("未找到对应的 LVM 逻辑卷")
}

// PrintDetailedResults prints detailed test results
func (t *DevboxDeleteTester) PrintDetailedResults(result *DeleteTestResult) {
	log.Printf("\n========== 详细删除结果 ==========")

	for i, detail := range result.DeleteDetails {
		log.Printf("\n[%d] Devbox: %s", i+1, detail.DevboxName)
		log.Printf("  删除成功: %v (耗时: %v)", detail.DeleteSuccess, detail.DeleteDuration)
		log.Printf("  资源清理:")
		log.Printf("    Devbox: %v", formatCheckResult(detail.DevboxCleaned))
		log.Printf("    Pod: %v", formatCheckResult(detail.PodCleaned))
		log.Printf("    Service: %v", formatCheckResult(detail.ServiceCleaned))
		log.Printf("    Secret: %v", formatCheckResult(detail.SecretCleaned))
		log.Printf("    LVM: %v", formatCheckResult(detail.LVMCleaned))
		log.Printf("  清理耗时: %v", detail.CleanupDuration)
		log.Printf("  总耗时: %v", detail.TotalDuration)

		if detail.Error != "" {
			log.Printf("  错误: %s", detail.Error)
		}
		if detail.RemainingResource != "" {
			log.Printf("  残留资源: %s", detail.RemainingResource)
		}
	}
}

// formatCheckResult 格式化检查结果
func formatCheckResult(cleaned bool) string {
	if cleaned {
		return "✓ 已清理"
	}
	return "✗ 未清理"
}
