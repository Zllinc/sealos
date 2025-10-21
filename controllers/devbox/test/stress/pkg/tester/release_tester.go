package tester

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	"github.com/openebs/lvm-localpv/pkg/lvm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewDevboxReleaseTester 创建发版测试器
func NewDevboxReleaseTester(config *ReleaseTestConfig) (*DevboxReleaseTester, error) {
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

	return &DevboxReleaseTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
	}, nil
}

// RunBasicReleaseTest 运行基础发版测试
func (t *DevboxReleaseTester) RunBasicReleaseTest(ctx context.Context) (*ReleaseTestResult, error) {
	log.Printf("开始基础发版测试: 创建 %d 个发版", t.config.ReleaseCount)

	result := &ReleaseTestResult{}
	startTime := time.Now()

	// 1. 准备基础 Devbox
	devboxName := t.config.BaseDevboxName
	if devboxName == "" {
		devboxName = fmt.Sprintf("release-test-devbox-%d", time.Now().Unix())
		log.Printf("创建基础 Devbox: %s", devboxName)
		if err := t.createBaseDevbox(ctx, devboxName); err != nil {
			return nil, fmt.Errorf("创建基础 Devbox 失败: %w", err)
		}
	}

	// 等待 Devbox 准备就绪（确保有 CommitRecord）
	log.Printf("等待 Devbox 准备就绪...")
	if err := t.waitForDevboxReady(ctx, devboxName, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("等待 Devbox 准备就绪失败: %w", err)
	}

	// 2. 批量创建 DevBoxRelease
	for i := 0; i < t.config.ReleaseCount; i++ {
		releaseName := fmt.Sprintf("%s-release-%d", devboxName, i)
		version := fmt.Sprintf(t.config.VersionPattern, i)

		log.Printf("创建 DevBoxRelease: %s (版本: %s)", releaseName, version)

		releaseStart := time.Now()
		release, err := t.createDevBoxRelease(ctx, releaseName, devboxName, version)
		if err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 %s 失败: %v", releaseName, err))
			log.Printf("创建 DevBoxRelease %s 失败: %v", releaseName, err)
			result.TotalReleases++
			continue
		}

		// 3. 等待发版完成
		phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
		releaseDuration := time.Since(releaseStart)

		if err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 超时: %v", releaseName, err))
			log.Printf("DevBoxRelease %s 超时: %v", releaseName, err)
		} else if phase == devboxv1alpha2.DevBoxReleasePhaseSuccess {
			result.SuccessfulReleases++
			log.Printf("DevBoxRelease %s 成功完成, 耗时: %v", releaseName, releaseDuration)

			// 4. 验证镜像一致性
			imageCheck := t.verifyImageConsistency(ctx, release)
			result.ImageVerifications = append(result.ImageVerifications, imageCheck)

			if !imageCheck.DigestMatch {
				log.Printf("警告: %s 镜像 digest 不匹配", releaseName)
			}
		} else if phase == devboxv1alpha2.DevBoxReleasePhaseFailed {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版失败", releaseName))
			log.Printf("DevBoxRelease %s 发版失败", releaseName)
		} else {
			result.PendingReleases++
			log.Printf("DevBoxRelease %s 仍在 %s 状态", releaseName, phase)
		}

		result.TotalReleases++

		// 检查超时
		if time.Since(startTime) > t.config.TestTimeout {
			log.Printf("测试超时，停止创建更多发版")
			break
		}
	}

	// 5. 如果启用了 StartAfterRelease，验证 Devbox 状态
	if t.config.StartAfterRelease {
		log.Printf("验证 Devbox 是否已启动...")
		if err := t.verifyDevboxStarted(ctx, devboxName); err != nil {
			log.Printf("警告: Devbox 启动验证失败: %v", err)
		}
	}

	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulReleases > 0 {
		result.AverageReleaseTime = result.TotalTestTime / time.Duration(result.SuccessfulReleases)
	}
	if result.TotalTestTime.Seconds() > 0 {
		result.MaxQPS = float64(result.SuccessfulReleases) / result.TotalTestTime.Seconds()
	}

	log.Printf("基础发版测试完成: 成功 %d, 失败 %d, 待处理 %d, 总耗时 %v",
		result.SuccessfulReleases, result.FailedReleases, result.PendingReleases, result.TotalTestTime)

	return result, nil
}

