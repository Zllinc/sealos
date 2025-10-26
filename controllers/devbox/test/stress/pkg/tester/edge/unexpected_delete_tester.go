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

// NewUnexpectedDeleteTester creates a new unexpected delete tester
func NewUnexpectedDeleteTester(config *UnexpectedDeleteTestConfig) (*UnexpectedDeleteTester, error) {
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

	return &UnexpectedDeleteTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
		helper:     helper,
	}, nil
}

// RunUnexpectedDeleteTest runs the unexpected delete test
func (t *UnexpectedDeleteTester) RunUnexpectedDeleteTest(ctx context.Context) (*UnexpectedDeleteTestResult, error) {
	log.Printf("========== start unexpected delete test ==========")
	log.Printf("configuration:")
	log.Printf("  namespace: %s", t.config.Namespace)
	log.Printf("  devbox count: %d", t.config.DevboxCount)
	log.Printf("  concurrent count: %d", t.config.ConcurrentCount)
	log.Printf("  target state: %s", t.config.DevboxState)
	log.Printf("  recovery timeout: %v", t.config.RecoveryTimeout)

	result := &UnexpectedDeleteTestResult{}
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
	log.Printf("\n========== unexpected delete test completed ==========")
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

// runSequentialTest runs sequential unexpected delete test
func (t *UnexpectedDeleteTester) runSequentialTest(ctx context.Context) *UnexpectedDeleteTestResult {
	result := &UnexpectedDeleteTestResult{}

	for i := 0; i < t.config.DevboxCount; i++ {
		devboxName := fmt.Sprintf("edge-delete-devbox-%d", i)
		log.Printf("\n[%d/%d] start testing devbox: %s", i+1, t.config.DevboxCount, devboxName)

		detail := t.testSingleDevboxUnexpectedDelete(ctx, devboxName)

		result.Details = append(result.Details, detail)
		result.TotalTests++

		if detail.PodDeleteOK && detail.SecretDeleteOK && detail.ServiceDeleteOK {
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

// runConcurrentTest runs concurrent unexpected delete test
func (t *UnexpectedDeleteTester) runConcurrentTest(ctx context.Context) *UnexpectedDeleteTestResult {
	result := &UnexpectedDeleteTestResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := 0; i < t.config.DevboxCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			devboxName := fmt.Sprintf("edge-delete-devbox-%d", index)
			log.Printf("\n[%d/%d] start testing devbox: %s", index+1, t.config.DevboxCount, devboxName)

			detail := t.testSingleDevboxUnexpectedDelete(ctx, devboxName)

			mu.Lock()
			defer mu.Unlock()

			result.Details = append(result.Details, detail)
			result.TotalTests++

			if detail.PodDeleteOK && detail.SecretDeleteOK && detail.ServiceDeleteOK {
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

// testSingleDevboxUnexpectedDelete tests unexpected delete for a single devbox
func (t *UnexpectedDeleteTester) testSingleDevboxUnexpectedDelete(ctx context.Context, name string) UnexpectedDeleteTestDetail {
	detail := UnexpectedDeleteTestDetail{
		DevboxName: name,
		State:      t.config.DevboxState,
	}

	startTime := time.Now()

	// Step 1: Create Devbox with specified state
	log.Printf("[%s] step 1: create devbox (target state: %s)", name, t.config.DevboxState)
	if err := t.createDevboxWithState(ctx, name); err != nil {
		detail.Error = fmt.Sprintf("create devbox failed: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// Get devbox object
	devbox := &devboxv1alpha2.Devbox{}
	if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
		detail.Error = fmt.Sprintf("get devbox failed: %v", err)
		detail.TotalDuration = time.Since(startTime)
		return detail
	}

	// Step 2: Delete Pod and check recovery
	log.Printf("[%s] step 2: delete pod and check recovery", name)
	if err := t.deletePodAndCheck(ctx, *devbox); err != nil {
		detail.Error = fmt.Sprintf("pod delete test failed: %v", err)
		detail.PodDeleteOK = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.PodDeleteOK = true
	detail.PodRecovered = true

	// Step 3: Delete Secret and check recovery
	log.Printf("[%s] step 3: delete secret and check recovery", name)
	if err := t.deleteSecretAndCheck(ctx, *devbox); err != nil {
		detail.Error = fmt.Sprintf("secret delete test failed: %v", err)
		detail.SecretDeleteOK = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.SecretDeleteOK = true
	detail.SecretRecovered = true

	// Step 4: Delete Service and check recovery
	log.Printf("[%s] step 4: delete service and check recovery", name)
	if err := t.deleteServiceAndCheck(ctx, *devbox); err != nil {
		detail.Error = fmt.Sprintf("service delete test failed: %v", err)
		detail.ServiceDeleteOK = false
		detail.TotalDuration = time.Since(startTime)
		return detail
	}
	detail.ServiceDeleteOK = true
	detail.ServiceRecovered = true

	detail.TotalDuration = time.Since(startTime)
	log.Printf("[%s] test completed, total duration: %v", name, detail.TotalDuration)

	return detail
}

// createDevboxWithState creates a Devbox with the specified state
func (t *UnexpectedDeleteTester) createDevboxWithState(ctx context.Context, name string) error {
	// Create Devbox in Running state first
	spec := tester.DevboxCreateSpec{
		Name:      name,
		Namespace: t.config.Namespace,
		Labels: map[string]string{
			"stress-test": "true",
			"test-type":   "edge-unexpected-delete",
		},
		Image:        t.config.Image,
		CPU:          t.config.CPU,
		Memory:       t.config.Memory,
		StorageLimit: t.config.Storage,
	}

	if err := t.helper.CreateDevbox(ctx, spec); err != nil {
		return fmt.Errorf("create devbox failed: %w", err)
	}

	// Wait for Running state
	_, err := t.helper.WaitForDevboxRunningWithResources(ctx, t.config.Namespace, name, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("wait for running timeout: %w", err)
	}

	// If target state is not Running, trigger commit
	if t.config.DevboxState != "Running" {
		log.Printf("[%s] triggering commit to %s state", name, t.config.DevboxState)

		// Change state to trigger commit
		devbox := &devboxv1alpha2.Devbox{}
		if err := t.ctrlClient.Get(ctx, client.ObjectKey{Namespace: t.config.Namespace, Name: name}, devbox); err != nil {
			return fmt.Errorf("get devbox failed: %w", err)
		}

		devbox.Spec.State = devboxv1alpha2.DevboxState(t.config.DevboxState)
		if err := t.ctrlClient.Update(ctx, devbox); err != nil {
			return fmt.Errorf("update devbox state failed: %w", err)
		}

		// Wait for commit to complete
		targetState := devboxv1alpha2.DevboxState(t.config.DevboxState)
		if err := t.helper.WaitForDevboxState(ctx, t.config.Namespace, name, targetState, t.config.RecoveryTimeout); err != nil {
			return fmt.Errorf("wait for commit timeout: %w", err)
		}

		log.Printf("[%s] devbox state changed to %s", name, t.config.DevboxState)
	}

	return nil
}

// deletePodAndCheck deletes Pod and checks recovery
func (t *UnexpectedDeleteTester) deletePodAndCheck(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	// Find Pod
	pods, err := t.helper.GetDevboxPods(ctx, devbox)
	if err != nil {
		return fmt.Errorf("get pods failed: %w", err)
	}

	// For Stopped/Shutdown state, Pod should not exist
	if t.config.DevboxState == "Stopped" || t.config.DevboxState == "Shutdown" {
		if len(pods) == 0 {
			log.Printf("[%s] ✓ pod not exists as expected (state: %s)", devbox.Name, t.config.DevboxState)
			return nil
		}
		// If Pod exists in Stopped/Shutdown state, this is an issue, but delete it anyway
	}

	if len(pods) == 0 {
		log.Printf("[%s] no pod to delete", devbox.Name)
		return nil
	}

	// Delete the first Pod
	pod := pods[0]
	log.Printf("[%s] deleting pod: %s", devbox.Name, pod.Name)
	if err := t.k8sClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete pod failed: %w", err)
	}

	// Wait for recovery or confirm non-existence
	if t.config.DevboxState == "Stopped" || t.config.DevboxState == "Shutdown" {
		// For Stopped/Shutdown, Pod should NOT recover
		return t.waitForPodNotExists(ctx, devbox)
	} else {
		// For Running, Pod should recover
		return t.waitForPodRecovery(ctx, devbox)
	}
}

// deleteSecretAndCheck deletes Secret and checks recovery
func (t *UnexpectedDeleteTester) deleteSecretAndCheck(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	// Find Secret
	secrets, err := t.helper.GetDevboxSecrets(ctx, devbox)
	if err != nil {
		return fmt.Errorf("get secrets failed: %w", err)
	}

	if len(secrets) == 0 {
		log.Printf("[%s] no secret to delete", devbox.Name)
		return nil
	}

	// Delete the first Secret
	secret := secrets[0]
	log.Printf("[%s] deleting secret: %s", devbox.Name, secret.Name)
	if err := t.k8sClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete secret failed: %w", err)
	}

	// Wait for recovery (Secret should always recover in all states)
	return t.waitForSecretRecovery(ctx, devbox)
}

// deleteServiceAndCheck deletes Service and checks recovery
func (t *UnexpectedDeleteTester) deleteServiceAndCheck(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	// Find Service
	services, err := t.helper.GetDevboxServices(ctx, devbox)
	if err != nil {
		return fmt.Errorf("get services failed: %w", err)
	}

	// For Shutdown state, Service should not exist
	if t.config.DevboxState == "Shutdown" {
		if len(services) == 0 {
			log.Printf("[%s] ✓ service not exists as expected (state: Shutdown)", devbox.Name)
			return nil
		}
		// If Service exists in Shutdown state, this is an issue, but delete it anyway
	}

	if len(services) == 0 {
		log.Printf("[%s] no service to delete", devbox.Name)
		return nil
	}

	// Delete the first Service
	service := services[0]
	log.Printf("[%s] deleting service: %s", devbox.Name, service.Name)
	if err := t.k8sClient.CoreV1().Services(service.Namespace).Delete(ctx, service.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete service failed: %w", err)
	}

	// Wait for recovery or confirm non-existence
	if t.config.DevboxState == "Shutdown" {
		// For Shutdown, Service should NOT recover
		return t.waitForServiceNotExists(ctx, devbox)
	} else {
		// For Running/Stopped, Service should recover
		return t.waitForServiceRecovery(ctx, devbox)
	}
}

// waitForPodRecovery waits for Pod to recover
func (t *UnexpectedDeleteTester) waitForPodRecovery(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting for pod to recover...", devbox.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("pod recovery timeout")
			}

			if t.helper.IsPodRunning(ctx, devbox) {
				log.Printf("[%s] ✓ pod recovered and running", devbox.Name)
				return nil
			}
		}
	}
}

// waitForPodNotExists waits to confirm Pod does not exist
func (t *UnexpectedDeleteTester) waitForPodNotExists(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting to confirm pod does not exist...", devbox.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for pod to not exist")
			}

			pods, err := t.helper.GetDevboxPods(ctx, devbox)
			if err != nil {
				continue
			}

			if len(pods) == 0 {
				log.Printf("[%s] ✓ pod does not exist as expected", devbox.Name)
				return nil
			}
		}
	}
}

