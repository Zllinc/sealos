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

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "execute DevBoxRelease release test",
	Long: `release test will create DevBoxRelease to test the correctness and performance of the release functionality.

this test supports two modes:

1. single DevBox multiple versions release (specified --base-devbox):
   - create multiple versions of DevBoxRelease for the specified DevBox
   - test concurrent release capability of the same DevBox
   - suitable for version management test scenarios

2. multiple DevBox concurrent release (not specified --base-devbox):
   - create multiple independent DevBox, each DevBox performs one release
   - test concurrent release capability of the cluster
   - more realistic pressure test scenario

test includes:
- basic release functionality verification
- state transition monitoring (Pending → Success/Failed)
- image digest consistency verification
- post-release Devbox startup verification (optional)
- concurrent pressure test

examples:
  # multiple DevBox concurrent release test (recommended, more realistic pressure test)
  devbox-stress release --count 10 --concurrent 5

  # single DevBox multiple versions release test
  devbox-stress release --count 5 --base-devbox my-devbox

  # high concurrent release test
  devbox-stress release --count 50 --concurrent 20 --timeout 60m

  # start devbox after release
  devbox-stress release --count 3 --start-after-release`,
	RunE: runReleaseTest,
}

var (
	releaseCount      int
	releaseConcurrent int
	releaseTimeout    time.Duration
	releaseCleanup    bool
	baseDevbox        string
	versionPattern    string
	startAfterRelease bool
	commitTrigger     string
)

func init() {
	rootCmd.AddCommand(releaseCmd)

	// Release specific flags
	releaseCmd.Flags().IntVarP(&releaseCount, "count", "c", 5, "number of releases to create")
	releaseCmd.Flags().IntVar(&releaseConcurrent, "concurrent", 1, "concurrent level: 1 for sequential test, >1 for concurrent test")
	releaseCmd.Flags().DurationVarP(&releaseTimeout, "timeout", "t", 30*time.Minute, "test timeout")
	releaseCmd.Flags().BoolVar(&releaseCleanup, "cleanup", false, "cleanup after test")
	releaseCmd.Flags().StringVar(&baseDevbox, "base-devbox", "", "base devbox name (if empty, will be created automatically)")
	releaseCmd.Flags().StringVar(&versionPattern, "version-pattern", "v1.0.%d", "version pattern")
	releaseCmd.Flags().BoolVar(&startAfterRelease, "start-after-release", false, "start devbox after release")
	releaseCmd.Flags().StringVar(&commitTrigger, "commit-trigger", "stop", "commit trigger mode: stop (Stopped) or pause (Paused)")

	// Mark required flags
	releaseCmd.MarkFlagRequired("count")
}

