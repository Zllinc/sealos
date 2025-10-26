package tester

import (
	"context"
	"fmt"
	"log"
	"strings"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewDevboxCleanupTester creates a new cleanup tester
func NewDevboxCleanupTester(config *CleanupConfig) (*DevboxCleanupTester, error) {
	// 创建 Kubernetes 客户端
	var restConfig *rest.Config
	var err error

	// 尝试从 kubeconfig 文件或集群配置创建配置
	restConfig, err = clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
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

	return &DevboxCleanupTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// ListTestResources lists test resources
func (t *DevboxCleanupTester) ListTestResources(ctx context.Context) (*CleanupResult, error) {
	result := &CleanupResult{}

	// 列出 Devbox
	devboxes, err := t.listTestDevboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出 Devbox 失败: %w", err)
	}

	for _, devbox := range devboxes {
		testType := ""
		if devbox.Labels != nil {
			testType = devbox.Labels["test-type"]
		}

		result.CleanupDetails = append(result.CleanupDetails, CleanupDetail{
			ResourceType: "Devbox",
			Namespace:    devbox.Namespace,
			Name:         devbox.Name,
			TestType:     testType,
			Deleted:      false,
		})
	}

	// 列出 DevBoxRelease
	releases, err := t.listTestReleases(ctx)
	if err != nil {
		log.Printf("列出 DevBoxRelease 失败: %v", err)
	} else {
		for _, release := range releases {
			testType := ""
			if release.Labels != nil {
				testType = release.Labels["test-type"]
			}

			result.CleanupDetails = append(result.CleanupDetails, CleanupDetail{
				ResourceType: "DevBoxRelease",
				Namespace:    release.Namespace,
				Name:         release.Name,
				TestType:     testType,
				Deleted:      false,
			})
		}
	}

	result.TotalResources = len(result.CleanupDetails)
	return result, nil
}

// RunCleanup performs cleanup
func (t *DevboxCleanupTester) RunCleanup(ctx context.Context) (*CleanupResult, error) {
	log.Printf("========== 开始清理测试资源 ==========")
	if t.config.AllNamespaces {
		log.Printf("范围: 所有命名空间")
	} else {
		log.Printf("范围: %s", t.config.Namespace)
	}
	log.Printf("强制删除: %v", t.config.Force)
	log.Printf("仅列出: %v", t.config.DryRun)

	result := &CleanupResult{}

	// 步骤 1: 清理 DevBoxRelease
	log.Printf("\n=== 步骤 1: 清理 DevBoxRelease ===")
	releases, err := t.listTestReleases(ctx)
	if err != nil {
		log.Printf("列出 DevBoxRelease 失败: %v", err)
	} else {
		log.Printf("找到 %d 个 DevBoxRelease", len(releases))
		for _, release := range releases {
			detail := t.deleteRelease(ctx, release)
			result.CleanupDetails = append(result.CleanupDetails, detail)
			if detail.Deleted {
				result.DeletedReleases++
			} else {
				result.FailedDeletes++
			}
		}
	}

	// 步骤 2: 清理 Devbox
	log.Printf("\n=== 步骤 2: 清理 Devbox ===")
	devboxes, err := t.listTestDevboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出 Devbox 失败: %w", err)
	}

	log.Printf("找到 %d 个测试 Devbox", len(devboxes))
	for _, devbox := range devboxes {
		detail := t.deleteDevbox(ctx, devbox)
		result.CleanupDetails = append(result.CleanupDetails, detail)
		if detail.Deleted {
			result.DeletedDevboxes++
		} else {
			result.FailedDeletes++
		}
	}

	result.TotalResources = len(result.CleanupDetails)

	// 打印汇总
	log.Printf("\n========== 清理完成 ==========")
	log.Printf("总资源数: %d", result.TotalResources)
	log.Printf("删除的 Devbox: %d", result.DeletedDevboxes)
	log.Printf("删除的 DevBoxRelease: %d", result.DeletedReleases)
	log.Printf("删除失败: %d", result.FailedDeletes)

	if len(result.ErrorMessages) > 0 {
		log.Printf("\n错误信息:")
		for i, msg := range result.ErrorMessages {
			log.Printf("  [%d] %s", i+1, msg)
		}
	}

	return result, nil
}

