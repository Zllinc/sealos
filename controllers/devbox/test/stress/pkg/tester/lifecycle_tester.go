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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewDevboxLifecycleTester creates a new lifecycle tester
func NewDevboxLifecycleTester(config *LifecycleTestConfig) (*DevboxLifecycleTester, error) {
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

	// 配置超时和 QPS，避免在高负载下超时
	restConfig.Timeout = 2 * time.Minute // API 请求总超时
	restConfig.QPS = 100                 // 增加 QPS 限制
	restConfig.Burst = 200               // 增加 Burst 限制

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

	helper := NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)

	return &DevboxLifecycleTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunLifecycleTest runs sequential lifecycle test
func (t *DevboxLifecycleTester) RunLifecycleTest(ctx context.Context) (*LifecycleTestResult, error) {
	log.Printf("开始顺序生命周期测试: 测试 %d 个 Devbox", t.config.DevboxCount)

	result := &LifecycleTestResult{
		TestDetails: make([]LifecycleTestDetail, 0, t.config.DevboxCount),
	}
	startTime := time.Now()

	// 顺序测试每个 Devbox
	for i := 0; i < t.config.DevboxCount; i++ {
		devboxName := fmt.Sprintf("lifecycle-test-%d", i)
		log.Printf("\n========== 开始测试 Devbox %s (%d/%d) ==========", devboxName, i+1, t.config.DevboxCount)

		detail, err := t.testSingleDevboxLifecycle(ctx, devboxName)
		result.TestDetails = append(result.TestDetails, *detail)
		result.TotalTests++

		if err != nil {
			result.FailedTests++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %v", devboxName, err))
			log.Printf("Devbox %s 测试失败: %v", devboxName, err)
		} else {
			result.SuccessfulTests++
			log.Printf("Devbox %s 测试成功! 耗时: %v", devboxName, detail.TotalDuration)
		}

		// 更新各阶段统计
		if detail.ResourceCheckOK {
			result.ResourceCheckPassed++
		}
		if detail.StoppedCommitOK {
			result.StoppedCommitPassed++
		}
		if detail.ShutdownCommitOK {
			result.ShutdownCommitPassed++
		}
		if detail.ReleaseOK {
			result.ReleasePassed++
		}

		// 检查超时
		if time.Since(startTime) > t.config.TestTimeout {
			log.Printf("测试超时，停止后续测试")
			break
		}

		log.Printf("========== Devbox %s 测试完成 ==========\n", devboxName)
	}

	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulTests > 0 {
		result.AverageTestTime = result.TotalTestTime / time.Duration(result.SuccessfulTests)
	}

	log.Printf("\n顺序生命周期测试完成: 成功 %d/%d, 总耗时 %v",
		result.SuccessfulTests, result.TotalTests, result.TotalTestTime)

	return result, nil
}

// RunConcurrentLifecycleTest runs concurrent lifecycle test
func (t *DevboxLifecycleTester) RunConcurrentLifecycleTest(ctx context.Context) (*LifecycleTestResult, error) {
	log.Printf("开始并发生命周期测试: %d 个 Devbox，并发级别 %d", t.config.DevboxCount, t.config.ConcurrentCount)

	result := &LifecycleTestResult{
		TestDetails: make([]LifecycleTestDetail, 0, t.config.DevboxCount),
	}
	startTime := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	// 并发测试多个 Devbox
	for i := 0; i < t.config.DevboxCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devboxName := fmt.Sprintf("lifecycle-test-%d", index)
			log.Printf("[%s] 开始测试 (%d/%d)", devboxName, index+1, t.config.DevboxCount)

			detail, err := t.testSingleDevboxLifecycle(ctx, devboxName)

			mu.Lock()
			defer mu.Unlock()

			result.TestDetails = append(result.TestDetails, *detail)
			result.TotalTests++

			if err != nil {
				result.FailedTests++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %v", devboxName, err))
				log.Printf("[%s] 测试失败: %v", devboxName, err)
			} else {
				result.SuccessfulTests++
				log.Printf("[%s] 测试成功! 耗时: %v", devboxName, detail.TotalDuration)
			}

			// 更新各阶段统计
			if detail.ResourceCheckOK {
				result.ResourceCheckPassed++
			}
			if detail.StoppedCommitOK {
				result.StoppedCommitPassed++
			}
			if detail.ShutdownCommitOK {
				result.ShutdownCommitPassed++
			}
			if detail.ReleaseOK {
				result.ReleasePassed++
			}
		}(i)
	}

	wg.Wait()
	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulTests > 0 {
		result.AverageTestTime = result.TotalTestTime / time.Duration(result.SuccessfulTests)
	}

	log.Printf("\n并发生命周期测试完成: 成功 %d/%d, 总耗时 %v",
		result.SuccessfulTests, result.TotalTests, result.TotalTestTime)

	return result, nil
}

