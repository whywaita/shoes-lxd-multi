package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

var (
	// ErrInstanceIsNotFound is error message for instance is not found
	ErrInstanceIsNotFound = fmt.Errorf("instance is not found")
)

// isExistInstance search created instance in same name
func (s *ShoesLXDMultiServer) isExistInstance(targetLXDHosts []*lxdclient.LXDHost, instanceName string, logger *slog.Logger) (*lxdclient.LXDHost, error) {
	eg := errgroup.Group{}
	var foundHost *lxdclient.LXDHost
	foundHost = nil

	for _, host := range targetLXDHosts {
		func(host *lxdclient.LXDHost) {
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

				foundHost = host
				return nil
			})
		}(host)
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

func isExistInstanceWithTimeout(targetLXDHost *lxdclient.LXDHost, instanceName string) error {
	cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client := targetLXDHost.Client
	c := client.WithContext(cctx)

	targetLXDHost.APICallMutex.Lock()
	defer targetLXDHost.APICallMutex.Unlock()

	_, _, err := c.GetInstance(instanceName)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "Instance not found"):
			return ErrInstanceIsNotFound
		case errors.Is(err, context.DeadlineExceeded):
			return ErrTimeoutGetInstance
		}
		return fmt.Errorf("failed to found instance: %w", err)
	}

	// Reset context (remove timeout)
	client.WithContext(context.Background())

	return nil
}
