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

// NewDevboxReleaseTester creates a new release tester
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

// RunBasicReleaseTest runs basic release test
// Flow: Create Devbox(Running) → Wait for running → Commit → Wait for Commit → Create Release → Verify
func (t *DevboxReleaseTester) RunBasicReleaseTest(ctx context.Context) (*ReleaseTestResult, error) {
	log.Printf("开始基础发版测试: 创建 %d 个发版", t.config.ReleaseCount)

	result := &ReleaseTestResult{}
	startTime := time.Now()

	// 循环创建多个 Devbox，每个进行发版
	for i := 0; i < t.config.ReleaseCount; i++ {
		devboxName := fmt.Sprintf("release-test-devbox-%d", i)
		releaseName := fmt.Sprintf("%s-release", devboxName)
		version := fmt.Sprintf(t.config.VersionPattern, i)

		log.Printf("========== 开始处理 Devbox %s (版本: %s) ==========", devboxName, version)

		// 步骤 1: 创建 Devbox (Running 状态)
		log.Printf("步骤 1: 创建 Devbox %s", devboxName)
		if err := t.createBaseDevbox(ctx, devboxName); err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 Devbox %s 失败: %v", devboxName, err))
			result.TotalReleases++
			log.Printf("创建 Devbox %s 失败: %v", devboxName, err)
			continue
		}

		// 步骤 2: 等待 Devbox 运行就绪
		log.Printf("步骤 2: 等待 Devbox %s 运行就绪", devboxName)
		_, err := t.waitForDevboxRunningAndReady(ctx, devboxName, 5*time.Minute)
		if err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("Devbox %s 运行超时: %v", devboxName, err))
			result.TotalReleases++
			log.Printf("Devbox %s 运行超时: %v", devboxName, err)
			continue
		}
		log.Printf("Devbox %s 已运行就绪", devboxName)

		// 步骤 3: 触发 Commit
		targetState := t.getCommitTargetState()
		log.Printf("步骤 3: 触发 Commit (修改状态为 %s)", targetState)
		commitStart := time.Now()
		if err := t.triggerCommitByStateChange(ctx, devboxName, targetState); err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("触发 Commit 失败: %v", err))
			result.TotalReleases++
			log.Printf("触发 Commit 失败: %v", err)
			continue
		}

		// 步骤 4: 等待 Commit 完成
		log.Printf("步骤 4: 等待 Commit 完成")
		if err := t.waitForCommitComplete(ctx, devboxName, targetState, 10*time.Minute); err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("Commit 超时: %v", err))
			result.TotalReleases++
			log.Printf("Commit 超时: %v", err)
			continue
		}
		commitDuration := time.Since(commitStart)
		log.Printf("Commit 完成，耗时: %v", commitDuration)

		// 步骤 5: 创建 DevBoxRelease
		log.Printf("步骤 5: 创建 DevBoxRelease %s", releaseName)
		releaseStart := time.Now()
		_, err = t.createDevBoxRelease(ctx, releaseName, devboxName, version)
		if err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 DevBoxRelease %s 失败: %v", releaseName, err))
			result.TotalReleases++
			log.Printf("创建 DevBoxRelease %s 失败: %v", releaseName, err)
			continue
		}

		// 步骤 6: 等待发版完成
		log.Printf("步骤 6: 等待发版完成")
		phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
		releaseDuration := time.Since(releaseStart)
		totalDuration := time.Since(startTime)

		if err != nil {
			result.FailedReleases++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版超时: %v", releaseName, err))
			log.Printf("DevBoxRelease %s 发版超时: %v", releaseName, err)
		} else if phase == devboxv1alpha2.DevBoxReleasePhaseSuccess {
			result.SuccessfulReleases++
			log.Printf("DevBoxRelease %s 发版成功! Commit: %v, Release: %v", releaseName, commitDuration, releaseDuration)

			// // 步骤 7: 验证镜像一致性
			// imageCheck := t.verifyImageConsistency(ctx, release)
			// result.ImageVerifications = append(result.ImageVerifications, imageCheck)

			// if !imageCheck.DigestMatch {
			// 	log.Printf("警告: %s 镜像 digest 不匹配", releaseName)
			// }
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

		log.Printf("========== Devbox %s 处理完成 (总耗时: %v) ==========\n", devboxName, totalDuration)
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

// createBaseDevbox 创建基础 Devbox（普通的 Running 状态 Devbox）
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
			State: devboxv1alpha2.DevboxStateRunning, // 创建为 Running 状态
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

	log.Printf("Devbox %s 创建成功（Running 状态）", name)
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

// waitForDevboxRunningAndReady 等待 Devbox 运行并准备就绪
func (t *DevboxReleaseTester) waitForDevboxRunningAndReady(ctx context.Context, name string, timeout time.Duration) (*devboxv1alpha2.Devbox, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("等待 Devbox 运行就绪超时")
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

			// 检查是否达到 Running 状态
			if devbox.Status.State == devboxv1alpha2.DevboxStateRunning {
				log.Printf("Devbox %s 已运行，状态: %s", name, devbox.Status.State)
				return devbox, nil
			}

			log.Printf("等待 Devbox %s 运行... (当前状态: %s)", name, devbox.Status.State)
		}
	}
}