// testSingleDevboxLifecycle 测试单个 Devbox 的完整生命周期
func (t *DevboxLifecycleTester) testSingleDevboxLifecycle(ctx context.Context, name string) (*LifecycleTestDetail, error) {
	detail := &LifecycleTestDetail{
		DevboxName: name,
	}
	startTime := time.Now()

	// 阶段 1: 创建 Devbox 并验证资源
	log.Printf("[%s] 阶段 1: 创建 Devbox 并验证资源", name)
	if err := t.phase1_CreateAndVerifyResources(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("阶段1失败: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail, err
	}
	detail.ResourceCheckOK = true
	log.Printf("[%s] ✓ 阶段 1 完成: 资源验证通过", name)

	// 阶段 2: Stopped Commit 测试
	log.Printf("[%s] 阶段 2: Stopped Commit 测试", name)
	if err := t.phase2_StoppedCommitAndVerify(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("阶段2失败: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail, err
	}
	detail.StoppedCommitOK = true
	detail.Data1VerifyOK = true
	log.Printf("[%s] ✓ 阶段 2 完成: Stopped Commit 成功", name)

	// 阶段 3: Shutdown Commit 测试
	log.Printf("[%s] 阶段 3: Shutdown Commit 测试", name)
	if err := t.phase3_ShutdownCommitAndVerify(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("阶段3失败: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail, err
	}
	detail.ShutdownCommitOK = true
	detail.Data2VerifyOK = true
	log.Printf("[%s] ✓ 阶段 3 完成: Shutdown Commit 成功", name)

	// 阶段 3.5: 停止 Devbox 准备发版（发版前必须是 Stopped 状态）
	log.Printf("[%s] 阶段 3.5: 停止 Devbox 准备发版", name)
	if err := t.phasePreRelease_StopDevbox(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("阶段3.5失败: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail, err
	}
	log.Printf("[%s] ✓ 阶段 3.5 完成: Devbox 已停止，准备发版", name)

	// 阶段 4: 发版测试
	log.Printf("[%s] 阶段 4: 发版测试", name)
	if err := t.phase4_ReleaseAndVerify(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("阶段4失败: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail, err
	}
	detail.ReleaseOK = true
	log.Printf("[%s] ✓ 阶段 4 完成: 发版成功", name)

	detail.TotalDuration = time.Since(startTime)
	return detail, nil
}

// phase1_CreateAndVerifyResources 阶段1: 创建 Devbox 并验证资源
func (t *DevboxLifecycleTester) phase1_CreateAndVerifyResources(ctx context.Context, name string) error {
	// 创建 Devbox
	spec := DevboxCreateSpec{
		Name:      name,
		Namespace: t.config.Namespace,
		Labels: map[string]string{
			"stress-test": "true",
			"test-type":   "lifecycle",
		},
		Image:        t.config.Image,
		CPU:          t.config.CPU,
		Memory:       t.config.Memory,
		StorageLimit: t.config.StorageLimit,
	}

	if err := t.helper.CreateDevbox(ctx, spec); err != nil {
		return fmt.Errorf("创建 Devbox 失败: %w", err)
	}
	log.Printf("[%s] Devbox 创建成功", name)

	// 等待 Devbox 运行就绪
	if err := t.waitForDevboxRunning(ctx, name, 5*time.Minute); err != nil {
		return fmt.Errorf("等待 Devbox 运行超时: %w", err)
	}

	// 验证所有资源
	if err := t.verifyAllResources(ctx, name); err != nil {
		return fmt.Errorf("资源验证失败: %w", err)
	}

	return nil
}

// phase2_StoppedCommitAndVerify 阶段2: Stopped Commit 测试
func (t *DevboxLifecycleTester) phase2_StoppedCommitAndVerify(ctx context.Context, name string) error {
	// 获取 Devbox
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	// 写入第一批测试数据
	log.Printf("[%s] 写入第一批测试数据 (目录: test_data_phase1)", name)
	if err := t.writeDataToDirectory(ctx, *devbox, "test_data_phase1", t.config.Data1Size, t.config.FileCount); err != nil {
		return fmt.Errorf("写入数据失败: %w", err)
	}

	// 强制同步文件系统，确保数据写入磁盘
	log.Printf("[%s] 同步文件系统，确保数据持久化", name)
	syncCmd := []string{"sync"}
	if err := t.execCommandInPod(ctx, devbox.Namespace, devbox.Name, devbox.Name, syncCmd); err != nil {
		log.Printf("[%s] ⚠ 同步文件系统失败: %v (继续执行)", name, err)
	}
	// 等待一小段时间，确保同步完成
	time.Sleep(3 * time.Second)

	// 修改状态为 Stopped 触发 commit
	log.Printf("[%s] 修改状态为 Stopped 触发 commit", name)
	devbox.Spec.State = devboxv1alpha2.DevboxStateStopped
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("修改状态失败: %w", err)
	}

	// 等待 commit 完成
	if err := t.waitForCommitComplete(ctx, name, devboxv1alpha2.DevboxStateStopped, 10*time.Minute); err != nil {
		return fmt.Errorf("等待 commit 完成超时: %w", err)
	}

	// 恢复 Running 状态
	log.Printf("[%s] 恢复 Running 状态", name)
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}
	devbox.Spec.State = devboxv1alpha2.DevboxStateRunning
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("恢复状态失败: %w", err)
	}

	// 等待恢复运行
	if err := t.waitForDevboxRunning(ctx, name, 5*time.Minute); err != nil {
		return fmt.Errorf("等待 Devbox 恢复运行超时: %w", err)
	}

	// 验证第一批数据
	if t.config.VerifyData {
		log.Printf("[%s] 验证第一批数据", name)
		if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
			return fmt.Errorf("获取 Devbox 失败: %w", err)
		}
		if err := t.verifyDataInDirectory(ctx, *devbox, "test_data_phase1"); err != nil {
			return fmt.Errorf("数据验证失败: %w", err)
		}
	}

	return nil
}

// phase3_ShutdownCommitAndVerify 阶段3: Shutdown Commit 测试
func (t *DevboxLifecycleTester) phase3_ShutdownCommitAndVerify(ctx context.Context, name string) error {
	// 获取 Devbox
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	// 写入第二批测试数据
	log.Printf("[%s] 写入第二批测试数据 (目录: test_data_phase2)", name)
	if err := t.writeDataToDirectory(ctx, *devbox, "test_data_phase2", t.config.Data2Size, t.config.FileCount); err != nil {
		return fmt.Errorf("写入数据失败: %w", err)
	}

	// 强制同步文件系统，确保数据写入磁盘（非常重要！）
	log.Printf("[%s] 同步文件系统，确保数据持久化到 LVM", name)
	syncCmd := []string{"sync"}
	if err := t.execCommandInPod(ctx, devbox.Namespace, devbox.Name, devbox.Name, syncCmd); err != nil {
		log.Printf("[%s] ⚠ 同步文件系统失败: %v (继续执行)", name, err)
	}
	// 等待一小段时间，确保同步完成并让 I/O 缓冲区刷新
	time.Sleep(3 * time.Second)

	// 修改状态为 Shutdown 触发 commit
	log.Printf("[%s] 修改状态为 Shutdown 触发 commit", name)
	devbox.Spec.State = devboxv1alpha2.DevboxStateShutdown
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("修改状态失败: %w", err)
	}

	// 等待 commit 完成
	if err := t.waitForCommitComplete(ctx, name, devboxv1alpha2.DevboxStateShutdown, 10*time.Minute); err != nil {
		return fmt.Errorf("等待 commit 完成超时: %w", err)
	}

	// 恢复 Running 状态
	log.Printf("[%s] 恢复 Running 状态", name)
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}
	devbox.Spec.State = devboxv1alpha2.DevboxStateRunning
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("恢复状态失败: %w", err)
	}

	// 等待恢复运行
	if err := t.waitForDevboxRunning(ctx, name, 5*time.Minute); err != nil {
		return fmt.Errorf("等待 Devbox 恢复运行超时: %w", err)
	}

	// 验证两批数据都存在
	if t.config.VerifyData {
		log.Printf("[%s] 验证两批数据", name)
		if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
			return fmt.Errorf("获取 Devbox 失败: %w", err)
		}
		if err := t.verifyDataInDirectory(ctx, *devbox, "test_data_phase1"); err != nil {
			return fmt.Errorf("第一批数据验证失败: %w", err)
		}
		if err := t.verifyDataInDirectory(ctx, *devbox, "test_data_phase2"); err != nil {
			return fmt.Errorf("第二批数据验证失败: %w", err)
		}
	}

	return nil
}

