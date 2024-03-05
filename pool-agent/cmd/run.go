package cmd

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

		reg := prometheus.NewRegistry()
		reg.MustRegister(agent)
		handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})

		http.Handle("/metrics", handler)

		server := http.Server{
			Addr:    ":" + metricsPort,
			Handler: nil,
		}
		eg, ctx := errgroup.WithContext(ctx)
		eg.Go(func() error {
			return server.ListenAndServe()
		})
		eg.Go(func() error {
			<-ctx.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			return server.Shutdown(ctx)
		})
		eg.Go(func() error {
			return agent.Run(ctx, sigHupCh)
		})
		return eg.Wait()
	}}