// triggerCommitByStateChange 通过修改状态触发 commit
func (t *DevboxReleaseTester) triggerCommitByStateChange(ctx context.Context, devboxName string, targetState devboxv1alpha2.DevboxState) error {
	// 获取最新的 Devbox
	devbox := &devboxv1alpha2.Devbox{}
	err := t.ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: t.config.Namespace,
		Name:      devboxName,
	}, devbox)
	if err != nil {
		return fmt.Errorf("获取 Devbox 失败: %w", err)
	}

	// 修改状态
	devbox.Spec.State = targetState
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("修改 Devbox 状态失败: %w", err)
	}

	log.Printf("已将 Devbox %s 状态修改为 %s，触发 commit", devboxName, targetState)
	return nil
}

// waitForCommitComplete 等待 commit 完成（检查 CommitRecord）
func (t *DevboxReleaseTester) waitForCommitComplete(ctx context.Context, devboxName string, targetState devboxv1alpha2.DevboxState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("等待 commit 完成超时")
			}

			devbox := &devboxv1alpha2.Devbox{}
			err := t.ctrlClient.Get(ctx, client.ObjectKey{
				Namespace: t.config.Namespace,
				Name:      devboxName,
			}, devbox)

			if err != nil {
				log.Printf("获取 Devbox 状态失败: %v", err)
				continue
			}

			// 检查是否达到目标状态
			if devbox.Status.State != targetState {
				log.Printf("等待 Devbox %s 状态变为 %s... (当前: %s)", devboxName, targetState, devbox.Status.State)
				continue
			}

			log.Printf("Devbox %s has completed state transition", devboxName)
			return nil
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
				log.Printf("等待发版 %s 完成... (Phase: %s, SourceImage: %s, TargetImage: %s)", name, release.Status.Phase, release.Status.SourceImage, release.Status.TargetImage)
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

