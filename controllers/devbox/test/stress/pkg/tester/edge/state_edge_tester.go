package edge

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

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
)

// NewStateEdgeTester creates a new state edge tester
func NewStateEdgeTester(config *StateToggleTestConfig) (*StateEdgeTester, error) {
	// create kubernetes client
	var restConfig *rest.Config
	var err error

	// try to create config from kubeconfig file or cluster config
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

	// create controller-runtime client
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

	// create common helper
	helper := tester.NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)

	return &StateEdgeTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunStateToggleTest runs state toggle test
func (t *StateEdgeTester) RunStateToggleTest(ctx context.Context) (*StateToggleTestResult, error) {
	log.Printf("========== start state toggle edge test ==========")
	log.Printf("configuration:")
	log.Printf("   namespace: %s", t.config.Namespace)
	log.Printf("   devbox count: %d", t.config.DevboxCount)
	log.Printf("   concurrent count: %d", t.config.ConcurrentCount)
	log.Printf("   toggle mode: %s", t.config.StateMode)
	log.Printf("   toggle cycles: %d", t.config.ToggleCycles)
	log.Printf("   wait after state: %v", t.config.WaitAfterState)
	log.Printf("   data size: %s × %d files", t.config.DataSize, t.config.FileCount)

	result := &StateToggleTestResult{}
	startTime := time.Now()

	// check if it is sequential or concurrent
	if t.config.ConcurrentCount > 1 {
		result = t.runConcurrentToggleTest(ctx)
	} else {
		result = t.runSequentialToggleTest(ctx)
	}

	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulTests > 0 {
		result.AverageTestTime = result.TotalTestTime / time.Duration(result.SuccessfulTests)
	}

	// 打印汇总
	log.Printf("\n========== state toggle test completed ==========")
	log.Printf("total test count: %d", result.TotalDevboxes)
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

// runSequentialToggleTest runs sequential state toggle test
func (t *StateEdgeTester) runSequentialToggleTest(ctx context.Context) *StateToggleTestResult {
	result := &StateToggleTestResult{}

	for i := 0; i < t.config.DevboxCount; i++ {
		devboxName := fmt.Sprintf("edge-toggle-devbox-%d", i)
		log.Printf("\n[%d/%d] start testing devbox: %s", i+1, t.config.DevboxCount, devboxName)

		detail := t.testSingleDevboxToggle(ctx, devboxName)

		result.ToggleDetails = append(result.ToggleDetails, detail)
		result.TotalDevboxes++

		if detail.DataWriteSuccess && detail.DataVerifySuccess && detail.ResourceCheckOK {
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

// runConcurrentToggleTest runs concurrent state toggle test
func (t *StateEdgeTester) runConcurrentToggleTest(ctx context.Context) *StateToggleTestResult {
	result := &StateToggleTestResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	// create semaphore to control concurrent count
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := 0; i < t.config.DevboxCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devboxName := fmt.Sprintf("edge-toggle-devbox-%d", index)
			log.Printf("\n[%d/%d] start testing devbox: %s", index+1, t.config.DevboxCount, devboxName)

			detail := t.testSingleDevboxToggle(ctx, devboxName)

			mu.Lock()
			defer mu.Unlock()

			result.ToggleDetails = append(result.ToggleDetails, detail)
			result.TotalDevboxes++

			if detail.DataWriteSuccess && detail.DataVerifySuccess && detail.ResourceCheckOK {
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

// testSingleDevboxToggle tests the state toggle of a single devbox
func (t *StateEdgeTester) testSingleDevboxToggle(ctx context.Context, name string) StateToggleTestDetail {
	detail := StateToggleTestDetail{
		DevboxName: name,
	}

	startTime := time.Now()

	// step 1: create devbox
	log.Printf("[%s] step 1: create devbox", name)
	if err := t.createDevbox(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("create devbox failed: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// step 2: wait for devbox running and all resources ready (including pod container)
	log.Printf("[%s] step 2: wait for devbox running and all resources ready", name)
	devbox, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, 5*time.Minute)
	if err != nil {
		detail.Error = fmt.Sprintf("wait for running timeout: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// step 3: write test data
	log.Printf("[%s] step 3: write test data (%s × %d)", name, t.config.DataSize, t.config.FileCount)
	dataDir := "edge_toggle_data"
	if err := t.helper.WriteTestDataToDevbox(ctx, *devbox, dataDir, t.config.DataSize, t.config.FileCount); err != nil {
		detail.Error = fmt.Sprintf("write data failed: %v", err)
		detail.DataWriteSuccess = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.DataWriteSuccess = true
	log.Printf("[%s] data write successful", name)

	// step 4: loop toggle power on/off
	log.Printf("[%s] step 4: start %d times toggle power on/off loop", name, t.config.ToggleCycles)
	targetState := t.getTargetState()

	for cycle := 0; cycle < t.config.ToggleCycles; cycle++ {
		cycleStart := time.Now()

		// 4.1: power off (Running → Stopped/Shutdown)
		log.Printf("[%s] loop %d/%d: toggle to %s", name, cycle+1, t.config.ToggleCycles, targetState)
		if err := t.changeDevboxState(ctx, name, targetState); err != nil {
			detail.Error = fmt.Sprintf("power off failed: %v, cycle: %d", err, cycle+1)
			detail.ToggleCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			return detail
		}

		// 4.2: wait for state confirmation
		log.Printf("[%s] loop %d/%d: wait for state to be %s", name, cycle+1, t.config.ToggleCycles, targetState)
		if err := t.helper.WaitForDevboxState(ctx, t.config.Namespace, name, targetState, t.config.StateTimeout); err != nil {
			detail.Error = fmt.Sprintf("power off state confirmation timeout: %v, cycle: %d", err, cycle+1)
			detail.ToggleCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			return detail
		}
		log.Printf("[%s] loop %d/%d: confirmed %s state", name, cycle+1, t.config.ToggleCycles, targetState)

		// 4.3: extra wait (optional, for stability test)
		if t.config.WaitAfterState > 0 {
			time.Sleep(t.config.WaitAfterState)
		}

		// 4.4: power on (Stopped/Shutdown → Running)
		log.Printf("[%s] loop %d/%d: toggle to Running", name, cycle+1, t.config.ToggleCycles)
		if err := t.changeDevboxState(ctx, name, devboxv1alpha2.DevboxStateRunning); err != nil {
			detail.Error = fmt.Sprintf("power on failed: %v, cycle: %d", err, cycle+1)
			detail.ToggleCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			return detail
		}

		// 4.5: wait for power on state confirmation
		log.Printf("[%s] loop %d/%d: wait for state to be Running", name, cycle+1, t.config.ToggleCycles)
		if err := t.helper.WaitForDevboxState(ctx, t.config.Namespace, name, devboxv1alpha2.DevboxStateRunning, t.config.StateTimeout); err != nil {
			detail.Error = fmt.Sprintf("power on state confirmation timeout: %v, cycle: %d", err, cycle+1)
			detail.ToggleCycles = cycle
			detail.TotalDuration = time.Since(startTime)
			return detail
		}
		log.Printf("[%s] loop %d/%d: confirmed Running state", name, cycle+1, t.config.ToggleCycles)

		// 4.6: extra wait (optional, for stability test)
		if t.config.WaitAfterState > 0 {
			time.Sleep(t.config.WaitAfterState)
		}

		cycleDuration := time.Since(cycleStart)
		detail.ToggleDurations = append(detail.ToggleDurations, cycleDuration)
		log.Printf("[%s] loop %d/%d completed, duration: %v", name, cycle+1, t.config.ToggleCycles, cycleDuration)
	}

	detail.ToggleCycles = t.config.ToggleCycles
	log.Printf("[%s] all toggle loops completed", name)

	// step 5: wait for last Running ready
	log.Printf("[%s] step 5: wait for devbox completely ready", name)
	devbox, err = t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, 5*time.Minute)
	if err != nil {
		detail.Error = fmt.Sprintf("wait for last Running timeout: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// step 6: verify data integrity
	log.Printf("[%s] step 6: verify data integrity", name)
	if err := t.helper.VerifyTestDataInDevbox(ctx, *devbox, dataDir); err != nil {
		detail.Error = fmt.Sprintf("data verification failed: %v", err)
		detail.DataVerifySuccess = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.DataVerifySuccess = true
	log.Printf("[%s] data verification successful", name)

	// step 7: check resources
	log.Printf("[%s] step 7: check resources", name)
	missing := t.checkResources(ctx, *devbox)
	if len(missing) > 0 {
		detail.Error = fmt.Sprintf("resources check failed: missing %s", missing)
		detail.MissingResources = missing
		detail.ResourceCheckOK = false
	} else {
		detail.ResourceCheckOK = true
		log.Printf("[%s] resources check successful", name)
	}

	detail.TotalDuration = time.Since(startTime)
	log.Printf("[%s] test completed, total duration: %v", name, detail.TotalDuration)

	return detail
}

// createDevbox creates a devbox
func (t *StateEdgeTester) createDevbox(ctx context.Context, name string) error {
	spec := tester.DevboxCreateSpec{
		Name:      name,
		Namespace: t.config.Namespace,
		Labels: map[string]string{
			"stress-test": "true",
			"test-type":   "edge-state-toggle",
		},
		Image:        t.config.Image,
		CPU:          t.config.CPU,
		Memory:       t.config.Memory,
		StorageLimit: t.config.Storage,
	}

	return t.helper.CreateDevbox(ctx, spec)
}

// changeDevboxState changes the state of a devbox
func (t *StateEdgeTester) changeDevboxState(ctx context.Context, name string, targetState devboxv1alpha2.DevboxState) error {
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: t.config.Namespace,
		Name:      name,
	}, devbox); err != nil {
		return fmt.Errorf("get devbox failed: %w", err)
	}

	devbox.Spec.State = targetState
	if err := t.ctrlClient.Update(ctx, devbox); err != nil {
		return fmt.Errorf("update devbox state failed: %w", err)
	}

	return nil
}

// getTargetState gets the target shutdown state
func (t *StateEdgeTester) getTargetState() devboxv1alpha2.DevboxState {
	if t.config.StateMode == StateToggleShutdown {
		return devboxv1alpha2.DevboxStateShutdown
	}
	return devboxv1alpha2.DevboxStateStopped
}

// checkResources checks the completeness of resources
func (t *StateEdgeTester) checkResources(ctx context.Context, devbox devboxv1alpha2.Devbox) string {
	var missing []string

	if !t.helper.IsPodRunning(ctx, devbox) {
		missing = append(missing, "Pod")
	}
	if !t.helper.IsServiceCreated(ctx, devbox) {
		missing = append(missing, "Service")
	}
	if !t.helper.IsSecretCreated(ctx, devbox) {
		missing = append(missing, "Secret")
	}
	// if !t.helper.IsLVMCreated(ctx, devbox) {
	// 	missing = append(missing, "LVM")
	// }

	if len(missing) == 0 {
		return ""
	}

	result := ""
	for i, res := range missing {
		if i > 0 {
			result += ", "
		}
		result += res
	}
	return result
}

// Cleanup cleans up test resources
func (t *StateEdgeTester) Cleanup(ctx context.Context) error {
	log.Printf("cleanup state toggle test resources...")

	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		return fmt.Errorf("list devbox failed: %w", err)
	}

	deletedCount := 0
	for _, devbox := range devboxList.Items {
		if devbox.Labels != nil {
			if testType, ok := devbox.Labels["test-type"]; ok && testType == "edge-state-toggle" {
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
