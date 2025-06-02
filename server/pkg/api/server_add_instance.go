package api

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"

	"github.com/whywaita/myshoes/pkg/datastore"
	"github.com/whywaita/myshoes/pkg/runner"
	pb "github.com/whywaita/shoes-lxd-multi/proto.go"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AddInstance add instance to LXD server
func (s *ShoesLXDMultiServer) AddInstance(ctx context.Context, req *pb.AddInstanceRequest) (*pb.AddInstanceResponse, error) {
	slog.Info("AddInstance", "req", req)
	l := slog.With("method", "AddInstance")
	if _, err := runner.ToUUID(req.RunnerName); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse request name: %+v", err)
	}
	l = l.With("runnerName", req.RunnerName)

	targetLXDHosts, err := s.validateTargetHosts(ctx, req.TargetHosts, l)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to validate target hosts: %+v", err)
	}

	host, instanceName, err := s.addInstancePoolMode(ctx, targetLXDHosts, req, l)
	if err != nil {
		return nil, err
	}
	i, _, err := host.Client.GetInstance(instanceName) // this line needs to assurance, So I will get instance information again from API
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve instance information: %+v", err)
	}

	l.Info("Success AddInstance", "host", host.HostConfig.LxdHost)

	return &pb.AddInstanceResponse{
		CloudId:      i.Name,
		ShoesType:    "lxd",
		IpAddress:    "",
		ResourceType: req.ResourceType,
	}, nil
}

func (s *ShoesLXDMultiServer) addInstancePoolMode(ctx context.Context, targets []*lxdclient.LXDHost, req *pb.AddInstanceRequest, _l *slog.Logger) (*lxdclient.LXDHost, string, error) {
	host, instanceName, found := findInstanceByJob(ctx, targets, req.RunnerName, _l)
	if !found {
		resourceTypeName := datastore.UnmarshalResourceTypePb(req.ResourceType).String()
		retried := 0
		for {
			var err error
			host, instanceName, err = allocatePooledInstance(ctx, targets, resourceTypeName, s.parseImageAliasMap(req.OsVersion), s.overCommitPercent, req.RunnerName, _l)
			if err != nil {
				if retried < 10 {
					retried++
					_l.Info("AddInstance failed allocating instance", "retrying", retried, "err", err.Error())
					time.Sleep(1 * time.Second)
					continue
				} else {
					return nil, "", status.Errorf(codes.Internal, "can not allocate instance")
				}
			}
			break
		}
	}
	l := _l.With("host", host.HostConfig.LxdHost, "instance", instanceName)
	l.Info("AddInstance for pool mode", "runnerName", instanceName)
	client := host.Client

	err := unfreezeInstance(client, instanceName)
	if err != nil {
		l.Error("failed to unfreeze instance, will delete...")
		if err := recoverInvalidInstance(client, instanceName); err != nil {
			l.Error("failed to delete invalid instance", "error", err.Error())
		}
		return nil, "", status.Errorf(codes.Internal, "unfreeze instance: %+v", err)
	}

	scriptFilename := fmt.Sprintf("/tmp/myshoes_setup_script.%d", rand.Int())
	err = client.CreateInstanceFile(instanceName, scriptFilename, lxd.InstanceFileArgs{
		Content:   strings.NewReader(req.SetupScript),
		Mode:      0744,
		Type:      "file",
		WriteMode: "overwrite",
	})
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to copy setup script: %+v", err)
	}
	op, err := client.ExecInstance(instanceName, api.InstanceExecPost{
		Command: []string{
			"systemd-run",
			"--unit", "myshoes-setup",
			"--property", "After=multi-user.target",
			"--property", "StandardOutput=journal+console",
			"--property", fmt.Sprintf("ExecStartPre=/usr/bin/hostnamectl set-hostname %s", req.RunnerName),
			"--property", fmt.Sprintf("ExecStartPre=/bin/sh -c 'echo 127.0.1.1 %s >> /etc/hosts'", req.RunnerName),
			scriptFilename,
		},
	}, nil)
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to execute setup script: %+v", err)
	}
	if err := op.Wait(); err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to wait executing setup script: %+v", err)
	}

	return host, instanceName, nil
}

// func (s *ShoesLXDMultiServer) setLXDStatusCache(
// 	reqInstance *api.InstancesPost,
// 	newInstance api.Instance,
// 	scheduledHost *lxdclient.LXDHost,
// ) error {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	cpu, err := strconv.ParseUint(reqInstance.InstancePut.Config["limits.cpu"], 10, 64)
// 	if err != nil {
// 		return fmt.Errorf("failde to parse limits.cpu: %w", err)
// 	}

// 	memory, err := units.FromHumanSize(reqInstance.InstancePut.Config["limits.memory"])
// 	if err != nil {
// 		return fmt.Errorf("failde to parse limits.memory: %w", err)
// 	}

// 	cache, err := lxdclient.GetStatusCache(scheduledHost.HostConfig.LxdHost)
// 	if err != nil {
// 		return fmt.Errorf("failed to get status cache: %w", err)
// 	}
// 	cache.Resource.CPUUsed += cpu
// 	cache.Resource.MemoryUsed += uint64(memory)
// 	cache.Resource.Instances = append(cache.Resource.Instances, newInstance)
// 	if err := lxdclient.SetStatusCache(scheduledHost.HostConfig.LxdHost, cache); err != nil {
// 		return fmt.Errorf("failed to set status cache: %s", err)
// 	}
// 	return nil
// }

