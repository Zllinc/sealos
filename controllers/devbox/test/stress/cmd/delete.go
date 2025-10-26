package cmd

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "test devbox deletion and resource cleanup",
	Long: `test devbox deletion and resource cleanup.

this command will execute the following test flow:
1. scan all devboxes in the specified namespace
2. delete devboxes concurrently
3. monitor and verify resource cleanup (devbox, pod, service, secret, lvm)
4. output detailed deletion report

example:
  # delete all devboxes in the specified namespace (sequential deletion)
  devbox-stress delete --namespace devbox-test

  # concurrent deletion (concurrent count is 5)
  devbox-stress delete --namespace devbox-test --concurrent 5

  # custom timeout and check interval
  devbox-stress delete --namespace devbox-test --concurrent 3 --timeout 5m --check-interval 2s

  # view detailed results
  devbox-stress delete --namespace devbox-test --verbose

note:
- deletion operation is irreversible, please use with caution
- it is recommended to use in test environment
- you can first use 'kubectl get devbox -n <namespace>' to confirm the devboxes to be deleted
`,
	Run: runDeleteTest,
}

var (
	deleteNamespace  string
	deleteConcurrent int
	deleteTimeout    time.Duration
	checkInterval    time.Duration
	testTimeout      time.Duration
	verboseOutput    bool
)

func init() {
	rootCmd.AddCommand(deleteCmd)

	deleteCmd.Flags().StringVarP(&deleteNamespace, "namespace", "n", "devbox-test", "namespace of devboxes")
	deleteCmd.Flags().IntVarP(&deleteConcurrent, "concurrent", "c", 1, "concurrent deletion count (1 means sequential deletion)")
	deleteCmd.Flags().DurationVar(&deleteTimeout, "timeout", 10*time.Minute, "timeout for single devbox deletion")
	deleteCmd.Flags().DurationVar(&checkInterval, "check-interval", 2*time.Second, "resource check interval")
	deleteCmd.Flags().DurationVar(&testTimeout, "test-timeout", 30*time.Minute, "total test timeout")
	deleteCmd.Flags().BoolVarP(&verboseOutput, "verbose", "v", true, "display detailed output")
}

func runDeleteTest(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// create configuration
	config := &tester.DeleteTestConfig{
		Namespace:       deleteNamespace,
		ConcurrentCount: deleteConcurrent,
		CheckInterval:   checkInterval,
		DeleteTimeout:   deleteTimeout,
		TestTimeout:     testTimeout,
	}

	// create delete tester
	deleteTester, err := tester.NewDevboxDeleteTester(config)
	if err != nil {
		log.Fatalf("failed to create delete tester: %v", err)
	}

	// run delete test
	log.Printf("start delete test...")
	log.Printf("configuration:")
	log.Printf("  namespace: %s", config.Namespace)
	log.Printf("  concurrent count: %d", config.ConcurrentCount)
	log.Printf("  delete timeout: %v", config.DeleteTimeout)
	log.Printf("  check interval: %v", config.CheckInterval)
	log.Printf("  total test timeout: %v", config.TestTimeout)
	log.Printf("")

	result, err := deleteTester.RunDeleteTest(ctx)
	if err != nil {
		log.Fatalf("delete test failed: %v", err)
	}

	// print results
	printDeleteResults(result, deleteTester, verboseOutput)
}

func printDeleteResults(result *tester.DeleteTestResult, deleteTester *tester.DevboxDeleteTester, verbose bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("delete test result summary")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("total delete count: %d\n", result.TotalDevboxes)
	fmt.Printf("successful delete: %d (%.1f%%)\n", result.SuccessfulDeletes,
		float64(result.SuccessfulDeletes)/float64(result.TotalDevboxes)*100)
	fmt.Printf("failed delete: %d (%.1f%%)\n", result.FailedDeletes,
		float64(result.FailedDeletes)/float64(result.TotalDevboxes)*100)

	fmt.Printf("\nTime statistics:\n")
	fmt.Printf("  average delete time: %v\n", result.AverageDeleteTime)
	fmt.Printf("  total test time: %v\n", result.TotalTestTime)
	fmt.Printf("  delete QPS: %.2f/s\n", result.MaxQPS)

	fmt.Printf("\nresource cleanup statistics:\n")
	printResourceStat("Devbox", result.ResourceCheckStats.DevboxCleaned, result.TotalDevboxes)
	printResourceStat("Pod", result.ResourceCheckStats.PodCleaned, result.TotalDevboxes)
	printResourceStat("Service", result.ResourceCheckStats.ServiceCleaned, result.TotalDevboxes)
	printResourceStat("Secret", result.ResourceCheckStats.SecretCleaned, result.TotalDevboxes)
	printResourceStat("LVM", result.ResourceCheckStats.LVMCleaned, result.TotalDevboxes)

	if len(result.ErrorMessages) > 0 {
		fmt.Printf("\nerror messages (%d):\n", len(result.ErrorMessages))
		for i, msg := range result.ErrorMessages {
			fmt.Printf("  [%d] %s\n", i+1, msg)
		}
	}

	// detailed output
	if verbose {
		deleteTester.PrintDetailedResults(result)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))

	// success/failure status
	if result.FailedDeletes == 0 {
		fmt.Println("✓ All devboxes deleted successfully, resource cleanup completed!")
	} else {
		fmt.Printf("⚠ Some devboxes deleted failed or resource not fully cleaned (%d/%d)\n",
			result.FailedDeletes, result.TotalDevboxes)
	}
}

func printResourceStat(resourceType string, cleaned, total int) {
	percentage := 0.0
	if total > 0 {
		percentage = float64(cleaned) / float64(total) * 100
	}
	status := "✓"
	if cleaned < total {
		status = "⚠"
	}
	fmt.Printf("  %s %-10s: %3d/%3d (%.1f%%)\n", status, resourceType, cleaned, total, percentage)
}
