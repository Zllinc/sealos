package cmd

import (
	"context"
	"fmt"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var lvmCmd = &cobra.Command{
	Use:   "lvm",
	Short: "LVM 相关操作",
	Long: `LVM 相关操作，包括检查 LVM 状态和清理 LVM 资源。

示例:
  devbox-stress lvm status    # 检查 LVM 状态
  devbox-stress lvm cleanup   # 清理 LVM 资源`,
}

var lvmStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "检查 LVM 状态",
	Long:  `检查 LVM 卷组和逻辑卷的状态`,
	RunE:  runLVMStatus,
}

var lvmCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "清理 LVM 资源",
	Long:  `清理与测试 devbox 相关的 LVM 逻辑卷`,
	RunE:  runLVMCleanup,
}

func init() {
	rootCmd.AddCommand(lvmCmd)
	lvmCmd.AddCommand(lvmStatusCmd)
	lvmCmd.AddCommand(lvmCleanupCmd)
}

func runLVMStatus(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		Namespace: viper.GetString("namespace"),
	}

	fmt.Printf("检查 LVM 状态...\n")
	fmt.Printf("命名空间: %s\n\n", config.Namespace)

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	if err := stressTester.CheckLVMStatus(); err != nil {
		return fmt.Errorf("检查 LVM 状态失败: %w", err)
	}

	fmt.Println("LVM 状态检查完成")
	return nil
}

func runLVMCleanup(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		Namespace: viper.GetString("namespace"),
	}

	fmt.Printf("开始清理 LVM 资源...\n")
	fmt.Printf("命名空间: %s\n\n", config.Namespace)

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()

	// 直接根据标签清理 LVM 资源
	if err := stressTester.CleanupLVMByLabels(ctx, false); err != nil {
		return fmt.Errorf("清理 LVM 资源失败: %w", err)
	}

	fmt.Println("LVM 资源清理完成")
	return nil
}
