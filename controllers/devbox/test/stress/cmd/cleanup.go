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
	Short: "清理测试资源",
	Long: `清理命令会删除所有带有压测标签的 devbox 资源。

这个命令会查找所有带有 "stress-test=true" 标签的 devbox，
并将它们全部删除。通常在测试完成后使用。

示例:
  devbox-stress cleanup
  devbox-stress cleanup --namespace test-namespace
  devbox-stress cleanup --all-namespaces`,
	RunE: runCleanup,
}

var (
	allNamespaces bool
	force         bool
)

func init() {
	rootCmd.AddCommand(cleanupCmd)

	// Cleanup specific flags
	cleanupCmd.Flags().BoolVar(&allNamespaces, "all-namespaces", false, "清理所有命名空间中的测试资源")
	cleanupCmd.Flags().BoolVarP(&force, "force", "f", false, "强制删除，不询问确认")
	cleanupCmd.Flags().BoolVar(&force, "force-delete", false, "强制删除，移除 finalizer 后删除")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     0,
		ConcurrentCount: 0,
		CreateInterval:  0,
		TestTimeout:     0,
		CleanupAfter:    false,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始清理测试资源...\n")
	if allNamespaces {
		fmt.Printf("目标: 所有命名空间\n")
	} else {
		fmt.Printf("目标命名空间: %s\n", config.Namespace)
	}
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()

	// List resources to be cleaned up
	resources, err := stressTester.ListTestResources(ctx, allNamespaces)
	if err != nil {
		return fmt.Errorf("列出测试资源失败: %w", err)
	}

	if len(resources) == 0 {
		fmt.Println("未找到需要清理的测试资源")
		return nil
	}

	fmt.Printf("找到 %d 个测试资源:\n", len(resources))
	for _, resource := range resources {
		fmt.Printf("  - %s/%s\n", resource.Namespace, resource.Name)
	}
	fmt.Println()

	// Confirm deletion unless force flag is set
	if !force {
		fmt.Printf("确认删除这些资源? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" && response != "YES" {
			fmt.Println("取消清理操作")
			return nil
		}
	}

	// Perform cleanup
	fmt.Println("正在清理资源...")

	var deletedCount int

	// 检查是否使用强制删除
	if force {
		fmt.Println("使用强制删除模式...")
		if err := stressTester.ForceCleanup(ctx); err != nil {
			return fmt.Errorf("强制清理失败: %w", err)
		}
		deletedCount = len(resources) // 假设全部删除
	} else {
		var err error
		deletedCount, err = stressTester.CleanupWithDetails(ctx, allNamespaces)
		if err != nil {
			return fmt.Errorf("清理失败: %w", err)
		}
	}

	fmt.Printf("清理完成，已删除 %d 个资源\n", deletedCount)
	return nil
}