// createBaseDevbox 创建基础 Devbox
func (t *DevboxReleaseTester) createBaseDevbox(ctx context.Context, name string) error {
	devbox := &devboxv1alpha2.Devbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: t.config.Namespace,
			Labels: map[string]string{
				"stress-test": "true",
				"test-type":   "devbox-release",
			},
		},
		Spec: devboxv1alpha2.DevboxSpec{
			State: devboxv1alpha2.DevboxStateStopped, // 创建为停止状态，用于发版测试
			Image: t.config.Image,
			Resource: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(t.config.CPU),
				corev1.ResourceMemory: resource.MustParse(t.config.Memory),
			},
			Config: devboxv1alpha2.Config{
				User:       "devbox",
				WorkingDir: "/home/devbox/project",
			},
			StorageLimit:     t.config.StorageLimit,
			RuntimeClassName: "devbox-runtime",
			NetworkSpec: devboxv1alpha2.NetworkSpec{
				Type: devboxv1alpha2.NetworkTypeNodePort,
			},
		},
	}

	if err := t.ctrlClient.Create(ctx, devbox); err != nil {
		return fmt.Errorf("创建 Devbox 失败: %w", err)
	}

	log.Printf("基础 Devbox %s 创建成功", name)
	return nil
}

// createDevBoxRelease 创建 DevBoxRelease
func (t *DevboxReleaseTester) createDevBoxRelease(ctx context.Context, name, devboxName, version string) (*devboxv1alpha2.DevBoxRelease, error) {
	release := &devboxv1alpha2.DevBoxRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: t.config.Namespace,
		},
		Spec: devboxv1alpha2.DevBoxReleaseSpec{
			DevboxName:              devboxName,
			Version:                 version,
			StartDevboxAfterRelease: t.config.StartAfterRelease,
			Notes:                   fmt.Sprintf("Release test version %s", version),
		},
	}

	if err := t.ctrlClient.Create(ctx, release); err != nil {
		return nil, fmt.Errorf("创建 DevBoxRelease 失败: %w", err)
	}

	return release, nil
}

// waitForDevboxReady 等待 Devbox 准备就绪（有 CommitRecord）
func (t *DevboxReleaseTester) waitForDevboxReady(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("等待 Devbox 准备就绪超时")
			}

			devbox := &devboxv1alpha2.Devbox{}
			err := t.ctrlClient.Get(ctx, client.ObjectKey{
				Namespace: t.config.Namespace,
				Name:      name,
			}, devbox)

			if err != nil {
				log.Printf("获取 Devbox 状态失败: %v", err)
				continue
			}

			// 检查是否有 CommitRecord
			if len(devbox.Status.CommitRecords) > 0 && devbox.Status.ContentID != "" {
				if _, ok := devbox.Status.CommitRecords[devbox.Status.ContentID]; ok {
					log.Printf("Devbox %s 已准备就绪，ContentID: %s", name, devbox.Status.ContentID)
					return nil
				}
			}

			log.Printf("等待 Devbox %s 准备就绪... (State: %s)", name, devbox.Status.State)
		}
	}
}

// waitForReleaseComplete 等待发版完成
func (t *DevboxReleaseTester) waitForReleaseComplete(ctx context.Context, name string, timeout time.Duration) (devboxv1alpha2.DevBoxReleasePhase, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("等待发版完成超时")
			}

			release := &devboxv1alpha2.DevBoxRelease{}
			err := t.ctrlClient.Get(ctx, client.ObjectKey{
				Namespace: t.config.Namespace,
				Name:      name,
			}, release)

			if err != nil {
				return "", fmt.Errorf("获取 DevBoxRelease 状态失败: %w", err)
			}

			// 检查状态
			switch release.Status.Phase {
			case devboxv1alpha2.DevBoxReleasePhaseSuccess:
				return devboxv1alpha2.DevBoxReleasePhaseSuccess, nil
			case devboxv1alpha2.DevBoxReleasePhaseFailed:
				return devboxv1alpha2.DevBoxReleasePhaseFailed, nil
			default:
				log.Printf("等待发版 %s 完成... (Phase: %s)", name, release.Status.Phase)
			}
		}
	}
}