// phasePreRelease_StopDevbox 阶段3.5: 停止 Devbox 准备发版
func (t *DevboxLifecycleTester) phasePreRelease_StopDevbox(ctx context.Context, name string) error {
	// 获取 Devbox
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	// 修改状态为 Stopped（发版要求）
	log.Printf("[%s] 将 Devbox 状态设置为 Stopped（发版前要求）", name)
	devbox.Spec.State = devboxv1alpha2.DevboxStateStopped
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("修改状态失败: %w", err)
	}

	// 等待状态变更完成
	if err := t.waitForDevboxState(ctx, name, devboxv1alpha2.DevboxStateStopped, 5*time.Minute); err != nil {
		return fmt.Errorf("等待 Devbox 停止超时: %w", err)
	}

	log.Printf("[%s] Devbox 已成功停止，可以进行发版", name)
	return nil
}

// phase4_ReleaseAndVerify 阶段4: 发版测试
func (t *DevboxLifecycleTester) phase4_ReleaseAndVerify(ctx context.Context, name string) error {
	releaseName := fmt.Sprintf("%s-release", name)
	version := fmt.Sprintf(t.config.ReleaseVersionPattern, 1)

	// 创建 DevBoxRelease
	release := &devboxv1alpha2.DevBoxRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: t.config.Namespace,
			Labels: map[string]string{
				"stress-test": "true",
				"test-type":   "lifecycle",
			},
		},
		Spec: devboxv1alpha2.DevBoxReleaseSpec{
			DevboxName:              name,
			Version:                 version,
			StartDevboxAfterRelease: false,
			Notes:                   fmt.Sprintf("Lifecycle test release %s", version),
		},
	}

	log.Printf("[%s] 创建 DevBoxRelease: %s (版本: %s)", name, releaseName, version)
	if err := t.ctrlClient.Create(ctx, release); err != nil {
		return fmt.Errorf("创建 DevBoxRelease 失败: %w", err)
	}

	// 等待发版完成
	phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("等待发版完成超时: %w", err)
	}

	if phase != devboxv1alpha2.DevBoxReleasePhaseSuccess {
		return fmt.Errorf("发版失败，状态: %s", phase)
	}

	log.Printf("[%s] 发版成功: %s", name, releaseName)
	return nil
}