// func (s *ShoesLXDMultiServer) getInstanceConfig(setupScript string, rt myshoespb.ResourceType) map[string]string {
// 	rawLXCConfig := `lxc.apparmor.profile = unconfined
// lxc.cgroup.devices.allow = a
// lxc.cap.drop=`

// 	instanceConfig := map[string]string{
// 		"security.nesting":    "true",
// 		"security.privileged": "true",
// 		"raw.lxc":             rawLXCConfig,
// 		"user.user-data":      setupScript,
// 	}

// 	if mapping, ok := s.resourceMapping[rt]; ok {
// 		instanceConfig["limits.cpu"] = strconv.Itoa(mapping.CPUCore)
// 		instanceConfig["limits.memory"] = mapping.Memory
// 	}

// 	return instanceConfig
// }

// func (s *ShoesLXDMultiServer) getInstanceDevices() map[string]map[string]string {
// 	instanceDevices := map[string]map[string]string{
// 		"kmsg": {
// 			"path":   "/dev/kmsg",
// 			"source": "/dev/kmsg",
// 			"type":   "unix-char",
// 		},
// 		"kvm": {
// 			"path":   "/dev/kvm",
// 			"source": "/dev/kvm",
// 			"type":   "unix-char",
// 		},
// 	}

// 	return instanceDevices
// }

// type targetHost struct {
// 	host     *lxdclient.LXDHost
// 	resource lxdclient.Resource

// 	percentOverCommit uint64
// }

// func (s *ShoesLXDMultiServer) scheduleHost(ctx context.Context, targetLXDHosts []*lxdclient.LXDHost) (*lxdclient.LXDHost, error) {
// 	targets, err := getResources(ctx, targetLXDHosts)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get resources: %w", err)
// 	}

// 	target, err := schedule(targets, s.overCommitPercent)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to schedule: %w", err)
// 	}
// 	return target.host, nil
// }

// func getResources(ctx context.Context, targetLXDHosts []*lxdclient.LXDHost) ([]*targetHost, error) {
// 	var targets []*targetHost

// 	eg := errgroup.Group{}
// 	mu := sync.Mutex{}

// 	for _, t := range targetLXDHosts {
// 		t := t
// 		eg.Go(func() error {
// 			l := slog.With("host", t.HostConfig.LxdHost)
// 			resources, err := lxdclient.GetResource(ctx, t.HostConfig, l)
// 			if err != nil {
// 				l.Warn("failed to get resource", "err", err.Error())
// 				return nil
// 			}

// 			mu.Lock()
// 			targets = append(targets, &targetHost{
// 				host:              t,
// 				resource:          *resources,
// 				percentOverCommit: lxdclient.GetCPUOverCommitPercent(*resources),
// 			})
// 			mu.Unlock()

// 			return nil
// 		})
// 	}

// 	if err := eg.Wait(); err != nil {
// 		return nil, fmt.Errorf("failed to get resources: %w", err)
// 	}

// 	return targets, nil
// }

// var (
// 	// ErrNoValidHost is not valid host in targets
// 	ErrNoValidHost = fmt.Errorf("no valid host in targets")
// )

// func schedule(targets []*targetHost, limitOverCommit uint64) (*targetHost, error) {
// 	var schedulableTargets []*targetHost
// 	for _, target := range targets {
// 		l := slog.With("host", target.host.HostConfig.LxdHost)
// 		if target.percentOverCommit < limitOverCommit {
// 			schedulableTargets = append(schedulableTargets, target)
// 		} else {
// 			l.Info("is percentage of over-commit is high. ignore", "now", target.percentOverCommit, "limit", limitOverCommit)
// 		}
// 	}
// 	if len(schedulableTargets) == 0 {
// 		return nil, ErrNoValidHost
// 	}

//		return schedulableTargets[rand.Intn(len(schedulableTargets))], nil
//	}
//
// // ParseAlias parse user input
//
//	func ParseAlias(input string) (*api.InstanceSource, error) {
//		if strings.EqualFold(input, "") {
//			// default value is ubuntu:bionic
//			return &api.InstanceSource{
//				Type: "image",
//				Properties: map[string]string{
//					"os":      "ubuntu",
//					"release": "bionic",
//				},
//			}, nil
//		}
//
//		if strings.HasPrefix(input, "http") {
//			// https://<FQDN or IP>:8443/<alias>
//			u, err := url.Parse(input)
//			if err != nil {
//				return nil, fmt.Errorf("failed to parse alias: %w", err)
//			}
//
//			urlImageServer := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
//			alias := strings.TrimPrefix(u.Path, "/")
//
//			return &api.InstanceSource{
//				Type:   "image",
//				Mode:   "pull",
//				Server: urlImageServer,
//				Alias:  alias,
//			}, nil
//		}
//
//		return &api.InstanceSource{
//			Type:  "image",
//			Alias: input,
//		}, nil
//	}
func (s *ShoesLXDMultiServer) parseImageAliasMap(version string) string {
	if version == "" {
		return s.parseImageAliasMap("default")
	}
	if alias, ok := s.imageAliasMap[version]; ok {
		if _, ok := s.imageAliasMap[alias]; ok {
			return s.parseImageAliasMap(alias)
		}
		return alias
	}
	return ""
}
