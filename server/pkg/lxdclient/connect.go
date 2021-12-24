package lxdclient

import (
	"errors"
	"fmt"
	"log"
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

// ConnectLXDs connect LXDs
func ConnectLXDs(hostConfigs []config.HostConfig) ([]LXDHost, error) {
	var targetLXDHosts []LXDHost

	eg := errgroup.Group{}
	mu := sync.Mutex{}

	for _, hc := range hostConfigs {
		hc := hc
		eg.Go(func() error {
			conn, err := ConnectLXDWithTimeout(hc.LxdHost, hc.LxdClientCert, hc.LxdClientKey)
			if err != nil && !errors.Is(err, ErrTimeoutConnectLXD) {
				log.Printf("failed to connect LXD with timeout (host: %s): %+v\n", err, hc.LxdHost)
				return nil
			} else if errors.Is(err, ErrTimeoutConnectLXD) {
				log.Printf("failed to connect LXD, So ignore host (host: %s)\n", hc.LxdHost)
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
		return nil, fmt.Errorf("failed to connect LXD servers: %w", err)
	}

	return targetLXDHosts, nil
}

var (
	// ErrTimeoutConnectLXD is error message for timeout of ConnectLXD
	ErrTimeoutConnectLXD = fmt.Errorf("timeout of ConnectLXD")
)

// ConnectLXDWithTimeout connect LXD API with timeout
// lxd.ConnectLXD is not support context yet. So ConnectLXDWithTimeout occurred goroutine leak if timeout.
func ConnectLXDWithTimeout(host, clientCert, clientKey string) (*lxd.InstanceServer, error) {
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
		return &result.client, nil
	case <-time.After(2 * time.Second):
		// This block occurred goroutine leak when timeout. But shoes-lxd is short range. maybe safety.
		// lxd.ConnectLXD() is not support context.Context yet. need to refactor it after support context.Context.
		return nil, ErrTimeoutConnectLXD
	}
}
