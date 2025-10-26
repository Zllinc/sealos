package edge

import (
	"context"
	"fmt"
	"log"
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

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
)

// NewCrashRecoveryTester creates a new crash recovery tester
func NewCrashRecoveryTester(config *CrashRecoveryTestConfig) (*CrashRecoveryTester, error) {
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
	helper := tester.NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)

	return &CrashRecoveryTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunCrashRecoveryTest runs the crash recovery test
func (t *CrashRecoveryTester) RunCrashRecoveryTest(ctx context.Context) (*CrashRecoveryTestResult, error) {
	log.Printf("========== start crash recovery test ==========")
	log.Printf("configuration:")
	log.Printf("  namespace: %s", t.config.Namespace)
	log.Printf("  devbox count: %d", t.config.DevboxCount)
	log.Printf("  concurrent count: %d", t.config.ConcurrentCount)
	log.Printf("  crash cycles: %d", t.config.CrashCycles)
	log.Printf("  data size: %s × %d files", t.config.DataSize, t.config.FileCount)
	log.Printf("  recovery timeout: %v", t.config.RecoveryTimeout)

	result := &CrashRecoveryTestResult{}
	startTime := time.Now()

	// Decide sequential or concurrent
	if t.config.ConcurrentCount > 1 {
		result = t.runConcurrentTest(ctx)
	} else {
		result = t.runSequentialTest(ctx)
	}

	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulTests > 0 {
		result.AverageTestTime = result.TotalTestTime / time.Duration(result.SuccessfulTests)
	}

	// Print summary
	log.Printf("\n========== crash recovery test completed ==========")
	log.Printf("total test count: %d", result.TotalTests)
	log.Printf("successful: %d", result.SuccessfulTests)
	log.Printf("failed: %d", result.FailedTests)
	log.Printf("total time: %v", result.TotalTestTime)
	log.Printf("average time: %v", result.AverageTestTime)

	if len(result.ErrorMessages) > 0 {
		log.Printf("\nerror messages:")
		for i, msg := range result.ErrorMessages {
			log.Printf("  [%d] %s", i+1, msg)
		}
	}

	return result, nil
}

// runSequentialTest runs sequential crash recovery test
func (t *CrashRecoveryTester) runSequentialTest(ctx context.Context) *CrashRecoveryTestResult {
	result := &CrashRecoveryTestResult{}

	for i := 0; i < t.config.DevboxCount; i++ {
		devboxName := fmt.Sprintf("edge-crash-devbox-%d", i)
		log.Printf("\n[%d/%d] start testing devbox: %s", i+1, t.config.DevboxCount, devboxName)

		detail := t.testSingleDevboxCrashRecovery(ctx, devboxName)

		result.Details = append(result.Details, detail)
		result.TotalTests++

		if detail.DataWriteSuccess && detail.DataVerifySuccess && detail.CrashCycles == t.config.CrashCycles {
			result.SuccessfulTests++
			log.Printf("[%d/%d] ✓ devbox %s test successful", i+1, t.config.DevboxCount, devboxName)
		} else {
			result.FailedTests++
			if detail.Error != "" {
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %s", devboxName, detail.Error))
			}
			log.Printf("[%d/%d] ✗ devbox %s test failed: %s", i+1, t.config.DevboxCount, devboxName, detail.Error)
		}
	}

	return result
}

// runConcurrentTest runs concurrent crash recovery test
func (t *CrashRecoveryTester) runConcurrentTest(ctx context.Context) *CrashRecoveryTestResult {
	result := &CrashRecoveryTestResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := 0; i < t.config.DevboxCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devboxName := fmt.Sprintf("edge-crash-devbox-%d", index)
			log.Printf("\n[%d/%d] start testing devbox: %s", index+1, t.config.DevboxCount, devboxName)

			detail := t.testSingleDevboxCrashRecovery(ctx, devboxName)

			mu.Lock()
			defer mu.Unlock()

			result.Details = append(result.Details, detail)
			result.TotalTests++

			if detail.DataWriteSuccess && detail.DataVerifySuccess && detail.CrashCycles == t.config.CrashCycles {
				result.SuccessfulTests++
				log.Printf("[%d/%d] ✓ devbox %s test successful", index+1, t.config.DevboxCount, devboxName)
			} else {
				result.FailedTests++
				if detail.Error != "" {
					result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("%s: %s", devboxName, detail.Error))
				}
				log.Printf("[%d/%d] ✗ devbox %s test failed: %s", index+1, t.config.DevboxCount, devboxName, detail.Error)
			}
		}(i)
	}

	wg.Wait()
	return result
}

