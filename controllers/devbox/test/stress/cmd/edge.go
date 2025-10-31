package cmd

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester/edge"
	"github.com/spf13/cobra"
)

var edgeCmd = &cobra.Command{
	Use:   "edge",
	Short: "run edge condition test",
	Long: `test various edge conditions and abnormal scenarios of devboxes.

currently supported tests:
  - toggle: state toggle test (continuous toggle)
  - unexpected-delete: unexpected delete test (simulate resource unexpected deletion)
  - crash: container crash recovery test (simulate process crash)

examples:
  devbox-stress edge toggle --count 5 --cycles 10
  devbox-stress edge unexpected-delete --count 3 --state Running
  devbox-stress edge crash --count 3 --cycles 5
`,
}

var toggleCmd = &cobra.Command{
	Use:   "toggle",
	Short: "test continuous toggle of devboxes",
	Long: `test the stability and data persistence of continuous toggle of devboxes.

test flow:
1. create specified number of devboxes and write test data
2. loop through toggle operations (Running ↔ Stopped/Shutdown)
3. wait for specified time after each state toggle
4. finally verify data completeness and resource state

example:
  # single devbox, toggle 10 times
  devbox-stress edge toggle --cycles 10

  # 5 devboxes, concurrent 3, toggle 5 times
  devbox-stress edge toggle --count 5 --concurrent 3 --cycles 5

  # use Shutdown mode, wait 20 seconds after each toggle
  devbox-stress edge toggle --mode shutdown --wait 20s --cycles 8

  # large data test
  devbox-stress edge toggle --data-size 500M --file-count 20 --cycles 3

  # clean up resources after test
  devbox-stress edge toggle --cycles 5 --cleanup
`,
	Run: runToggleTest,
}

var (
	// base config
	edgeNamespace string
	edgeImage     string
	edgeCPU       string
	edgeMemory    string
	edgeStorage   string

	// 测试规模
	toggleCount      int
	toggleConcurrent int

	// 切换配置
	toggleCycles int
	toggleWait   time.Duration
	toggleMode   string

	// 数据配置
	toggleDataSize  string
	toggleFileCount int

	// 超时配置
	toggleStateTimeout time.Duration
	toggleTestTimeout  time.Duration

	// 其他选项
	toggleCleanup bool
	toggleVerbose bool
)

func init() {
	rootCmd.AddCommand(edgeCmd)
	edgeCmd.AddCommand(toggleCmd)
	edgeCmd.AddCommand(unexpectedDeleteCmd)
	edgeCmd.AddCommand(crashCmd)

	// base config
	toggleCmd.Flags().StringVarP(&edgeNamespace, "namespace", "n", "devbox-test", "namespace")
	toggleCmd.Flags().StringVar(&edgeImage, "image", "ghcr.io/labring-actions/devbox/go-1.23.0:13aacd8", "Devbox image")
	toggleCmd.Flags().StringVar(&edgeCPU, "cpu", "2000m", "CPU resource")
	toggleCmd.Flags().StringVar(&edgeMemory, "memory", "4096Mi", "memory resource")
	toggleCmd.Flags().StringVar(&edgeStorage, "storage", "10Gi", "storage limit")

	// test scale
	toggleCmd.Flags().IntVarP(&toggleCount, "count", "c", 1, "number of devboxes to test")
	toggleCmd.Flags().IntVar(&toggleConcurrent, "concurrent", 1, "number of concurrent tests (1 means sequential tests)")

	// toggle config
	toggleCmd.Flags().IntVar(&toggleCycles, "cycles", 5, "number of toggle cycles")
	toggleCmd.Flags().DurationVar(&toggleWait, "wait", 5*time.Second, "wait time after each state toggle")
	toggleCmd.Flags().StringVar(&toggleMode, "mode", "shutdown", "toggle mode: stopped or shutdown")

	// data config
	toggleCmd.Flags().StringVar(&toggleDataSize, "data-size", "100M", "test data size (e.g. 100M)")
	toggleCmd.Flags().IntVar(&toggleFileCount, "file-count", 5, "number of test files")

	// timeout config
	toggleCmd.Flags().DurationVar(&toggleStateTimeout, "state-timeout", 5*time.Minute, "state toggle timeout")
	toggleCmd.Flags().DurationVar(&toggleTestTimeout, "test-timeout", 60*time.Minute, "total test timeout")

	// other options
	toggleCmd.Flags().BoolVar(&toggleCleanup, "cleanup", false, "clean up resources after test")
	toggleCmd.Flags().BoolVarP(&toggleVerbose, "verbose", "v", false, "display detailed output")
}

