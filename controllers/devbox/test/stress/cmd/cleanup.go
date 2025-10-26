package cmd

import (
	"context"
	"fmt"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "cleanup test resources",
	Long: `cleanup command will delete all Devbox and DevBoxRelease resources with test labels.

this command will find all test resources (including Devbox and DevBoxRelease) and delete them. It is usually used after the test.

supported test resources:
  - Devbox (label: stress-test=true)
  - DevBoxRelease (name contains release-test/lifecycle-test)

example:
  # cleanup test resources in a specific namespace
  devbox-stress cleanup --namespace devbox-test

  # cleanup all namespaces
  devbox-stress cleanup --all-namespaces

  # force delete (remove finalizer)
  devbox-stress cleanup --force

  # list resources, but not delete (DryRun)
  devbox-stress cleanup --dry-run

  # skip confirmation prompt
  devbox-stress cleanup --yes`,
	RunE: runCleanup,
}

var (
	allNamespaces bool
	forceCleanup  bool
	dryRun        bool
	skipConfirm   bool
)

func init() {
	rootCmd.AddCommand(cleanupCmd)

	// Cleanup specific flags
	cleanupCmd.Flags().BoolVar(&allNamespaces, "all-namespaces", false, "cleanup test resources in all namespaces")
	cleanupCmd.Flags().BoolVarP(&forceCleanup, "force", "f", false, "force delete, remove finalizer after deletion")
	cleanupCmd.Flags().BoolVar(&dryRun, "dry-run", false, "list resources, but not delete")
	cleanupCmd.Flags().BoolVarP(&skipConfirm, "yes", "y", false, "skip confirmation prompt, directly delete")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	config := &tester.CleanupConfig{
		Namespace:     viper.GetString("namespace"),
		AllNamespaces: allNamespaces,
		Force:         forceCleanup,
		DryRun:        dryRun,
	}

	fmt.Printf("start cleaning up test resources...\n")
	if allNamespaces {
		fmt.Printf("scope: all namespaces\n")
	} else {
		fmt.Printf("scope: %s\n", config.Namespace)
	}
	if dryRun {
		fmt.Printf("mode: DryRun (list resources, but not delete)\n")
	}
	if forceCleanup {
		fmt.Printf("force delete: yes (remove finalizer)\n")
	}
	fmt.Println()

	cleanupTester, err := tester.NewDevboxCleanupTester(config)
	if err != nil {
		return fmt.Errorf("failed to create cleanup tester: %w", err)
	}

	ctx := context.Background()

	// list resources to be cleaned up
	resources, err := cleanupTester.ListTestResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to list test resources: %w", err)
	}

	if resources.TotalResources == 0 {
		fmt.Println("no test resources found to be cleaned up")
		return nil
	}

	// print cleanup plan
	cleanupTester.PrintCleanupPlan(resources)

	// if DryRun, return directly
	if dryRun {
		fmt.Println("\nℹ DryRun mode: no actual deletion performed")
		return nil
	}

	// confirm deletion
	if !skipConfirm {
		fmt.Printf("\nconfirm deletion of these resources? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" && response != "YES" {
			fmt.Println("cancel cleanup operation")
			return nil
		}
	}

	// execute cleanup
	fmt.Println("\n正在清理资源...")
	result, err := cleanupTester.RunCleanup(ctx)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// print result
	cleanupTester.PrintCleanupResult(result)

	return nil
}
