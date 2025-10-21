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

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "执行 DevBoxRelease 发版测试",
	Long: `发版测试会创建 DevBoxRelease 来测试发版功能的正确性和性能。

这个测试支持两种模式：

1. 单一 DevBox 多版本发版（指定 --base-devbox）：
   - 对指定的 DevBox 创建多个版本的 DevBoxRelease
   - 测试同一 DevBox 的并发发版能力
   - 适用于版本管理测试场景

2. 多 DevBox 并发发版（不指定 --base-devbox）：
   - 创建多个独立的 DevBox，每个 DevBox 进行一次发版
   - 测试集群的并发发版承载能力
   - 更真实的压力测试场景

测试包括：
- 基础发版功能验证
- 状态转换监控（Pending → Success/Failed）
- 镜像 digest 一致性验证
- 发版后 Devbox 启动验证（可选）
- 并发压力测试

示例:
  # 多 DevBox 并发发版测试（推荐，更真实的压力测试）
  devbox-stress release --count 10 --concurrent 5

  # 单一 DevBox 多版本发版测试
  devbox-stress release --count 5 --base-devbox my-devbox

  # 高并发发版测试
  devbox-stress release --count 50 --concurrent 20 --timeout 60m

  # 发版后自动启动 devbox
  devbox-stress release --count 3 --start-after-release`,
	RunE: runReleaseTest,
}

var (
	releaseCount      int
	releaseConcurrent int
	releaseTimeout    time.Duration
	releaseCleanup    bool
	baseDevbox        string
	versionPattern    string
	startAfterRelease bool
)

func init() {
	rootCmd.AddCommand(releaseCmd)

	// Release specific flags
	releaseCmd.Flags().IntVarP(&releaseCount, "count", "c", 5, "要创建的发版数量")
	releaseCmd.Flags().IntVar(&releaseConcurrent, "concurrent", 1, "并发级别（默认1为顺序测试，大于1为并发测试）")
	releaseCmd.Flags().DurationVarP(&releaseTimeout, "timeout", "t", 30*time.Minute, "测试超时时间")
	releaseCmd.Flags().BoolVar(&releaseCleanup, "cleanup", false, "测试后清理资源")
	releaseCmd.Flags().StringVar(&baseDevbox, "base-devbox", "", "基础 devbox 名称（如果为空，会自动创建）")
	releaseCmd.Flags().StringVar(&versionPattern, "version-pattern", "v1.0.%d", "版本号模式")
	releaseCmd.Flags().BoolVar(&startAfterRelease, "start-after-release", false, "发版后是否启动 devbox")

	// Mark required flags
	releaseCmd.MarkFlagRequired("count")
}

