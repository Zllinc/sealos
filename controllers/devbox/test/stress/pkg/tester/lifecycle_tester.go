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

// phase2_StoppedCommitAndVerify phase2: Stopped Commit test
func (t *DevboxLifecycleTester) phase2_StoppedCommitAndVerify(ctx context.Context, name string) error {
	// get Devbox
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("get Devbox failed: %w", err)
	}

	// write first batch of test data
	log.Printf("[%s] write first batch of test data (directory: test_data_phase1)", name)
	if err := t.writeDataToDirectory(ctx, *devbox, "test_data_phase1", t.config.Data1Size, t.config.FileCount); err != nil {
		return fmt.Errorf("write data failed: %w", err)
	}

	// force sync file system, ensure data written to disk
	log.Printf("[%s] sync file system, ensure data persisted", name)
	syncCmd := []string{"sync"}
	if err := t.execCommandInPod(ctx, devbox.Namespace, devbox.Name, devbox.Name, syncCmd); err != nil {
		log.Printf("[%s] ⚠ sync file system failed: %v (continue execution)", name, err)
	}
	// wait for a short period, ensure sync completed
	time.Sleep(3 * time.Second)

	// modify state to Stopped to trigger commit
	log.Printf("[%s] modify state to Stopped to trigger commit", name)
	devbox.Spec.State = devboxv1alpha2.DevboxStateStopped
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("modify state failed: %w", err)
	}

	// wait for commit complete
	if err := t.waitForCommitComplete(ctx, name, devboxv1alpha2.DevboxStateStopped, 10*time.Minute); err != nil {
		return fmt.Errorf("wait for commit complete timeout: %w", err)
	}

	// restore Running state
	log.Printf("[%s] restore Running state", name)
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("get Devbox failed: %w", err)
	}
	devbox.Spec.State = devboxv1alpha2.DevboxStateRunning
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("restore state failed: %w", err)
	}

	// wait for restore running
	if err := t.waitForDevboxRunning(ctx, name, 5*time.Minute); err != nil {
		return fmt.Errorf("wait for restore running timeout: %w", err)
	}

	// verify first batch of data
	if t.config.VerifyData {
		log.Printf("[%s] verify first batch of data", name)
		if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
			return fmt.Errorf("get Devbox failed: %w", err)
		}
		if err := t.verifyDataInDirectory(ctx, *devbox, "test_data_phase1"); err != nil {
			return fmt.Errorf("verify data failed: %w", err)
		}
	}

	return nil
}

// phase3_ShutdownCommitAndVerify phase3: Shutdown Commit test
func (t *DevboxLifecycleTester) phase3_ShutdownCommitAndVerify(ctx context.Context, name string) error {
	// get Devbox
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("get Devbox failed: %w", err)
	}

	// write second batch of test data
	log.Printf("[%s] write second batch of test data (directory: test_data_phase2)", name)
	if err := t.writeDataToDirectory(ctx, *devbox, "test_data_phase2", t.config.Data2Size, t.config.FileCount); err != nil {
		return fmt.Errorf("write data failed: %w", err)
	}

	// force sync file system, ensure data written to disk (very important!)
	log.Printf("[%s] sync file system, ensure data persisted to LVM", name)
	syncCmd := []string{"sync"}
	if err := t.execCommandInPod(ctx, devbox.Namespace, devbox.Name, devbox.Name, syncCmd); err != nil {
		log.Printf("[%s] ⚠ sync file system failed: %v (continue execution)", name, err)
	}
	// wait for a short period, ensure sync completed and flush I/O buffer
	time.Sleep(3 * time.Second)

	// modify state to Shutdown to trigger commit
	log.Printf("[%s] modify state to Shutdown to trigger commit", name)
	devbox.Spec.State = devboxv1alpha2.DevboxStateShutdown
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("modify state failed: %w", err)
	}

	// wait for commit complete
	if err := t.waitForCommitComplete(ctx, name, devboxv1alpha2.DevboxStateShutdown, 10*time.Minute); err != nil {
		return fmt.Errorf("wait for commit complete timeout: %w", err)
	}

	// restore Running state
	log.Printf("[%s] restore Running state", name)
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("get Devbox failed: %w", err)
	}
	devbox.Spec.State = devboxv1alpha2.DevboxStateRunning
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("restore state failed: %w", err)
	}

	// wait for restore running
	if err := t.waitForDevboxRunning(ctx, name, 5*time.Minute); err != nil {
		return fmt.Errorf("wait for restore running timeout: %w", err)
	}

	// verify both batches of data exist
	if t.config.VerifyData {
		log.Printf("[%s] verify both batches of data", name)
		if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
			return fmt.Errorf("get Devbox failed: %w", err)
		}
		if err := t.verifyDataInDirectory(ctx, *devbox, "test_data_phase1"); err != nil {
			return fmt.Errorf("first batch of data verify failed: %w", err)
		}
		if err := t.verifyDataInDirectory(ctx, *devbox, "test_data_phase2"); err != nil {
			return fmt.Errorf("second batch of data verify failed: %w", err)
		}
	}

	return nil
}

