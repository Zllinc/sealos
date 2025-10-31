package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var concurrentCmd = &cobra.Command{
	Use:   "concurrent",
	Short: "test concurrent create operation",
	Long: `test concurrent create operation complete流程。

this command will execute the following test flow:
1. create specified number of devbox concurrently
2. wait for all devbox to become Running state
3. calculate success rate, QPS, average create time etc.

example:
  devbox-stress concurrent --count 50 --concurrent 10
  devbox-stress concurrent --count 100 --concurrent 20 --timeout 20m
  devbox-stress concurrent --count 50 --concurrent 10 --verbose
  devbox-stress concurrent --count 50 --concurrent 10 --cleanup`,
	RunE: runConcurrentTest,
}

var (
	concurrentCount    int
	concurrentLevel    int
	concurrentTimeout  time.Duration
	concurrentWaitTime time.Duration
	concurrentCleanup  bool
	concurrentVerbose  bool
)

func init() {
	rootCmd.AddCommand(concurrentCmd)

	// Concurrent specific flags
	concurrentCmd.Flags().IntVarP(&concurrentCount, "count", "c", 5, "number of devbox to create")
	concurrentCmd.Flags().IntVar(&concurrentLevel, "concurrent", 1, "concurrent level")
	concurrentCmd.Flags().DurationVar(&concurrentTimeout, "timeout", 20*time.Minute, "test timeout")
	concurrentCmd.Flags().DurationVar(&concurrentWaitTime, "wait-timeout", 5*time.Minute, "wait for devbox to become Running timeout")
	concurrentCmd.Flags().BoolVar(&concurrentCleanup, "cleanup", false, "cleanup after test")
	concurrentCmd.Flags().BoolVarP(&concurrentVerbose, "verbose", "v", false, "display detailed output")
}

func runConcurrentTest(cmd *cobra.Command, args []string) error {
	config := &tester.ConcurrentTestConfig{
		Namespace:       viper.GetString("namespace"),
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		DevboxCount:     concurrentCount,
		ConcurrentCount: concurrentLevel,
		TestTimeout:     concurrentTimeout,
		WaitTimeout:     concurrentWaitTime,
	}

	fmt.Printf("start concurrent create test...\n")
	fmt.Printf("configuration:\n")
	fmt.Printf("  Devbox number: %d\n", concurrentCount)
	fmt.Printf("  Concurrent level: %d\n", concurrentLevel)
	fmt.Printf("  Timeout: %v\n", concurrentTimeout)
	fmt.Printf("  Wait for Running timeout: %v\n", concurrentWaitTime)
	fmt.Printf("  Resources: CPU=%s, Memory=%s, Storage=%s\n", config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("  Image: %s\n", config.Image)
	fmt.Printf("  Namespace: %s\n", config.Namespace)
	fmt.Println()

	concurrentTester, err := tester.NewDevboxConcurrentTester(config)
	if err != nil {
		return fmt.Errorf("failed to create concurrent tester: %w", err)
	}

	ctx := context.Background()
	result, err := concurrentTester.RunConcurrentTest(ctx)
	if err != nil {
		return fmt.Errorf("concurrent test failed: %w", err)
	}

	// Print results
	printConcurrentResult(result, concurrentVerbose)

	if concurrentCleanup {
		fmt.Println("\ncleanup test resources...")
		if err := concurrentTester.Cleanup(ctx); err != nil {
			fmt.Printf("cleanup failed: %v\n", err)
		} else {
			fmt.Println("cleanup completed")
		}
	}

	return nil
}

func printConcurrentResult(result *tester.ConcurrentTestResult, verbose bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("concurrent test result summary")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Total Devbox number: %d\n", result.TotalDevboxes)
	fmt.Printf("Successful creates: %d (%.1f%%)\n", result.SuccessfulCreates,
		float64(result.SuccessfulCreates)/float64(result.TotalDevboxes)*100)
	fmt.Printf("Failed creates: %d (%.1f%%)\n", result.FailedCreates,
		float64(result.FailedCreates)/float64(result.TotalDevboxes)*100)

	fmt.Printf("\nTime statistics:\n")
	fmt.Printf("  Average create time: %v\n", result.AverageCreateTime)
	fmt.Printf("  Total test time: %v\n", result.TotalTestTime)
	fmt.Printf("  Max QPS: %.2f/s\n", result.MaxQPS)

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nError messages (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			if i >= 10 { // only show first 10 errors
				fmt.Printf("  ... remaining %d errors\n", len(result.ErrorMessages)-10)
				break
			}
			fmt.Printf("  [%d] %s\n", i+1, msg)
		}
	}

	// detailed output
	if verbose && len(result.Details) > 0 {
		fmt.Printf("\nDetailed test results:\n")
		fmt.Println(strings.Repeat("-", 60))

		successCount := 0
		for i, detail := range result.Details {
			if detail.CreateSuccess && detail.RunningSuccess {
				successCount++
				if successCount <= 5 { // only show first 5 successful creates
					fmt.Printf("\n[%d] Devbox: %s\n", i+1, detail.DevboxName)
					fmt.Printf("  Create success: ✓ (duration: %v)\n", detail.CreateDuration)
					fmt.Printf("  Running ready: ✓\n")
					fmt.Printf("  Total duration: %v\n", detail.TotalDuration)
				}
			} else {
				fmt.Printf("\n[%d] Devbox: %s\n", i+1, detail.DevboxName)
				fmt.Printf("  Create success: %s\n", formatBool(detail.CreateSuccess))
				fmt.Printf("  Running ready: %s\n", formatBool(detail.RunningSuccess))
				if detail.Error != "" {
					fmt.Printf("  Error: %s\n", detail.Error)
				}
				fmt.Printf("  Total duration: %v\n", detail.TotalDuration)
			}
		}

		if successCount > 5 {
			fmt.Printf("\n... remaining %d successful devboxes (omitted)\n", successCount-5)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// success/failure status
	if result.FailedCreates == 0 {
		fmt.Println("✓ All devboxes created successfully!")
	} else {
		fmt.Printf("⚠ Some devboxes created failed (%d/%d)\n", result.FailedCreates, result.TotalDevboxes)
	}
}

func formatBool(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