// Cleanup cleans up test resources
func (t *DevboxReleaseTester) Cleanup(ctx context.Context) error {
	log.Printf("清理测试资源...")

	// 删除所有测试创建的 DevBoxRelease
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}
	if err := t.ctrlClient.List(ctx, releaseList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("列出 DevBoxRelease 失败: %v", err)
	} else {
		deletedReleases := 0
		for _, release := range releaseList.Items {
			if t.isTestRelease(release) {
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

	// 删除所有测试相关的 Devbox（因为现在都是自动创建的）
	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		log.Printf("列出 Devbox 失败: %v", err)
	} else {
		deletedDevboxes := 0
		for _, devbox := range devboxList.Items {
			if t.isTestDevbox(devbox) {
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

	log.Printf("清理完成")
	return nil
}

// ListReleaseTestResources lists test resources
func (t *DevboxReleaseTester) ListReleaseTestResources(ctx context.Context, allNamespaces bool) ([]devboxv1alpha2.DevBoxRelease, []devboxv1alpha2.Devbox, error) {
	var releases []devboxv1alpha2.DevBoxRelease
	var devboxes []devboxv1alpha2.Devbox

	// 列出 DevBoxRelease
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}
	var listOptions []client.ListOption
	if !allNamespaces {
		listOptions = append(listOptions, client.InNamespace(t.config.Namespace))
	}

	if err := t.ctrlClient.List(ctx, releaseList, listOptions...); err != nil {
		return nil, nil, fmt.Errorf("列出 DevBoxRelease 失败: %w", err)
	}

	// 过滤测试相关的 DevBoxRelease
	for _, release := range releaseList.Items {
		if t.isTestRelease(release) {
			releases = append(releases, release)
		}
	}

	// 列出测试创建的 Devbox
	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, listOptions...); err != nil {
		log.Printf("列出 Devbox 失败: %v", err)
	} else {
		for _, devbox := range devboxList.Items {
			if t.isTestDevbox(devbox) {
				devboxes = append(devboxes, devbox)
			}
		}
	}

	return releases, devboxes, nil
}

// CleanupWithDetails cleans up test resources and returns details
func (t *DevboxReleaseTester) CleanupWithDetails(ctx context.Context, allNamespaces bool, cleanupDevbox bool) (int, int, error) {
	log.Printf("开始详细清理测试资源...")

	deletedReleases := 0
	deletedDevboxes := 0

	// 删除 DevBoxRelease
	releaseList := &devboxv1alpha2.DevBoxReleaseList{}
	var listOptions []client.ListOption
	if !allNamespaces {
		listOptions = append(listOptions, client.InNamespace(t.config.Namespace))
	}

	if err := t.ctrlClient.List(ctx, releaseList, listOptions...); err != nil {
		log.Printf("列出 DevBoxRelease 失败: %v", err)
	} else {
		for _, release := range releaseList.Items {
			if t.isTestRelease(release) {
				// 尝试移除 finalizer
				if len(release.Finalizers) > 0 {
					release.Finalizers = []string{}
					if err := t.ctrlClient.Update(ctx, &release); err != nil {
						log.Printf("移除 DevBoxRelease %s finalizer 失败: %v", release.Name, err)
					}
				}

				// 删除资源
				if err := t.ctrlClient.Delete(ctx, &release); err != nil {
					log.Printf("删除 DevBoxRelease %s 失败: %v", release.Name, err)
				} else {
					log.Printf("已删除 DevBoxRelease: %s/%s", release.Namespace, release.Name)
					deletedReleases++
				}
			}
		}
	}

	// 删除测试创建的 Devbox（如果启用）
	if cleanupDevbox {
		devboxList := &devboxv1alpha2.DevboxList{}
		if err := t.ctrlClient.List(ctx, devboxList, listOptions...); err != nil {
			log.Printf("列出 Devbox 失败: %v", err)
		} else {
			for _, devbox := range devboxList.Items {
				if t.isTestDevbox(devbox) {
					// 尝试移除 finalizer
					if len(devbox.Finalizers) > 0 {
						devbox.Finalizers = []string{}
						if err := t.ctrlClient.Update(ctx, &devbox); err != nil {
							log.Printf("移除 Devbox %s finalizer 失败: %v", devbox.Name, err)
						}
					}

					// 删除资源
					if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
						log.Printf("删除 Devbox %s 失败: %v", devbox.Name, err)
					} else {
						log.Printf("已删除 Devbox: %s/%s", devbox.Namespace, devbox.Name)
						deletedDevboxes++
					}
				}
			}
		}
	}

	log.Printf("详细清理完成: 删除 %d 个 DevBoxRelease, %d 个 Devbox", deletedReleases, deletedDevboxes)
	return deletedReleases, deletedDevboxes, nil
}

// isTestRelease 判断是否为测试创建的 DevBoxRelease
func (t *DevboxReleaseTester) isTestRelease(release devboxv1alpha2.DevBoxRelease) bool {
	return strings.Contains(release.Name, "release-test-devbox") ||
		strings.Contains(release.Name, "concurrent-release-devbox") ||
		strings.Contains(release.Name, "lifecycle-test")
}

// isTestDevbox 判断是否为测试创建的 Devbox
func (t *DevboxReleaseTester) isTestDevbox(devbox devboxv1alpha2.Devbox) bool {
	// 检查标签
	if devbox.Labels != nil {
		if testType, ok := devbox.Labels["test-type"]; ok && testType == "devbox-release" {
			return true
		}
		if stressTest, ok := devbox.Labels["stress-test"]; ok && stressTest == "true" {
			// 进一步检查名称
			return strings.Contains(devbox.Name, "release-test-devbox") ||
				strings.Contains(devbox.Name, "concurrent-release-devbox")
		}
	}

	// 如果没有标签，通过名称判断
	return strings.Contains(devbox.Name, "release-test-devbox") ||
		strings.Contains(devbox.Name, "concurrent-release-devbox")
}

// RunConcurrentReleaseTest runs concurrent release test
// Flow: Concurrent create Devboxes → Wait for running → Commit → Create Release → Verify
func (t *DevboxReleaseTester) RunConcurrentReleaseTest(ctx context.Context) (*ReleaseTestResult, error) {
	log.Printf("开始并发发版测试: %d 并发创建 %d 个 Devbox 并进行发版", t.config.ConcurrentCount, t.config.ReleaseCount)

	result := &ReleaseTestResult{}
	startTime := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	// 并发创建多个 DevBox，每个进行完整的发版流程
	for i := 0; i < t.config.ReleaseCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devboxName := fmt.Sprintf("concurrent-release-devbox-%d", index)
			releaseName := fmt.Sprintf("%s-release", devboxName)
			version := fmt.Sprintf(t.config.VersionPattern, index)

			log.Printf("========== 开始处理 Devbox %s (版本: %s) ==========", devboxName, version)

			// 步骤 1: 创建 Devbox (Running 状态)
			devboxStart := time.Now()
			if err := t.createBaseDevbox(ctx, devboxName); err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 Devbox %s 失败: %v", devboxName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("创建 Devbox %s 失败: %v", devboxName, err)
				return
			}
			log.Printf("[%s] Devbox 创建成功", devboxName)

			// 步骤 2: 等待 Devbox 运行就绪
			_, err := t.waitForDevboxRunningAndReady(ctx, devboxName, 5*time.Minute)
			if err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("Devbox %s 运行超时: %v", devboxName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("[%s] Devbox 运行超时: %v", devboxName, err)
				return
			}
			log.Printf("[%s] Devbox 已运行就绪", devboxName)

			// 步骤 3: 触发 Commit
			targetState := t.getCommitTargetState()
			commitStart := time.Now()
			if err := t.triggerCommitByStateChange(ctx, devboxName, targetState); err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("触发 Commit 失败: %v", err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("[%s] 触发 Commit 失败: %v", devboxName, err)
				return
			}
			log.Printf("[%s] 已触发 Commit (状态: %s)", devboxName, targetState)

			// 步骤 4: 等待 Commit 完成
			if err := t.waitForCommitComplete(ctx, devboxName, targetState, 10*time.Minute); err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("Commit 超时: %v", err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("[%s] Commit 超时: %v", devboxName, err)
				return
			}
			commitDuration := time.Since(commitStart)
			log.Printf("[%s] Commit 完成，耗时: %v", devboxName, commitDuration)

			// 步骤 5: 创建 DevBoxRelease
			releaseStart := time.Now()
			_, err = t.createDevBoxRelease(ctx, releaseName, devboxName, version)
			if err != nil {
				mu.Lock()
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("创建 DevBoxRelease %s 失败: %v", releaseName, err))
				result.TotalReleases++
				mu.Unlock()
				log.Printf("[%s] 创建 DevBoxRelease 失败: %v", devboxName, err)
				return
			}
			log.Printf("[%s] DevBoxRelease 创建成功", devboxName)

			// 步骤 6: 等待发版完成
			phase, err := t.waitForReleaseComplete(ctx, releaseName, 5*time.Minute)
			releaseDuration := time.Since(releaseStart)
			totalDuration := time.Since(devboxStart)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版超时: %v", releaseName, err))
				log.Printf("[%s] 发版超时: %v", devboxName, err)
			} else if phase == devboxv1alpha2.DevBoxReleasePhaseSuccess {
				result.SuccessfulReleases++
				log.Printf("[%s] 发版成功! Commit: %v, Release: %v, 总耗时: %v",
					devboxName, commitDuration, releaseDuration, totalDuration)

				// // 验证镜像一致性
				// imageCheck := t.verifyImageConsistency(ctx, release)
				// result.ImageVerifications = append(result.ImageVerifications, imageCheck)
			} else if phase == devboxv1alpha2.DevBoxReleasePhaseFailed {
				result.FailedReleases++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s 发版失败", releaseName))
				log.Printf("[%s] 发版失败", devboxName)
			} else {
				result.PendingReleases++
				log.Printf("[%s] 仍在 %s 状态", devboxName, phase)
			}

			result.TotalReleases++
			log.Printf("========== Devbox %s 处理完成 ==========", devboxName)
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

	log.Printf("并发发版测试完成: 成功 %d, 失败 %d, 待处理 %d, QPS: %.2f, 总耗时 %v",
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

// getCommitTargetState 根据配置获取 commit 目标状态
func (t *DevboxReleaseTester) getCommitTargetState() devboxv1alpha2.DevboxState {
	if t.config.CommitTrigger == CommitTriggerByShutdown {
		return devboxv1alpha2.DevboxStateShutdown
	}
	// 默认使用 Stopped
	return devboxv1alpha2.DevboxStateStopped
}
