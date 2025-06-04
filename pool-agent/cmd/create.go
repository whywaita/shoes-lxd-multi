package cmd

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/lxd/shared/api"
)

func (a *Agent) createInstances(createMap map[string]map[string]int) {
	for imageKey, createCount := range createMap {
		l := slog.With(slog.String("imageKey", imageKey))
		_, ok := a.Image[imageKey]
		if !ok {
			l.Error("failed to get image")
			continue
		}
		for rtName, count := range createCount {
			ll := l.With(slog.String("flavor", rtName))
			rt, ok := a.ResourceTypesMap[rtName]
			if !ok {
				ll.Error("failed to get resource type")
				continue
			}
			for range count {
				func() {
					iname, err := generateInstanceName()
					if err != nil {
						ll.Error("failed to generate instance name", slog.String("err", err.Error()))
						return
					}
					lll := ll.With(slog.String("instance", iname))
					a.Image[imageKey].Status.CreatingInstances[rtName][iname] = struct{}{}
					defer delete(a.Image[imageKey].Status.CreatingInstances[rtName], iname)
					lll.Info("Creating instance")
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
								configKeyImageAlias:   a.Image[imageKey].Config.ImageAlias,
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
						Source: a.Image[imageKey].InstanceSource,
					})
					if err != nil {
						lll.Error("failed to create creating operation", slog.String("err", err.Error()))
						return
					}
					if err := op.Wait(); err != nil {
						lll.Error("failed to create operation", slog.String("err", err.Error()))
						return
					}
					lll.Info("Starting instance")
					op, err = a.Client.UpdateInstanceState(iname, api.InstanceStatePut{
						Action:  "start",
						Timeout: -1,
					}, "")
					if err != nil {
						lll.Error("failed to create starting operation", slog.String("err", err.Error()))
					}
					if err := op.Wait(); err != nil {
						lll.Error("failed to start operation", slog.String("err", err.Error()))
						return
					}

					lll.Info("Waiting system bus in instance")
					op, err = a.Client.ExecInstance(iname, api.InstanceExecPost{
						Command: []string{"bash", "-c", "until test -e /var/run/dbus/system_bus_socket; do sleep 0.5; done"},
					}, nil)
					if err != nil {
						lll.Error("failed to create waiting system bus operation", slog.String("err", err.Error()))
						return
					}
					if err := op.Wait(); err != nil {
						lll.Error("failed to wait system bus operation", slog.String("err", err.Error()))
						return
					}

					lll.Info("Waiting system running for instance")
					op, err = a.Client.ExecInstance(iname, api.InstanceExecPost{
						Command: []string{"systemctl", "is-system-running", "--wait"},
					}, nil)
					if err != nil {
						lll.Error("failed to create waiting system running operation", slog.String("err", err.Error()))
						return
					}
					if err := op.Wait(); err != nil {
						lll.Error("failed to wait system running operation", slog.String("err", err.Error()))
						return
					}

					lll.Info("Disabling systemd service watchdogs in instance")
					op, err = a.Client.ExecInstance(iname, api.InstanceExecPost{
						Command: []string{"systemctl", "service-watchdogs", "no"},
					}, nil)
					if err != nil {
						lll.Error("failed to create disable systemd service watchdogs operation", slog.String("err", err.Error()))
						return
					}
					if err := op.Wait(); err != nil {
						lll.Error("failed to disable systemd service watchdogs operation", slog.String("err", err.Error()))
						return
					}

					lll.Info("Waiting for instance idle")
					time.Sleep(a.WaitIdleTime)

					lll.Info("Freezing instance")
					op, err = a.Client.UpdateInstanceState(iname, api.InstanceStatePut{
						Action:  "freeze",
						Timeout: -1,
					}, "")
					if err != nil {
						lll.Error("failed to create freeze operation", slog.String("err", err.Error()))
						return
					}
					if err := op.Wait(); err != nil {
						lll.Error("failed to freeze operation", slog.String("err", err.Error()))
						return
					}

					lll.Info("Created instance successfully")

				}()
			}
		}
	}
}