// waitForSecretRecovery waits for Secret to recover
func (t *UnexpectedDeleteTester) waitForSecretRecovery(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting for secret to recover...", devbox.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("secret recovery timeout")
			}

			if t.helper.IsSecretCreated(ctx, devbox) {
				log.Printf("[%s] ✓ secret recovered", devbox.Name)
				return nil
			}
		}
	}
}

// waitForServiceRecovery waits for Service to recover
func (t *UnexpectedDeleteTester) waitForServiceRecovery(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting for service to recover...", devbox.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("service recovery timeout")
			}

			if t.helper.IsServiceCreated(ctx, devbox) {
				log.Printf("[%s] ✓ service recovered", devbox.Name)
				return nil
			}
		}
	}
}

// waitForServiceNotExists waits to confirm Service does not exist
func (t *UnexpectedDeleteTester) waitForServiceNotExists(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	deadline := time.Now().Add(t.config.RecoveryTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] waiting to confirm service does not exist...", devbox.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for service to not exist")
			}

			services, err := t.helper.GetDevboxServices(ctx, devbox)
			if err != nil {
				continue
			}

			if len(services) == 0 {
				log.Printf("[%s] ✓ service does not exist as expected", devbox.Name)
				return nil
			}
		}
	}
}

// Cleanup cleans up test resources
func (t *UnexpectedDeleteTester) Cleanup(ctx context.Context) error {
	log.Printf("cleanup unexpected delete test resources...")

	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		return fmt.Errorf("list devbox failed: %w", err)
	}

	deletedCount := 0
	for _, devbox := range devboxList.Items {
		if devbox.Labels != nil {
			if testType, ok := devbox.Labels["test-type"]; ok && testType == "edge-unexpected-delete" {
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
