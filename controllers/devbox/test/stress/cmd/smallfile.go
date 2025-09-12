package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	smallFileCount      int
	smallFileTotalSize  string
	smallFileConcurrent int
	smallFileDevboxCount int
	smallFileTimeout    time.Duration
)

// smallfileCmd represents the smallfile command
var smallfileCmd = &cobra.Command{
	Use:   "smallfile",
	Short: "run smallfile commit test",
	Long: `run smallfile commit test, test smallfile commit performance.

test features:
- file size: 1K to 500K, mainly distributed around 200K
- total data size: 1GB
- test flow: only execute to commit complete
- target state: Stopped

test flow:
1. find existing running devbox
2. concurrent write smallfile test data
3. modify state to Stopped to trigger commit
4. wait for commit complete

test purpose:
- test smallfile commit performance
- verify commit mechanism for smallfile
- performance benchmark`,
	Example: `  # basic smallfile test
  devbox-stress smallfile

  # custom file count and total size
  devbox-stress smallfile --count 1000 --totalsize 2G

  # high concurrent test
  devbox-stress smallfile --concurrent 10 --filecount 2000

  # long time test
  devbox-stress smallfile --timeout 30m --filecount 5000`,
	RunE: runSmallFileTest,
}

func init() {
	rootCmd.AddCommand(smallfileCmd)

	// Small file specific flags
	smallfileCmd.Flags().IntVarP(&smallFileDevboxCount, "count", "d", 10, "number of running devbox to find")
	smallfileCmd.Flags().IntVarP(&smallFileCount, "filecount", "c", 5000, "number of smallfile to create")
	smallfileCmd.Flags().StringVar(&smallFileTotalSize, "totalsize", "1G", "total data size")
	smallfileCmd.Flags().IntVar(&smallFileConcurrent, "concurrent", 1, "number of concurrent operation")
	smallfileCmd.Flags().DurationVar(&smallFileTimeout, "timeout", 20*time.Minute, "test timeout")
}

func runSmallFileTest(cmd *cobra.Command, args []string) error {
	config := &tester.StressTestConfig{
		DevboxCount:     smallFileDevboxCount,
		ConcurrentCount: smallFileConcurrent,
		TestTimeout:     smallFileTimeout,
		CleanupAfter:    false, 
		Image:           viper.GetString("image"),
		CPU:             viper.GetString("cpu"),
		Memory:          viper.GetString("memory"),
		StorageLimit:    viper.GetString("storage"),
		Namespace:       viper.GetString("namespace"),
	}

	fmt.Printf("start smallfile commit test...\n")
	fmt.Printf("config: file count=%d, total size=%s, concurrent count=%d, timeout=%v\n",
		smallFileCount, smallFileTotalSize, smallFileConcurrent, smallFileTimeout)
	fmt.Printf("image: %s, cpu: %s, memory: %s, storage: %s\n",
		config.Image, config.CPU, config.Memory, config.StorageLimit)
	fmt.Printf("namespace: %s\n", config.Namespace)
	fmt.Println()

	stressTester, err := tester.NewDevboxStressTester(config)
	if err != nil {
		return fmt.Errorf("create stress tester failed: %w", err)
	}

	ctx := context.Background()

	// run smallfile commit test
	return stressTester.RunSmallFileCommitTest(ctx, smallFileCount, smallFileTotalSize, smallFileConcurrent, smallFileTimeout, smallFileDevboxCount)
}