func runToggleTest(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// validate toggle mode
	var stateMode edge.StateToggleMode
	switch toggleMode {
	case "stopped":
		stateMode = edge.StateToggleStopped
	case "shutdown":
		stateMode = edge.StateToggleShutdown
	default:
		log.Fatalf("invalid toggle mode: %s (supported: stopped, shutdown)", toggleMode)
	}

	// create configuration
	config := &edge.StateToggleTestConfig{
		Namespace:       edgeNamespace,
		Image:           edgeImage,
		CPU:             edgeCPU,
		Memory:          edgeMemory,
		Storage:         edgeStorage,
		DevboxCount:     toggleCount,
		ConcurrentCount: toggleConcurrent,
		ToggleCycles:    toggleCycles,
		WaitAfterState:  toggleWait,
		StateMode:       stateMode,
		DataSize:        toggleDataSize,
		FileCount:       toggleFileCount,
		StateTimeout:    toggleStateTimeout,
		TestTimeout:     toggleTestTimeout,
	}

	// create tester
	tester, err := edge.NewStateEdgeTester(config)
	if err != nil {
		log.Fatalf("failed to create tester: %v", err)
	}

	// run test
	log.Printf("start state toggle boundary test...")
	log.Printf("")

	result, err := tester.RunStateToggleTest(ctx)
	if err != nil {
		log.Fatalf("test failed: %v", err)
	}

	// print results
	printToggleResults(result, toggleVerbose)

	// clean up resources
	if toggleCleanup {
		log.Printf("\nclean up test resources...")
		if err := tester.Cleanup(ctx); err != nil {
			log.Printf("clean up failed: %v", err)
		}
	}
}

