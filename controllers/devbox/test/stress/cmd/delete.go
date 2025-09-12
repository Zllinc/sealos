package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var deleteTestCmd = &cobra.Command{
	Use:   "delete",
	Short: "删除测试 - 验证 devbox 删除后所有资源都被清理",
	Long: `删除测试会删除现有的测试 devbox，并验证所有相关资源都被正确清理。

这个测试会检查：
1. Devbox 资源是否被删除
2. Pod 是否被删除
3. LVM 逻辑卷是否被删除
4. 其他相关资源是否被清理

注意：此测试需要在并发创建测试之后执行，用于验证删除操作。

示例:
  devbox-stress delete --count 5
  devbox-stress delete --count 3 --namespace test-namespace`,
	RunE: runDeleteTest,
}

var (
	deleteTestCount   int
	deleteTestTimeout time.Duration
	deleteTestVerify  bool
)

func init() {
	rootCmd.AddCommand(deleteTestCmd)

	// Delete test specific flags
	deleteTestCmd.Flags().IntVar(&deleteTestCount, "count", 3, "要创建和删除的 devbox 数量")
	deleteTestCmd.Flags().DurationVar(&deleteTestTimeout, "timeout", 5*time.Minute, "删除测试的超时时间")
	deleteTestCmd.Flags().BoolVar(&deleteTestVerify, "verify", true, "是否验证资源删除")
}

func runDeleteTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     deleteTestCount,
		ConcurrentCount: 1, // make sure to delete one by one
		TestTimeout:     deleteTestTimeout,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始删除测试...\n")
	fmt.Printf("配置: 最大删除数量=%d, 超时=%v, 验证=%t\n", deleteTestCount, deleteTestTimeout, deleteTestVerify)
	fmt.Printf("命名空间: %s\n\n", config.Namespace)

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), deleteTestTimeout)
	defer cancel()

	return stressTester.RunDeleteTest(ctx, deleteTestCount, deleteTestVerify)
}
