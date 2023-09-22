package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/lxd/shared/api"
)

func (a *Agent) createInstance(name string, rt ResourceType) error {
	log.Printf("Creating instance %q...", name)
	op, err := a.Client.CreateInstance(api.InstancesPost{
		Name: name,
		InstancePut: api.InstancePut{
			Config: map[string]string{
				"limits.cpu":          strconv.Itoa(rt.CPUCore),
				"limits.memory":       rt.Memory,
				"security.nesting":    "true",
				"security.privileged": "true",
				"raw.lxc": strings.Join([]string{
					"lxc.apparmor.profile = unconfined",
					"lxc.cgroup.devices.allow = a",
					"lxc.cap.drop=",
				}, "\n"),
				configKeyImageAlias:   a.ImageAlias,
				configKeyResourceType: rt.Name,
			},
			Devices: map[string]map[string]string{
				"kmsg": {
					"path":   "/dev/kmsg",
					"source": "/dev/kmsg",
					"type":   "unix-char",
				},
			},
		},
		Source: a.InstanceSource,
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("create operation: %w", err)
	}

	log.Printf("Starting instance %q...", name)
	op, err = a.Client.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("start operation: %w", err)
	}

	log.Printf("Waiting system bus in instance %q...", name)
	op, err = a.Client.ExecInstance(name, api.InstanceExecPost{
		Command: []string{"bash", "-c", "until test -e /var/run/dbus/system_bus_socket; do sleep 0.5; done"},
	}, nil)
	if err != nil {
		return fmt.Errorf("wait system bus: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait system bus operation: %w", err)
	}

	log.Printf("Waiting system running for instance %q...", name)
	op, err = a.Client.ExecInstance(name, api.InstanceExecPost{
		Command: []string{"systemctl", "is-system-running", "--wait"},
	}, nil)
	if err != nil {
		return fmt.Errorf("wait system running: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait system running operation: %w", err)
	}

	log.Printf("Disabling systemd service watchdogs in instance %q...", name)
	op, err = a.Client.ExecInstance(name, api.InstanceExecPost{
		Command: []string{"systemctl", "service-watchdogs", "no"},
	}, nil)
	if err != nil {
		return fmt.Errorf("disable systemd service watchdogs: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("disable systemd service watchdogs operation: %w", err)
	}

	log.Printf("Waiting for instance %q idle...", name)
	time.Sleep(a.WaitIdleTime)

	log.Printf("Freezing instance %q...", name)
	op, err = a.Client.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "freeze",
		Timeout: -1,
	}, "")
	if err != nil {
		return fmt.Errorf("freeze: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("freeze operation: %w", err)
	}

	log.Printf("Created instance %q successfully", name)
	return nil
}