// listTestDevboxes 列出测试 Devbox
func (t *DevboxCleanupTester) listTestDevboxes(ctx context.Context) ([]devboxv1alpha2.Devbox, error) {
	devboxList := &devboxv1alpha2.DevboxList{}

	var listOptions []client.ListOption
	if !t.config.AllNamespaces {
		listOptions = append(listOptions, client.InNamespace(t.config.Namespace))
	}

	if err := t.ctrlClient.List(ctx, devboxList, listOptions...); err != nil {
		return nil, err
	}

	// 过滤测试 Devbox
	var testDevboxes []devboxv1alpha2.Devbox
	for _, devbox := range devboxList.Items {
		if t.isTestDevbox(devbox) {
			testDevboxes = append(testDevboxes, devbox)
		}
	}

	return testDevboxes, nil
}

// listTestReleases 列出测试 DevBoxRelease
func (t *DevboxCleanupTester) listTestReleases(ctx context.Context) ([]devboxv1alpha2.DevBoxRelease, error) {
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}

	var listOptions []client.ListOption
	if !t.config.AllNamespaces {
		listOptions = append(listOptions, client.InNamespace(t.config.Namespace))
	}

	if err := t.ctrlClient.List(ctx, releaseList, listOptions...); err != nil {
		return nil, err
	}

	// 过滤测试 Release
	var testReleases []devboxv1alpha2.DevBoxRelease
	for _, release := range releaseList.Items {
		if t.isTestRelease(release) {
			testReleases = append(testReleases, release)
		}
	}

	return testReleases, nil
}

// deleteDevbox 删除 Devbox
func (t *DevboxCleanupTester) deleteDevbox(ctx context.Context, devbox devboxv1alpha2.Devbox) CleanupDetail {
	detail := CleanupDetail{
		ResourceType: "Devbox",
		Namespace:    devbox.Namespace,
		Name:         devbox.Name,
	}

	if devbox.Labels != nil {
		detail.TestType = devbox.Labels["test-type"]
	}

	// 如果是 DryRun，只记录不删除
	if t.config.DryRun {
		log.Printf("DryRun: 将删除 Devbox %s/%s", devbox.Namespace, devbox.Name)
		detail.Deleted = true
		return detail
	}

	// 如果需要强制删除，先移除 finalizer
	if t.config.Force && len(devbox.Finalizers) > 0 {
		devbox.Finalizers = []string{}
		if err := t.ctrlClient.Update(ctx, &devbox); err != nil {
			detail.Error = fmt.Sprintf("移除 finalizer 失败: %v", err)
			log.Printf("移除 Devbox %s/%s finalizer 失败: %v", devbox.Namespace, devbox.Name, err)
		}
	}

	// 删除 Devbox
	if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
		detail.Error = fmt.Sprintf("删除失败: %v", err)
		detail.Deleted = false
		log.Printf("删除 Devbox %s/%s 失败: %v", devbox.Namespace, devbox.Name, err)
		return detail
	}

	detail.Deleted = true
	log.Printf("✓ 已删除 Devbox: %s/%s", devbox.Namespace, devbox.Name)
	return detail
}

// deleteRelease 删除 DevBoxRelease
func (t *DevboxCleanupTester) deleteRelease(ctx context.Context, release devboxv1alpha2.DevBoxRelease) CleanupDetail {
	detail := CleanupDetail{
		ResourceType: "DevBoxRelease",
		Namespace:    release.Namespace,
		Name:         release.Name,
	}

	if release.Labels != nil {
		detail.TestType = release.Labels["test-type"]
	}

	// 如果是 DryRun，只记录不删除
	if t.config.DryRun {
		log.Printf("DryRun: 将删除 DevBoxRelease %s/%s", release.Namespace, release.Name)
		detail.Deleted = true
		return detail
	}

	// 如果需要强制删除，先移除 finalizer
	if t.config.Force && len(release.Finalizers) > 0 {
		release.Finalizers = []string{}
		if err := t.ctrlClient.Update(ctx, &release); err != nil {
			detail.Error = fmt.Sprintf("移除 finalizer 失败: %v", err)
			log.Printf("移除 DevBoxRelease %s/%s finalizer 失败: %v", release.Namespace, release.Name, err)
		}
	}

	// 删除 DevBoxRelease
	if err := t.ctrlClient.Delete(ctx, &release); err != nil {
		detail.Error = fmt.Sprintf("删除失败: %v", err)
		detail.Deleted = false
		log.Printf("删除 DevBoxRelease %s/%s 失败: %v", release.Namespace, release.Name, err)
		return detail
	}

	detail.Deleted = true
	log.Printf("✓ 已删除 DevBoxRelease: %s/%s", release.Namespace, release.Name)
	return detail
}