// phasePreRelease_StopDevbox phase3.5: stop Devbox before release
func (t *DevboxLifecycleTester) phasePreRelease_StopDevbox(ctx context.Context, name string) error {
	// get Devbox
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("get Devbox failed: %w", err)
	}

	// modify state to Stopped (required before release)
	log.Printf("[%s] modify state to Stopped (required before release)", name)
	devbox.Spec.State = devboxv1alpha2.DevboxStateStopped
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("modify state failed: %w", err)
	}

	// wait for state change complete
	if err := t.waitForDevboxState(ctx, name, devboxv1alpha2.DevboxStateStopped, 5*time.Minute); err != nil {
		return fmt.Errorf("wait for Devbox stop timeout: %w", err)
	}

	log.Printf("[%s] Devbox stopped successfully, can proceed with release", name)
	return nil
}

// phase4_ReleaseAndVerify phase4: release test
func (t *DevboxLifecycleTester) phase4_ReleaseAndVerify(ctx context.Context, name string) error {
	releaseName := fmt.Sprintf("%s-release", name)
	version := fmt.Sprintf(t.config.ReleaseVersionPattern, 1)

	// create DevBoxRelease
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

	log.Printf("[%s] create DevBoxRelease: %s (version: %s)", name, releaseName, version)
	if err := t.ctrlClient.Create(ctx, release); err != nil {
		return fmt.Errorf("create DevBoxRelease failed: %w", err)
	}

	// 等待发版完成
	phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("wait for release complete timeout: %w", err)
	}

	if phase != devboxv1alpha2.DevBoxReleasePhaseSuccess {
		return fmt.Errorf("release failed, phase: %s", phase)
	}

	log.Printf("[%s] release successful: %s", name, releaseName)
	return nil
}

// waitForDevboxRunning wait for Devbox running ready
func (t *DevboxLifecycleTester) waitForDevboxRunning(ctx context.Context, name string, timeout time.Duration) error {
	_, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, timeout)
	return err
}

// waitForDevboxState wait for Devbox reach specified state
func (t *DevboxLifecycleTester) waitForDevboxState(ctx context.Context, name string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	return t.helper.WaitForDevboxState(ctx, t.config.Namespace, name, targetState, timeout)
}

// waitForCommitComplete wait for commit complete
func (t *DevboxLifecycleTester) waitForCommitComplete(ctx context.Context, name string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	return t.helper.WaitForCommitComplete(ctx, t.config.Namespace, name, targetState, timeout)
}

// waitForReleaseComplete wait for release complete
func (t *DevboxLifecycleTester) waitForReleaseComplete(ctx context.Context, name string, timeout time.Duration) (devboxv1alpha2.DevBoxReleasePhase, error) {
	return t.helper.WaitForReleaseComplete(ctx, t.config.Namespace, name, timeout)
}