// testSingleDevboxCrashRecovery tests crash recovery for a single devbox
// Note: Since Devbox uses restartPolicy=Never, we manually recreate Pod after crash
func (t *CrashRecoveryTester) testSingleDevboxCrashRecovery(ctx context.Context, name string) CrashRecoveryTestDetail {
	detail := CrashRecoveryTestDetail{
		DevboxName: name,
	}

	startTime := time.Now()

	// Phase 1: Preparation - Create Devbox
	log.Printf("[%s] phase 1: create devbox", name)
	if err := t.createDevbox(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("create devbox failed: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// Wait for Pod fully ready
	log.Printf("[%s] phase 1: wait for pod ready", name)
	devbox, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, 5*time.Minute)
	if err != nil {
		detail.Error = fmt.Sprintf("wait for running timeout: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// Write test data
	log.Printf("[%s] phase 1: write test data (%s × %d files)", name, t.config.DataSize, t.config.FileCount)
	dataDir := "crash_recovery_data"
	if err := t.helper.WriteTestDataToDevbox(ctx, *devbox, dataDir, t.config.DataSize, t.config.FileCount); err != nil {
		detail.Error = fmt.Sprintf("write data failed: %v", err)
		detail.DataWriteSuccess = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.DataWriteSuccess = true
	log.Printf("[%s] data write successful", name)

	// Phase 2-4: Crash and Recovery Cycles
	log.Printf("[%s] starting %d crash-recovery cycles", name, t.config.CrashCycles)

	for cycle := 0; cycle < t.config.CrashCycles; cycle++ {
		cycleInfo := CrashRecoveryInfo{
			CycleNumber: cycle + 1,
		}

		// Phase 2: Crash - Kill critical processes
		log.Printf("[%s] cycle %d/%d: phase 2 - killing critical processes", name, cycle+1, t.config.CrashCycles)
		cycleInfo.CrashTime = time.Now()
		if err := t.killCriticalProcesses(ctx, *devbox); err != nil {
			detail.Error = fmt.Sprintf("kill process failed (cycle %d): %v", cycle+1, err)
			detail.CrashCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			cycleInfo.Error = err.Error()
			detail.CrashRecoveries = append(detail.CrashRecoveries, cycleInfo)
			return detail
		}

		// Wait for Pod termination (restartPolicy=Never means no auto-restart)
		log.Printf("[%s] cycle %d/%d: waiting for pod termination", name, cycle+1, t.config.CrashCycles)
		if err := t.waitForPodTerminated(ctx, *devbox); err != nil {
			detail.Error = fmt.Sprintf("wait for pod termination failed (cycle %d): %v", cycle+1, err)
			detail.CrashCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			cycleInfo.Error = err.Error()
			detail.CrashRecoveries = append(detail.CrashRecoveries, cycleInfo)
			return detail
		}
		cycleInfo.PodTerminated = true
		log.Printf("[%s] cycle %d/%d: pod terminated successfully", name, cycle+1, t.config.CrashCycles)

		// Optional wait after crash
		if t.config.WaitAfterCrash > 0 {
			time.Sleep(t.config.WaitAfterCrash)
		}

		// Phase 3: Manual Recovery - Delete Pod and wait for controller to recreate
		log.Printf("[%s] cycle %d/%d: phase 3 - deleting pod to trigger recreation", name, cycle+1, t.config.CrashCycles)
		if err := t.deletePodAndWaitForRecreation(ctx, *devbox); err != nil {
			detail.Error = fmt.Sprintf("pod recreation failed (cycle %d): %v", cycle+1, err)
			detail.CrashCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			cycleInfo.Error = err.Error()
			detail.CrashRecoveries = append(detail.CrashRecoveries, cycleInfo)
			return detail
		}
		cycleInfo.PodRecreated = true
		log.Printf("[%s] cycle %d/%d: pod recreated successfully", name, cycle+1, t.config.CrashCycles)

		// Wait for new Pod to be ready
		log.Printf("[%s] cycle %d/%d: waiting for new pod ready", name, cycle+1, t.config.CrashCycles)
		devbox, err = t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, t.config.RecoveryTimeout)
		if err != nil {
			detail.Error = fmt.Sprintf("wait for pod ready failed (cycle %d): %v", cycle+1, err)
			detail.CrashCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			cycleInfo.Error = err.Error()
			detail.CrashRecoveries = append(detail.CrashRecoveries, cycleInfo)
			return detail
		}

		cycleInfo.RecoveryTime = time.Now()
		cycleInfo.RecoveryDuration = cycleInfo.RecoveryTime.Sub(cycleInfo.CrashTime)

		// Phase 4: Verify data persistence
		log.Printf("[%s] cycle %d/%d: phase 4 - verifying data persistence", name, cycle+1, t.config.CrashCycles)
		if err := t.helper.VerifyTestDataInDevbox(ctx, *devbox, dataDir); err != nil {
			detail.Error = fmt.Sprintf("data verification failed after cycle %d: %v", cycle+1, err)
			detail.CrashCycles = cycle + 1 // This cycle completed but data lost
			detail.TotalDuration = time.Since(startTime)
			cycleInfo.Error = fmt.Sprintf("data verification failed: %v", err)
			cycleInfo.DataPersisted = false
			detail.CrashRecoveries = append(detail.CrashRecoveries, cycleInfo)
			return detail
		}
		cycleInfo.DataPersisted = true
		cycleInfo.Recovered = true

		log.Printf("[%s] cycle %d/%d: ✓ recovery successful (duration: %v, data persisted: true)",
			name, cycle+1, t.config.CrashCycles, cycleInfo.RecoveryDuration)

		detail.CrashRecoveries = append(detail.CrashRecoveries, cycleInfo)
	}

	detail.CrashCycles = t.config.CrashCycles
	detail.DataVerifySuccess = true

	detail.TotalDuration = time.Since(startTime)
	log.Printf("[%s] test completed, total duration: %v", name, detail.TotalDuration)

	return detail
}

// createDevbox creates a Devbox
func (t *CrashRecoveryTester) createDevbox(ctx context.Context, name string) error {
	spec := tester.DevboxCreateSpec{
		Name:      name,
		Namespace: t.config.Namespace,
		Labels: map[string]string{
			"stress-test": "true",
			"test-type":   "edge-crash-recovery",
		},
		Image:        t.config.Image,
		CPU:          t.config.CPU,
		Memory:       t.config.Memory,
		StorageLimit: t.config.Storage,
	}

	return t.helper.CreateDevbox(ctx, spec)
}

// killCriticalProcesses kills critical processes to cause container termination
// Strategy: Kill sleep infinity and other key processes (avoid PID 1 which is protected by dumb-init)
func (t *CrashRecoveryTester) killCriticalProcesses(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	pods, err := t.helper.GetDevboxPods(ctx, devbox)
	if err != nil {
		return fmt.Errorf("get pods failed: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("no pod found")
	}

	pod := pods[0]
	containerName := "devbox"
	if len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	log.Printf("[%s] killing container processes in pod %s", devbox.Name, pod.Name)

	// Strategy: Kill critical processes (avoid PID 1 as it's often protected by init wrappers like dumb-init)
	// We kill multiple processes to ensure the container crashes
	killCommands := [][]string{
		// 1. Kill sleep processes (commonly used to keep container alive)
		{"sh", "-c", "pkill -9 sleep"},
		// 2. Kill startup scripts
		{"sh", "-c", "pkill -9 -f startup.sh"},
		// 3. Kill sudo processes
		{"sh", "-c", "pkill -9 sudo"},
		// 4. Kill sshd (SSH daemon)
		{"sh", "-c", "pkill -9 sshd"},
		// 5. Kill all bash/sh processes
		{"sh", "-c", "pkill -9 bash; pkill -9 sh"},
		// 6. Kill all processes except PID 1 (init)
		{"sh", "-c", "ps aux | awk 'NR>1 && $2!=1 {print $2}' | xargs -r kill -9"},
		// 7. As last resort, try triggering OOM
		{"sh", "-c", "tail /dev/zero &"},
	}

	successfulKills := 0
	for i, cmd := range killCommands {
		err := t.helper.ExecCommandInPod(ctx, pod.Namespace, pod.Name, containerName, cmd)
		if err == nil {
			successfulKills++
		}
		log.Printf("[%s] kill command %d/%d executed (success: %v)", devbox.Name, i+1, len(killCommands), err == nil)

		// Give it a moment to take effect
		time.Sleep(500 * time.Millisecond)

		// Check if container crashed early
		pods, err := t.helper.GetDevboxPods(ctx, devbox)
		if err == nil && len(pods) > 0 {
			pod = pods[0]
			if len(pod.Status.ContainerStatuses) > 0 {
				containerStatus := pod.Status.ContainerStatuses[0]
				if containerStatus.State.Terminated != nil || containerStatus.State.Waiting != nil {
					log.Printf("[%s] ✓ container crashed after command %d", devbox.Name, i+1)
					return nil
				}
			}
		}
	}

	// Final check after all commands executed
	log.Printf("[%s] executed %d kill commands, checking final state...", devbox.Name, successfulKills)
	time.Sleep(3 * time.Second)

	pods, err = t.helper.GetDevboxPods(ctx, devbox)
	if err == nil && len(pods) > 0 {
		pod = pods[0]
		if len(pod.Status.ContainerStatuses) > 0 {
			containerStatus := pod.Status.ContainerStatuses[0]
			if containerStatus.State.Terminated != nil || containerStatus.State.Waiting != nil {
				log.Printf("[%s] ✓ container crashed (detected in final check)", devbox.Name)
				return nil
			}

			// Log current state for debugging
			log.Printf("[%s] container still running - state: %+v", devbox.Name, containerStatus.State)
		}
	}

	return fmt.Errorf("failed to crash container after %d kill attempts (Note: restartPolicy might be 'Never', check Pod spec)", successfulKills)
}

// waitForPodTerminated waits for Pod to terminate (all containers stopped)
// Since restartPolicy=Never, the Pod won't auto-restart
func (t *CrashRecoveryTester) waitForPodTerminated(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting for pod termination...", devbox.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("pod termination timeout")
			}

			pods, err := t.helper.GetDevboxPods(ctx, devbox)
			if err != nil {
				continue
			}

			if len(pods) == 0 {
				log.Printf("[%s] ✓ pod has been deleted", devbox.Name)
				return nil
			}

			pod := pods[0]

			// Check Pod phase
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				log.Printf("[%s] ✓ pod terminated (phase: %s)", devbox.Name, pod.Status.Phase)
				return nil
			}

			// Check container statuses
			if len(pod.Status.ContainerStatuses) > 0 {
				containerStatus := pod.Status.ContainerStatuses[0]
				if containerStatus.State.Terminated != nil {
					log.Printf("[%s] ✓ container terminated (exit code: %d, reason: %s)",
						devbox.Name,
						containerStatus.State.Terminated.ExitCode,
						containerStatus.State.Terminated.Reason)
					// Wait a bit for Pod phase to update
					time.Sleep(2 * time.Second)
					return nil
				}
			}

			log.Printf("[%s] pod still running (phase: %s), waiting...", devbox.Name, pod.Status.Phase)
		}
	}
}

