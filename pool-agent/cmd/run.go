package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(agentRunCommand)
}

var agentRunCommand = &cobra.Command{
	Use: "run",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf, err := LoadConfig()
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		sigHupCh := make(chan os.Signal, 1)
		signal.Notify(sigHupCh, syscall.SIGHUP)

		agent, err := newAgent(ctx, conf)
		if err != nil {
			return err
		}

		return agent.Run(ctx, sigHupCh)
	}}