// verifyAllResources verify all resources are created
func (t *DevboxLifecycleTester) verifyAllResources(ctx context.Context, name string) error {
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		return fmt.Errorf("get Devbox failed: %w", err)
	}

	// check Secret
	if !t.isSecretCreated(ctx, *devbox) {
		return fmt.Errorf("Secret not created")
	}
	log.Printf("[%s] ✓ Secret created", name)

	// check Service
	if !t.isServiceCreated(ctx, *devbox) {
		return fmt.Errorf("Service not created")
	}
	log.Printf("[%s] ✓ Service created", name)

	// check Pod
	if !t.isPodRunning(ctx, *devbox) {
		return fmt.Errorf("Pod not running")
	}
	log.Printf("[%s] ✓ Pod running", name)

	// check LVM
	if !t.isLVMCreated(ctx, *devbox) {
		log.Printf("[%s] ⚠ LVM not found (may be normal)", name)
	} else {
		log.Printf("[%s] ✓ LVM created", name)
	}

	return nil
}

// writeDataToDirectory write test data to specified directory
func (t *DevboxLifecycleTester) writeDataToDirectory(ctx context.Context, devbox devboxv1alpha2.Devbox, directory string, dataSize string, fileCount int) error {
	return t.helper.WriteTestDataToDevbox(ctx, devbox, directory, dataSize, fileCount)
}

// verifyDataInDirectory verify data in specified directory
func (t *DevboxLifecycleTester) verifyDataInDirectory(ctx context.Context, devbox devboxv1alpha2.Devbox, directory string) error {
	return t.helper.VerifyTestDataInDevbox(ctx, devbox, directory)
}

// execCommandInPod execute command in Pod
func (t *DevboxLifecycleTester) execCommandInPod(ctx context.Context, namespace, podName, containerName string, cmd []string) error {
	return t.helper.ExecCommandInPod(ctx, namespace, podName, containerName, cmd)
}

// isPodRunning check if Pod is running
func (t *DevboxLifecycleTester) isPodRunning(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsPodRunning(ctx, devbox)
}

// isServiceCreated check if Service is created
func (t *DevboxLifecycleTester) isServiceCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsServiceCreated(ctx, devbox)
}

// isSecretCreated check if Secret is created
func (t *DevboxLifecycleTester) isSecretCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsSecretCreated(ctx, devbox)
}

// isLVMCreated check if LVM logical volume is created
func (t *DevboxLifecycleTester) isLVMCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	return t.helper.IsLVMCreated(ctx, devbox)
}

// Cleanup cleans up test resources
func (t *DevboxLifecycleTester) Cleanup(ctx context.Context) error {
	log.Printf("start cleaning up lifecycle test resources...")

	// delete all DevBoxRelease
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}
	if err := t.ctrlClient.List(ctx, releaseList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("list DevBoxRelease failed: %v", err)
	} else {
		for _, release := range releaseList.Items {
			if t.isLifecycleTestRelease(release) {
				if err := t.ctrlClient.Delete(ctx, &release); err != nil {
					log.Printf("delete DevBoxRelease %s failed: %v", release.Name, err)
				} else {
					log.Printf("DevBoxRelease deleted: %s", release.Name)
				}
			}
		}
	}

	// delete all Devbox
	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("list Devbox failed: %v", err)
	} else {
		for _, devbox := range devboxList.Items {
			if t.isLifecycleTestDevbox(devbox) {
				if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
					log.Printf("delete Devbox %s failed: %v", devbox.Name, err)
				} else {
					log.Printf("Devbox deleted: %s", devbox.Name)
				}
			}
		}
	}

	log.Printf("cleanup completed")
	return nil
}

// isLifecycleTestRelease check if it is a lifecycle test Release
func (t *DevboxLifecycleTester) isLifecycleTestRelease(release devboxv1alpha2.DevBoxRelease) bool {
	if release.Labels != nil {
		if testType, ok := release.Labels["test-type"]; ok && testType == "lifecycle" {
			return true
		}
	}
	return strings.Contains(release.Name, "lifecycle-test-")
}

// isLifecycleTestDevbox check if it is a lifecycle test Devbox
func (t *DevboxLifecycleTester) isLifecycleTestDevbox(devbox devboxv1alpha2.Devbox) bool {
	if devbox.Labels != nil {
		if testType, ok := devbox.Labels["test-type"]; ok && testType == "lifecycle" {
			return true
		}
	}
	return strings.Contains(devbox.Name, "lifecycle-test-")
}
