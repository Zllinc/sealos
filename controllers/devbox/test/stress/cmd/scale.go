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

var scaleCmd = &cobra.Command{
	Use:   "scale",
	Short: "执行规模测试",
	Long: `规模测试会创建大量的 devbox 来测试集群的承载能力。

这个测试会按顺序创建指定数量的 devbox，并监控创建过程中的性能指标，
包括创建成功率、平均创建时间等。

示例:
  devbox-stress scale --count 100
  devbox-stress scale --count 500 --interval 2s
  devbox-stress scale --count 200 --timeout 45m`,
	RunE: runScaleTest,
}

var (
	scaleCount    int
	scaleInterval time.Duration
	scaleTimeout  time.Duration
	scaleCleanup  bool
)

func init() {
	rootCmd.AddCommand(scaleCmd)

	// Scale specific flags
	scaleCmd.Flags().IntVarP(&scaleCount, "count", "c", 100, "要创建的 devbox 数量")
	scaleCmd.Flags().DurationVarP(&scaleInterval, "interval", "i", 1*time.Second, "创建间隔")
	scaleCmd.Flags().DurationVarP(&scaleTimeout, "timeout", "t", 30*time.Minute, "测试超时时间")
	scaleCmd.Flags().BoolVar(&scaleCleanup, "cleanup", false, "测试后清理资源")

	// Mark required flags
	scaleCmd.MarkFlagRequired("count")
}

func runScaleTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     scaleCount,
		ConcurrentCount: 1, // Scale test is sequential
		CreateInterval:  scaleInterval,
		TestTimeout:     scaleTimeout,
		CleanupAfter:    scaleCleanup,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始规模测试...\n")
	fmt.Printf("配置: 数量=%d, 间隔=%v, 超时=%v\n", scaleCount, scaleInterval, scaleTimeout)
	fmt.Printf("资源: CPU=%s, Memory=%s, Storage=%s\n", config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("镜像: %s\n", config.Image)
	fmt.Printf("命名空间: %s\n", config.Namespace)
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()
	result, err := stressTester.RunScaleTest(ctx)
	if err != nil {
		return fmt.Errorf("规模测试失败: %w", err)
	}

	// Print results
	printTestResult(result)

	if scaleCleanup {
		fmt.Println("\n清理测试资源...")
		if err := stressTester.Cleanup(ctx); err != nil {
			fmt.Printf("清理失败: %v\n", err)
		} else {
			fmt.Println("清理完成")
		}
	}

	return nil
}

func printTestResult(result *tester.StressTestResult) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("               规模测试结果")
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

	if result.MaxQPS > 0 {
		fmt.Printf("最大 QPS: %.2f\n", result.MaxQPS)
	}

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