func printToggleResults(result *edge.StateToggleTestResult, verbose bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("state toggle test result summary")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("total test count: %d\n", result.TotalDevboxes)
	fmt.Printf("successful: %d (%.1f%%)\n", result.SuccessfulTests,
		float64(result.SuccessfulTests)/float64(result.TotalDevboxes)*100)
	fmt.Printf("failed: %d (%.1f%%)\n", result.FailedTests,
		float64(result.FailedTests)/float64(result.TotalDevboxes)*100)

	fmt.Printf("\nTime statistics:\n")
	fmt.Printf("  total test time: %v\n", result.TotalTestTime)
	fmt.Printf("  average test time: %v\n", result.AverageTestTime)

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nerror messages (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			fmt.Printf("  [%d] %s\n", i+1, msg)
		}
	}

	// detailed output
	if verbose {
		fmt.Printf("\ndetailed test results:\n")
		fmt.Println(strings.Repeat("-", 60))

		for i, detail := range result.ToggleDetails {
			fmt.Printf("\n[%d] Devbox: %s\n", i+1, detail.DevboxName)
			fmt.Printf("  toggle count: %d/%d\n", detail.ToggleCycles, len(result.ToggleDetails[0].ToggleDurations))
			fmt.Printf("  data write: %s\n", formatStatus(detail.DataWriteSuccess))
			fmt.Printf("  data verify: %s\n", formatStatus(detail.DataVerifySuccess))
			fmt.Printf("  resource check: %s\n", formatStatus(detail.ResourceCheckOK))
			fmt.Printf("  total duration: %v\n", detail.TotalDuration)

			if len(detail.ToggleDurations) > 0 {
				fmt.Printf("  toggle duration:\n")
				for j, d := range detail.ToggleDurations {
					fmt.Printf("    cycle %d: %v\n", j+1, d)
				}
			}

			if detail.Error != "" {
				fmt.Printf("  error: %s\n", detail.Error)
			}
			if detail.MissingResources != "" {
				fmt.Printf("  missing resources: %s\n", detail.MissingResources)
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// success/failure status
	if result.FailedTests == 0 {
		fmt.Println("✓ all tests passed!")
	} else {
		fmt.Printf("⚠ some tests failed (%d/%d)\n", result.FailedTests, result.TotalDevboxes)
	}
}

func formatStatus(success bool) string {
	if success {
		return "✓ passed"
	}
	return "✗ failed"
}

// ==================== Unexpected Delete Command ====================

var unexpectedDeleteCmd = &cobra.Command{
	Use:   "unexpected-delete",
	Short: "test unexpected resource deletion",
	Long: `test controller's ability to recover from unexpected resource deletion.

test flow:
1. create devbox in specified state (Running/Stopped/Shutdown)
2. delete pod and check recovery behavior
3. delete secret and check recovery behavior
4. delete service and check recovery behavior

expected behavior by state:
  - Running: all resources should recover
  - Stopped: pod should not exist, secret/service should recover
  - Shutdown: pod/service should not exist, secret should recover

examples:
  # test in running state
  devbox-stress edge unexpected-delete --state Running --count 3

  # test in stopped state
  devbox-stress edge unexpected-delete --state Stopped --count 2

  # test in shutdown state
  devbox-stress edge unexpected-delete --state Shutdown --count 2

  # concurrent test with custom recovery timeout
  devbox-stress edge unexpected-delete --count 5 --concurrent 3 --recovery-timeout 10m

  # test with cleanup
  devbox-stress edge unexpected-delete --count 3 --cleanup
`,
	Run: runUnexpectedDeleteTest,
}

var (
	// unexpected-delete specific flags
	deleteTestCount       int
	deleteTestConcurrent  int
	deleteTestState       string
	deleteRecoveryTimeout time.Duration
	deleteTestTimeout     time.Duration
	deleteTestCleanup     bool
	deleteTestVerbose     bool
)

func init() {
	// Add flags
	unexpectedDeleteCmd.Flags().IntVarP(&deleteTestCount, "count", "c", 1, "number of devboxes to test")
	unexpectedDeleteCmd.Flags().IntVar(&deleteTestConcurrent, "concurrent", 1, "concurrent count (1 for sequential)")
	unexpectedDeleteCmd.Flags().StringVar(&deleteTestState, "state", "Running", "target devbox state: Running/Stopped/Shutdown")
	unexpectedDeleteCmd.Flags().DurationVar(&deleteRecoveryTimeout, "recovery-timeout", 5*time.Minute, "resource recovery timeout")
	unexpectedDeleteCmd.Flags().DurationVar(&deleteTestTimeout, "test-timeout", 30*time.Minute, "total test timeout")
	unexpectedDeleteCmd.Flags().BoolVar(&deleteTestCleanup, "cleanup", false, "cleanup resources after test")
	unexpectedDeleteCmd.Flags().BoolVarP(&deleteTestVerbose, "verbose", "v", true, "show detailed output")
}

func runUnexpectedDeleteTest(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// Validate state
	validStates := map[string]bool{
		"Running":  true,
		"Stopped":  true,
		"Shutdown": true,
	}

	if !validStates[deleteTestState] {
		log.Fatalf("invalid state: %s (supported: Running, Stopped, Shutdown)", deleteTestState)
	}

	// Create configuration
	config := &edge.UnexpectedDeleteTestConfig{
		Namespace:       edgeNamespace,
		Image:           edgeImage,
		CPU:             edgeCPU,
		Memory:          edgeMemory,
		Storage:         edgeStorage,
		DevboxCount:     deleteTestCount,
		ConcurrentCount: deleteTestConcurrent,
		DevboxState:     deleteTestState,
		RecoveryTimeout: deleteRecoveryTimeout,
		TestTimeout:     deleteTestTimeout,
	}

	// Create tester
	tester, err := edge.NewUnexpectedDeleteTester(config)
	if err != nil {
		log.Fatalf("failed to create tester: %v", err)
	}

	// Run test
	log.Printf("starting unexpected delete test...")
	log.Printf("")

	result, err := tester.RunUnexpectedDeleteTest(ctx)
	if err != nil {
		log.Fatalf("test failed: %v", err)
	}

	// Print result
	printUnexpectedDeleteResult(result, deleteTestVerbose)

	// Cleanup
	if deleteTestCleanup {
		log.Printf("\ncleaning up resources...")
		if err := tester.Cleanup(ctx); err != nil {
			log.Printf("cleanup failed: %v", err)
		}
	}
}

func printUnexpectedDeleteResult(result *edge.UnexpectedDeleteTestResult, verbose bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("unexpected delete test summary")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("total tests: %d\n", result.TotalTests)
	fmt.Printf("successful: %d (%.1f%%)\n", result.SuccessfulTests,
		float64(result.SuccessfulTests)/float64(result.TotalTests)*100)
	fmt.Printf("failed: %d (%.1f%%)\n", result.FailedTests,
		float64(result.FailedTests)/float64(result.TotalTests)*100)

	fmt.Printf("\ntime statistics:\n")
	fmt.Printf("  total test time: %v\n", result.TotalTestTime)
	fmt.Printf("  average test time: %v\n", result.AverageTestTime)

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nerror messages (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			fmt.Printf("  [%d] %s\n", i+1, msg)
		}
	}

	// Detailed output
	if verbose {
		fmt.Printf("\ndetailed test results:\n")
		fmt.Println(strings.Repeat("-", 60))

		for i, detail := range result.Details {
			fmt.Printf("\n[%d] devbox: %s (state: %s)\n", i+1, detail.DevboxName, detail.State)
			fmt.Printf("  pod delete test: %s\n", formatStatus(detail.PodDeleteOK))
			fmt.Printf("    recovered: %s\n", formatStatus(detail.PodRecovered))
			fmt.Printf("  secret delete test: %s\n", formatStatus(detail.SecretDeleteOK))
			fmt.Printf("    recovered: %s\n", formatStatus(detail.SecretRecovered))
			fmt.Printf("  service delete test: %s\n", formatStatus(detail.ServiceDeleteOK))
			fmt.Printf("    recovered: %s\n", formatStatus(detail.ServiceRecovered))
			fmt.Printf("  total duration: %v\n", detail.TotalDuration)

			if detail.Error != "" {
				fmt.Printf("  error: %s\n", detail.Error)
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// Success/fail status
	if result.FailedTests == 0 {
		fmt.Println("✓ all tests passed!")
	} else {
		fmt.Printf("⚠ some tests failed (%d/%d)\n", result.FailedTests, result.TotalTests)
	}
}

// ==================== Crash Recovery Command ====================

var crashCmd = &cobra.Command{
	Use:   "crash",
	Short: "test container crash recovery",
	Long: `test container crash recovery.

test flow:
1. create devbox and write test data to container
2. loop N times to execute the following steps:
   - kill critical container processes (sleep, sudo, sshd, etc.) to cause container crash
   - wait for Devbox Controller to automatically detect and recreate the Pod
   - wait for new Pod to be fully ready
3. verify data integrity (all data should persist after crashes)

flags:
  --cycles: continuous crash count (default 3)
  --recovery-timeout: single recovery timeout (default 5m)
  --data-size: test data size (default 100M)
  --file-count: test file count (default 5)

examples:
  # basic test: single crash
  devbox-stress edge crash --count 1 --cycles 1

  # continuous crash test 5 times
  devbox-stress edge crash --count 1 --cycles 5

  # concurrent test: 3 devboxes, each with 3 continuous crashes
  devbox-stress edge crash --count 3 --concurrent 3 --cycles 3

  # large data persistence test (500M data, 3 crashes)
  devbox-stress edge crash --count 2 --cycles 3 \
    --data-size 500M --file-count 20

  # stress test: continuous crashes 10 times + cleanup
  devbox-stress edge crash --count 1 --cycles 10 --cleanup
`,
	Run: runCrashRecoveryTest,
}

var (
	// crash specific flags
	crashCount           int
	crashConcurrent      int
	crashCycles          int
	crashWaitAfter       time.Duration
	crashDataSize        string
	crashFileCount       int
	crashRecoveryTimeout time.Duration
	crashTestTimeout     time.Duration
	crashCleanup         bool
	crashVerbose         bool
)

func init() {
	// Add flags
	crashCmd.Flags().IntVarP(&crashCount, "count", "c", 1, "number of devboxes to test")
	crashCmd.Flags().IntVar(&crashConcurrent, "concurrent", 1, "concurrent count (1 for sequential)")
	crashCmd.Flags().IntVar(&crashCycles, "cycles", 3, "number of crash-recovery cycles")
	crashCmd.Flags().DurationVar(&crashWaitAfter, "wait-after-crash", 5*time.Second, "wait time after crash")
	crashCmd.Flags().StringVar(&crashDataSize, "data-size", "100M", "test data size (e.g. 100M)")
	crashCmd.Flags().IntVar(&crashFileCount, "file-count", 5, "number of test files")
	crashCmd.Flags().DurationVar(&crashRecoveryTimeout, "recovery-timeout", 5*time.Minute, "single recovery timeout")
	crashCmd.Flags().DurationVar(&crashTestTimeout, "test-timeout", 30*time.Minute, "total test timeout")
	crashCmd.Flags().BoolVar(&crashCleanup, "cleanup", false, "cleanup resources after test")
	crashCmd.Flags().BoolVarP(&crashVerbose, "verbose", "v", true, "show detailed output")
}

func runCrashRecoveryTest(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// Create configuration
	config := &edge.CrashRecoveryTestConfig{
		Namespace:       edgeNamespace,
		Image:           edgeImage,
		CPU:             edgeCPU,
		Memory:          edgeMemory,
		Storage:         edgeStorage,
		DevboxCount:     crashCount,
		ConcurrentCount: crashConcurrent,
		CrashCycles:     crashCycles,
		WaitAfterCrash:  crashWaitAfter,
		DataSize:        crashDataSize,
		FileCount:       crashFileCount,
		RecoveryTimeout: crashRecoveryTimeout,
		TestTimeout:     crashTestTimeout,
	}

	// Create tester
	tester, err := edge.NewCrashRecoveryTester(config)
	if err != nil {
		log.Fatalf("failed to create tester: %v", err)
	}

	// Run test
	log.Printf("starting crash recovery test...")
	log.Printf("")

	result, err := tester.RunCrashRecoveryTest(ctx)
	if err != nil {
		log.Fatalf("test failed: %v", err)
	}

	// Print result
	printCrashRecoveryResult(result, crashVerbose)

	// Cleanup
	if crashCleanup {
		log.Printf("\ncleaning up resources...")
		if err := tester.Cleanup(ctx); err != nil {
			log.Printf("cleanup failed: %v", err)
		}
	}
}

func printCrashRecoveryResult(result *edge.CrashRecoveryTestResult, verbose bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("crash recovery test summary")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("total tests: %d\n", result.TotalTests)
	fmt.Printf("successful: %d (%.1f%%)\n", result.SuccessfulTests,
		float64(result.SuccessfulTests)/float64(result.TotalTests)*100)
	fmt.Printf("failed: %d (%.1f%%)\n", result.FailedTests,
		float64(result.FailedTests)/float64(result.TotalTests)*100)

	fmt.Printf("\ntime statistics:\n")
	fmt.Printf("  total test time: %v\n", result.TotalTestTime)
	fmt.Printf("  average test time: %v\n", result.AverageTestTime)

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nerror messages (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			fmt.Printf("  [%d] %s\n", i+1, msg)
		}
	}

	// Detailed output
	if verbose {
		fmt.Printf("\ndetailed test results:\n")
		fmt.Println(strings.Repeat("-", 60))

		for i, detail := range result.Details {
			fmt.Printf("\n[%d] devbox: %s\n", i+1, detail.DevboxName)
			fmt.Printf("  crash cycles: %d/%d\n", detail.CrashCycles, len(detail.CrashRecoveries))
			fmt.Printf("  data write: %s\n", formatStatus(detail.DataWriteSuccess))
			fmt.Printf("  data verify: %s\n", formatStatus(detail.DataVerifySuccess))
			fmt.Printf("  total duration: %v\n", detail.TotalDuration)

			if len(detail.CrashRecoveries) > 0 {
				fmt.Printf("  crash recovery details:\n")
				for _, recovery := range detail.CrashRecoveries {
					fmt.Printf("    cycle %d: ", recovery.CycleNumber)
					if recovery.Recovered {
						fmt.Printf("✓ pod recreated in %v\n", recovery.RecoveryDuration)
					} else {
						fmt.Printf("✗ failed - %s\n", recovery.Error)
					}
				}
			}

			if detail.Error != "" {
				fmt.Printf("  error: %s\n", detail.Error)
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// Success/fail status
	if result.FailedTests == 0 {
		fmt.Println("✓ all tests passed!")
	} else {
		fmt.Printf("⚠ some tests failed (%d/%d)\n", result.FailedTests, result.TotalTests)
	}
}
