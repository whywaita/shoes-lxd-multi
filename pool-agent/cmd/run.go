package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	lxd "github.com/lxc/lxd/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func init() {
	rootCmd.AddCommand(agentRunCommand)
}

var agentRunCommand = &cobra.Command{
	Use: "run",
	RunE: func(cmd *cobra.Command, args []string) error {
		configMap, err := LoadConfig()
		if err != nil {
			return err
		}

		checkInterval, concurrentCreateLimit, waitIdleTime, zombieAllowTime, err := LoadParams()
		if err != nil {
			return err
		}

		var agents []*Agent

		for stadium, conf := range configMap {
			source, err := ParseImageAlias(conf.ImageAlias)
			if err != nil {
				return err
			}

			lxdClientCert, err := os.ReadFile(conf.CertPath)
			if err != nil {
				return err
			}
			certString := string(lxdClientCert)
			lxdClientKey, err := os.ReadFile(conf.KeyPath)
			if err != nil {
				return err
			}
			keyString := string(lxdClientKey)
			c, err := lxd.ConnectLXD(stadium, &lxd.ConnectionArgs{
				TLSClientCert:      certString,
				TLSClientKey:       keyString,
				UserAgent:          "myshoes-pool-agent",
				InsecureSkipVerify: inSecure,
			})
			if err != nil {
				return errors.Wrap(err, "failed to connect lxd")
			}
			agent := &Agent{
				ImageAlias:     conf.ImageAlias,
				InstanceSource: source,

				ResourceTypes: conf.ResouceTypes,
				Client:        c,

				CheckInterval:         checkInterval,
				ConcurrentCreateLimit: concurrentCreateLimit,
				WaitIdleTime:          waitIdleTime,
				ZombieAllowTime:       zombieAllowTime,
			}
			agents = append(agents, agent)
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		eg, ctx := errgroup.WithContext(ctx)

		for _, agent := range agents {
			func(agent *Agent) {
				eg.Go(func() error {
					return agent.Run(ctx)
				})
			}(agent)
		}
		return eg.Wait()
	}}
