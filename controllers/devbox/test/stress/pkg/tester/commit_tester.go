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

// NewDevboxCommitTester creates a new commit tester
func NewDevboxCommitTester(config *CommitTestConfig) (*DevboxCommitTester, error) {
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

	return &DevboxCommitTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunCommitTest runs the commit test
func (t *DevboxCommitTester) RunCommitTest(ctx context.Context) (*CommitTestResult, error) {
	log.Printf("========== 开始 Commit 测试 ==========")
	log.Printf("配置:")
	log.Printf("  命名空间: %s", t.config.Namespace)
	log.Printf("  目标状态: %s", t.config.TargetState)
	log.Printf("  Devbox 数量: %d", t.config.DevboxCount)
	log.Printf("  并发数: %d", t.config.ConcurrentCount)
	log.Printf("  数据大小: %s × %d 文件", t.config.DataSize, t.config.FileCount)
	log.Printf("  验证数据: %v", t.config.VerifyData)

	result := &CommitTestResult{}
	startTime := time.Now()

	// 步骤 1: 查找运行中的 Devbox
	log.Printf("\n=== 步骤 1: 查找运行中的 Devbox ===")
	devboxes, err := t.findRunningDevboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("查找 Devbox 失败: %w", err)
	}

	if len(devboxes) == 0 {
		return nil, fmt.Errorf("未找到运行中的 Devbox，请先创建一些 Devbox")
	}

	actualCount := len(devboxes)
	if actualCount < t.config.DevboxCount {
		log.Printf("警告: 只找到 %d 个运行中的 Devbox，少于请求的 %d 个", actualCount, t.config.DevboxCount)
	}

	log.Printf("找到 %d 个运行中的 Devbox:", actualCount)
	for i, devbox := range devboxes {
		log.Printf("  [%d] %s/%s", i+1, devbox.Namespace, devbox.Name)
	}

	result.TotalDevboxes = actualCount

	// 步骤 2: 并发测试每个 Devbox 的完整流程
	log.Printf("\n=== 步骤 2: 开始并发测试 (并发级别: %d) ===", t.config.ConcurrentCount)

	details := t.testDevboxesFullLifecycle(ctx, devboxes)

	// 统计结果
	var totalWriteTime, totalCommitTime, totalVerifyTime time.Duration
	for _, detail := range details {
		result.Details = append(result.Details, detail)

		totalWriteTime += detail.WriteDuration
		totalCommitTime += detail.CommitDuration
		totalVerifyTime += detail.VerifyDuration

		if detail.CommitSuccess {
			result.SuccessfulCommits++
		} else {
			result.FailedCommits++
		}

		if detail.Error != "" {
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %s", detail.DevboxName, detail.Error))
		}
	}

	result.TotalTestTime = time.Since(startTime)

	// 计算平均时间
	if actualCount > 0 {
		result.WriteDataTime = totalWriteTime / time.Duration(actualCount)
		result.CommitTime = totalCommitTime / time.Duration(actualCount)
		result.VerifyTime = totalVerifyTime / time.Duration(actualCount)
	}

	// 计算写入速度和 QPS
	dataSizeBytes, _ := parseDataSize(t.config.DataSize)
	if result.WriteDataTime.Seconds() > 0 {
		totalDataSize := dataSizeBytes * int64(actualCount) * int64(t.config.FileCount)
		result.WriteSpeedMBps = float64(totalDataSize) / (1024 * 1024) / result.WriteDataTime.Seconds()
	}

	if result.TotalTestTime.Seconds() > 0 {
		result.CommitQPS = float64(result.SuccessfulCommits) / result.TotalTestTime.Seconds()
	}

	// 打印汇总
	log.Printf("\n========== Commit 测试完成 ==========")
	log.Printf("总测试数: %d", result.TotalDevboxes)
	log.Printf("成功 Commit: %d (%.1f%%)", result.SuccessfulCommits,
		float64(result.SuccessfulCommits)/float64(result.TotalDevboxes)*100)
	log.Printf("失败 Commit: %d", result.FailedCommits)
	log.Printf("平均写入耗时: %v (速度: %.2f MB/s)", result.WriteDataTime, result.WriteSpeedMBps)
	log.Printf("平均 Commit 耗时: %v", result.CommitTime)
	if t.config.VerifyData {
		log.Printf("平均验证耗时: %v", result.VerifyTime)
	}
	log.Printf("总测试耗时: %v (QPS: %.2f)", result.TotalTestTime, result.CommitQPS)

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

// testDevboxesFullLifecycle tests full lifecycle for each Devbox concurrently
func (t *DevboxCommitTester) testDevboxesFullLifecycle(ctx context.Context, devboxes []devboxv1alpha2.Devbox) []CommitTestDetail {
	var wg sync.WaitGroup
	var mu sync.Mutex
	details := make([]CommitTestDetail, 0, len(devboxes))
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(index int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("[%d/%d] 开始测试 Devbox: %s", index+1, len(devboxes), db.Name)

			// 测试单个 Devbox 的完整流程
			detail := t.testSingleDevboxFullLifecycle(ctx, db, index+1, len(devboxes))

			mu.Lock()
			details = append(details, detail)
			mu.Unlock()

			if detail.Error == "" {
				log.Printf("[%d/%d] ✓ Devbox 测试成功: %s (总耗时: %v)",
					index+1, len(devboxes), db.Name, detail.TotalDuration)
			} else {
				log.Printf("[%d/%d] ✗ Devbox 测试失败: %s - %s",
					index+1, len(devboxes), db.Name, detail.Error)
			}
		}(i, devbox)
	}

	wg.Wait()
	return details
}

