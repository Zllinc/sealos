package cmd

import (
	"context"
	"fmt"
	"time"

	devboxv1alpha2 "github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "测试 Devbox commit 操作",
	Long: `测试 Devbox commit 操作的完整流程。

这个命令会执行以下测试流程：
1. 查找现有的运行中的 devbox
2. 向容器内写入测试数据
3. 修改 devbox 状态触发 commit
4. 恢复状态并验证数据完整性

示例:
  devbox-stress commit stopped --count 5 --concurrent 3
  devbox-stress commit shutdown --count 3 --timeout 10m --concurrent 3`,
}

var stoppedCmd = &cobra.Command{
	Use:   "stopped",
	Short: "测试通过 Stopped 状态触发 commit",
	Long: `测试通过将 Devbox 状态设置为 Stopped 来触发 commit 操作。

流程：
1. 选择指定数量的运行中 devbox
2. 向容器内写入随机数据文件
3. 将状态改为 Stopped 触发 commit
4. 等待 commit 完成
5. 将状态改回 Running
6. 验证数据是否保持完整`,
	RunE: runStoppedCommitTest,
}

var shutdownCmd = &cobra.Command{
	Use:   "shutdown",
	Short: "测试通过 Shutdown 状态触发 commit",
	Long: `测试通过将 Devbox 状态设置为 Shutdown 来触发 commit 操作。

流程：
1. 选择指定数量的运行中 devbox
2. 向容器内写入随机数据文件
3. 将状态改为 Shutdown 触发 commit
4. 等待 commit 完成
5. 将状态改回 Running
6. 验证数据是否保持完整`,
	RunE: runShutdownCommitTest,
}

var (
	commitCount       int
	commitTimeout     time.Duration
	dataSize          string
	verifyData        bool
	commitConcurrency int
	fileCount         int
)

func init() {
	rootCmd.AddCommand(commitCmd)
	commitCmd.AddCommand(stoppedCmd)
	commitCmd.AddCommand(shutdownCmd)

	// Commit specific flags
	commitCmd.PersistentFlags().IntVarP(&commitCount, "count", "c", 5, "要测试的 devbox 数量")
	commitCmd.PersistentFlags().DurationVar(&commitTimeout, "timeout", 20*time.Minute, "commit 操作超时时间")
	commitCmd.PersistentFlags().StringVar(&dataSize, "datasize", "100M", "每个测试文件大小")
	commitCmd.PersistentFlags().BoolVar(&verifyData, "verify", true, "是否验证数据完整性")
	commitCmd.PersistentFlags().IntVar(&commitConcurrency, "concurrent", 3, "并发操作数量")
	commitCmd.PersistentFlags().IntVar(&fileCount, "filecount", 20, "每个 devbox 创建的文件数量")
}

func runStoppedCommitTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     commitCount,
		ConcurrentCount: commitConcurrency, // 使用指定的并发数
		TestTimeout:     commitTimeout,
		CleanupAfter:    false,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始 Stopped 状态 commit 测试...\n")
	fmt.Printf("配置: 数量=%d, 并发数=%d, 文件数=%d, 超时=%v, 数据大小=%s\n", commitCount, commitConcurrency, fileCount, commitTimeout, dataSize)
	fmt.Printf("命名空间: %s\n", config.Namespace)
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()
	return stressTester.RunCommitTest(ctx, string(devboxv1alpha2.DevboxStateStopped), commitCount, dataSize, verifyData, commitConcurrency, fileCount, commitTimeout)
}

func runShutdownCommitTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     commitCount,
		ConcurrentCount: commitConcurrency, // 使用指定的并发数
		TestTimeout:     commitTimeout,
		CleanupAfter:    false,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始 Shutdown 状态 commit 测试...\n")
	fmt.Printf("配置: 数量=%d, 并发数=%d, 文件数=%d, 超时=%v, 数据大小=%s\n", commitCount, commitConcurrency, fileCount, commitTimeout, dataSize)
	fmt.Printf("命名空间: %s\n", config.Namespace)
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()
	return stressTester.RunCommitTest(ctx, string(devboxv1alpha2.DevboxStateShutdown), commitCount, dataSize, verifyData, commitConcurrency, fileCount, commitTimeout)
}
