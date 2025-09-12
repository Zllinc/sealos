package tester

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os/exec"
	"sort"
	"strconv"
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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewDevboxStressTester(config *StressTestConfig) (*DevboxStressTester, error) {
	// create Kubernetes client
	var restConfig *rest.Config
	var err error

	// try to build config from kubeconfig file or cluster config
	restConfig, err = clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		// try to build config from cluster config
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			// try to build config from default config
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

	return &DevboxStressTester{
		config:     config,
		k8sClient:  k8sClient,
		ctrlClient: ctrlClient,
		scheme:     scheme,
		restConfig: restConfig,
	}, nil
}

// 1. 规模测试：创建大量 devbox
func (t *DevboxStressTester) RunScaleTest(ctx context.Context) (*StressTestResult, error) {
	log.Printf("start scale test: create %d devboxes", t.config.DevboxCount)

	result := &StressTestResult{}
	startTime := time.Now()

	// simulate batch create devbox
	for i := 0; i < t.config.DevboxCount; i++ {
		devboxName := fmt.Sprintf("stress-test-devbox-%d", i)

		devbox := t.generateDevbox(devboxName)

		createStart := time.Now()
		err := t.ctrlClient.Create(ctx, devbox)
		createDuration := time.Since(createStart)

		if err != nil {
			result.FailedCreates++
			result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("Failed to create %s: %v", devboxName, err))
			log.Printf("create devbox %s failed: %v", devboxName, err)
		} else {
			result.SuccessfulCreates++
			log.Printf("create devbox %s successful, duration: %v", devboxName, createDuration)
		}

		result.TotalDevboxes++

		// create interval
		if t.config.CreateInterval > 0 {
			time.Sleep(t.config.CreateInterval)
		}

		// check timeout
		if time.Since(startTime) > t.config.TestTimeout {
			log.Printf("test timeout, stop creating")
			break
		}
	}

	result.TotalTestTime = time.Since(startTime)
	if result.SuccessfulCreates > 0 {
		result.AverageCreateTime = result.TotalTestTime / time.Duration(result.SuccessfulCreates)
	}

	log.Printf("scale test completed: success %d, failed %d, total time %v",
		result.SuccessfulCreates, result.FailedCreates, result.TotalTestTime)

	return result, nil
}

// 2. concurrent test: test concurrent create ability
func (t *DevboxStressTester) RunConcurrentTest(ctx context.Context) (*StressTestResult, error) {
	log.Printf("start concurrent test: %d concurrent create %d devboxes", t.config.ConcurrentCount, t.config.DevboxCount)

	result := &StressTestResult{}
	startTime := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex

	// use semaphore to control concurrent count
	semaphore := make(chan struct{}, t.config.ConcurrentCount)

	for i := 0; i < t.config.DevboxCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}        // get semaphore
			defer func() { <-semaphore }() // release semaphore

			devboxName := fmt.Sprintf("concurrent-test-devbox-%d", index)
			devbox := t.generateDevbox(devboxName)

			createStart := time.Now()
			err := t.ctrlClient.Create(ctx, devbox)
			createDuration := time.Since(createStart)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.FailedCreates++
				result.ErrorMessages = append(result.ErrorMessages, fmt.Sprintf("Failed to create %s: %v", devboxName, err))
				log.Printf("concurrent create devbox %s failed: %v", devboxName, err)
			} else {
				// Wait for devbox to become Running
				if t.waitForDevboxToBeRunning(ctx, *devbox, 2*time.Minute) {
					result.SuccessfulCreates++
					log.Printf("concurrent create devbox %s successful, duration: %v", devboxName, createDuration)
				} else {
					log.Printf("devbox %s created but failed to reach Running state within timeout or pod is not running", devboxName)
					result.FailedCreates++
				}
			}
			result.TotalDevboxes++
		}(i)
	}

	wg.Wait()
	result.TotalTestTime = time.Since(startTime)

	// calculate QPS
	if result.TotalTestTime.Seconds() > 0 {
		result.MaxQPS = float64(result.SuccessfulCreates) / result.TotalTestTime.Seconds()
	}

	if result.SuccessfulCreates > 0 {
		result.AverageCreateTime = result.TotalTestTime / time.Duration(result.SuccessfulCreates)
	}

	log.Printf("concurrent test completed: success %d, failed %d, QPS: %.2f, total time %v",
		result.SuccessfulCreates, result.FailedCreates, result.MaxQPS, result.TotalTestTime)

	return result, nil
}

// // 3. resource monitor test
// func (t *DevboxStressTester) MonitorResources(ctx context.Context, duration time.Duration) error {
// 	log.Printf("start monitoring cluster resources, duration: %v", duration)

// 	ticker := time.NewTicker(5 * time.Second)
// 	defer ticker.Stop()

// 	timeout := time.After(duration)

// 	for {
// 		select {
// 		case <-timeout:
// 			log.Printf("resource monitor completed")
// 			return nil
// 		case <-ticker.C:
// 			if err := t.CollectResourceMetrics(ctx); err != nil {
// 				log.Printf("collect resource metrics failed: %v", err)
// 			}
// 		case <-ctx.Done():
// 			return ctx.Err()
// 		}
// 	}
// }

func (t *DevboxStressTester) CollectResourceMetrics(ctx context.Context) error {
	// collect Node resource usage
	nodes, err := t.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	log.Printf("=== cluster resource status ===")
	log.Printf("node count: %d", len(nodes.Items))

	// collect Pod resource usage
	pods, err := t.k8sClient.CoreV1().Pods(t.config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	runningPods := 0
	pendingPods := 0
	failedPods := 0

	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			runningPods++
		case corev1.PodPending:
			pendingPods++
		case corev1.PodFailed:
			failedPods++
		}
	}

	log.Printf("pod status - running: %d, pending: %d, failed: %d", runningPods, pendingPods, failedPods)

	// collect Devbox resource usage
	devboxList := &devboxv1alpha2.DevboxList{}
	if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
		return fmt.Errorf("failed to list devboxes: %w", err)
	}

	devboxCount := len(devboxList.Items)
	runningDevboxes := 0
	for _, devbox := range devboxList.Items {
		if devbox.Status.Phase == devboxv1alpha2.DevboxPhaseRunning {
			runningDevboxes++
		}
	}

	log.Printf("Devbox status - total: %d, running: %d", devboxCount, runningDevboxes)
	log.Printf("=== monitor completed ===")

	return nil
}

func (t *DevboxStressTester) generateDevbox(name string) *devboxv1alpha2.Devbox {
	return &devboxv1alpha2.Devbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: t.config.Namespace,
			Labels: map[string]string{
				"stress-test": "true",
				"test-type":   "devbox-stress",
			},
		},
		Spec: devboxv1alpha2.DevboxSpec{
			State: devboxv1alpha2.DevboxStateRunning,
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
}

// cleanup test created devbox
func (t *DevboxStressTester) Cleanup(ctx context.Context) error {
	log.Printf("start cleaning up test resources...")

	devboxList := &devboxv1alpha2.DevboxList{}
	err := t.ctrlClient.List(ctx, devboxList,
		client.InNamespace(t.config.Namespace),
		client.MatchingLabels{"stress-test": "true"})
	if err != nil {
		return fmt.Errorf("failed to list test devboxes: %w", err)
	}

	for _, devbox := range devboxList.Items {
		// try to remove finalizer
		if len(devbox.Finalizers) > 0 {
			log.Printf("remove devbox %s/%s finalizer: %v", devbox.Namespace, devbox.Name, devbox.Finalizers)
			devbox.Finalizers = []string{}
			if err := t.ctrlClient.Update(ctx, &devbox); err != nil {
				log.Printf("remove devbox %s/%s finalizer failed: %v", devbox.Namespace, devbox.Name, err)
			} else {
				log.Printf("successfully remove devbox %s/%s finalizer", devbox.Namespace, devbox.Name)
			}
		}

		// then delete resources
		if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
			log.Printf("delete devbox %s/%s failed: %v", devbox.Namespace, devbox.Name, err)
		} else {
			log.Printf("successfully delete devbox %s/%s", devbox.Namespace, devbox.Name)
		}
	}

	log.Printf("cleanup completed, deleted %d test devboxes", len(devboxList.Items))
	return nil
}

