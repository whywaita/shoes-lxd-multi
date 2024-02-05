package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

var (
	// ErrInstanceIsNotFound is error message for instance is not found
	ErrInstanceIsNotFound = fmt.Errorf("instance is not found")
)

// isExistInstance search created instance in same name
func (s *ShoesLXDMultiServer) isExistInstance(targetLXDHosts []lxdclient.LXDHost, instanceName string, logger *slog.Logger) (*lxdclient.LXDHost, error) {
	eg := errgroup.Group{}
	var foundHost *lxdclient.LXDHost
	foundHost = nil

	for _, host := range targetLXDHosts {
		host := host
		eg.Go(func() error {
			l := logger.With("host", host.HostConfig.LxdHost)
			err := isExistInstanceWithTimeout(host, instanceName)
			if err != nil {
				switch {
				case errors.Is(err, ErrInstanceIsNotFound):
					// not found instance, It's a many case in this. so ignore this host
					return nil
				case errors.Is(err, ErrTimeoutGetInstance):
					l.Warn("failed to get instance (reach timeout), So ignore host", "err", err.Error())
					return nil
				default:
					l.Warn("failed to get instance", "err", err.Error())
					return nil
				}
			}

			foundHost = &host
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	if foundHost == nil {
		return nil, ErrInstanceIsNotFound
	}

	return foundHost, nil
}

var (
	// ErrTimeoutGetInstance is error message for timeout of GetInstance
	ErrTimeoutGetInstance = fmt.Errorf("timeout of GetInstance")
)

func isExistInstanceWithTimeout(targetLXDHost lxdclient.LXDHost, instanceName string) error {
	errCh := make(chan error, 1)
	go func() {
		instance, _, err := targetLXDHost.Client.GetInstance(instanceName)
		if instance.StatusCode == http.StatusNotFound {
			errCh <- ErrInstanceIsNotFound
			return
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("failed to found instance: %w", err)
		}

		// non-error, found
		return nil
	case <-time.After(2 * time.Second):
		// lxd.GetInstance() is not support context.Context yet. need to refactor it after support context.Context.
		return ErrTimeoutGetInstance
	}
}