// verifyImageConsistency 验证镜像一致性
func (t *DevboxReleaseTester) verifyImageConsistency(ctx context.Context, release *devboxv1alpha2.DevBoxRelease) ImageCheck {
	check := ImageCheck{
		ReleaseName: release.Name,
		SourceImage: release.Status.SourceImage,
		TargetImage: release.Status.TargetImage,
	}

	// 获取源镜像 digest
	sourceDigest, err := t.getImageDigest(release.Status.SourceImage)
	if err != nil {
		check.Error = fmt.Sprintf("获取源镜像 digest 失败: %v", err)
		check.SourceFound = false
		log.Printf("警告: %s", check.Error)
		return check
	}
	check.SourceFound = true

	// 获取目标镜像 digest
	targetDigest, err := t.getImageDigest(release.Status.TargetImage)
	if err != nil {
		check.Error = fmt.Sprintf("获取目标镜像 digest 失败: %v", err)
		check.TargetFound = false
		log.Printf("警告: %s", check.Error)
		return check
	}
	check.TargetFound = true

	// 比较 digest
	check.DigestMatch = (sourceDigest == targetDigest)
	if check.DigestMatch {
		log.Printf("镜像一致性验证通过: %s (digest: %s)", release.Name, sourceDigest)
	} else {
		check.Error = fmt.Sprintf("digest 不匹配: 源=%s, 目标=%s", sourceDigest, targetDigest)
		log.Printf("警告: %s", check.Error)
	}

	return check
}

// getImageDigest 获取镜像 digest
func (t *DevboxReleaseTester) getImageDigest(image string) (string, error) {
	// 使用 crane 或 skopeo 获取镜像 digest
	// 优先尝试 crane
	cmd := exec.Command("crane", "digest", image)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// 如果 crane 失败，尝试 skopeo
	cmd = exec.Command("skopeo", "inspect", "--format", "{{.Digest}}", fmt.Sprintf("docker://%s", image))
	output, err = cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// 如果都失败，尝试使用 crictl
	cmd = exec.Command("crictl", "inspecti", "--output", "json", image)
	output, err = cmd.CombinedOutput()
	if err == nil {
		// 简单解析，提取 digest（实际应该用 JSON 解析）
		outputStr := string(output)
		if strings.Contains(outputStr, "repoDigests") {
			// 这里简化处理，实际应该用 json 解析
			lines := strings.Split(outputStr, "\n")
			for _, line := range lines {
				if strings.Contains(line, "sha256:") {
					parts := strings.Split(line, "@")
					if len(parts) > 1 {
						digest := strings.TrimSpace(strings.Trim(parts[1], `",`))
						return digest, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("无法获取镜像 digest (尝试了 crane, skopeo, crictl)")
}

// verifyDevboxStarted 验证 Devbox 是否已启动
func (t *DevboxReleaseTester) verifyDevboxStarted(ctx context.Context, name string) error {
	devbox := &devboxv1alpha2.Devbox{}
	err := t.ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: t.config.Namespace,
		Name:      name,
	}, devbox)

	if err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	// 等待 Devbox 完全启动到 Running 状态
	if !t.waitForDevboxRunning(ctx, *devbox, 5*time.Minute) {
		return fmt.Errorf("devbox %s 未能在规定时间内启动到 Running 状态", name)
	}

	log.Printf("Devbox %s 已成功启动到 Running 状态", name)
	return nil
}

// Cleanup 清理测试资源
func (t *DevboxReleaseTester) Cleanup(ctx context.Context) error {
	log.Printf("清理测试资源...")

	// 删除所有测试创建的 DevBoxRelease
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}
	if err := t.ctrlClient.List(ctx, releaseList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("列出 DevBoxRelease 失败: %v", err)
	} else {
		deletedReleases := 0
		for _, release := range releaseList.Items {
			// 清理所有测试相关的 release
			if strings.Contains(release.Name, "release-test") || strings.Contains(release.Name, "multi-release-devbox") {
				if err := t.ctrlClient.Delete(ctx, &release); err != nil {
					log.Printf("删除 DevBoxRelease %s 失败: %v", release.Name, err)
				} else {
					log.Printf("已删除 DevBoxRelease: %s", release.Name)
					deletedReleases++
				}
			}
		}
		log.Printf("共删除 %d 个 DevBoxRelease", deletedReleases)
	}

	// 如果 BaseDevboxName 为空（由测试自动创建的），则删除所有测试相关的 Devbox
	if t.config.BaseDevboxName == "" {
		devboxList := &devboxv1alpha2.DevboxList{}
		if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
			log.Printf("列出 Devbox 失败: %v", err)
		} else {
			deletedDevboxes := 0
			for _, devbox := range devboxList.Items {
				// 清理所有测试相关的 devbox
				if strings.Contains(devbox.Name, "release-test-devbox") || strings.Contains(devbox.Name, "multi-release-devbox") || strings.Contains(devbox.Name, "concurrent-release-test-devbox") {
					if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
						log.Printf("删除 Devbox %s 失败: %v", devbox.Name, err)
					} else {
						log.Printf("已删除 Devbox: %s", devbox.Name)
						deletedDevboxes++
					}
				}
			}
			log.Printf("共删除 %d 个 Devbox", deletedDevboxes)
		}
	}

	log.Printf("清理完成")
	return nil
}

