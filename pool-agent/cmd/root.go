package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "pool-agent",
}

var (
	configPath string
	inSecure   bool
)

// Execute executes the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "/etc/pool-agent/config.toml", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&inSecure, "insecure", "k", false, "allow insecure server connections when using SSL")
}