// testSingleDevboxFullLifecycle tests complete lifecycle for a single Devbox
func (t *DevboxCommitTester) testSingleDevboxFullLifecycle(ctx context.Context, devbox devboxv1alpha2.Devbox, index, total int) CommitTestDetail {
	detail := CommitTestDetail{
		DevboxName: devbox.Name,
	}
	testStart := time.Now()

	// 阶段 1: 写入测试数据
	log.Printf("[%d/%d] 写入数据: %s", index, total, devbox.Name)
	writeStart := time.Now()
	if err := t.helper.WriteTestDataToDevbox(ctx, devbox, "test_commit_data", t.config.DataSize, t.config.FileCount); err != nil {
		detail.Error = fmt.Sprintf("写入数据失败: %v", err)
		detail.TotalDuration = time.Since(testStart)
		return detail
	}
	detail.WriteDuration = time.Since(writeStart)
	detail.WriteSuccess = true
	log.Printf("[%d/%d] ✓ 数据写入完成: %s (耗时: %v)", index, total, devbox.Name, detail.WriteDuration)

	// 阶段 2: 同步文件系统
	log.Printf("[%d/%d] 同步文件系统: %s", index, total, devbox.Name)
	syncCmd := []string{"sync"}
	if err := t.helper.ExecCommandInPod(ctx, devbox.Namespace, devbox.Name, devbox.Name, syncCmd); err != nil {
		log.Printf("[%d/%d] ⚠ 同步文件系统失败: %s - %v (继续执行)", index, total, devbox.Name, err)
	}
	time.Sleep(3 * time.Second)

	// 阶段 3: 触发 Commit
	log.Printf("[%d/%d] 触发 Commit (状态: %s): %s", index, total, t.config.TargetState, devbox.Name)
	commitStart := time.Now()
	if err := t.changeDevboxState(ctx, devbox.Name, t.config.TargetState); err != nil {
		detail.Error = fmt.Sprintf("触发 Commit 失败: %v", err)
		detail.TotalDuration = time.Since(testStart)
		return detail
	}

	// 阶段 4: 等待 Commit 完成
	log.Printf("[%d/%d] 等待 Commit 完成: %s", index, total, devbox.Name)
	targetState := t.getTargetDevboxState()
	if err := t.helper.WaitForDevboxState(ctx, t.config.Namespace, devbox.Name, targetState, t.config.CommitTimeout); err != nil {
		detail.Error = fmt.Sprintf("等待 Commit 超时: %v", err)
		detail.CommitDuration = time.Since(commitStart)
		detail.TotalDuration = time.Since(testStart)
		return detail
	}
	detail.CommitDuration = time.Since(commitStart)
	detail.CommitSuccess = true
	log.Printf("[%d/%d] ✓ Commit 完成: %s (耗时: %v)", index, total, devbox.Name, detail.CommitDuration)

	// 阶段 5: 验证数据（可选）
	if t.config.VerifyData {
		// 恢复 Running 状态
		log.Printf("[%d/%d] 恢复 Running 状态: %s", index, total, devbox.Name)
		if err := t.changeDevboxState(ctx, devbox.Name, "Running"); err != nil {
			detail.Error = fmt.Sprintf("恢复 Running 失败: %v", err)
			detail.TotalDuration = time.Since(testStart)
			return detail
		}

		// 等待 Running 就绪（包含 Pod 就绪）
		log.Printf("[%d/%d] 等待 Running 就绪: %s", index, total, devbox.Name)
		if _, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, devbox.Name, 5*time.Minute); err != nil {
			detail.Error = fmt.Sprintf("等待 Running 超时: %v", err)
			detail.TotalDuration = time.Since(testStart)
			return detail
		}

		// 验证数据完整性
		log.Printf("[%d/%d] 验证数据完整性: %s", index, total, devbox.Name)
		verifyStart := time.Now()
		// 重新获取 Devbox 对象（状态已变化）
		updatedDevbox := &devboxv1alpha2.Devbox{}
		if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: devbox.Namespace, Name: devbox.Name}, updatedDevbox); err != nil {
			detail.Error = fmt.Sprintf("获取 Devbox 失败: %v", err)
			detail.TotalDuration = time.Since(testStart)
			return detail
		}

		if err := t.helper.VerifyTestDataInDevbox(ctx, *updatedDevbox, "test_commit_data"); err != nil {
			detail.Error = fmt.Sprintf("数据验证失败: %v", err)
			detail.VerifyDuration = time.Since(verifyStart)
			detail.TotalDuration = time.Since(testStart)
			return detail
		}
		detail.VerifyDuration = time.Since(verifyStart)
		detail.VerifySuccess = true
		log.Printf("[%d/%d] ✓ 数据验证成功: %s (耗时: %v)", index, total, devbox.Name, detail.VerifyDuration)
	}

	detail.TotalDuration = time.Since(testStart)
	return detail
}

