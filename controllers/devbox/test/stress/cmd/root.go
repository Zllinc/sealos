package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "devbox-stress",
	Short: "Devbox stress test tool",
	Long: `Devbox stress test tool provides multiple stress test scenarios:

- scale: scale test, create a lot of devbox
- concurrent: concurrent test, test concurrent creation ability
- release: DevBoxRelease test, test release functionality
- lifecycle: full lifecycle test, from creation to release
- commit: commit test, test commit functionality
- delete: delete test, test devbox deletion and resource cleanup
- edge: edge case test, test boundary conditions
- monitor: resource monitoring, continuously monitor cluster status
- cleanup: cleanup test resources
- smallfile: smallfile test, test smallfile write ability

examples:
  devbox-stress scale --count 100
  devbox-stress concurrent --count 50 --concurrent 10
  devbox-stress release --count 10 --concurrent 5
  devbox-stress lifecycle --count 5 --concurrent 3
  devbox-stress commit stopped --count 5
  devbox-stress delete --namespace devbox-test --concurrent 5
  devbox-stress edge toggle --cycles 10 --count 5
  devbox-stress monitor --duration 30m
  devbox-stress cleanup -n namespace
  devbox-stress smallfile --count 100 --concurrent 10`,
	Version: "1.0.0",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.devbox-stress.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Global flags for all commands
	rootCmd.PersistentFlags().String("kubeconfig", "", "path to kubeconfig file")
	rootCmd.PersistentFlags().StringP("namespace", "n", "devbox-test", "kubernetes namespace")
	rootCmd.PersistentFlags().String("image", "ghcr.io/labring-actions/devbox/go-1.23.0:13aacd8", "devbox image")
	rootCmd.PersistentFlags().String("cpu", "2000m", "CPU resource")
	rootCmd.PersistentFlags().String("memory", "4096Mi", "memory resource")
	rootCmd.PersistentFlags().String("storage", "10Gi", "storage limit")

	// Bind flags to viper
	viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	viper.BindPFlag("namespace", rootCmd.PersistentFlags().Lookup("namespace"))
	viper.BindPFlag("image", rootCmd.PersistentFlags().Lookup("image"))
	viper.BindPFlag("cpu", rootCmd.PersistentFlags().Lookup("cpu"))
	viper.BindPFlag("memory", rootCmd.PersistentFlags().Lookup("memory"))
	viper.BindPFlag("storage", rootCmd.PersistentFlags().Lookup("storage"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".devbox-stress" (without extension).
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".devbox-stress")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil && verbose {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
