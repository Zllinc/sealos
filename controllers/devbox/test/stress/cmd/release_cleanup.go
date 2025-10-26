package cmd

import (
	"context"
	"fmt"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var releaseCleanupCmd = &cobra.Command{
	Use:   "release-cleanup",
	Short: "cleanup test resources for DevBoxRelease",
	Long: `cleanup command will delete all test created DevBoxRelease and related Devbox resources.

this command will find all test related DevBoxRelease and Devbox, and delete them all.
it is usually used after the release test.

supported cleanup scopes:
- DevBoxRelease: all test created release records
- Devbox: test created devbox (optional)
- image tag: test created image tag (optional, need to be cleaned up manually)

example:
  # cleanup release test resources in current namespace
  devbox-stress release-cleanup
  
  # cleanup all namespaces
  devbox-stress release-cleanup --all-namespaces
  
  # force cleanup, without confirmation
  devbox-stress release-cleanup --force
  
  # cleanup test created Devbox
  devbox-stress release-cleanup --cleanup-devbox`,
	RunE: runReleaseCleanup,
}

var (
	cleanupAllNamespaces bool
	cleanupForce         bool
	cleanupDevbox        bool
	cleanupImages        bool
)

func init() {
	rootCmd.AddCommand(releaseCleanupCmd)

	// Release cleanup specific flags
	releaseCleanupCmd.Flags().BoolVar(&cleanupAllNamespaces, "all-namespaces", false, "cleanup test resources in all namespaces")
	releaseCleanupCmd.Flags().BoolVarP(&cleanupForce, "force", "f", false, "force delete, without confirmation")
	releaseCleanupCmd.Flags().BoolVar(&cleanupDevbox, "cleanup-devbox", false, "cleanup test created Devbox")
	releaseCleanupCmd.Flags().BoolVar(&cleanupImages, "cleanup-images", false, "list images that need to be cleaned up manually (not automatically deleted)")
}

func runReleaseCleanup(cmd *cobra.Command, args []string) error {
	config := &tester.ReleaseTestConfig{
		ReleaseCount:      0,
		ConcurrentCount:   0,
		TestTimeout:       0,
		CleanupAfter:      false,
		BaseDevboxName:    "",
		VersionPattern:    "",
		StartAfterRelease: false,
		Namespace:         viper.GetString("namespace"),
		Image:             viper.GetString("image"),
		CPU:               viper.GetString("cpu"),
		Memory:            viper.GetString("memory"),
		StorageLimit:      viper.GetString("storage"),
	}

	fmt.Printf("start cleaning up DevBoxRelease test resources...\n")
	if cleanupAllNamespaces {
		fmt.Printf("scope: all namespaces\n")
	} else {
		fmt.Printf("scope: %s\n", config.Namespace)
	}
	fmt.Println()

	releaseTester, err := tester.NewDevboxReleaseTester(config)
	if err != nil {
		return fmt.Errorf("failed to create release tester: %w", err)
	}

	ctx := context.Background()

	// list resources to be cleaned up
	releases, devboxes, err := releaseTester.ListReleaseTestResources(ctx, cleanupAllNamespaces)
	if err != nil {
		return fmt.Errorf("failed to list test resources: %w", err)
	}

	if len(releases) == 0 && len(devboxes) == 0 {
		fmt.Println("no test resources found to be cleaned up")
		return nil
	}

	// print resources to be cleaned up
	if len(releases) > 0 {
		fmt.Printf("found %d DevBoxRelease:\n", len(releases))
		for i, release := range releases {
			if i < 20 { // only show first 20
				fmt.Printf("  - %s/%s (版本: %s, 状态: %s)\n",
					release.Namespace, release.Name, release.Spec.Version, release.Status.Phase)
			}
		}
		if len(releases) > 20 {
			fmt.Printf("  ... there are %d more\n", len(releases)-20)
		}
		fmt.Println()
	}

	if cleanupDevbox && len(devboxes) > 0 {
		fmt.Printf("found %d test Devbox:\n", len(devboxes))
		for i, devbox := range devboxes {
			if i < 20 { // only show first 20
				fmt.Printf("  - %s/%s (state: %s)\n",
					devbox.Namespace, devbox.Name, devbox.Status.State)
			}
		}
		if len(devboxes) > 20 {
			fmt.Printf("  ... there are %d more\n", len(devboxes)-20)
		}
		fmt.Println()
	}

	// 显示镜像信息（如果启用）
	if cleanupImages && len(releases) > 0 {
		fmt.Println("test created image tags (need to be cleaned up manually):")
		imageSet := make(map[string]bool)
		for _, release := range releases {
			if release.Status.TargetImage != "" && !imageSet[release.Status.TargetImage] {
				fmt.Printf("  - %s\n", release.Status.TargetImage)
				imageSet[release.Status.TargetImage] = true
			}
		}
		fmt.Printf("\nnote: images need to be deleted manually in the repository, or use image cleanup strategy\n\n")
	}

	// confirm deletion (unless using force)
	if !cleanupForce {
		totalToDelete := len(releases)
		if cleanupDevbox {
			totalToDelete += len(devboxes)
		}
		fmt.Printf("confirm deletion of these %d resources? (y/N): ", totalToDelete)
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" && response != "YES" {
			fmt.Println("cancel cleanup operation")
			return nil
		}
	}

	// execute cleanup
	fmt.Println("cleaning up resources...")

	deletedReleases, deletedDevboxes, err := releaseTester.CleanupWithDetails(ctx, cleanupAllNamespaces, cleanupDevbox)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	fmt.Printf("\ncleanup completed:\n")
	fmt.Printf("  deleted DevBoxRelease: %d\n", deletedReleases)
	if cleanupDevbox {
		fmt.Printf("  deleted Devbox: %d\n", deletedDevboxes)
	}

	if cleanupImages && deletedReleases > 0 {
		fmt.Printf("\nnote: images are not deleted, need to be cleaned up manually in the repository\n")
	}

	return nil
}