// waitForDevboxRunning 等待 Devbox 运行就绪
func (t *DevboxLifecycleTester) waitForDevboxRunning(ctx context.Context, name string, timeout time.Duration) error {
	_, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, timeout)
	return err
}

// waitForDevboxState 等待 Devbox 达到指定状态
func (t *DevboxLifecycleTester) waitForDevboxState(ctx context.Context, name string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	return t.helper.WaitForDevboxState(ctx, t.config.Namespace, name, targetState, timeout)
}

// waitForCommitComplete 等待 commit 完成
func (t *DevboxLifecycleTester) waitForCommitComplete(ctx context.Context, name string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	return t.helper.WaitForCommitComplete(ctx, t.config.Namespace, name, targetState, timeout)
}

// waitForReleaseComplete 等待发版完成
func (t *DevboxLifecycleTester) waitForReleaseComplete(ctx context.Context, name string, timeout time.Duration) (devboxv1alpha2.DevBoxReleasePhase, error) {
	return t.helper.WaitForReleaseComplete(ctx, t.config.Namespace, name, timeout)
}

// verifyAllResources 验证所有资源是否创建
func (t *DevboxLifecycleTester) verifyAllResources(ctx context.Context, name string) error {
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	// 检查 Secret
	if !t.isSecretCreated(ctx, *devbox) {
		return fmt.Errorf("Secret 未创建")
	}
	log.Printf("[%s] ✓ Secret 已创建", name)

	// 检查 Service
	if !t.isServiceCreated(ctx, *devbox) {
		return fmt.Errorf("Service 未创建")
	}
	log.Printf("[%s] ✓ Service 已创建", name)

	// 检查 Pod
	if !t.isPodRunning(ctx, *devbox) {
		return fmt.Errorf("Pod 未运行")
	}
	log.Printf("[%s] ✓ Pod 正在运行", name)

	// 检查 LV
	if !t.isLVMCreated(ctx, *devbox) {
		log.Printf("[%s] ⚠ LVM 逻辑卷未找到（可能正常）", name)
	} else {
		log.Printf("[%s] ✓ LVM 逻辑卷已创建", name)
	}

	return nil
}

