package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func init() {
	rootCmd.AddCommand(agentRunCommand)
}

var agentRunCommand = &cobra.Command{
	Use: "run",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		sigHupCh := make(chan os.Signal, 1)
		signal.Notify(sigHupCh, syscall.SIGHUP)

		agent, err := newAgent(ctx)
		if err != nil {
			return err
		}

		eg, egCtx := errgroup.WithContext(ctx)

		eg.Go(func() error {
			if err := agent.CollectMetrics(egCtx); err != nil {
				return fmt.Errorf("collect metrics: %w", err)
			}
			return nil
		})

		eg.Go(func() error {
			if err := agent.Run(egCtx, sigHupCh); err != nil {
				return fmt.Errorf("run agent: %w", err)
			}
			return nil
		})
		return eg.Wait()
	}}
