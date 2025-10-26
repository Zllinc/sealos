package tester

import (
	"context"
	"fmt"
	"log"
	"strings"
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

	// 步骤 2: 并发写入测试数据
	log.Printf("\n=== 步骤 2: 写入测试数据 (并发数: %d, 文件数: %d) ===", t.config.ConcurrentCount, t.config.FileCount)
	writeStart := time.Now()
	if err := t.writeTestDataConcurrently(ctx, devboxes); err != nil {
		return nil, fmt.Errorf("写入测试数据失败: %w", err)
	}
	result.WriteDataTime = time.Since(writeStart)

	// 计算写入速度
	dataSizeBytes, _ := parseDataSize(t.config.DataSize)
	totalDataSize := dataSizeBytes * int64(actualCount) * int64(t.config.FileCount)
	result.WriteSpeedMBps = float64(totalDataSize) / (1024 * 1024) / result.WriteDataTime.Seconds()

	log.Printf("数据写入完成，耗时: %v, 写入速度: %.2f MB/s",
		result.WriteDataTime, result.WriteSpeedMBps)

	// 步骤 3: 并发触发 Commit
	log.Printf("\n=== 步骤 3: 触发 Commit (修改状态为 %s) ===", t.config.TargetState)
	commitStart := time.Now()
	if err := t.triggerCommitConcurrently(ctx, devboxes); err != nil {
		return nil, fmt.Errorf("触发 Commit 失败: %w", err)
	}

	// 步骤 4: 等待 Commit 完成
	log.Printf("\n=== 步骤 4: 等待 Commit 完成 ===")
	commitResults := t.waitForCommitsConcurrently(ctx, devboxes)
	result.CommitTime = time.Since(commitStart)

	// 统计结果
	for _, detail := range commitResults {
		result.Details = append(result.Details, detail)
		if detail.CommitSuccess {
			result.SuccessfulCommits++
		} else {
			result.FailedCommits++
			if detail.Error != "" {
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %s", detail.DevboxName, detail.Error))
			}
		}
	}

	// 步骤 5: 验证数据（可选）
	if t.config.VerifyData {
		log.Printf("\n=== 步骤 5: 恢复 Running 并验证数据 ===")
		verifyStart := time.Now()
		if err := t.verifyDataAfterCommit(ctx, devboxes, &result.Details); err != nil {
			log.Printf("数据验证失败: %v", err)
		}
		result.VerifyTime = time.Since(verifyStart)
	}

	result.TotalTestTime = time.Since(startTime)
	if result.CommitTime.Seconds() > 0 {
		result.CommitQPS = float64(result.SuccessfulCommits) / result.CommitTime.Seconds()
	}

	// 打印汇总
	log.Printf("\n========== Commit 测试完成 ==========")
	log.Printf("总测试数: %d", result.TotalDevboxes)
	log.Printf("成功 Commit: %d (%.1f%%)", result.SuccessfulCommits,
		float64(result.SuccessfulCommits)/float64(result.TotalDevboxes)*100)
	log.Printf("失败 Commit: %d", result.FailedCommits)
	log.Printf("写入数据耗时: %v (速度: %.2f MB/s)", result.WriteDataTime, result.WriteSpeedMBps)
	log.Printf("Commit 耗时: %v (QPS: %.2f)", result.CommitTime, result.CommitQPS)
	if t.config.VerifyData {
		log.Printf("验证耗时: %v", result.VerifyTime)
	}
	log.Printf("总耗时: %v", result.TotalTestTime)

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

// writeTestDataConcurrently 并发写入测试数据
func (t *DevboxCommitTester) writeTestDataConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(index int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("[%d/%d] 写入数据到 Devbox: %s", index+1, len(devboxes), db.Name)

			// 使用通用方法写入数据
			if err := t.helper.WriteTestDataToDevbox(ctx, db, "test_commit_data", t.config.DataSize, t.config.FileCount); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", db.Name, err))
				mu.Unlock()
				log.Printf("[%d/%d] 写入数据失败: %s - %v", index+1, len(devboxes), db.Name, err)
				return
			}

			log.Printf("[%d/%d] ✓ 数据写入成功: %s", index+1, len(devboxes), db.Name)
		}(i, devbox)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("写入数据失败: %s", strings.Join(errors, "; "))
	}

	return nil
}

// triggerCommitConcurrently 并发触发 Commit
func (t *DevboxCommitTester) triggerCommitConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(index int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("[%d/%d] 触发 Commit: %s", index+1, len(devboxes), db.Name)

			// 修改状态触发 Commit
			if err := t.changeDevboxState(ctx, db.Name, t.config.TargetState); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", db.Name, err))
				mu.Unlock()
				log.Printf("[%d/%d] 触发 Commit 失败: %s - %v", index+1, len(devboxes), db.Name, err)
				return
			}

			log.Printf("[%d/%d] ✓ Commit 已触发: %s", index+1, len(devboxes), db.Name)
		}(i, devbox)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("触发 Commit 失败: %s", strings.Join(errors, "; "))
	}

	return nil
}