// ForceCleanup force cleanup all test resources
func (t *DevboxStressTester) ForceCleanup(ctx context.Context) error {
	log.Printf("start force cleanup test resources...")

	// 先列出要删除的 devbox，用于 LVM 清理
	devboxList := &devboxv1alpha2.DevboxList{}
	err := t.ctrlClient.List(ctx, devboxList,
		client.InNamespace(t.config.Namespace),
		client.MatchingLabels{"stress-test": "true"})
	if err != nil {
		log.Printf("warning: failed to list devboxes for LVM cleanup: %v", err)
	}

	// use DeleteAllOf to force delete all matching resources
	err = t.ctrlClient.DeleteAllOf(ctx, &devboxv1alpha2.Devbox{},
		client.InNamespace(t.config.Namespace),
		client.MatchingLabels{"stress-test": "true"})

	if err != nil {
		return fmt.Errorf("force delete test devbox failed: %w", err)
	}

	// 检查并清理 LVM 逻辑卷
	if len(devboxList.Items) > 0 {
		if err := t.cleanupLVMResources(ctx, devboxList.Items); err != nil {
			log.Printf("warning: cleanup LVM resources failed: %v", err)
		}
	}

	log.Printf("force cleanup completed")
	return nil
}

// ListTestResources list test resources
func (t *DevboxStressTester) ListTestResources(ctx context.Context, allNamespaces bool) ([]DevboxResource, error) {
	var resources []DevboxResource

	// create Devbox list
	devboxList := &devboxv1alpha2.DevboxList{}

	if allNamespaces {
		// list all namespaces Devbox
		if err := t.ctrlClient.List(ctx, devboxList); err != nil {
			return nil, fmt.Errorf("failed to list devboxes in all namespaces: %w", err)
		}
	} else {
		// list specified namespaces Devbox
		if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
			return nil, fmt.Errorf("failed to list devboxes in namespace %s: %w", t.config.Namespace, err)
		}
	}

	// filter test resources (through label selector)
	for _, devbox := range devboxList.Items {
		if devbox.Labels != nil {
			if testType, exists := devbox.Labels["test-type"]; exists && testType == "devbox-stress" {
				resources = append(resources, DevboxResource{
					Name:      devbox.Name,
					Namespace: devbox.Namespace,
				})
			}
		}
	}

	return resources, nil
}

// RunCommitTest run commit test
func (t *DevboxStressTester) RunCommitTest(ctx context.Context, targetState string, count int, dataSize string, verify bool, concurrentCount int, fileCount int, commitTimeout time.Duration) error {
	log.Printf("=== start %s state commit test ===", targetState)
	log.Printf("并发配置: 总数量=%d, 并发数=%d", count, concurrentCount)

	// 1. find existing running devbox
	runningDevboxes, err := t.findRunningDevboxes(ctx, count)
	if err != nil {
		return fmt.Errorf("find running devbox failed: %w", err)
	}

	if len(runningDevboxes) == 0 {
		return fmt.Errorf("no running devbox found, please create some devbox first")
	}

	actualCount := len(runningDevboxes)
	if actualCount < count {
		log.Printf("warning: only found %d running devboxes, less than requested %d", actualCount, count)
	}

	log.Printf("found %d running devboxes", actualCount)
	for _, devbox := range runningDevboxes {
		fmt.Printf("  %s/%s", devbox.Namespace, devbox.Name)
	}

	// 2. concurrently write test data to container
	log.Printf("=== step 2: write test data (并发数: %d, 文件数: %d) ===", concurrentCount, fileCount)
	writeStartTime := time.Now()
	if err := t.writeTestDataConcurrentlyWithLimit(ctx, runningDevboxes, dataSize, concurrentCount, fileCount); err != nil {
		return fmt.Errorf("write test data failed: %w", err)
	}
	writeDuration := time.Since(writeStartTime)

	// calculate write speed
	dataSizeBytes, _ := parseDataSize(dataSize)
	totalDataSize := dataSizeBytes * int64(actualCount)
	writeSpeedMBps := float64(totalDataSize) / (1024 * 1024) / writeDuration.Seconds()

	log.Printf("data write completed in %v, QPS: %.2f, Write speed: %.2f MB/s",
		writeDuration, float64(actualCount)/writeDuration.Seconds(), writeSpeedMBps)

	// 3. concurrently modify state to trigger commit
	log.Printf("=== step 3: modify state to %s trigger commit (并发数: %d) ===", targetState, concurrentCount)
	commitStartTime := time.Now()
	if err := t.changeDevboxStatesConcurrentlyWithLimit(ctx, runningDevboxes, targetState, concurrentCount); err != nil {
		return fmt.Errorf("modify state failed: %w", err)
	}

	// 4. wait for commit completion
	log.Printf("=== step 4: wait for commit completion ===")
	if err := t.waitForCommitCompletion(ctx, runningDevboxes, targetState, commitTimeout); err != nil {
		return fmt.Errorf("wait for commit completion failed: %w", err)
	}
	commitDuration := time.Since(commitStartTime)

	// // 5. restore state to Running
	// log.Printf("=== step 5: restore state to Running (并发数: %d) ===", concurrentCount)
	// if err := t.changeDevboxStatesConcurrentlyWithLimit(ctx, runningDevboxes, "Running", concurrentCount); err != nil {
	// 	return fmt.Errorf("restore state failed: %w", err)
	// }

	// // 6. wait for Devbox to recover to Running state
	// log.Printf("=== step 6: wait for Devbox to recover to Running state ===")
	// waitRunningStartTime := time.Now()
	// if err := t.waitForDevboxRunning(ctx, runningDevboxes); err != nil {
	// 	return fmt.Errorf("wait for Devbox to recover failed: %w", err)
	// }
	// waitRunningDuration := time.Since(waitRunningStartTime)

	// // 7. verify data integrity
	// if verify {
	// 	log.Printf("=== step 7: verify data integrity (并发数: %d) ===", concurrentCount)
	// 	if err := t.verifyDataIntegrityConcurrently(ctx, runningDevboxes, concurrentCount); err != nil {
	// 		return fmt.Errorf("verify data integrity failed: %w", err)
	// 	}
	// }

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("               concurrent commit result")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Total Devbox number: %d\n", actualCount)
	fmt.Printf("Concurrent count: %d\n", concurrentCount)

	fmt.Printf("File count: %d\n", fileCount)
	fmt.Printf("Data size: %s\n", dataSize)
	fmt.Printf("Data write QPS: %.2f (Write speed: %.2f MB/s)\n",
		float64(actualCount)/writeDuration.Seconds(), writeSpeedMBps)

	fmt.Printf("Commit duration: %v\n", commitDuration)
	fmt.Printf("Average Commit duration: %v\n", commitDuration.Seconds()/float64(actualCount))
	fmt.Printf("Commit processing QPS: %.2f\n", float64(actualCount)/commitDuration.Seconds())

	// fmt.Printf("Wait running duration: %v\n", waitRunningDuration)
	// fmt.Printf("Average Wait running duration: %v\n", waitRunningDuration.Seconds()/float64(actualCount))
	// fmt.Printf("Wait running QPS: %.2f\n", float64(actualCount)/waitRunningDuration.Seconds())
	fmt.Println(strings.Repeat("=", 50))
	return nil
}

// findRunningDevboxes find running devbox
func (t *DevboxStressTester) findRunningDevboxes(ctx context.Context, maxCount int) ([]devboxv1alpha2.Devbox, error) {
	devboxList := &devboxv1alpha2.DevboxList{}
	err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace))
	if err != nil {
		return nil, fmt.Errorf("list devbox failed: %w", err)
	}

	var runningDevboxes []devboxv1alpha2.Devbox
	for _, devbox := range devboxList.Items {
		if devbox.Spec.State == devboxv1alpha2.DevboxStateRunning {
			runningDevboxes = append(runningDevboxes, devbox)
			if len(runningDevboxes) >= maxCount {
				break
			}
		}
	}

	return runningDevboxes, nil
}

// writeTestDataConcurrently concurrently write test data to container
func (t *DevboxStressTester) writeTestDataConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox, dataSize string, fileCount int) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(devboxes))

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(idx int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			log.Printf("write test data to devbox %s (%d/%d)", db.Name, idx+1, len(devboxes))

			if err := t.writeTestDataToContainer(ctx, db, dataSize, fileCount); err != nil {
				log.Printf("write data to devbox %s failed: %v", db.Name, err)
				errChan <- fmt.Errorf("devbox %s: %w", db.Name, err)
				return
			}

			log.Printf("successfully write test data to devbox %s", db.Name)
		}(i, devbox)
	}

	wg.Wait()
	close(errChan)

	// check if there are errors
	var errors []string
	for err := range errChan {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		return fmt.Errorf("write data failed: %s", strings.Join(errors, "; "))
	}

	return nil
}