// RunConcurrentReleaseTest 运行并发发版测试
func (t *DevboxReleaseTester) RunConcurrentReleaseTest(ctx context.Context) (*ReleaseTestResult, error) {
	log.Printf("开始多 DevBox 并发发版测试: %d 并发创建 %d 个 DevBox，每个 DevBox 对应 1 个发版", t.config.ConcurrentCount, t.config.ReleaseCount)

	// 如果指定了基础 Devbox，使用原有的并发发版逻辑
	if t.config.BaseDevboxName != "" {
		log.Printf("使用指定的基础 Devbox: %s", t.config.BaseDevboxName)
		return t.runSingleDevboxConcurrentReleaseTest(ctx)
	}

	// 创建多个 DevBox，每个 DevBox 进行发版
	return t.runMultiDevboxConcurrentReleaseTest(ctx)
}

// runSingleDevboxConcurrentReleaseTest 对单一 DevBox 进行并发发版
func (t *DevboxReleaseTester) runSingleDevboxConcurrentReleaseTest(ctx context.Context) (*ReleaseTestResult, error) {
	log.Printf("对单一 DevBox 进行并发发版测试")

	result := &ReleaseTestResult{}
	startTime := time.Now()

	devboxName := t.config.BaseDevboxName

	// 等待 Devbox 准备就绪
	log.Printf("等待 Devbox 准备就绪...")
	if err := t.waitForDevboxReady(ctx, devboxName, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("等待 Devbox 准备就绪失败: %w", err)
	}

	// 并发创建 DevBoxRelease
	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := 0; i < t.config.ReleaseCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			releaseName := fmt.Sprintf("%s-release-%d", devboxName, index)
			version := fmt.Sprintf(t.config.VersionPattern, index)

			releaseStart := time.Now()
			release, err := t.createDevBoxRelease(ctx, releaseName, devboxName, version)
			if err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 %s 失败: %v", releaseName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("并发创建 DevBoxRelease %s 失败: %v", releaseName, err)
				return
			}

			// 等待发版完成
			phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
			releaseDuration := time.Since(releaseStart)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 超时: %v", releaseName, err))
			} else if phase == devboxv1alpha2.DevBoxReleasePhaseSuccess {
				result.SuccessfulReleases++
				log.Printf("并发 DevBoxRelease %s 成功完成, 耗时: %v", releaseName, releaseDuration)

				// 验证镜像一致性
				imageCheck := t.verifyImageConsistency(ctx, release)
				result.ImageVerifications = append(result.ImageVerifications, imageCheck)
			} else if phase == devboxv1alpha2.DevBoxReleasePhaseFailed {
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版失败", releaseName))
			} else {
				result.PendingReleases++
			}

			result.TotalReleases++
		}(i)
	}

	wg.Wait()
	result.TotalTestTime = time.Since(startTime)

	if result.SuccessfulReleases > 0 {
		result.AverageReleaseTime = result.TotalTestTime / time.Duration(result.SuccessfulReleases)
	}
	if result.TotalTestTime.Seconds() > 0 {
		result.MaxQPS = float64(result.SuccessfulReleases) / result.TotalTestTime.Seconds()
	}

	log.Printf("单一 DevBox 并发发版测试完成: 成功 %d, 失败 %d, 待处理 %d, QPS: %.2f, 总耗时 %v",
		result.SuccessfulReleases, result.FailedReleases, result.PendingReleases, result.MaxQPS, result.TotalTestTime)

	return result, nil
}