// waitForCommitsConcurrently 并发等待 Commit 完成
func (t *DevboxCommitTester) waitForCommitsConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox) []CommitTestDetail {
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

			detail := CommitTestDetail{
				DevboxName:   db.Name,
				WriteSuccess: true, // 写入阶段已完成
			}

			commitStart := time.Now()
			log.Printf("[%d/%d] 等待 Commit 完成: %s", index+1, len(devboxes), db.Name)

			// 等待状态变为目标状态
			targetState := t.getTargetDevboxState()
			err := t.helper.WaitForDevboxState(ctx, t.config.Namespace, db.Name, targetState, t.config.CommitTimeout)
			detail.CommitDuration = time.Since(commitStart)

			if err != nil {
				detail.Error = fmt.Sprintf("等待 Commit 超时: %v", err)
				detail.CommitSuccess = false
				log.Printf("[%d/%d] ✗ Commit 超时: %s", index+1, len(devboxes), db.Name)
			} else {
				detail.CommitSuccess = true
				log.Printf("[%d/%d] ✓ Commit 完成: %s (耗时: %v)", index+1, len(devboxes), db.Name, detail.CommitDuration)
			}

			detail.TotalDuration = detail.CommitDuration

			mu.Lock()
			details = append(details, detail)
			mu.Unlock()
		}(i, devbox)
	}

	wg.Wait()
	return details
}

// verifyDataAfterCommit 验证 Commit 后的数据（恢复 Running 并验证）
func (t *DevboxCommitTester) verifyDataAfterCommit(ctx context.Context, devboxes []devboxv1alpha2.Devbox, details *[]CommitTestDetail) error {
	// 步骤 1: 恢复到 Running 状态
	log.Printf("恢复所有 Devbox 到 Running 状态...")
	if err := t.restoreToRunningConcurrently(ctx, devboxes); err != nil {
		return fmt.Errorf("恢复 Running 失败: %w", err)
	}

	// 步骤 2: 验证数据
	log.Printf("验证数据完整性...")
	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := range devboxes {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devbox := devboxes[index]
			verifyStart := time.Now()

			log.Printf("[%d/%d] 验证数据: %s", index+1, len(devboxes), devbox.Name)

			// 使用通用方法验证数据
			err := t.helper.VerifyTestDataInDevbox(ctx, devbox, "test_commit_data")

			mu.Lock()
			// 找到对应的 detail 并更新
			for j := range *details {
				if (*details)[j].DevboxName == devbox.Name {
					(*details)[j].VerifyDuration = time.Since(verifyStart)
					(*details)[j].TotalDuration += (*details)[j].VerifyDuration
					if err != nil {
						(*details)[j].VerifySuccess = false
						(*details)[j].Error += fmt.Sprintf("; 验证失败: %v", err)
						log.Printf("[%d/%d] ✗ 数据验证失败: %s", index+1, len(devboxes), devbox.Name)
					} else {
						(*details)[j].VerifySuccess = true
						log.Printf("[%d/%d] ✓ 数据验证成功: %s", index+1, len(devboxes), devbox.Name)
					}
					break
				}
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	return nil
}

// restoreToRunningConcurrently 并发恢复到 Running 状态
func (t *DevboxCommitTester) restoreToRunningConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	// 步骤 1: 修改状态为 Running
	for i, devbox := range devboxes {
		wg.Add(1)
		go func(index int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := t.changeDevboxState(ctx, db.Name, "Running"); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", db.Name, err))
				mu.Unlock()
			}
		}(i, devbox)
	}
	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("修改状态失败: %s", strings.Join(errors, "; "))
	}

	// 步骤 2: 等待所有 Devbox Running（包含资源就绪）
	errors = []string{}
	for i, devbox := range devboxes {
		wg.Add(1)
		go func(index int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("[%d/%d] 等待 Running: %s", index+1, len(devboxes), db.Name)
			_, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, db.Name, 5*time.Minute)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", db.Name, err))
				mu.Unlock()
				log.Printf("[%d/%d] ✗ 等待 Running 超时: %s", index+1, len(devboxes), db.Name)
			} else {
				log.Printf("[%d/%d] ✓ Running 就绪: %s", index+1, len(devboxes), db.Name)
			}
		}(i, devbox)
	}
	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("等待 Running 失败: %s", strings.Join(errors, "; "))
	}

	return nil
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