// deletePodAndWaitForRecreation deletes the Pod and waits for Devbox controller to recreate it
func (t *CrashRecoveryTester) deletePodAndWaitForRecreation(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	// Step 1: Get current Pod
	pods, err := t.helper.GetDevboxPods(ctx, devbox)
	if err != nil || len(pods) == 0 {
		return fmt.Errorf("no pod found to delete")
	}

	oldPodName := pods[0].Name
	oldPodUID := pods[0].UID

	// Step 2: Delete the Pod
	log.Printf("[%s] deleting pod %s to trigger recreation...", devbox.Name, oldPodName)
	err = t.k8sClient.CoreV1().Pods(devbox.Namespace).Delete(ctx, oldPodName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete pod failed: %w", err)
	}

	// Step 3: Wait for old Pod to be deleted
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting for old pod deletion...", devbox.Name)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for pod recreation")
			}

			pods, err := t.helper.GetDevboxPods(ctx, devbox)
			if err != nil || len(pods) == 0 {
				log.Printf("[%s] old pod deleted, waiting for new pod...", devbox.Name)
				time.Sleep(2 * time.Second) // Give controller time to react
				continue
			}

			// Check if this is a new Pod (different UID)
			newPod := pods[0]
			if newPod.UID != oldPodUID {
				log.Printf("[%s] ✓ new pod created: %s (UID changed: %s -> %s)",
					devbox.Name, newPod.Name, oldPodUID, newPod.UID)
				return nil
			}

			log.Printf("[%s] still seeing old pod, waiting for deletion...", devbox.Name)
		}
	}
}

// Cleanup cleans up test resources
func (t *CrashRecoveryTester) Cleanup(ctx context.Context) error {
	log.Printf("cleanup crash recovery test resources...")

	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		return fmt.Errorf("list devbox failed: %w", err)
	}

	deletedCount := 0
	for _, devbox := range devboxList.Items {
		if devbox.Labels != nil {
			if testType, ok := devbox.Labels["test-type"]; ok && testType == "edge-crash-recovery" {
				if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
					log.Printf("delete devbox %s failed: %v", devbox.Name, err)
				} else {
					log.Printf("devbox %s deleted", devbox.Name)
					deletedCount++
				}
			}
		}
	}

	log.Printf("cleanup completed, deleted %d devboxes", deletedCount)
	return nil
}
