package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "test Devbox commit operation",
	Long: `test Devbox commit operation complete流程。

this command will execute the following test flow:
1. find existing running devbox
2. write test data to container
3. modify devbox state to trigger commit
4. restore state and verify data integrity

example:
  devbox-stress commit stopped --count 5 --concurrent 3
  devbox-stress commit shutdown --count 3 --timeout 10m --concurrent 3`,
}

var stoppedCmd = &cobra.Command{
	Use:   "stopped",
	Short: "test through Stopped state to trigger commit",
	Long: `test through setting Devbox state to Stopped to trigger commit operation.

test flow:
1. select specified number of running devbox
2. write test data to container
3. modify devbox state to Stopped to trigger commit
4. wait for commit complete
5. restore state and verify data integrity`,
	RunE: runStoppedCommitTest,
}

var shutdownCmd = &cobra.Command{
	Use:   "shutdown",
	Short: "test through Shutdown state to trigger commit",
	Long: `test through setting Devbox state to Shutdown to trigger commit operation.

test flow:
1. select specified number of running devbox
2. write test data to container
3. modify devbox state to Shutdown to trigger commit
4. wait for commit complete
5. restore state and verify data integrity`,
	RunE: runShutdownCommitTest,
}

var (
	commitCount       int
	commitTimeout     time.Duration
	dataSize          string
	verifyData        bool
	commitConcurrency int
	fileCount         int
	commitVerbose     bool
)

func init() {
	rootCmd.AddCommand(commitCmd)
	commitCmd.AddCommand(stoppedCmd)
	commitCmd.AddCommand(shutdownCmd)

	// Commit specific flags
	commitCmd.PersistentFlags().IntVarP(&commitCount, "count", "c", 5, "number of devbox to test")
	commitCmd.PersistentFlags().DurationVar(&commitTimeout, "timeout", 20*time.Minute, "commit operation timeout")
	commitCmd.PersistentFlags().StringVar(&dataSize, "datasize", "100M", "size of each test file")
	commitCmd.PersistentFlags().BoolVar(&verifyData, "verify", false, "verify data integrity")
	commitCmd.PersistentFlags().IntVar(&commitConcurrency, "concurrent", 3, "number of concurrent operation")
	commitCmd.PersistentFlags().IntVar(&fileCount, "filecount", 5, "number of files created for each devbox")
	commitCmd.PersistentFlags().BoolVarP(&commitVerbose, "verbose", "v", true, "display detailed output")
}

func runStoppedCommitTest(cmd *cobra.Command, args []string) error {
	return runCommitTestWithState(string(devboxv1alpha2.DevboxStateStopped))
}

func runShutdownCommitTest(cmd *cobra.Command, args []string) error {
	return runCommitTestWithState(string(devboxv1alpha2.DevboxStateShutdown))
}

func runCommitTestWithState(targetState string) error {
	config := &tester.CommitTestConfig{
		Namespace:       viper.GetString("namespace"),
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		DevboxCount:     commitCount,
		ConcurrentCount: commitConcurrency,
		TargetState:     targetState,
		DataSize:        dataSize,
		FileCount:       fileCount,
		VerifyData:      verifyData,
		CommitTimeout:   commitTimeout,
		TestTimeout:     commitTimeout,
	}

	fmt.Printf("start %s state commit test...\n", targetState)
	fmt.Printf("configuration:\n")
	fmt.Printf("  Devbox number: %d\n", commitCount)
	fmt.Printf("  Concurrent count: %d\n", commitConcurrency)
	fmt.Printf("  File count: %d\n", fileCount)
	fmt.Printf("  Data size: %s\n", dataSize)
	fmt.Printf("  Verify data: %v\n", verifyData)
	fmt.Printf("  Timeout: %v\n", commitTimeout)
	fmt.Printf("  Namespace: %s\n", config.Namespace)
	fmt.Println()

	commitTester, err := tester.NewDevboxCommitTester(config)
	if err != nil {
		return fmt.Errorf("failed to create commit tester: %w", err)
	}

	ctx := context.Background()
	result, err := commitTester.RunCommitTest(ctx)
	if err != nil {
		return fmt.Errorf("commit test failed: %w", err)
	}

	// print result
	printCommitResult(result, commitVerbose)

	return nil
}

func printCommitResult(result *tester.CommitTestResult, verbose bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("commit test result summary")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("Total Devbox number: %d\n", result.TotalDevboxes)
	fmt.Printf("Successful commits: %d (%.1f%%)\n", result.SuccessfulCommits,
		float64(result.SuccessfulCommits)/float64(result.TotalDevboxes)*100)
	fmt.Printf("Failed commits: %d (%.1f%%)\n", result.FailedCommits,
		float64(result.FailedCommits)/float64(result.TotalDevboxes)*100)

	fmt.Printf("\nTime statistics:\n")
	fmt.Printf("  Write data time: %v (speed: %.2f MB/s)\n", result.WriteDataTime, result.WriteSpeedMBps)
	fmt.Printf("  Commit time: %v (QPS: %.2f)\n", result.CommitTime, result.CommitQPS)
	if result.VerifyTime > 0 {
		fmt.Printf("  Verify time: %v\n", result.VerifyTime)
	}
	fmt.Printf("  Total test time: %v\n", result.TotalTestTime)

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nError messages (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			if i >= 10 {
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
			if detail.CommitSuccess {
				successCount++
				if successCount <= 5 { // only show first 5 successful commits
					fmt.Printf("\n[%d] Devbox: %s\n", i+1, detail.DevboxName)
					fmt.Printf("  Write data: ✓ (duration: %v)\n", detail.WriteDuration)
					fmt.Printf("  Commit: ✓ (duration: %v)\n", detail.CommitDuration)
					if detail.VerifySuccess {
						fmt.Printf("  Verify data: ✓ (duration: %v)\n", detail.VerifyDuration)
					}
					fmt.Printf("  Total duration: %v\n", detail.TotalDuration)
				}
			} else {
				fmt.Printf("\n[%d] Devbox: %s\n", i+1, detail.DevboxName)
				fmt.Printf("  Write data: %s\n", formatCommitStatus(detail.WriteSuccess))
				fmt.Printf("  Commit: %s\n", formatCommitStatus(detail.CommitSuccess))
				if detail.VerifySuccess {
					fmt.Printf("  Verify data: %s\n", formatCommitStatus(detail.VerifySuccess))
				}
				if detail.Error != "" {
					fmt.Printf("  Error: %s\n", detail.Error)
				}
				fmt.Printf("  Total duration: %v\n", detail.TotalDuration)
			}
		}

		if successCount > 5 {
			fmt.Printf("\n... remaining %d successful commits (omitted)\n", successCount-5)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// success/failure status
	if result.FailedCommits == 0 {
		fmt.Println("✓ All commit tests passed!")
	} else {
		fmt.Printf("⚠ Some commit tests failed (%d/%d)\n", result.FailedCommits, result.TotalDevboxes)
	}
}

func formatCommitStatus(success bool) string {
	if success {
		return "✓ Success"
	}
	return "✗ Failed"
}
