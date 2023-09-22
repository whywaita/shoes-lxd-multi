package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	lxd "github.com/lxc/lxd/client"
)

func main() {
	resourceTypes, err := LoadResourceTypes()
	if err != nil {
		log.Fatal(err)
	}

	imageAlias, source, err := LoadImageAlias()
	if err != nil {
		log.Fatal(err)
	}

	checkInterval, concurrentCreateLimit, waitIdleTime, zombieAllowTime, err := LoadParams()
	if err != nil {
		log.Fatal(err)
	}

	c, err := lxd.ConnectLXDUnix("", nil)
	if err != nil {
		log.Fatalf("failed to connect lxd: %+v", err)
	}

	agent := &Agent{
		ImageAlias:     imageAlias,
		InstanceSource: source,

		ResourceTypes: resourceTypes,
		Client:        c,

		CheckInterval:         checkInterval,
		ConcurrentCreateLimit: concurrentCreateLimit,
		WaitIdleTime:          waitIdleTime,
		ZombieAllowTime:       zombieAllowTime,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agent.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}
