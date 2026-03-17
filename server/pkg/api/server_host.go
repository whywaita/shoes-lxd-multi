package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
)

var (
	// ErrInstanceIsNotFound is error message for instance is not found
	ErrInstanceIsNotFound = fmt.Errorf("instance is not found")
)

// isExistInstance search created instance in same name
func (s *ShoesLXDMultiServer) isExistInstance(targetLXDHosts []*lxdclient.LXDHost, instanceName string) (*lxdclient.LXDHost, error) {
	wg := sync.WaitGroup{}
	var foundHost *lxdclient.LXDHost
	foundHost = nil

	for _, host := range targetLXDHosts {
		wg.Add(1)
		go func(host *lxdclient.LXDHost) {
			defer wg.Done()
			err := isExistInstanceWithTimeout(host, instanceName)
			if err != nil {
				// If instance is not found or timeout, ignore and continue to search other host
				return
			}

			foundHost = host
		}(host)
	}

	wg.Wait()
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
