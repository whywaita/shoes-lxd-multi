package lxdclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	lxd "github.com/lxc/lxd/client"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
)

// LXDHost is client of LXD and host config
type LXDHost struct {
	Client     *lxd.ProtocolLXD
	HostConfig config.HostConfig
}

// ErrLXDHost is error for LXD host
type ErrLXDHost struct {
	HostConfig config.HostConfig
	Err        error
}

// ConnectLXDs connect LXDs
func ConnectLXDs(ctx context.Context, hostConfigs []config.HostConfig) ([]LXDHost, []ErrLXDHost, error) {
	var targetLXDHosts []LXDHost
	var errLXDHosts []ErrLXDHost

	eg := errgroup.Group{}
	mu := sync.Mutex{}

	for _, hc := range hostConfigs {
		hc := hc
		l := slog.With("host", hc.LxdHost)
		eg.Go(func() error {
			conn, err := ConnectLXDWithTimeout(ctx, hc.LxdHost, hc.LxdClientCert, hc.LxdClientKey)
			if err != nil && !errors.Is(err, ErrTimeoutConnectLXD) {
				l.Warn("failed to connect LXD with timeout (not ErrTimeoutConnectLXD)", "err", err.Error())
				errLXDHosts = append(errLXDHosts, ErrLXDHost{
					HostConfig: hc,
					Err:        err,
				})
				return nil
			} else if errors.Is(err, ErrTimeoutConnectLXD) {
				l.Warn("failed to connect LXD, So ignore host")
				errLXDHosts = append(errLXDHosts, ErrLXDHost{
					HostConfig: hc,
					Err:        err,
				})
				return nil
			}

			mu.Lock()
			targetLXDHosts = append(targetLXDHosts, LXDHost{
				Client:     conn,
				HostConfig: hc,
			})
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, nil, fmt.Errorf("failed to connect LXD servers: %w", err)
	}

	// errHost needs to delete connected instance
	for _, errHost := range errLXDHosts {
		l := slog.With("host", errHost.HostConfig.LxdHost)
		l.Warn("failed to connect LXD", "err", errHost.Err.Error())

		deleteConnectedInstance(errHost.HostConfig.LxdHost)
	}

	return targetLXDHosts, errLXDHosts, nil
}

var (
	// ErrTimeoutConnectLXD is error message for timeout of ConnectLXD
	ErrTimeoutConnectLXD = fmt.Errorf("timeout of ConnectLXD")
)

// ConnectLXDWithTimeout connect LXD API with timeout
// lxd.ConnectLXD is not support context yet. So ConnectLXDWithTimeout occurred goroutine leak if timeout.
func ConnectLXDWithTimeout(ctx context.Context, host, clientCert, clientKey string) (*lxd.ProtocolLXD, error) {
	if client, ok := loadConnectedInstance(host); ok {
		return client, nil
	}

	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	args := &lxd.ConnectionArgs{
		UserAgent:          "shoes-lxd",
		TLSClientCert:      clientCert,
		TLSClientKey:       clientKey,
		InsecureSkipVerify: true,
	}
	client, err := lxd.ConnectLXDWithContext(cctx, host, args)
	if err != nil {
		// if timeout, return ErrTimeoutConnectLXD
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrTimeoutConnectLXD
		}

		return nil, fmt.Errorf("failed to connect LXD: %w", err)
	}
	c, ok := client.(*lxd.ProtocolLXD)
	if !ok {
		return nil, fmt.Errorf("failed to cast client to *lxd.ProtocolLXD")
	}

	// Reset context (remove timeout)
	c.WithContext(context.Background())

	storeConnectedInstance(host, c)
	return c, nil

}

// connectedInstances is map of connected LXD instances
// key: lxdhost value: LXDHost
var connectedInstances sync.Map

// storeConnectedInstance store connected instance
func storeConnectedInstance(host string, lh *lxd.ProtocolLXD) {
	connectedInstances.Store(host, lh)
}

// loadConnectedInstance load connected instance
func loadConnectedInstance(host string) (*lxd.ProtocolLXD, bool) {
	v, ok := connectedInstances.Load(host)
	if !ok {
		return nil, false
	}
	i := v.(*lxd.ProtocolLXD)

	return i, true
}

// deleteConnectedInstance delete connected instance
func deleteConnectedInstance(host string) {
	connectedInstances.Delete(host)
}