func runReleaseTest(cmd *cobra.Command, args []string) error {
	config := &tester.ReleaseTestConfig{
		ReleaseCount:      releaseCount,
		ConcurrentCount:   releaseConcurrent,
		TestTimeout:       releaseTimeout,
		CleanupAfter:      releaseCleanup,
		BaseDevboxName:    baseDevbox,
		VersionPattern:    versionPattern,
		StartAfterRelease: startAfterRelease,
		Namespace:         viper.GetString("namespace"),
		Image:             viper.GetString("image"),
		CPU:               viper.GetString("cpu"),
		Memory:            viper.GetString("memory"),
		StorageLimit:      viper.GetString("storage"),
	}

	// 根据是否指定基础 Devbox 判断测试模式
	var testMode string
	if baseDevbox != "" {
		testMode = "单一 DevBox 多版本发版"
		fmt.Printf("开始发版测试...\n")
		fmt.Printf("模式: %s\n", testMode)
		fmt.Printf("基础 Devbox: %s\n", baseDevbox)
		fmt.Printf("配置: 发版数量=%d, 并发级别=%d, 超时=%v\n", releaseCount, releaseConcurrent, releaseTimeout)
	} else {
		testMode = "多 DevBox 并发发版"
		fmt.Printf("开始发版测试...\n")
		fmt.Printf("模式: %s\n", testMode)
		fmt.Printf("配置: 创建 %d 个 DevBox，并发级别=%d, 超时=%v\n", releaseCount, releaseConcurrent, releaseTimeout)
		fmt.Printf("说明: 每个 DevBox 将独立创建并进行一次发版\n")
	}

	fmt.Printf("版本模式: %s\n", versionPattern)
	fmt.Printf("发版后启动: %v\n", startAfterRelease)
	fmt.Printf("命名空间: %s\n", config.Namespace)
	fmt.Printf("资源: CPU=%s, Memory=%s, Storage=%s\n", config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("镜像: %s\n", config.Image)
	fmt.Println()

	releaseTester, err := tester.NewDevboxReleaseTester(config)
	if err != nil {
		return fmt.Errorf("创建发版测试器失败: %w", err)
	}

	ctx := context.Background()
	var result *tester.ReleaseTestResult

	// 根据并发级别选择测试方法
	if releaseConcurrent > 1 {
		fmt.Println("执行并发发版测试...")
		result, err = releaseTester.RunConcurrentReleaseTest(ctx)
	} else {
		fmt.Println("执行基础发版测试...")
		result, err = releaseTester.RunBasicReleaseTest(ctx)
	}

	if err != nil {
		return fmt.Errorf("发版测试失败: %w", err)
	}

	// 打印结果
	printReleaseResult(result)

	if releaseCleanup {
		fmt.Println("\n清理测试资源...")
		if err := releaseTester.Cleanup(ctx); err != nil {
			fmt.Printf("清理失败: %v\n", err)
		} else {
			fmt.Println("清理完成")
		}
	}

	return nil
}

func printReleaseResult(result *tester.ReleaseTestResult) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("                    发版测试结果")
	fmt.Println(strings.Repeat("=", 60))

	// 基础统计
	fmt.Printf("总发版数量: %d\n", result.TotalReleases)
	fmt.Printf("成功发版: %d\n", result.SuccessfulReleases)
	fmt.Printf("失败发版: %d\n", result.FailedReleases)
	fmt.Printf("待处理发版: %d\n", result.PendingReleases)

	successRate := float64(0)
	if result.TotalReleases > 0 {
		successRate = float64(result.SuccessfulReleases) / float64(result.TotalReleases) * 100
	}
	fmt.Printf("成功率: %.2f%%\n", successRate)

	// 性能指标
	fmt.Println("\n" + strings.Repeat("-", 60))
	fmt.Println("性能指标:")
	fmt.Printf("  平均发版时间: %v\n", result.AverageReleaseTime)
	fmt.Printf("  总测试时间: %v\n", result.TotalTestTime)
	fmt.Printf("  最大 QPS: %.2f\n", result.MaxQPS)

	// 镜像验证结果
	if len(result.ImageVerifications) > 0 {
		fmt.Println("\n" + strings.Repeat("-", 60))
		fmt.Println("镜像一致性验证:")

		matchCount := 0
		for _, check := range result.ImageVerifications {
			if check.DigestMatch {
				matchCount++
			}
		}

		verifyRate := float64(0)
		if len(result.ImageVerifications) > 0 {
			verifyRate = float64(matchCount) / float64(len(result.ImageVerifications)) * 100
		}

		fmt.Printf("  验证总数: %d\n", len(result.ImageVerifications))
		fmt.Printf("  验证通过: %d\n", matchCount)
		fmt.Printf("  验证失败: %d\n", len(result.ImageVerifications)-matchCount)
		fmt.Printf("  验证通过率: %.2f%%\n", verifyRate)

		// 显示失败的验证详情
		failedChecks := 0
		for _, check := range result.ImageVerifications {
			if !check.DigestMatch || !check.SourceFound || !check.TargetFound {
				if failedChecks == 0 {
					fmt.Println("\n  失败的验证详情:")
				}
				failedChecks++
				if failedChecks <= 5 { // 只显示前5个失败
					fmt.Printf("    - %s:\n", check.ReleaseName)
					if !check.SourceFound {
						fmt.Printf("      源镜像未找到: %s\n", check.SourceImage)
					}
					if !check.TargetFound {
						fmt.Printf("      目标镜像未找到: %s\n", check.TargetImage)
					}
					if check.SourceFound && check.TargetFound && !check.DigestMatch {
						fmt.Printf("      Digest 不匹配\n")
					}
					if check.Error != "" {
						fmt.Printf("      错误: %s\n", check.Error)
					}
				}
			}
		}
		if failedChecks > 5 {
			fmt.Printf("    ... 还有 %d 个失败的验证\n", failedChecks-5)
		}
	}

	// 错误信息
	if len(result.ErrorMessages) > 0 {
		fmt.Println("\n" + strings.Repeat("-", 60))
		fmt.Println("错误信息:")
		for i, msg := range result.ErrorMessages {
			if i >= 10 { // 只显示前10个错误
				fmt.Printf("  ... 还有 %d 个错误\n", len(result.ErrorMessages)-10)
				break
			}
			fmt.Printf("  %d. %s\n", i+1, msg)
		}
	}

	fmt.Println(strings.Repeat("=", 60))
}