func runReleaseTest(cmd *cobra.Command, args []string) error {
	// parse commit trigger mode
	var commitTriggerMode tester.CommitTriggerMode
	switch commitTrigger {
	case "shutdown":
		commitTriggerMode = tester.CommitTriggerByShutdown
	case "stopped":
		commitTriggerMode = tester.CommitTriggerByStopped
	default:
		commitTriggerMode = tester.CommitTriggerByStopped
	}

	config := &tester.ReleaseTestConfig{
		ReleaseCount:      releaseCount,
		ConcurrentCount:   releaseConcurrent,
		TestTimeout:       releaseTimeout,
		CleanupAfter:      releaseCleanup,
		BaseDevboxName:    baseDevbox,
		VersionPattern:    versionPattern,
		StartAfterRelease: startAfterRelease,
		CommitTrigger:     commitTriggerMode,
		Namespace:         viper.GetString("namespace"),
		Image:             viper.GetString("image"),
		CPU:               viper.GetString("cpu"),
		Memory:            viper.GetString("memory"),
		StorageLimit:      viper.GetString("storage"),
	}

	// test mode based on concurrent level
	testMode := "sequential"
	if releaseConcurrent > 1 {
		testMode = "concurrent"
	}

	fmt.Printf("start release test...\n")
	fmt.Printf("mode: %s\n", testMode)
	fmt.Printf("configuration: create %d DevBox, concurrent level=%d, timeout=%v\n", releaseCount, releaseConcurrent, releaseTimeout)
	fmt.Printf("each DevBox will go through the complete Running → Commit → Release process\n")

	fmt.Printf("version pattern: %s\n", versionPattern)
	fmt.Printf("commit trigger mode: %s\n", commitTrigger)
	fmt.Printf("start after release: %v\n", startAfterRelease)
	fmt.Printf("namespace: %s\n", config.Namespace)
	fmt.Printf("resources: CPU=%s, Memory=%s, Storage=%s\n", config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("image: %s\n", config.Image)
	fmt.Println()

	releaseTester, err := tester.NewDevboxReleaseTester(config)
	if err != nil {
		return fmt.Errorf("failed to create release tester: %w", err)
	}

	ctx := context.Background()
	var result *tester.ReleaseTestResult

	// test method based on concurrent level
	if releaseConcurrent > 1 {
		fmt.Println("execute concurrent release test...")
		result, err = releaseTester.RunConcurrentReleaseTest(ctx)
	} else {
		fmt.Println("execute basic release test...")
		result, err = releaseTester.RunBasicReleaseTest(ctx)
	}

	if err != nil {
		return fmt.Errorf("release test failed: %w", err)
	}

	// print results
	printReleaseResult(result)

	if releaseCleanup {
		fmt.Println("\ncleanup test resources...")
		if err := releaseTester.Cleanup(ctx); err != nil {
			fmt.Printf("cleanup failed: %v\n", err)
		} else {
			fmt.Println("cleanup completed")
		}
	}

	return nil
}

func printReleaseResult(result *tester.ReleaseTestResult) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("                     release test result")
	fmt.Println(strings.Repeat("=", 60))

	// basic statistics
	fmt.Printf("total release count: %d\n", result.TotalReleases)
	fmt.Printf("successful releases: %d\n", result.SuccessfulReleases)
	fmt.Printf("failed releases: %d\n", result.FailedReleases)
	fmt.Printf("pending releases: %d\n", result.PendingReleases)

	successRate := float64(0)
	if result.TotalReleases > 0 {
		successRate = float64(result.SuccessfulReleases) / float64(result.TotalReleases) * 100
	}
	fmt.Printf("success rate: %.2f%%\n", successRate)

	// performance metrics
	fmt.Println("\n" + strings.Repeat("-", 60))
	fmt.Println("performance metrics:")
	fmt.Printf("   average release time: %v\n", result.AverageReleaseTime)
	fmt.Printf("   total test time: %v\n", result.TotalTestTime)
	fmt.Printf("   max QPS: %.2f\n", result.MaxQPS)

	// image verification result
	if len(result.ImageVerifications) > 0 {
		fmt.Println("\n" + strings.Repeat("-", 60))
		fmt.Println("image consistency verification:")

		matchCount := 0
		for _, check := range result.ImageVerifications {
			if check.DigestMatch {
				matchCount++
			}
		}

		verifyRate := float64(0)
		if len(result.ImageVerifications) > 0 {
			verifyRate = float64(matchCount) / float64(len(result.ImageVerifications)) * 100
		}

		fmt.Printf("   verification total: %d\n", len(result.ImageVerifications))
		fmt.Printf("   verification passed: %d\n", matchCount)
		fmt.Printf("   verification failed: %d\n", len(result.ImageVerifications)-matchCount)
		fmt.Printf("   verification pass rate: %.2f%%\n", verifyRate)

		// show failed verification details
		failedChecks := 0
		for _, check := range result.ImageVerifications {
			if !check.DigestMatch || !check.SourceFound || !check.TargetFound {
				if failedChecks == 0 {
					fmt.Println("\n   failed verification details:")
				}
				failedChecks++
				if failedChecks <= 5 { // only show first 5 failed
					fmt.Printf("    - %s:\n", check.ReleaseName)
					if !check.SourceFound {
						fmt.Printf("       source image not found: %s\n", check.SourceImage)
					}
					if !check.TargetFound {
						fmt.Printf("       target image not found: %s\n", check.TargetImage)
					}
					if check.SourceFound && check.TargetFound && !check.DigestMatch {
						fmt.Printf("      digest mismatch\n")
					}
					if check.Error != "" {
						fmt.Printf("       error: %s\n", check.Error)
					}
				}
			}
		}
		if failedChecks > 5 {
			fmt.Printf("    ... there are %d more failed verifications\n", failedChecks-5)
		}
	}

	// error messages
	if len(result.ErrorMessages) > 0 {
		fmt.Println("\n" + strings.Repeat("-", 60))
		fmt.Println("error messages:")
		for i, msg := range result.ErrorMessages {
			if i >= 10 { // only show first 10 errors
				fmt.Printf("  ... there are %d more errors\n", len(result.ErrorMessages)-10)
				break
			}
			fmt.Printf("  %d. %s\n", i+1, msg)
		}
	}

	fmt.Println(strings.Repeat("=", 60))
}