// writeTestDataToContainer write test data to specified container
func (t *DevboxStressTester) writeTestDataToContainer(ctx context.Context, devbox devboxv1alpha2.Devbox, dataSize string, fileCount int) error {
	// find devbox corresponding pod
	podList := &corev1.PodList{}
	err := t.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
	if err != nil {
		return fmt.Errorf("find pod failed: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no corresponding pod found")
	}

	pod := podList.Items[0]
	fmt.Printf("found pod %s in namespace %s\n", pod.Name, pod.Namespace)

	// parse data size (e.g. "100M" -> 100)
	sizeValue := strings.TrimSuffix(dataSize, "M")

	// create command to write data - adapt to storage limit
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`
mkdir -p /home/devbox/test_commit_data
cd /home/devbox/test_commit_data
echo "start writing test data..."

# Calculate available space (leave some buffer)
available_space=$(df /home/devbox/test_commit_data | tail -1 | awk '{print $4}')
echo "available space: $available_space KB"

# Try to write files until we hit storage limit
file_count=0
for i in $(seq 1 %d); do
    if dd if=/dev/urandom of=file_$i.bin bs=1M count=%s 2>/dev/null; then
        file_count=$((file_count + 1))
    else
        echo "failed to create file_$i.bin (storage limit reached)"
        break
    fi
done

echo "data writing completed: created $file_count files"
echo "final directory size:"
du -sh .
`, fileCount, sizeValue),
	}

	// get first container name
	containerName := "devbox"
	if len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}
	fmt.Printf("found container %s in pod %s in namespace %s\n", containerName, pod.Name, pod.Namespace)
	return t.execCommandInPod(ctx, pod.Namespace, pod.Name, containerName, cmd)
}

// changeDevboxStatesConcurrently concurrently modify devbox state
func (t *DevboxStressTester) changeDevboxStatesConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox, targetState string) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(devboxes))

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(idx int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			log.Printf("modify devbox %s state to %s (%d/%d)", db.Name, targetState, idx+1, len(devboxes))

			if err := t.changeDevboxState(ctx, db, targetState); err != nil {
				log.Printf("modify devbox %s state failed: %v", db.Name, err)
				errChan <- fmt.Errorf("devbox %s: %w", db.Name, err)
				return
			}

			log.Printf("successfully modify devbox %s state to %s", db.Name, targetState)
		}(i, devbox)
	}

	wg.Wait()
	close(errChan)

	// check if there are errors
	var errors []string
	for err := range errChan {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		return fmt.Errorf("modify state failed: %s", strings.Join(errors, "; "))
	}

	return nil
}

// changeDevboxState modify single devbox state
func (t *DevboxStressTester) changeDevboxState(ctx context.Context, devbox devboxv1alpha2.Devbox, targetState string) error {
	// get latest devbox object
	latestDevbox := &devboxv1alpha2.Devbox{}
	err := t.ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: devbox.Namespace,
		Name:      devbox.Name,
	}, latestDevbox)
	if err != nil {
		return fmt.Errorf("get latest devbox object failed: %w", err)
	}

	// modify state
	latestDevbox.Spec.State = devboxv1alpha2.DevboxState(targetState)
	log.Printf("modify devbox %s spec.state to %s", latestDevbox.Name, latestDevbox.Spec.State)

	return t.ctrlClient.Update(ctx, latestDevbox)
}

// waitForCommitCompletion wait for commit completion
func (t *DevboxStressTester) waitForCommitCompletion(ctx context.Context, devboxes []devboxv1alpha2.Devbox, targetState string, td time.Duration) error {
	timeout := time.After(td)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	log.Printf("waiting for state transition to complete...")

	for {
		select {
		case <-timeout:
			return fmt.Errorf("wait for commit completion timeout")
		case <-ticker.C:
			allCompleted := true
			for _, devbox := range devboxes {
				// check devbox state
				latestDevbox := &devboxv1alpha2.Devbox{}
				err := t.ctrlClient.Get(ctx, client.ObjectKey{
					Namespace: devbox.Namespace,
					Name:      devbox.Name,
				}, latestDevbox)
				if err != nil {
					log.Printf("get devbox %s state failed: %v", devbox.Name, err)
					allCompleted = false
					continue
				}

				// check if reach target state
				if string(latestDevbox.Status.State) != targetState {
					// log.Printf("devbox %s state is %s, waiting for %s", devbox.Name, latestDevbox.Status.State, targetState)
					allCompleted = false
					continue
				}
			}

			if allCompleted {
				log.Printf("all devboxes have completed state transition")
				return nil
			}

		}
	}
}

// waitForDevboxRunning wait devbox completely recover to Running state
func (t *DevboxStressTester) waitForDevboxRunning(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	timeout := time.After(10 * time.Minute) // 10 minutes timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	log.Printf("waiting for Devbox to recover to Running state...")

	for {
		select {
		case <-timeout:
			return fmt.Errorf("wait for Devbox to recover to Running state timeout")
		case <-ticker.C:
			allRunning := true
			for _, devbox := range devboxes {
				// check devbox state
				latestDevbox := &devboxv1alpha2.Devbox{}
				err := t.ctrlClient.Get(ctx, client.ObjectKey{
					Namespace: devbox.Namespace,
					Name:      devbox.Name,
				}, latestDevbox)
				if err != nil {
					log.Printf("get devbox %s state failed: %v", devbox.Name, err)
					allRunning = false
					continue
				}

				// check state is Running
				if string(latestDevbox.Status.State) != "Running" {
					// log.Printf("devbox %s state is %s, waiting for Running", devbox.Name, latestDevbox.Status.State)
					allRunning = false
					continue
				}

				// check Pod is running normally
				if !t.isPodRunning(ctx, devbox) {
					// log.Printf("devbox %s Pod is not running, waiting for running", devbox.Name)
					allRunning = false
					continue
				}

				// check Service is created
				if !t.isServiceCreated(ctx, devbox) {
					log.Printf("devbox %s Service is not created, waiting for creation", devbox.Name)
					allRunning = false
					continue
				}

				// check Secret is created
				if !t.isSecretCreated(ctx, devbox) {
					log.Printf("devbox %s Secret is not created, waiting for creation", devbox.Name)
					allRunning = false
					continue
				}

				// check LVM logical volume is created
				// if !t.isLVMCreated(ctx, devbox) {
				// 	log.Printf("devbox %s LVM logical volume is not created, waiting for creation", devbox.Name)
				// 	allRunning = false
				// 	continue
				// }
			}

			if allRunning {
				log.Printf("all devboxes have recovered to Running state with all resources created")
				return nil
			}
		}
	}
}

// isPodRunning check Pod is running normally
func (t *DevboxStressTester) isPodRunning(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	podList := &corev1.PodList{}
	err := t.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
	if err != nil {
		log.Printf("find devbox %s Pod failed: %v", devbox.Name, err)
		return false
	}

	if len(podList.Items) == 0 {
		log.Printf("devbox %s has no corresponding Pod", devbox.Name)
		return false
	}

	pod := podList.Items[0]

	// check Pod status
	if pod.Status.Phase != corev1.PodRunning {
		log.Printf("devbox %s Pod status is %s", devbox.Name, pod.Status.Phase)
		return false
	}

	// check all containers are ready
	for _, container := range pod.Status.ContainerStatuses {
		if !container.Ready {
			log.Printf("devbox %s container %s is not ready", devbox.Name, container.Name)
			return false
		}
	}

	return true
}

// parseDataSize parse data size string like "100M", "1G" to bytes
func parseDataSize(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// 获取数字部分和单位
	var numStr string
	var unit string

	// 找到最后一个非数字字符
	for i := len(sizeStr) - 1; i >= 0; i-- {
		if sizeStr[i] >= '0' && sizeStr[i] <= '9' {
			numStr = sizeStr[:i+1]
			unit = sizeStr[i+1:]
			break
		}
	}

	if numStr == "" {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numStr)
	}

	// 根据单位转换
	unit = strings.ToUpper(unit)
	switch unit {
	case "B", "":
		return int64(num), nil
	case "K", "KB":
		return int64(num * 1024), nil
	case "M", "MB":
		return int64(num * 1024 * 1024), nil
	case "G", "GB":
		return int64(num * 1024 * 1024 * 1024), nil
	case "T", "TB":
		return int64(num * 1024 * 1024 * 1024 * 1024), nil
	default:
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}
}