// runMultiDevboxConcurrentReleaseTest 创建多个 DevBox，每个进行发版
func (t *DevboxReleaseTester) runMultiDevboxConcurrentReleaseTest(ctx context.Context) (*ReleaseTestResult, error) {
	log.Printf("开始多 DevBox 并发发版测试: 创建 %d 个 DevBox，每个 DevBox 进行 1 次发版", t.config.ReleaseCount)

	result := &ReleaseTestResult{}
	startTime := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	// 并发创建多个 DevBox，每个 DevBox 进行一次发版
	for i := 0; i < t.config.ReleaseCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devboxName := fmt.Sprintf("multi-release-devbox-%d-%d", index, time.Now().Unix())
			releaseName := fmt.Sprintf("%s-release", devboxName)
			version := fmt.Sprintf(t.config.VersionPattern, index)

			log.Printf("开始处理 DevBox %s (版本: %s)", devboxName, version)

			// 1. 创建 DevBox
			devboxStart := time.Now()
			if err := t.createBaseDevbox(ctx, devboxName); err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 DevBox %s 失败: %v", devboxName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("创建 DevBox %s 失败: %v", devboxName, err)
				return
			}
			devboxCreateTime := time.Since(devboxStart)
			log.Printf("DevBox %s 创建成功，耗时: %v", devboxName, devboxCreateTime)

			// 2. 等待 DevBox 准备就绪
			if err := t.waitForDevboxReady(ctx, devboxName, 5*time.Minute); err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("DevBox %s 准备超时: %v", devboxName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("DevBox %s 准备超时: %v", devboxName, err)
				return
			}
			log.Printf("DevBox %s 准备就绪", devboxName)

			// 3. 创建 DevBoxRelease
			releaseStart := time.Now()
			release, err := t.createDevBoxRelease(ctx, releaseName, devboxName, version)
			if err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 DevBoxRelease %s 失败: %v", releaseName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("创建 DevBoxRelease %s 失败: %v", releaseName, err)
				return
			}
			log.Printf("DevBoxRelease %s 创建成功", releaseName)

			// 4. 等待发版完成
			phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
			totalDuration := time.Since(devboxStart)
			releaseDuration := time.Since(releaseStart)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版超时: %v", releaseName, err))
				log.Printf("DevBoxRelease %s 发版超时: %v", releaseName, err)
			} else if phase == devboxv1alpha2.DevBoxReleasePhaseSuccess {
				result.SuccessfulReleases++
				log.Printf("DevBox %s 发版成功完成! DevBox创建: %v, 发版: %v, 总耗时: %v",
					devboxName, devboxCreateTime, releaseDuration, totalDuration)

				// 验证镜像一致性
				imageCheck := t.verifyImageConsistency(ctx, release)
				result.ImageVerifications = append(result.ImageVerifications, imageCheck)
			} else if phase == devboxv1alpha2.DevBoxReleasePhaseFailed {
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版失败", releaseName))
				log.Printf("DevBoxRelease %s 发版失败", releaseName)
			} else {
				result.PendingReleases++
				log.Printf("DevBoxRelease %s 仍在 %s 状态", releaseName, phase)
			}

			result.TotalReleases++
		}(i)
	}

	wg.Wait()
	result.TotalTestTime = time.Since(startTime)

	if result.SuccessfulReleases > 0 {
		result.AverageReleaseTime = result.TotalTestTime / time.Duration(result.SuccessfulReleases)
	}
	if result.TotalTestTime.Seconds() > 0 {
		result.MaxQPS = float64(result.SuccessfulReleases) / result.TotalTestTime.Seconds()
	}

	log.Printf("多 DevBox 并发发版测试完成: 成功 %d, 失败 %d, 待处理 %d, QPS: %.2f, 总耗时 %v",
		result.SuccessfulReleases, result.FailedReleases, result.PendingReleases, result.MaxQPS, result.TotalTestTime)

	return result, nil
}

