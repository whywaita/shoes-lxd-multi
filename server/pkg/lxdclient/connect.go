package lxdclient

import (
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
	Client     lxd.InstanceServer
	HostConfig config.HostConfig
}

// ErrLXDHost is error for LXD host
type ErrLXDHost struct {
	HostConfig config.HostConfig
	Err        error
}

// ConnectLXDs connect LXDs
func ConnectLXDs(hostConfigs []config.HostConfig) ([]LXDHost, []ErrLXDHost, error) {
	var targetLXDHosts []LXDHost
	var errLXDHosts []ErrLXDHost

	eg := errgroup.Group{}
	mu := sync.Mutex{}

	for _, hc := range hostConfigs {
		hc := hc
		l := slog.With("host", hc.LxdHost)
		eg.Go(func() error {
			conn, err := ConnectLXDWithTimeout(hc.LxdHost, hc.LxdClientCert, hc.LxdClientKey)
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
				Client:     *conn,
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
func ConnectLXDWithTimeout(host, clientCert, clientKey string) (*lxd.InstanceServer, error) {
	client, ok := loadConnectedInstance(host)
	if ok {
		return client, nil
	}

	type resultConnectLXD struct {
		client lxd.InstanceServer
		err    error
	}

	resultCh := make(chan resultConnectLXD, 1)
	go func() {
		args := &lxd.ConnectionArgs{
			UserAgent:          "shoes-lxd",
			TLSClientCert:      clientCert,
			TLSClientKey:       clientKey,
			InsecureSkipVerify: true,
		}

		client, err := lxd.ConnectLXD(host, args)
		resultCh <- resultConnectLXD{
			client: client,
			err:    err,
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("failed to connect LXD: %w", result.err)
		}

		storeConnectedInstance(host, result.client)

		return &result.client, nil
	case <-time.After(2 * time.Second):
		// lxd.ConnectLXD() is not support context.Context yet. need to refactor it after support context.Context.
		return nil, ErrTimeoutConnectLXD
	}
}

// connectedInstances is map of connected LXD instances
// key: lxdhost value: LXDHost
var connectedInstances sync.Map

// storeConnectedInstance store connected instance
func storeConnectedInstance(host string, lh lxd.InstanceServer) {
	connectedInstances.Store(host, lh)
}

// loadConnectedInstance load connected instance
func loadConnectedInstance(host string) (*lxd.InstanceServer, bool) {
	v, ok := connectedInstances.Load(host)
	if !ok {
		return nil, false
	}
	i := v.(lxd.InstanceServer)

	return &i, true
}

// deleteConnectedInstance delete connected instance
func deleteConnectedInstance(host string) {
	connectedInstances.Delete(host)
}
