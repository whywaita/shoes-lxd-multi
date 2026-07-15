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
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
)

var (
	// ErrInstanceIsNotFound is error message for instance is not found
	ErrInstanceIsNotFound = fmt.Errorf("instance is not found")
)

// isExistInstance search created instance in same name.
// It first tries to locate the host from the in-memory resource cache and confirms the
// result with a single API call, instead of querying every target host. When the cache
// does not know the instance (cache miss or a stale entry), it falls back to querying all
// target hosts.
func (s *ShoesLXDMultiServer) isExistInstance(targetLXDHosts []*lxdclient.LXDHost, instanceName string, logger *slog.Logger) (*lxdclient.LXDHost, error) {
	if host := findInstanceHostFromCache(targetLXDHosts, instanceName); host != nil {
		// The cache can be stale, so confirm the instance really exists on the host with
		// a single API call before trusting it.
		if err := isExistInstanceWithTimeout(host, instanceName); err == nil {
			return host, nil
		}
		logger.With("host", host.HostConfig.LxdHost).Debug("instance found in cache but not confirmed on host, fall back to querying all hosts", "instanceName", instanceName)
	}

	return isExistInstanceInHosts(targetLXDHosts, instanceName, logger)
}

// findInstanceHostFromCache returns the host that has the instance according to the
// in-memory resource cache. It returns nil when no cached host has the instance, in which
// case the caller should fall back to querying the hosts directly.
func findInstanceHostFromCache(targetLXDHosts []*lxdclient.LXDHost, instanceName string) *lxdclient.LXDHost {
	for _, host := range targetLXDHosts {
		status, err := lxdclient.GetStatusCache(host.HostConfig.LxdHost)
		if err != nil {
			// cache miss for this host, try the next one.
			continue
		}
		for _, instance := range status.Resource.Instances {
			if instance.Name == instanceName {
				return host
			}
		}
	}
	return nil
}

// isExistInstanceInHosts searches the instance by querying all target hosts.
func isExistInstanceInHosts(targetLXDHosts []*lxdclient.LXDHost, instanceName string, logger *slog.Logger) (*lxdclient.LXDHost, error) {
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

	targetLXDHost.APICallMutex.Lock()
	defer targetLXDHost.APICallMutex.Unlock()

	c := targetLXDHost.Client.WithContext(cctx)
	defer targetLXDHost.Client.WithContext(context.Background())

	timer := metric.NewLXDAPITimer(targetLXDHost.HostConfig.LxdHost, "GetInstance")
	_, _, err := c.GetInstance(instanceName)
	timer.ObserveDuration(err)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "Instance not found"):
			return ErrInstanceIsNotFound
		case errors.Is(err, context.DeadlineExceeded):
			return ErrTimeoutGetInstance
		}
		return fmt.Errorf("failed to found instance: %w", err)
	}

	return nil
}
