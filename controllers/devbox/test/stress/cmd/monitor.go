package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "执行资源监控",
	Long: `资源监控会持续监控集群的资源使用情况，包括节点状态、Pod 状态、
Devbox 状态等信息。

这个测试不会创建新的 devbox，而是监控现有的集群状态，
适合在其他测试运行时并行执行。

示例:
  devbox-stress monitor --duration 30m
  devbox-stress monitor --duration 1h --interval 60s
  devbox-stress monitor -d 10m`,
	RunE: runMonitorTest,
}

var (
	monitorDuration time.Duration
	monitorInterval time.Duration
)

func init() {
	rootCmd.AddCommand(monitorCmd)

	// Monitor specific flags
	monitorCmd.Flags().DurationVarP(&monitorDuration, "duration", "d", 30*time.Minute, "监控持续时间")
	monitorCmd.Flags().DurationVarP(&monitorInterval, "interval", "i", 5*time.Second, "监控间隔")

	// Mark required flags
	monitorCmd.MarkFlagRequired("duration")
}

func runMonitorTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     0, // No devbox creation for monitor
		ConcurrentCount: 0,
		CreateInterval:  0,
		TestTimeout:     monitorDuration,
		CleanupAfter:    false,
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("开始资源监控...\n")
	fmt.Printf("配置: 持续时间=%v, 监控间隔=%v\n", monitorDuration, monitorInterval)
	fmt.Printf("命名空间: %s\n", config.Namespace)
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("创建压测工具失败: %w", err)
	}

	ctx := context.Background()

	// Create a custom monitor with specified interval
	fmt.Printf("监控开始，持续 %v，每 %v 输出一次状态...\n\n", monitorDuration, monitorInterval)

	err = runCustomMonitor(ctx, stressTester, monitorDuration, monitorInterval)
	if err != nil {
		return fmt.Errorf("资源监控失败: %w", err)
	}

	fmt.Println("\n监控完成")
	return nil
}

func runCustomMonitor(ctx context.Context, tester *tester.DevboxStressTester, duration, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	timeout := time.After(duration)
	startTime := time.Now()

	for {
		select {
		case <-timeout:
			return nil
		case <-ticker.C:
			elapsed := time.Since(startTime)
			fmt.Printf("=== 监控状态 (%v / %v) ===\n", elapsed.Round(time.Second), duration)

			if err := tester.CollectResourceMetrics(ctx); err != nil {
				fmt.Printf("收集资源指标失败: %v\n", err)
			}
			fmt.Println()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
