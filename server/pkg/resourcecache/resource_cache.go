package resourcecache

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

// RunLXDResourceCacheTicker is run ticker for set lxd resource cache
func RunLXDResourceCacheTicker(ctx context.Context, hcs []config.HostConfig, periodSec int64) {
	ticker := time.NewTicker(time.Duration(periodSec) * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		if err := reloadLXDHostResourceCache(ctx, hcs); err != nil {
			log.Fatal("failed to set lxd resource cache", "err", err.Error())
		}
	}
}

func reloadLXDHostResourceCache(ctx context.Context, hcs []config.HostConfig) error {
	l := slog.With("method", "reloadLXDHostResourceCache")
	hosts, _, err := lxdclient.ConnectLXDs(hcs)
	if err != nil {
		return fmt.Errorf("failed to connect LXD hosts: %s", err)
	}

	for _, host := range hosts {
		_l := l.With("host", host.HostConfig.LxdHost)
		if err := setLXDHostResourceCache(ctx, &host, _l); err != nil {
			_l.Warn("failed to set lxd host resource cache", "err", err.Error())
			continue
		}
	}
	return nil
}

func setLXDHostResourceCache(ctx context.Context, host *lxdclient.LXDHost, logger *slog.Logger) error {
	resources, _, _, err := lxdclient.GetResourceFromLXDWithClient(ctx, host.Client, host.HostConfig.LxdHost, logger)
	if err != nil {
		return fmt.Errorf("failed to get resource from lxd: %s", err)
	}

	s := lxdclient.LXDStatus{
		Resource:   *resources,
		HostConfig: host.HostConfig,
	}
	if err := lxdclient.SetStatusCache(host.HostConfig.LxdHost, s); err != nil {
		return fmt.Errorf("failed to set status cache: %s", err)
	}
	return nil
}