// isTestDevbox 判断是否为测试 Devbox
func (t *DevboxCleanupTester) isTestDevbox(devbox devboxv1alpha2.Devbox) bool {
	// 检查标签
	if devbox.Labels != nil {
		if stressTest, ok := devbox.Labels["stress-test"]; ok && stressTest == "true" {
			return true
		}
	}

	// 检查名称前缀
	testPrefixes := []string{
		"stress-test-devbox-",
		"concurrent-test-devbox-",
		"release-test-devbox-",
		"lifecycle-test-",
		"edge-toggle-devbox-",
	}

	for _, prefix := range testPrefixes {
		if strings.HasPrefix(devbox.Name, prefix) {
			return true
		}
	}

	return false
}

// isTestRelease 判断是否为测试 DevBoxRelease
func (t *DevboxCleanupTester) isTestRelease(release devboxv1alpha2.DevBoxRelease) bool {
	// 检查名称包含测试关键字
	testKeywords := []string{
		"release-test",
		"concurrent-release",
		"lifecycle-test",
	}

	for _, keyword := range testKeywords {
		if strings.Contains(release.Name, keyword) {
			return true
		}
	}

	return false
}

// PrintCleanupPlan prints the cleanup plan
func (t *DevboxCleanupTester) PrintCleanupPlan(result *CleanupResult) {
	if result.TotalResources == 0 {
		fmt.Println("未找到需要清理的测试资源")
		return
	}

	fmt.Printf("\n找到 %d 个测试资源:\n", result.TotalResources)
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-20s %-30s %-20s %s\n", "资源类型", "命名空间", "名称", "测试类型")
	fmt.Println(strings.Repeat("-", 80))

	devboxCount := 0
	releaseCount := 0

	for _, detail := range result.CleanupDetails {
		fmt.Printf("%-20s %-30s %-20s %s\n",
			detail.ResourceType,
			detail.Namespace,
			detail.Name,
			detail.TestType)

		if detail.ResourceType == "Devbox" {
			devboxCount++
		} else if detail.ResourceType == "DevBoxRelease" {
			releaseCount++
		}
	}

	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Devbox: %d, DevBoxRelease: %d\n", devboxCount, releaseCount)
}

// PrintCleanupResult prints the cleanup result
func (t *DevboxCleanupTester) PrintCleanupResult(result *CleanupResult) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("清理结果汇总")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("总资源数: %d\n", result.TotalResources)
	fmt.Printf("删除的 Devbox: %d\n", result.DeletedDevboxes)
	fmt.Printf("删除的 DevBoxRelease: %d\n", result.DeletedReleases)
	fmt.Printf("删除失败: %d\n", result.FailedDeletes)
	if result.SkippedResources > 0 {
		fmt.Printf("跳过的资源: %d (DryRun)\n", result.SkippedResources)
	}

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\n错误信息 (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			if i >= 10 {
				fmt.Printf("  ... 还有 %d 个错误\n", len(result.ErrorMessages)-10)
				break
			}
			fmt.Printf("  [%d] %s\n", i+1, msg)
		}
	}

	// 打印详细信息（失败的资源）
	failedDetails := []CleanupDetail{}
	for _, detail := range result.CleanupDetails {
		if !detail.Deleted && !t.config.DryRun {
			failedDetails = append(failedDetails, detail)
		}
	}

	if len(failedDetails) > 0 {
		fmt.Printf("\n删除失败的资源:\n")
		for i, detail := range failedDetails {
			fmt.Printf("  [%d] %s %s/%s", i+1, detail.ResourceType, detail.Namespace, detail.Name)
			if detail.Error != "" {
				fmt.Printf(" - %s", detail.Error)
			}
			fmt.Println()
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// 成功/失败状态
	if t.config.DryRun {
		fmt.Println("ℹ DryRun 模式：未执行实际删除操作")
	} else if result.FailedDeletes == 0 {
		fmt.Println("✓ 所有资源清理成功！")
	} else {
		fmt.Printf("⚠ 部分资源清理失败 (%d/%d)\n", result.FailedDeletes, result.TotalResources)
	}
}
