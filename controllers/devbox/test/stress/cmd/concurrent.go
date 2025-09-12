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

var concurrentCmd = &cobra.Command{
	Use:   "concurrent",
	Short: "执行并发测试",
	Long: `并发测试会同时创建多个 devbox 来测试集群的并发处理能力。

这个测试会使用多个 goroutine 并发创建 devbox，并计算最大 QPS、
平均创建时间等性能指标。

示例:
  devbox-stress concurrent --count 50 --concurrent 10
  devbox-stress concurrent --count 100 --concurrent 20 --timeout 20m
  devbox-stress concurrent -c 200 --concurrent 50`,
	RunE: runConcurrentTest,
}

var (
	concurrentCount   int
	concurrentLevel   int
	concurrentTimeout time.Duration
	concurrentCleanup bool
)

func init() {
	rootCmd.AddCommand(concurrentCmd)

	// Concurrent specific flags
	concurrentCmd.Flags().IntVarP(&concurrentCount, "count", "c", 50, "要创建的 devbox 数量")
	concurrentCmd.Flags().IntVar(&concurrentLevel, "concurrent", 10, "并发级别")
	concurrentCmd.Flags().DurationVarP(&concurrentTimeout, "timeout", "t", 20*time.Minute, "测试超时时间")
	concurrentCmd.Flags().BoolVar(&concurrentCleanup, "cleanup", false, "测试后清理资源")

	// Mark required flags
	concurrentCmd.MarkFlagRequired("count")
}

func runConcurrentTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     concurrentCount,
		ConcurrentCount: concurrentLevel,
		CreateInterval:  0, // No interval for concurrent test
		TestTimeout:     concurrentTimeout,
		CleanupAfter:    concurrentCleanup,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始并发测试...\n")
	fmt.Printf("配置: 数量=%d, 并发=%d, 超时=%v\n", concurrentCount, concurrentLevel, concurrentTimeout)
	fmt.Printf("资源: CPU=%s, Memory=%s, Storage=%s\n", config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("镜像: %s\n", config.Image)
	fmt.Printf("命名空间: %s\n", config.Namespace)
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()
	result, err := stressTester.RunConcurrentTest(ctx)
	if err != nil {
		return fmt.Errorf("并发测试失败: %w", err)
	}

	// Print results
	printConcurrentResult(result)

	if concurrentCleanup {
		fmt.Println("\n清理测试资源...")
		if err := stressTester.Cleanup(ctx); err != nil {
			fmt.Printf("清理失败: %v\n", err)
		} else {
			fmt.Println("清理完成")
		}
	}

	return nil
}

func printConcurrentResult(result *tester.StressTestResult) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("               并发测试结果")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("总 Devbox 数量: %d\n", result.TotalDevboxes)
	fmt.Printf("成功创建: %d\n", result.SuccessfulCreates)
	fmt.Printf("创建失败: %d\n", result.FailedCreates)

	successRate := float64(0)
	if result.TotalDevboxes > 0 {
		successRate = float64(result.SuccessfulCreates) / float64(result.TotalDevboxes) * 100
	}
	fmt.Printf("成功率: %.2f%%\n", successRate)
	fmt.Printf("平均创建时间: %v\n", result.AverageCreateTime)
	fmt.Printf("总测试时间: %v\n", result.TotalTestTime)
	fmt.Printf("最大 QPS: %.2f\n", result.MaxQPS)

	if len(result.ErrorMessages) > 0 {
		fmt.Println("\n错误信息:")
		for i, msg := range result.ErrorMessages {
			if i >= 10 { // 只显示前10个错误
				fmt.Printf("... 还有 %d 个错误\n", len(result.ErrorMessages)-10)
				break
			}
			fmt.Printf("  %s\n", msg)
		}
	}
	fmt.Println(strings.Repeat("=", 50))
}
