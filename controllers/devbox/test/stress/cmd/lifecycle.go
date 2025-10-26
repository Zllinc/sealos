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

var lifecycleCmd = &cobra.Command{
	Use:   "lifecycle",
	Short: "test complete lifecycle of devboxes",
	Long: `test complete lifecycle of devboxes from creation to release.

测试流程:
1. create devbox and verify all resources (Secret, Service, Pod, LV)
2. write first batch of data → Stopped commit → restore Running → verify data
3. write second batch of data → Shutdown commit → restore Running → verify data
4. stop devbox (required before release)
5. test release of devbox

example:
  # test single devbox
  devbox-stress lifecycle --count 1

  # test 5 devboxes, concurrent count is 3
  devbox-stress lifecycle --count 5 --concurrent 3

  # custom data size and file count
  devbox-stress lifecycle --count 2 --data1-size 200M --data2-size 300M --file-count 20

  # clean up after test
  devbox-stress lifecycle --count 3 --cleanup`,
	RunE: runLifecycleTest,
}

var (
	lifecycleCount      int
	lifecycleConcurrent int
	data1Size           string
	data2Size           string
	lifecycleFileCount  int
	lifecycleVerify     bool
	releaseVersion      string
	lifecycleCleanup    bool
	lifecycleTimeout    time.Duration
)

func init() {
	rootCmd.AddCommand(lifecycleCmd)

	// Lifecycle specific flags
	lifecycleCmd.Flags().IntVarP(&lifecycleCount, "count", "c", 1, "要测试的 Devbox 数量")
	lifecycleCmd.Flags().IntVar(&lifecycleConcurrent, "concurrent", 1, "并发测试数量（1=顺序执行）")
	lifecycleCmd.Flags().StringVar(&data1Size, "data1-size", "100M", "第一次写入的数据大小")
	lifecycleCmd.Flags().StringVar(&data2Size, "data2-size", "100M", "第二次写入的数据大小")
	lifecycleCmd.Flags().IntVar(&lifecycleFileCount, "file-count", 10, "每次写入的文件数量")
	lifecycleCmd.Flags().BoolVar(&lifecycleVerify, "verify", true, "是否验证数据完整性")
	lifecycleCmd.Flags().StringVar(&releaseVersion, "release-version", "v1.0.%d", "发版版本号模式")
	lifecycleCmd.Flags().BoolVar(&lifecycleCleanup, "cleanup", false, "测试完成后是否清理资源")
	lifecycleCmd.Flags().DurationVarP(&lifecycleTimeout, "timeout", "t", 30*time.Minute, "整体测试超时时间")
}

