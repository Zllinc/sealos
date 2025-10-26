package tester

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewDevboxConcurrentTester creates a new concurrent tester
func NewDevboxConcurrentTester(config *ConcurrentTestConfig) (*DevboxConcurrentTester, error) {
	// Create Kubernetes client
	var restConfig *rest.Config
	var err error

	// Try to build config from kubeconfig file or cluster config
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

	// Create controller-runtime client
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

	// Create common helper utility
	helper := NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)

	return &DevboxConcurrentTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunConcurrentTest runs the concurrent test
func (t *DevboxConcurrentTester) RunConcurrentTest(ctx context.Context) (*ConcurrentTestResult, error) {
	log.Printf("========== 开始并发创建测试 ==========")
	log.Printf("配置:")
	log.Printf("  命名空间: %s", t.config.Namespace)
	log.Printf("  Devbox 数量: %d", t.config.DevboxCount)
	log.Printf("  并发数: %d", t.config.ConcurrentCount)
	log.Printf("  镜像: %s", t.config.Image)
	log.Printf("  资源: CPU=%s, Memory=%s, Storage=%s", t.config.CPU, t.config.Memory, t.config.StorageLimit)

	result := &ConcurrentTestResult{}
	startTime := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex

	// 使用信号量控制并发数
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := 0; i < t.config.DevboxCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			devboxName := fmt.Sprintf("concurrent-test-devbox-%d", index)
			detail := t.testSingleDevboxCreate(ctx, devboxName)

			mu.Lock()
			defer mu.Unlock()

			result.Details = append(result.Details, detail)
			result.TotalDevboxes++

			if detail.CreateSuccess && detail.RunningSuccess {
				result.SuccessfulCreates++
				log.Printf("[%d/%d] ✓ Devbox %s 创建成功并就绪，耗时: %v",
					index+1, t.config.DevboxCount, devboxName, detail.TotalDuration)
			} else {
				result.FailedCreates++
				if detail.Error != "" {
					result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %s", devboxName, detail.Error))
				}
				log.Printf("[%d/%d] ✗ Devbox %s 测试失败: %s",
					index+1, t.config.DevboxCount, devboxName, detail.Error)
			}
		}(i)
	}

	wg.Wait()
	result.TotalTestTime = time.Since(startTime)

	// 计算统计数据
	if result.SuccessfulCreates > 0 {
		result.AverageCreateTime = result.TotalTestTime / time.Duration(result.SuccessfulCreates)
	}
	if result.TotalTestTime.Seconds() > 0 {
		result.MaxQPS = float64(result.SuccessfulCreates) / result.TotalTestTime.Seconds()
	}

	// 打印汇总
	log.Printf("\n========== 并发测试完成 ==========")
	log.Printf("总测试数: %d", result.TotalDevboxes)
	log.Printf("成功: %d (%.1f%%)", result.SuccessfulCreates,
		float64(result.SuccessfulCreates)/float64(result.TotalDevboxes)*100)
	log.Printf("失败: %d (%.1f%%)", result.FailedCreates,
		float64(result.FailedCreates)/float64(result.TotalDevboxes)*100)
	log.Printf("总耗时: %v", result.TotalTestTime)
	log.Printf("平均创建时间: %v", result.AverageCreateTime)
	log.Printf("最大 QPS: %.2f/s", result.MaxQPS)

	if len(result.ErrorMessages) > 0 {
		log.Printf("\n错误信息 (前 10 条):")
		for i, msg := range result.ErrorMessages {
			if i >= 10 {
				log.Printf("  ... 还有 %d 个错误", len(result.ErrorMessages)-10)
				break
			}
			log.Printf("  [%d] %s", i+1, msg)
		}
	}

	return result, nil
}

// testSingleDevboxCreate tests the creation of a single Devbox
func (t *DevboxConcurrentTester) testSingleDevboxCreate(ctx context.Context, name string) ConcurrentTestDetail {
	detail := ConcurrentTestDetail{
		DevboxName: name,
	}

	startTime := time.Now()

	// Step 1: Create Devbox
	createStart := time.Now()
	if err := t.createDevbox(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("failed to create Devbox: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.CreateSuccess = true
	detail.CreateDuration = time.Since(createStart)

	// Step 2: Wait for Devbox Running (including all resources ready)
	_, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, t.config.WaitTimeout)
	if err != nil {
		detail.Error = fmt.Sprintf("wait for Running timeout: %v", err)
		detail.RunningSuccess = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.RunningSuccess = true
	detail.TotalDuration = time.Since(startTime)

	return detail
}

// createDevbox creates a Devbox
func (t *DevboxConcurrentTester) createDevbox(ctx context.Context, name string) error {
	spec := DevboxCreateSpec{
		Name:      name,
		Namespace: t.config.Namespace,
		Labels: map[string]string{
			"stress-test": "true",
			"test-type":   "concurrent",
		},
		Image:        t.config.Image,
		CPU:          t.config.CPU,
		Memory:       t.config.Memory,
		StorageLimit: t.config.StorageLimit,
	}

	return t.helper.CreateDevbox(ctx, spec)
}

// Cleanup cleans up test resources
func (t *DevboxConcurrentTester) Cleanup(ctx context.Context) error {
	log.Printf("Cleaning up concurrent test resources...")

	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		return fmt.Errorf("failed to list Devboxes: %w", err)
	}

	deletedCount := 0
	for _, devbox := range devboxList.Items {
		if t.isConcurrentTestDevbox(devbox) {
			if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
				log.Printf("Failed to delete Devbox %s: %v", devbox.Name, err)
			} else {
				log.Printf("Deleted Devbox: %s", devbox.Name)
				deletedCount++
			}
		}
	}

	log.Printf("Cleanup completed, deleted %d Devboxes", deletedCount)
	return nil
}

// isConcurrentTestDevbox checks if a Devbox was created by concurrent test
func (t *DevboxConcurrentTester) isConcurrentTestDevbox(devbox devboxv1alpha2.Devbox) bool {
	if devbox.Labels != nil {
		if testType, ok := devbox.Labels["test-type"]; ok && testType == "concurrent" {
			return true
		}
	}
	// Check by name
	return devbox.Name[:len("concurrent-test-devbox-")] == "concurrent-test-devbox-"
}