// waitForDevboxToBeRunning wait for a single devbox to become Running
func (t *DevboxStressTester) waitForDevboxToBeRunning(ctx context.Context, devbox devboxv1alpha2.Devbox, timeout time.Duration) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// short interval polling, ensure response speed while avoiding excessive polling
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			log.Printf("timeout waiting for devbox %s to become Running", devbox.Name)
			return false
		case <-ticker.C:
			latestDevbox := &devboxv1alpha2.Devbox{}
			if err := t.ctrlClient.Get(ctx, client.ObjectKey{
				Namespace: devbox.Namespace,
				Name:      devbox.Name,
			}, latestDevbox); err != nil {
				log.Printf("failed to get devbox %s status: %v", devbox.Name, err)
				continue
			}

			if latestDevbox.Status.State == devboxv1alpha2.DevboxStateRunning {
				log.Printf("devbox %s is now Running", devbox.Name)
				if !t.isPodRunning(ctx, devbox) {
					log.Printf("devbox %s pod is not running, waiting for running", devbox.Name)
					continue
				}
				if !t.isServiceCreated(ctx, devbox) {
					log.Printf("devbox %s service is not created, waiting for creation", devbox.Name)
					continue
				}
				if !t.isSecretCreated(ctx, devbox) {
					log.Printf("devbox %s secret is not created, waiting for creation", devbox.Name)
					continue
				}
				if !t.isLVMCreated(ctx, devbox) {
					log.Printf("devbox %s lvm logical volume is not created, waiting for creation", devbox.Name)
					continue
				}
				return true
			}

			// only print log when state changes, avoid too many logs
			if latestDevbox.Status.State != "" {
				log.Printf("devbox %s status: %s, waiting for Running...", devbox.Name, latestDevbox.Status.State)
			}
		}
	}
}

// verifyDataIntegrity verify data integrity
func (t *DevboxStressTester) verifyDataIntegrity(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(devboxes))
	successCount := 0
	var mu sync.Mutex

	for i, devbox := range devboxes {
		wg.Add(1)
		go func(idx int, db devboxv1alpha2.Devbox) {
			defer wg.Done()
			log.Printf("verify devbox %s data integrity (%d/%d)", db.Name, idx+1, len(devboxes))

			if err := t.verifyContainerData(ctx, db); err != nil {
				log.Printf("verify devbox %s data failed: %v", db.Name, err)
				errChan <- fmt.Errorf("devbox %s: %w", db.Name, err)
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
			log.Printf("devbox %s data verification successful", db.Name)
		}(i, devbox)
	}

	wg.Wait()
	close(errChan)

	// check result
	var errors []string
	for err := range errChan {
		errors = append(errors, err.Error())
	}

	log.Printf("data verification result: %d/%d successful", successCount, len(devboxes))

	if len(errors) > 0 {
		return fmt.Errorf("data verification failed: %s", strings.Join(errors, "; "))
	}

	return nil
}

// verifyContainerData verify container data
func (t *DevboxStressTester) verifyContainerData(ctx context.Context, devbox devboxv1alpha2.Devbox) error {
	// find devbox corresponding pod
	podList := &corev1.PodList{}
	err := t.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
	if err != nil {
		return fmt.Errorf("find pod failed: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no corresponding pod found")
	}

	pod := podList.Items[0]

	// verify data command - check if data exists and is not empty
	cmd := []string{
		"bash", "-c",
		`
echo "check test data..."
if [ -d "/home/devbox/test_commit_data" ]; then
    cd /home/devbox/test_commit_data
    file_count=$(ls -1 file_*.bin 2>/dev/null | wc -l)
    total_size=$(du -sh . 2>/dev/null | cut -f1)
    echo "found $file_count test files, total size: $total_size"
    
    if [ "$file_count" -gt 0 ]; then
        echo "data verification successful: found $file_count files with total size $total_size"
        if [ "$file_count" -gt 10 ]; then
            echo "... and $((file_count - 10)) more files"
        fi
        exit 0
    else
        echo "data verification failed: no test files found"
        ls -lah
        exit 1
    fi
else
    echo "data verification failed: test data directory does not exist"
    exit 1
fi
`,
	}

	// get first container name
	containerName := "devbox"
	if len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	return t.execCommandInPod(ctx, pod.Namespace, pod.Name, containerName, cmd)
}

// execCommandInPod exec command in pod
func (t *DevboxStressTester) execCommandInPod(ctx context.Context, namespace, podName, containerName string, cmd []string) error {
	req := t.k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: containerName,
		Command:   cmd,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer

	exec, err := remotecommand.NewSPDYExecutor(t.restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create executor failed: %w", err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		return fmt.Errorf("execute command failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("command output:\n%s", stdout.String())
	if stderr.Len() > 0 {
		log.Printf("command error output:\n%s", stderr.String())
	}

	return nil
}

// CleanupWithDetails cleanup resources and return details
func (t *DevboxStressTester) CleanupWithDetails(ctx context.Context, allNamespaces bool) (int, error) {
	log.Printf("start cleaning up test resources...")

	devboxList := &devboxv1alpha2.DevboxList{}
	var listOptions []client.ListOption

	if !allNamespaces {
		listOptions = append(listOptions, client.InNamespace(t.config.Namespace))
	}
	listOptions = append(listOptions, client.MatchingLabels{"stress-test": "true"})

	err := t.ctrlClient.List(ctx, devboxList, listOptions...)
	if err != nil {
		return 0, fmt.Errorf("failed to list test devboxes: %w", err)
	}

	deletedCount := 0
	for _, devbox := range devboxList.Items {
		// try to remove finalizer
		if len(devbox.Finalizers) > 0 {
			log.Printf("remove devbox %s/%s finalizer: %v", devbox.Namespace, devbox.Name, devbox.Finalizers)
			devbox.Finalizers = []string{}
			if err := t.ctrlClient.Update(ctx, &devbox); err != nil {
				log.Printf("remove devbox %s/%s finalizer failed: %v", devbox.Namespace, devbox.Name, err)
			} else {
				log.Printf("successfully remove devbox %s/%s finalizer", devbox.Namespace, devbox.Name)
			}
		}

		// then delete resources
		if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
			log.Printf("delete devbox %s/%s failed: %v", devbox.Namespace, devbox.Name, err)
		} else {
			log.Printf("successfully delete devbox %s/%s", devbox.Namespace, devbox.Name)
			deletedCount++
		}
	}

	// // 检查并清理 LVM 逻辑卷
	// if err := t.cleanupLVMResources(ctx, devboxList.Items); err != nil {
	// 	log.Printf("warning: cleanup LVM resources failed: %v", err)
	// }

	log.Printf("cleanup completed, deleted %d test devboxes", deletedCount)
	return deletedCount, nil
}

// cleanupLVMResources 清理 LVM 逻辑卷资源
func (t *DevboxStressTester) cleanupLVMResources(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	log.Printf("开始检查 LVM 逻辑卷资源...")

	// 收集所有需要检查的 devbox 信息
	var devboxInfos []DevboxLVMInfo
	for _, devbox := range devboxes {
		info := DevboxLVMInfo{
			Name:      devbox.Name,
			Namespace: devbox.Namespace,
			ContentID: devbox.Status.ContentID,
			BaseImage: devbox.Spec.Image,
		}
		devboxInfos = append(devboxInfos, info)
	}

	// 检查 LVM 逻辑卷
	lvList, err := t.listLVMLogicalVolumes()
	if err != nil {
		return fmt.Errorf("列出 LVM 逻辑卷失败: %w", err)
	}

	// 查找与 devbox 相关的 LVM 逻辑卷
	relatedLVs := t.findRelatedLVMVolumes(devboxInfos, lvList)

	if len(relatedLVs) == 0 {
		log.Printf("未找到与测试 devbox 相关的 LVM 逻辑卷")
		return nil
	}

	log.Printf("找到 %d 个相关的 LVM 逻辑卷:", len(relatedLVs))
	for _, lv := range relatedLVs {
		log.Printf("  - %s (大小: %s)", lv.Name, lv.Size)
	}

	// 删除 LVM 逻辑卷
	if err := t.deleteLVMVolumes(relatedLVs); err != nil {
		return fmt.Errorf("删除 LVM 逻辑卷失败: %w", err)
	}

	log.Printf("LVM 逻辑卷清理完成")
	return nil
}