func runLifecycleTest(cmd *cobra.Command, args []string) error {
	config := &tester.LifecycleTestConfig{
		DevboxCount:           lifecycleCount,
		ConcurrentCount:       lifecycleConcurrent,
		TestTimeout:           lifecycleTimeout,
		CleanupAfter:          lifecycleCleanup,
		Data1Size:             data1Size,
		Data2Size:             data2Size,
		FileCount:             lifecycleFileCount,
		VerifyData:            lifecycleVerify,
		ReleaseVersionPattern: releaseVersion,
		Image:                 viper.GetString("image"),
		CPU:                   viper.GetString("cpu"),
		Memory:                viper.GetString("memory"),
		StorageLimit:          viper.GetString("storage"),
		Namespace:             viper.GetString("namespace"),
	}

	// 打印配置
	fmt.Printf("========================================\n")
	fmt.Printf("      Devbox lifecycle test configuration\n")
	fmt.Printf("========================================\n")
	fmt.Printf("test count: %d\n", lifecycleCount)
	fmt.Printf("concurrent level: %d\n", lifecycleConcurrent)
	fmt.Printf("data configuration: Phase1=%s, Phase2=%s, file count=%d\n", data1Size, data2Size, lifecycleFileCount)
	fmt.Printf("release version: %s\n", releaseVersion)
	fmt.Printf("resource configuration: CPU=%s, Memory=%s, Storage=%s\n", config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("image: %s\n", config.Image)
	fmt.Printf("namespace: %s\n", config.Namespace)
	fmt.Printf("timeout: %v\n", lifecycleTimeout)
	fmt.Printf("cleanup after test: %v\n", lifecycleCleanup)
	fmt.Printf("========================================\n\n")

	lifecycleTester, err := tester.NewDevboxLifecycleTester(config)
	if err != nil {
		return fmt.Errorf("failed to create lifecycle tester: %w", err)
	}

	ctx := context.Background()

	var result *tester.LifecycleTestResult
	if lifecycleConcurrent > 1 {
		fmt.Printf("start concurrent lifecycle test (concurrent count: %d)...\n\n", lifecycleConcurrent)
		result, err = lifecycleTester.RunConcurrentLifecycleTest(ctx)
	} else {
		fmt.Printf("start sequential lifecycle test...\n\n")
		result, err = lifecycleTester.RunLifecycleTest(ctx)
	}

	if err != nil {
		return fmt.Errorf("lifecycle test failed: %w", err)
	}

	// print test results
	printLifecycleResult(result)

	// clean up resources
	if lifecycleCleanup {
		fmt.Println("\nclean up test resources...")
		if err := lifecycleTester.Cleanup(ctx); err != nil {
			fmt.Printf("clean up failed: %v\n", err)
		} else {
			fmt.Println("clean up completed")
		}
	}

	return nil
}

func printLifecycleResult(result *tester.LifecycleTestResult) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("         Devbox lifecycle test result")
	fmt.Println(strings.Repeat("=", 50))

	// total statistics
	fmt.Printf("\ntotal result:\n")
	fmt.Printf("  total test count: %d\n", result.TotalTests)
	fmt.Printf("  successful: %d\n", result.SuccessfulTests)
	fmt.Printf("  failed: %d\n", result.FailedTests)

	successRate := float64(0)
	if result.TotalTests > 0 {
		successRate = float64(result.SuccessfulTests) / float64(result.TotalTests) * 100
	}
	fmt.Printf("  success rate: %.2f%%\n", successRate)

	// success rate for each phase
	fmt.Printf("\nsuccess rate for each phase:\n")
	fmt.Printf("  resource check: %d/%d (%.2f%%)\n",
		result.ResourceCheckPassed, result.TotalTests,
		calculateRate(result.ResourceCheckPassed, result.TotalTests))
	fmt.Printf("  stopped commit: %d/%d (%.2f%%)\n",
		result.StoppedCommitPassed, result.TotalTests,
		calculateRate(result.StoppedCommitPassed, result.TotalTests))
	fmt.Printf("  shutdown commit: %d/%d (%.2f%%)\n",
		result.ShutdownCommitPassed, result.TotalTests,
		calculateRate(result.ShutdownCommitPassed, result.TotalTests))
	fmt.Printf("  release test: %d/%d (%.2f%%)\n",
		result.ReleasePassed, result.TotalTests,
		calculateRate(result.ReleasePassed, result.TotalTests))

	// performance metrics
	fmt.Printf("\nperformance metrics:\n")
	fmt.Printf("  average test time: %v\n", result.AverageTestTime)
	fmt.Printf("  total test time: %v\n", result.TotalTestTime)

	// detailed information
	if len(result.TestDetails) > 0 {
		fmt.Printf("\ndetailed information:\n")
		for i, detail := range result.TestDetails {
			status := "✓"
			if detail.Error != "" {
				status = "✗"
			}
			fmt.Printf("  %s %s: ", status, detail.DevboxName)

			// show status of each phase
			phases := []string{}
			if detail.ResourceCheckOK {
				phases = append(phases, "resource ✓")
			} else {
				phases = append(phases, "resource ✗")
			}
			if detail.StoppedCommitOK {
				phases = append(phases, "Stopped✓")
			} else {
				phases = append(phases, "Stopped✗")
			}
			if detail.ShutdownCommitOK {
				phases = append(phases, "Shutdown✓")
			} else {
				phases = append(phases, "Shutdown✗")
			}
			if detail.ReleaseOK {
				phases = append(phases, "Release✓")
			} else {
				phases = append(phases, "Release✗")
			}

			fmt.Printf("%s (duration: %v)\n", strings.Join(phases, ", "), detail.TotalDuration)

			if detail.Error != "" && i < 5 {
				fmt.Printf("      error: %s\n", detail.Error)
			}
		}

		if len(result.TestDetails) > 5 && result.FailedTests > 5 {
			fmt.Printf("  ... there are %d more test results\n", len(result.TestDetails)-5)
		}
	}

	// error messages
	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nerror summary:\n")
		for i, msg := range result.ErrorMessages {
			if i >= 10 {
				fmt.Printf("  ... there are %d more errors\n", len(result.ErrorMessages)-10)
				break
			}
			fmt.Printf("  - %s\n", msg)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
}

func calculateRate(success, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(success) / float64(total) * 100
}