// ==================== 通用 Devbox 状态检查方法 ====================

// waitForDevboxRunning 等待 Devbox 完全启动到 Running 状态（包含所有资源检查）
func (t *DevboxReleaseTester) waitForDevboxRunning(ctx context.Context, devbox devboxv1alpha2.Devbox, timeout time.Duration) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			log.Printf("等待 Devbox %s 启动超时", devbox.Name)
			return false
		case <-ticker.C:
			latestDevbox := &devboxv1alpha2.Devbox{}
			if err := t.ctrlClient.Get(ctx, client.ObjectKey{
				Namespace: devbox.Namespace,
				Name:      devbox.Name,
			}, latestDevbox); err != nil {
				log.Printf("获取 Devbox %s 状态失败: %v", devbox.Name, err)
				continue
			}

			if latestDevbox.Status.State == devboxv1alpha2.DevboxStateRunning {
				log.Printf("Devbox %s 状态为 Running", devbox.Name)

				// 检查 Pod 是否运行
				if !t.isPodRunning(ctx, devbox) {
					log.Printf("Devbox %s Pod 尚未运行，继续等待...", devbox.Name)
					continue
				}

				// 检查 Service 是否创建
				if !t.isServiceCreated(ctx, devbox) {
					log.Printf("Devbox %s Service 尚未创建，继续等待...", devbox.Name)
					continue
				}

				// 检查 Secret 是否创建
				if !t.isSecretCreated(ctx, devbox) {
					log.Printf("Devbox %s Secret 尚未创建，继续等待...", devbox.Name)
					continue
				}

				// 检查 LVM 是否创建
				if !t.isLVMCreated(ctx, devbox) {
					log.Printf("Devbox %s LVM 逻辑卷尚未创建，继续等待...", devbox.Name)
					continue
				}

				log.Printf("Devbox %s 所有资源已就绪", devbox.Name)
				return true
			}

			// 打印状态变化日志
			if latestDevbox.Status.State != "" {
				log.Printf("Devbox %s 当前状态: %s，等待 Running...", devbox.Name, latestDevbox.Status.State)
			}
		}
	}
}

// isPodRunning 检查 Pod 是否正常运行
func (t *DevboxReleaseTester) isPodRunning(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	podList := &corev1.PodList{}
	err := t.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
	if err != nil {
		log.Printf("查找 Devbox %s Pod 失败: %v", devbox.Name, err)
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

// isServiceCreated 检查 Service 是否已创建
func (t *DevboxReleaseTester) isServiceCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	services, err := t.getDevboxServices(ctx, devbox)
	if err != nil {
		return false
	}
	return len(services) > 0
}

// isSecretCreated 检查 Secret 是否已创建
func (t *DevboxReleaseTester) isSecretCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	secrets, err := t.getDevboxSecrets(ctx, devbox)
	if err != nil {
		return false
	}
	return len(secrets) > 0
}

// isLVMCreated 检查 LVM 逻辑卷是否已创建
func (t *DevboxReleaseTester) isLVMCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	lvs, err := lvm.ListLVMLogicalVolume()
	if err != nil {
		log.Printf("获取 LVM 逻辑卷失败: %v", err)
		return false
	}
	for _, lv := range lvs {
		if strings.Contains(lv.Name, devbox.Status.ContentID) {
			return true
		}
	}
	return false
}

// getDevboxServices 获取 Devbox 相关的 Service
func (t *DevboxReleaseTester) getDevboxServices(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Service, error) {
	serviceList, err := t.k8sClient.CoreV1().Services(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

// getDevboxSecrets 获取 Devbox 相关的 Secret
func (t *DevboxReleaseTester) getDevboxSecrets(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Secret, error) {
	secretList, err := t.k8sClient.CoreV1().Secrets(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return secretList.Items, nil
}