// DevboxLVMInfo 存储 devbox 的 LVM 相关信息
type DevboxLVMInfo struct {
	Name      string
	Namespace string
	ContentID string
	BaseImage string
}

// LVMVolumeInfo 存储 LVM 逻辑卷信息
type LVMVolumeInfo struct {
	Name    string `json:"lv_name"`
	VG      string `json:"vg_name"`
	Size    string `json:"lv_size"`
	SegType string `json:"segtype"`
	Attr    string `json:"lv_attr"`
}

// LogicalVolume 基于 openebs/lvm-localpv 的结构
type LogicalVolume struct {
	Name    string `json:"lv_name"`
	VG      string `json:"vg_name"`
	Size    string `json:"lv_size"`
	SegType string `json:"segtype"`
	Attr    string `json:"lv_attr"`
}

// listLVMLogicalVolumes 列出所有 LVM 逻辑卷
func (t *DevboxStressTester) listLVMLogicalVolumes() ([]LVMVolumeInfo, error) {
	// 使用 JSON 格式获取更准确的信息
	args := []string{
		"--options", "lv_all,vg_name,segtype",
		"--reportformat", "json",
		"--units", "b",
	}

	cmd := exec.Command("lvs", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("执行 lvs 命令失败: %w", err)
	}

	return t.decodeLvsJSON(output)
}

