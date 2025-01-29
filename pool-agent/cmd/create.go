package cmd

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/lxd/shared/api"
)

func (a *Agent) createInstance(iname, rtName string, rt resourceType, version string, l *slog.Logger) error {
	l.Info("Creating instance")
	op, err := a.Client.CreateInstance(api.InstancesPost{
		Name: iname,
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
				configKeyImageAlias:   a.Config[version].ImageAlias,
				configKeyResourceType: rtName,
			},
			Devices: map[string]map[string]string{
				"kmsg": {
					"path":   "/dev/kmsg",
					"source": "/dev/kmsg",
					"type":   "unix-char",
				},
				"kvm": {
					"path":   "/dev/kvm",
					"source": "/dev/kvm",
					"type":   "unix-char",
				},
			},
		},
		Source: a.Config[version].InstanceSource,
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("create operation: %w", err)
	}

	l.Info("Starting instance")
	op, err = a.Client.UpdateInstanceState(iname, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("start operation: %w", err)
	}

	l.Info("Waiting system bus in instance")
	op, err = a.Client.ExecInstance(iname, api.InstanceExecPost{
		Command: []string{"bash", "-c", "until test -e /var/run/dbus/system_bus_socket; do sleep 0.5; done"},
	}, nil)
	if err != nil {
		return fmt.Errorf("wait system bus: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait system bus operation: %w", err)
	}

	l.Info("Waiting system running for instance")
	op, err = a.Client.ExecInstance(iname, api.InstanceExecPost{
		Command: []string{"systemctl", "is-system-running", "--wait"},
	}, nil)
	if err != nil {
		return fmt.Errorf("wait system running: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait system running operation: %w", err)
	}

	l.Info("Disabling systemd service watchdogs in instance")
	op, err = a.Client.ExecInstance(iname, api.InstanceExecPost{
		Command: []string{"systemctl", "service-watchdogs", "no"},
	}, nil)
	if err != nil {
		return fmt.Errorf("disable systemd service watchdogs: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("disable systemd service watchdogs operation: %w", err)
	}

	l.Info("Waiting for instance idle")
	time.Sleep(a.WaitIdleTime)

	l.Info("Freezing instance")
	op, err = a.Client.UpdateInstanceState(iname, api.InstanceStatePut{
		Action:  "freeze",
		Timeout: -1,
	}, "")
	if err != nil {
		return fmt.Errorf("freeze: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("freeze operation: %w", err)
	}

	l.Info("Created instance successfully")
	return nil
}