// findRunningDevboxes 查找运行中的 Devbox
func (t *DevboxCommitTester) findRunningDevboxes(ctx context.Context) ([]devboxv1alpha2.Devbox, error) {
	devboxList := &devboxv1alpha2.DevboxList{}
	err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace))
	if err != nil {
		return nil, fmt.Errorf("列出 Devbox 失败: %w", err)
	}

	var runningDevboxes []devboxv1alpha2.Devbox
	for _, devbox := range devboxList.Items {
		if devbox.Spec.State == devboxv1alpha2.DevboxStateRunning {
			runningDevboxes = append(runningDevboxes, devbox)
			if len(runningDevboxes) >= t.config.DevboxCount {
				break
			}
		}
	}

	return runningDevboxes, nil
}

// changeDevboxState 修改 Devbox 状态
func (t *DevboxCommitTester) changeDevboxState(ctx context.Context, name string, targetState string) error {
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: t.config.Namespace,
		Name:      name,
	}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	devbox.Spec.State = devboxv1alpha2.DevboxState(targetState)
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("更新 Devbox 状态失败: %w", err)
	}

	return nil
}

// getTargetDevboxState 获取目标 DevboxState
func (t *DevboxCommitTester) getTargetDevboxState() devboxv1alpha2.DevboxState {
	return devboxv1alpha2.DevboxState(t.config.TargetState)
}