// decodeLvsJSON 解析 lvs JSON 输出
func (t *DevboxStressTester) decodeLvsJSON(output []byte) ([]LVMVolumeInfo, error) {
	var result struct {
		Report []struct {
			LV []LogicalVolume `json:"lv"`
		} `json:"report"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("解析 lvs JSON 输出失败: %w", err)
	}

	var volumes []LVMVolumeInfo
	if len(result.Report) > 0 {
		for _, lv := range result.Report[0].LV {
			volumes = append(volumes, LVMVolumeInfo(lv))
		}
	}

	return volumes, nil
}

// findRelatedLVMVolumes 查找与 devbox 相关的 LVM 逻辑卷
func (t *DevboxStressTester) findRelatedLVMVolumes(devboxInfos []DevboxLVMInfo, lvList []LVMVolumeInfo) []LVMVolumeInfo {
	var relatedLVs []LVMVolumeInfo

	for _, lv := range lvList {
		// 检查逻辑卷名称是否包含 devbox 相关信息
		for _, devbox := range devboxInfos {
			// 检查是否包含 devbox 名称或 contentID
			if devbox.ContentID != "" && strings.Contains(lv.Name, devbox.ContentID) {
				relatedLVs = append(relatedLVs, lv)
				break
			}
		}
	}

	return relatedLVs
}

// deleteLVMVolumes 删除 LVM 逻辑卷
func (t *DevboxStressTester) deleteLVMVolumes(volumes []LVMVolumeInfo) error {
	for _, lv := range volumes {
		log.Printf("正在删除 LVM 逻辑卷: %s/%s", lv.VG, lv.Name)

		// 执行 lvremove 命令删除逻辑卷
		cmd := exec.Command("lvremove", "-f", fmt.Sprintf("%s/%s", lv.VG, lv.Name))
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("删除 LVM 逻辑卷 %s/%s 失败: %v, 输出: %s", lv.VG, lv.Name, err, string(output))
			continue
		}

		log.Printf("成功删除 LVM 逻辑卷: %s/%s", lv.VG, lv.Name)
	}

	return nil
}

// checkLVMStatus 检查 LVM 状态
func (t *DevboxStressTester) checkLVMStatus() error {
	log.Printf("检查 LVM 状态...")

	// 检查 VG 状态
	cmd := exec.Command("vgs", "--noheadings", "--units", "b", "--separator", "|", "-o", "vg_name,vg_size,vg_free")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("检查 VG 状态失败: %w", err)
	}

	log.Printf("VG 状态:")
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		log.Printf("  %s", line)
	}

	// 检查 LV 状态
	cmd = exec.Command("lvs", "--noheadings", "--units", "b", "--separator", "|", "-o", "lv_name,vg_name,lv_size,lv_attr")
	output, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("检查 LV 状态失败: %w", err)
	}

	log.Printf("LV 状态:")
	lines = strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		log.Printf("  %s", line)
	}

	return nil
}

// CheckLVMStatus 检查 LVM 状态的公共方法
func (t *DevboxStressTester) CheckLVMStatus() error {
	return t.checkLVMStatus()
}

// CleanupLVMResources 清理 LVM 资源的公共方法
func (t *DevboxStressTester) CleanupLVMResources(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	return t.cleanupLVMResources(ctx, devboxes)
}

// CleanupLVMByLabels 根据标签清理 LVM 资源
func (t *DevboxStressTester) CleanupLVMByLabels(ctx context.Context, allNamespaces bool) error {
	log.Printf("开始根据标签清理 LVM 资源...")

	// 列出所有测试 devbox
	devboxList := &devboxv1alpha2.DevboxList{}
	var listOptions []client.ListOption

	if !allNamespaces {
		listOptions = append(listOptions, client.InNamespace(t.config.Namespace))
	}
	listOptions = append(listOptions, client.MatchingLabels{"stress-test": "true"})

	err := t.ctrlClient.List(ctx, devboxList, listOptions...)
	if err != nil {
		return fmt.Errorf("列出测试 devbox 失败: %w", err)
	}

	if len(devboxList.Items) == 0 {
		log.Printf("未找到测试 devbox，跳过 LVM 清理")
		return nil
	}

	log.Printf("找到 %d 个测试 devbox，开始清理 LVM 资源", len(devboxList.Items))

	// 清理 LVM 资源
	return t.cleanupLVMResources(ctx, devboxList.Items)
}

// RunDeleteTest 运行删除测试
func (t *DevboxStressTester) RunDeleteTest(ctx context.Context, maxCount int, verify bool) error {
	log.Printf("=== 开始删除测试 ===")
	log.Printf("测试配置: 最大删除数量=%d, 验证=%t", maxCount, verify)

	// 1. 查找现有的测试 devbox
	log.Printf("=== 步骤 1: 查找现有测试 devbox ===")
	devboxes, err := t.findTestDevboxes(ctx, maxCount)
	if err != nil {
		return fmt.Errorf("查找测试 devbox 失败: %w", err)
	}

	if len(devboxes) == 0 {
		return fmt.Errorf("未找到任何测试 devbox，请先运行并发创建测试")
	}

	log.Printf("找到 %d 个测试 devbox", len(devboxes))

	// 2. 记录删除前的资源状态
	var beforeResources *ResourceState
	if verify {
		log.Printf("=== 步骤 2: 记录删除前资源状态 ===")
		beforeResources, err = t.captureResourceState(ctx, devboxes)
		if err != nil {
			log.Printf("警告: 记录删除前资源状态失败: %v", err)
		}
	}

	// 3. 删除 devbox
	log.Printf("=== 步骤 3: 删除 devbox ===")
	deleteStartTime := time.Now()
	if err := t.deleteTestDevboxes(ctx, devboxes); err != nil {
		return fmt.Errorf("删除 devbox 失败: %w", err)
	}
	deleteDuration := time.Since(deleteStartTime)
	log.Printf("删除操作完成，耗时: %v", deleteDuration)

	// 4. 验证资源删除
	if verify {
		log.Printf("=== 步骤 4: 验证资源删除 ===")
		if err := t.verifyResourceDeletion(ctx, devboxes, beforeResources); err != nil {
			return fmt.Errorf("验证资源删除失败: %w", err)
		}
		log.Printf("资源删除验证完成")
	}

	log.Printf("=== 删除测试完成 ===")
	return nil
}

// ResourceState 记录资源状态
type ResourceState struct {
	Devboxes  []devboxv1alpha2.Devbox
	Pods      []corev1.Pod
	Services  []corev1.Service
	Secrets   []corev1.Secret
	LVs       []LVMVolumeInfo
	Timestamp time.Time
}

// findTestDevboxes 查找现有的测试 devbox
func (t *DevboxStressTester) findTestDevboxes(ctx context.Context, maxCount int) ([]devboxv1alpha2.Devbox, error) {
	devboxList := &devboxv1alpha2.DevboxList{}
	err := t.ctrlClient.List(ctx, devboxList,
		client.InNamespace(t.config.Namespace),
		client.MatchingLabels{"stress-test": "true"})
	if err != nil {
		return nil, fmt.Errorf("列出测试 devbox 失败: %w", err)
	}

	// 过滤出运行中的 devbox
	var runningDevboxes []devboxv1alpha2.Devbox
	for _, devbox := range devboxList.Items {
		if devbox.Status.State == devboxv1alpha2.DevboxStateRunning {
			runningDevboxes = append(runningDevboxes, devbox)
			if len(runningDevboxes) >= maxCount {
				break
			}
		}
	}

	log.Printf("找到 %d 个运行中的测试 devbox", len(runningDevboxes))
	for _, devbox := range runningDevboxes {
		log.Printf("  - %s (状态: %s)", devbox.Name, devbox.Status.State)
	}

	return runningDevboxes, nil
}

// createTestDevboxes 创建测试 devbox (保留用于其他测试)
func (t *DevboxStressTester) createTestDevboxes(ctx context.Context, count int) ([]devboxv1alpha2.Devbox, error) {
	var devboxes []devboxv1alpha2.Devbox

	for i := 0; i < count; i++ {
		devboxPtr := t.generateDevbox(fmt.Sprintf("delete-test-devbox-%d", i))
		devbox := *devboxPtr
		devbox.Labels["test-type"] = "delete-test"
		devbox.Labels["stress-test"] = "true"

		if err := t.ctrlClient.Create(ctx, &devbox); err != nil {
			return devboxes, fmt.Errorf("创建 devbox %s 失败: %w", devbox.Name, err)
		}

		devboxes = append(devboxes, devbox)
		log.Printf("创建 devbox: %s", devbox.Name)
	}

	return devboxes, nil
}

// waitForDevboxesRunning 等待所有 devbox 运行
func (t *DevboxStressTester) waitForDevboxesRunning(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	for _, devbox := range devboxes {
		if !t.waitForDevboxToBeRunning(ctx, devbox, 2*time.Minute) {
			return fmt.Errorf("devbox %s 未能运行", devbox.Name)
		}
	}
	return nil
}

// captureResourceState 捕获资源状态
func (t *DevboxStressTester) captureResourceState(ctx context.Context, devboxes []devboxv1alpha2.Devbox) (*ResourceState, error) {
	state := &ResourceState{
		Devboxes:  make([]devboxv1alpha2.Devbox, len(devboxes)),
		Pods:      []corev1.Pod{},
		Services:  []corev1.Service{},
		Secrets:   []corev1.Secret{},
		LVs:       []LVMVolumeInfo{},
		Timestamp: time.Now(),
	}

	// 复制 devbox 状态
	copy(state.Devboxes, devboxes)

	// 获取相关 Pod
	for _, devbox := range devboxes {
		pods, err := t.getDevboxPods(ctx, devbox)
		if err != nil {
			log.Printf("获取 devbox %s 的 Pod 失败: %v", devbox.Name, err)
		} else {
			state.Pods = append(state.Pods, pods...)
		}
	}

	// 获取相关 Service
	for _, devbox := range devboxes {
		services, err := t.getDevboxServices(ctx, devbox)
		if err != nil {
			log.Printf("获取 devbox %s 的 Service 失败: %v", devbox.Name, err)
		} else {
			state.Services = append(state.Services, services...)
		}
	}

	// 获取相关 Secret
	for _, devbox := range devboxes {
		secrets, err := t.getDevboxSecrets(ctx, devbox)
		if err != nil {
			log.Printf("获取 devbox %s 的 Secret 失败: %v", devbox.Name, err)
		} else {
			state.Secrets = append(state.Secrets, secrets...)
		}
	}

	// 获取 LVM 逻辑卷
	lvs, err := t.listLVMLogicalVolumes()
	if err != nil {
		log.Printf("获取 LVM 逻辑卷失败: %v", err)
	} else {
		// 过滤与测试相关的 LV
		devboxInfos := make([]DevboxLVMInfo, len(devboxes))
		for i, devbox := range devboxes {
			devboxInfos[i] = DevboxLVMInfo{
				Name:      devbox.Name,
				Namespace: devbox.Namespace,
				ContentID: devbox.Status.ContentID,
				BaseImage: devbox.Spec.Image,
			}
		}
		state.LVs = t.findRelatedLVMVolumes(devboxInfos, lvs)
	}

	log.Printf("资源状态记录完成: %d devboxes, %d pods, %d services, %d secrets, %d LVs",
		len(state.Devboxes), len(state.Pods), len(state.Services), len(state.Secrets), len(state.LVs))

	return state, nil
}

// deleteTestDevboxes 删除测试 devbox
func (t *DevboxStressTester) deleteTestDevboxes(ctx context.Context, devboxes []devboxv1alpha2.Devbox) error {
	for _, devbox := range devboxes {
		log.Printf("删除 devbox: %s", devbox.Name)

		// 先尝试移除 finalizer
		if len(devbox.Finalizers) > 0 {
			devbox.Finalizers = []string{}
			if err := t.ctrlClient.Update(ctx, &devbox); err != nil {
				log.Printf("移除 devbox %s finalizer 失败: %v", devbox.Name, err)
			}
		}

		// 删除 devbox
		if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
			log.Printf("删除 devbox %s 失败: %v", devbox.Name, err)
		} else {
			log.Printf("成功删除 devbox: %s", devbox.Name)
		}
	}
	return nil
}

// verifyResourceDeletion 验证资源删除（轮询方式）
func (t *DevboxStressTester) verifyResourceDeletion(ctx context.Context, devboxes []devboxv1alpha2.Devbox, beforeState *ResourceState) error {
	log.Printf("开始验证资源删除（轮询模式）...")

	// 设置轮询参数
	checkInterval := 1 * time.Second // 每1秒检查一次
	timeout := 5 * time.Minute       // 总超时时间5分钟

	log.Printf("轮询参数: 检查间隔=%v, 总超时=%v", checkInterval, timeout)

	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	checkCount := 0
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		checkCount++
		log.Printf("=== 第 %d 次检查 ===", checkCount)

		allDeleted := true
		var remainingResources []string

		// 验证 Devbox 删除
		for _, devbox := range devboxes {
			if t.devboxExists(timeoutCtx, devbox) {
				allDeleted = false
				remainingResources = append(remainingResources, fmt.Sprintf("devbox:%s", devbox.Name))
			}
		}

		// 验证 Pod 删除
		for _, pod := range beforeState.Pods {
			if t.podExists(timeoutCtx, pod) {
				allDeleted = false
				remainingResources = append(remainingResources, fmt.Sprintf("pod:%s", pod.Name))
			}
		}

		// 验证 Service 删除
		for _, service := range beforeState.Services {
			if t.serviceExists(timeoutCtx, service) {
				allDeleted = false
				remainingResources = append(remainingResources, fmt.Sprintf("service:%s", service.Name))
			}
		}

		// 验证 Secret 删除
		for _, secret := range beforeState.Secrets {
			if t.secretExists(timeoutCtx, secret) {
				allDeleted = false
				remainingResources = append(remainingResources, fmt.Sprintf("secret:%s", secret.Name))
			}
		}

		// 验证 LVM 逻辑卷删除
		currentLVs, err := t.listLVMLogicalVolumes()
		if err != nil {
			log.Printf("警告: 无法获取当前 LVM 状态: %v", err)
		} else {
			for _, lv := range beforeState.LVs {
				if t.lvExists(currentLVs, lv) {
					allDeleted = false
					remainingResources = append(remainingResources, fmt.Sprintf("lv:%s/%s", lv.VG, lv.Name))
				}
			}
		}

		if allDeleted {
			log.Printf("✓ 所有资源已成功删除")
			return nil
		}

		log.Printf("仍有 %d 个资源未删除: %v", len(remainingResources), remainingResources)

		// 等待下一次检查
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("验证超时: %w", timeoutCtx.Err())
		case <-ticker.C:
			// 继续下一次循环
		}
	}
}

// devboxExists 检查 devbox 是否存在
func (t *DevboxStressTester) devboxExists(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	err := t.ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: devbox.Namespace,
		Name:      devbox.Name,
	}, &devboxv1alpha2.Devbox{})
	return err == nil
}

// podExists 检查 pod 是否存在
func (t *DevboxStressTester) podExists(ctx context.Context, pod corev1.Pod) bool {
	_, err := t.k8sClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	return err == nil
}

// lvExists 检查 LVM 逻辑卷是否存在
func (t *DevboxStressTester) lvExists(currentLVs []LVMVolumeInfo, targetLV LVMVolumeInfo) bool {
	for _, lv := range currentLVs {
		if lv.Name == targetLV.Name && lv.VG == targetLV.VG {
			return true
		}
	}
	return false
}

// serviceExists 检查 service 是否存在
func (t *DevboxStressTester) serviceExists(ctx context.Context, service corev1.Service) bool {
	_, err := t.k8sClient.CoreV1().Services(service.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	return err == nil
}

// secretExists 检查 secret 是否存在
func (t *DevboxStressTester) secretExists(ctx context.Context, secret corev1.Secret) bool {
	_, err := t.k8sClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	return err == nil
}

// getDevboxPods 获取 devbox 相关的 Pod
func (t *DevboxStressTester) getDevboxPods(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Pod, error) {
	podList, err := t.k8sClient.CoreV1().Pods(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// getDevboxServices 获取 devbox 相关的 Service
func (t *DevboxStressTester) getDevboxServices(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Service, error) {
	serviceList, err := t.k8sClient.CoreV1().Services(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

// getDevboxSecrets 获取 devbox 相关的 Secret
func (t *DevboxStressTester) getDevboxSecrets(ctx context.Context, devbox devboxv1alpha2.Devbox) ([]corev1.Secret, error) {
	secretList, err := t.k8sClient.CoreV1().Secrets(devbox.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
	})
	if err != nil {
		return nil, err
	}
	return secretList.Items, nil
}

// isServiceCreated 检查 Service 是否已创建
func (t *DevboxStressTester) isServiceCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	services, err := t.getDevboxServices(ctx, devbox)
	if err != nil {
		log.Printf("get devbox %s services failed: %v", devbox.Name, err)
		return false
	}
	return len(services) > 0
}

// isSecretCreated 检查 Secret 是否已创建
func (t *DevboxStressTester) isSecretCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	secrets, err := t.getDevboxSecrets(ctx, devbox)
	if err != nil {
		log.Printf("get devbox %s secrets failed: %v", devbox.Name, err)
		return false
	}
	return len(secrets) > 0
}

// // isLVMCreated 检查 LVM 逻辑卷是否已创建
// func (t *DevboxStressTester) isLVMCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
// 	// 获取当前所有 LVM 逻辑卷
// 	currentLVs, err := t.listLVMLogicalVolumes()
// 	if err != nil {
// 		log.Printf("get LVM logical volumes failed: %v", err)
// 		return false
// 	}

// 	// 检查是否有与当前 devbox 相关的 LVM 逻辑卷
// 	devboxInfo := DevboxLVMInfo{
// 		Name:      devbox.Name,
// 		Namespace: devbox.Namespace,
// 		ContentID: devbox.Status.ContentID,
// 		BaseImage: devbox.Spec.Image,
// 	}

// 	relatedLVs := t.findRelatedLVMVolumes([]DevboxLVMInfo{devboxInfo}, currentLVs)
// 	return len(relatedLVs) > 0
// }

func (t *DevboxStressTester) isLVMCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
	lvs, err := lvm.ListLVMLogicalVolume()
	if err != nil {
		log.Printf("get LVM logical volumes failed: %v", err)
		return false
	}
	for _, lv := range lvs {
		if strings.Contains(lv.Name, devbox.Status.ContentID) {
			return true
		}
	}
	return false
}

// writeTestDataConcurrentlyWithLimit 并发写入测试数据（带并发限制）
func (t *DevboxStressTester) writeTestDataConcurrentlyWithLimit(ctx context.Context, devboxes []devboxv1alpha2.Devbox, dataSize string, concurrentCount int, fileCount int) error {
	semaphore := make(chan struct{}, concurrentCount)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	for _, devbox := range devboxes {
		wg.Add(1)
		go func(db devboxv1alpha2.Devbox) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := t.writeTestDataToContainer(ctx, db, dataSize, fileCount); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("write data to devbox %s failed: %w", db.Name, err))
				mu.Unlock()
			}
		}(devbox)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("write test data failed: %v", errors)
	}

	return nil
}

// changeDevboxStatesConcurrentlyWithLimit 并发修改 devbox 状态（带并发限制）
func (t *DevboxStressTester) changeDevboxStatesConcurrentlyWithLimit(ctx context.Context, devboxes []devboxv1alpha2.Devbox, targetState string, concurrentCount int) error {
	semaphore := make(chan struct{}, concurrentCount)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	for _, devbox := range devboxes {
		wg.Add(1)
		go func(db devboxv1alpha2.Devbox) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := t.changeDevboxState(ctx, db, targetState); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("change devbox %s state to %s failed: %w", db.Name, targetState, err))
				mu.Unlock()
			}
		}(devbox)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("change devbox states failed: %v", errors)
	}

	return nil
}

// verifyDataIntegrityConcurrently 并发验证数据完整性（带并发限制）
func (t *DevboxStressTester) verifyDataIntegrityConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox, concurrentCount int) error {
	semaphore := make(chan struct{}, concurrentCount)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	for _, devbox := range devboxes {
		wg.Add(1)
		go func(db devboxv1alpha2.Devbox) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := t.verifyContainerData(ctx, db); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("verify devbox %s data failed: %w", db.Name, err))
				mu.Unlock()
			}
		}(devbox)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("verify data integrity failed: %v", errors)
	}

	return nil
}

// RunSmallFileCommitTest 运行小文件 commit 测试
func (t *DevboxStressTester) RunSmallFileCommitTest(ctx context.Context, fileCount int, totalSize string, concurrentCount int, timeout time.Duration, devboxCount int) error {
	log.Printf("=== start smallfile commit test ===")
	log.Printf("file count: %d, total size: %s, concurrent count: %d", fileCount, totalSize, concurrentCount)

	// 1. find existing running devbox
	log.Printf("=== step 1: find running devbox ===")
	runningDevboxes, err := t.findRunningDevboxes(ctx, devboxCount) // find running devbox by devbox count
	if err != nil {
		return fmt.Errorf("find running devbox failed: %w", err)
	}

	if len(runningDevboxes) == 0 {
		return fmt.Errorf("no running devbox found, please create some devbox")
	}

	actualCount := len(runningDevboxes)
	if actualCount < devboxCount {
		log.Printf("warning: only found %d running devboxes, less than requested %d", actualCount, fileCount)
	}
	log.Printf("found %d running devbox in namespace %s: ", actualCount, runningDevboxes[0].Namespace)
	for _, devbox := range runningDevboxes {
		fmt.Printf("  %s/%s", devbox.Namespace, devbox.Name)
	}

	// 2. generate smallfile size distribution
	log.Printf("=== step 2: generate smallfile size distribution ===")
	fileSizes := t.generateSmallFileSizes(fileCount, totalSize)
	log.Printf("smallfile size distribution: min=%s, max=%s, average=%s",
		formatBytes(fileSizes[0]),
		formatBytes(fileSizes[len(fileSizes)-1]),
		formatBytes(calculateAverageSize(fileSizes)))

	// 3. concurrent write smallfile test data
	log.Printf("=== step 3: concurrent write smallfile data (concurrent count: %d) ===", concurrentCount)
	writeStartTime := time.Now()
	if err := t.writeSmallFilesConcurrently(ctx, runningDevboxes, fileSizes, concurrentCount); err != nil {
		return fmt.Errorf("write smallfile data failed: %w", err)
	}
	writeDuration := time.Since(writeStartTime)

	// calculate write statistics
	totalDataSize := calculateTotalSize(fileSizes)
	totalFilesWritten := fileCount * actualCount
	totalDataWritten := totalDataSize * int64(actualCount)
	writeSpeedMBps := float64(totalDataWritten) / (1024 * 1024) / writeDuration.Seconds()

	log.Printf("smallfile write complete, duration: %v", writeDuration)
	log.Printf("each devbox data size: %s, total write data size: %s", formatBytes(totalDataSize), formatBytes(totalDataWritten))
	log.Printf("write speed: %.2f MB/s", writeSpeedMBps)
	log.Printf("smallfile write QPS: %.2f (total file count: %d)", float64(totalFilesWritten)/writeDuration.Seconds(), totalFilesWritten)

	// 4. modify state to Stopped to trigger commit
	log.Printf("=== step 4: modify state to Stopped to trigger commit (concurrent count: %d) ===", concurrentCount)
	commitStartTime := time.Now()
	if err := t.changeDevboxStatesConcurrentlyWithLimit(ctx, runningDevboxes, "Stopped", concurrentCount); err != nil {
		return fmt.Errorf("modify state failed: %w", err)
	}

	// 5. wait for commit complete
	log.Printf("=== step 5: wait for commit complete ===")
	if err := t.waitForCommitCompletion(ctx, runningDevboxes, "Stopped", timeout); err != nil {
		return fmt.Errorf("wait for commit complete failed: %w", err)
	}
	commitDuration := time.Since(commitStartTime)

	// print test result
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("                    smallfile commit test result")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("devbox count: %d\n", actualCount)
	fmt.Printf("each devbox file count: %d\n", fileCount)
	fmt.Printf("each devbox data size: %s\n", formatBytes(totalDataSize))
	fmt.Printf("total file count: %d\n", totalFilesWritten)
	fmt.Printf("total data size: %s\n", formatBytes(totalDataWritten))
	fmt.Printf("concurrent count: %d\n", concurrentCount)
	fmt.Printf("file size range: %s - %s\n", formatBytes(fileSizes[0]), formatBytes(fileSizes[len(fileSizes)-1]))
	fmt.Printf("average file size: %s\n", formatBytes(calculateAverageSize(fileSizes)))

	fmt.Printf("\nperformance statistics:\n")
	fmt.Printf("  write duration: %v\n", writeDuration)
	fmt.Printf("  write speed: %.2f MB/s\n", writeSpeedMBps)
	fmt.Printf("  file write QPS: %.2f\n", float64(totalFilesWritten)/writeDuration.Seconds())

	fmt.Printf("  commit duration: %v\n", commitDuration)
	fmt.Printf("  average commit duration: %v\n", commitDuration/time.Duration(actualCount))
	fmt.Printf("  commit process QPS: %.2f\n", float64(actualCount)/commitDuration.Seconds())

	fmt.Printf("\n total test duration: %v\n", time.Since(writeStartTime))
	fmt.Println(strings.Repeat("=", 60))

	return nil
}

// generateSmallFileSizes generate smallfile size distribution
func (t *DevboxStressTester) generateSmallFileSizes(fileCount int, totalSize string) []int64 {
	// parse total size
	totalBytes, err := parseDataSize(totalSize)
	if err != nil {
		log.Printf("parse total size failed, use default value 1GB: %v", err)
		totalBytes = 1024 * 1024 * 1024 // 1GB
	}

	// set file size range: 1K to 500K
	minSize := int64(1024)             // 1K
	maxSize := int64(500 * 1024)       // 500K
	targetAvgSize := int64(200 * 1024) // target average 200K

	fileSizes := make([]int64, fileCount)

	// use normal distribution to generate file size, centered around 200K
	rand.Seed(time.Now().UnixNano())

	for i := 0; i < fileCount; i++ {
		// generate normal distribution file size
		// use Box-Muller transform to generate normal distribution
		u1 := rand.Float64()
		u2 := rand.Float64()
		z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)

		// map normal distribution to file size range
		// standard deviation set to (maxSize - minSize) / 6, so 99.7% of the values are in the range
		stdDev := float64(maxSize-minSize) / 6.0
		fileSize := int64(float64(targetAvgSize) + z*stdDev)

		// ensure in range
		if fileSize < minSize {
			fileSize = minSize
		}
		if fileSize > maxSize {
			fileSize = maxSize
		}

		fileSizes[i] = fileSize
	}

	// adjust file size to match total size
	actualTotal := calculateTotalSize(fileSizes)
	scaleFactor := float64(totalBytes) / float64(actualTotal)

	for i := range fileSizes {
		fileSizes[i] = int64(float64(fileSizes[i]) * scaleFactor)
		// ensure in range
		if fileSizes[i] < minSize {
			fileSizes[i] = minSize
		}
		if fileSizes[i] > maxSize {
			fileSizes[i] = maxSize
		}
	}

	// sort to view distribution
	sort.Slice(fileSizes, func(i, j int) bool {
		return fileSizes[i] < fileSizes[j]
	})

	return fileSizes
}

// writeSmallFilesConcurrently concurrently write smallfiles
func (t *DevboxStressTester) writeSmallFilesConcurrently(ctx context.Context, devboxes []devboxv1alpha2.Devbox, fileSizes []int64, concurrentCount int) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(devboxes))
	semaphore := make(chan struct{}, concurrentCount)

	for _, devbox := range devboxes {
		wg.Add(1)
		go func(db devboxv1alpha2.Devbox) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// each devbox write all smallfiles
			log.Printf("write smallfiles to devbox %s: %d", db.Name, len(fileSizes))

			if err := t.writeSmallFilesToContainer(ctx, db, fileSizes); err != nil {
				log.Printf("write smallfiles to devbox %s failed: %v", db.Name, err)
				errChan <- fmt.Errorf("devbox %s: %w", db.Name, err)
				return
			}

			log.Printf("write smallfiles to devbox %s: %d", db.Name, len(fileSizes))
		}(devbox)
	}

	wg.Wait()
	close(errChan)

	// check errors
	var errors []string
	for err := range errChan {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		return fmt.Errorf("write smallfiles failed: %v", errors)
	}

	return nil
}

// writeSmallFilesToContainer write smallfiles to container
func (t *DevboxStressTester) writeSmallFilesToContainer(ctx context.Context, devbox devboxv1alpha2.Devbox, fileSizes []int64) error {
	// find corresponding pod
	podList := &corev1.PodList{}
	err := t.ctrlClient.List(ctx, podList,
		client.InNamespace(devbox.Namespace),
		client.MatchingLabels{"app.kubernetes.io/name": devbox.Name})
	if err != nil {
		return fmt.Errorf("find pod failed: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no corresponding pod found")
	}

	pod := podList.Items[0]

	// log.Printf("===== found pod %s in namespace %s ======", pod.Name, pod.Namespace)

	// build write smallfiles command
	// calculate file size parameters
	var sizeStep int64
	var sizeRange int64
	if len(fileSizes) > 1 {
		sizeStep = (fileSizes[len(fileSizes)-1] - fileSizes[0]) / int64(len(fileSizes)-1)
		sizeRange = fileSizes[len(fileSizes)-1] - fileSizes[0]
	} else {
		sizeStep = 0
		sizeRange = 1
	}

	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`
mkdir -p /home/devbox/smallfile_test
cd /home/devbox/smallfile_test
echo "开始写入 %d 个小文件..." 

# 写入小文件
for i in $(seq 1 %d); do
    # 计算文件大小（字节）
    size=$((%d + (i * %d) %% %d))
    if [ $size -lt 1024 ]; then size=1024; fi
    if [ $size -gt 512000 ]; then size=512000; fi
    
    # 使用 head 命令创建指定大小的随机文件（更高效）
    head -c $size /dev/urandom > smallfile_$i.bin
done

echo "小文件写入完成"
echo "文件统计:"
ls -la smallfile_*.bin | wc -l
du -sh .
`, len(fileSizes), len(fileSizes), fileSizes[0], sizeStep, sizeRange),
	}

	// 获取容器名称
	containerName := "devbox"
	if len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	// 执行命令
	err = t.execCommandInPod(ctx, pod.Namespace, pod.Name, containerName, cmd)
	if err != nil {
		return fmt.Errorf("执行命令失败: %w", err)
	}

	log.Printf("成功向 Devbox %s 写入小文件", devbox.Name)
	return nil
}

// helper function
func calculateTotalSize(fileSizes []int64) int64 {
	var total int64
	for _, size := range fileSizes {
		total += size
	}
	return total
}

func calculateAverageSize(fileSizes []int64) int64 {
	if len(fileSizes) == 0 {
		return 0
	}
	return calculateTotalSize(fileSizes) / int64(len(fileSizes))
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