// writeDataToDirectory 向指定目录写入测试数据
func (t *DevboxLifecycleTester) writeDataToDirectory(ctx context.Context, devbox devboxv1alpha2.Devbox, directory string, dataSize string, fileCount int) error {
	return t.helper.WriteTestDataToDevbox(ctx, devbox, directory, dataSize, fileCount)
}

// verifyDataInDirectory 验证指定目录的数据
func (t *DevboxLifecycleTester) verifyDataInDirectory(ctx context.Context, devbox devboxv1alpha2.Devbox, directory string) error {
	return t.helper.VerifyTestDataInDevbox(ctx, devbox, directory)
}

// execCommandInPod 在 Pod 中执行命令
func (t *DevboxLifecycleTester) execCommandInPod(ctx context.Context, namespace, podName, containerName string, cmd []string) error {
	return t.helper.ExecCommandInPod(ctx, namespace, podName, containerName, cmd)
}

// isPodRunning 检查 Pod 是否运行
func (t *DevboxLifecycleTester) isPodRunning(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsPodRunning(ctx, devbox)
}

// isServiceCreated 检查 Service 是否创建
func (t *DevboxLifecycleTester) isServiceCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsServiceCreated(ctx, devbox)
}

// isSecretCreated 检查 Secret 是否创建
func (t *DevboxLifecycleTester) isSecretCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsSecretCreated(ctx, devbox)
}

// isLVMCreated 检查 LVM 逻辑卷是否创建
func (t *DevboxLifecycleTester) isLVMCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsLVMCreated(ctx, devbox)
}

// Cleanup cleans up test resources
func (t *DevboxLifecycleTester) Cleanup(ctx context.Context) error {
	log.Printf("开始清理生命周期测试资源...")

	// 删除所有 DevBoxRelease
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}
	if err := t.ctrlClient.List(ctx, releaseList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("列出 DevBoxRelease 失败: %v", err)
	} else {
		for _, release := range releaseList.Items {
			if t.isLifecycleTestRelease(release) {
				if err := t.ctrlClient.Delete(ctx, &release); err != nil {
					log.Printf("删除 DevBoxRelease %s 失败: %v", release.Name, err)
				} else {
					log.Printf("已删除 DevBoxRelease: %s", release.Name)
				}
			}
		}
	}

	// 删除所有 Devbox
	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("列出 Devbox 失败: %v", err)
	} else {
		for _, devbox := range devboxList.Items {
			if t.isLifecycleTestDevbox(devbox) {
				if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
					log.Printf("删除 Devbox %s 失败: %v", devbox.Name, err)
				} else {
					log.Printf("已删除 Devbox: %s", devbox.Name)
				}
			}
		}
	}

	log.Printf("清理完成")
	return nil
}

// isLifecycleTestRelease 判断是否为生命周期测试的 Release
func (t *DevboxLifecycleTester) isLifecycleTestRelease(release devboxv1alpha2.DevBoxRelease) bool {
	if release.Labels != nil {
		if testType, ok := release.Labels["test-type"]; ok && testType == "lifecycle" {
			return true
		}
	}
	return strings.Contains(release.Name, "lifecycle-test-")
}

// isLifecycleTestDevbox 判断是否为生命周期测试的 Devbox
func (t *DevboxLifecycleTester) isLifecycleTestDevbox(devbox devboxv1alpha2.Devbox) bool {
	if devbox.Labels != nil {
		if testType, ok := devbox.Labels["test-type"]; ok && testType == "lifecycle" {
			return true
		}
	}
	return strings.Contains(devbox.Name, "lifecycle-test-")
}
