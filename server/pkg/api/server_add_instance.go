package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	myshoespb "github.com/whywaita/myshoes/api/proto.go"

	"golang.org/x/sync/errgroup"

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
	log.Printf("AddInstance req: %+v\n", req)
	if _, err := runner.ToUUID(req.RunnerName); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse request name: %+v", err)
	}

	targetLXDHosts, err := s.validateTargetHosts(req.TargetHosts)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to validate target hosts: %+v", err)
	}

	var host *lxdclient.LXDHost
	var instanceName string

	if s.poolMode {
		host, instanceName, err = s.addInstancePoolMode(targetLXDHosts, req)
		if err != nil {
			return nil, err
		}
	} else {
		host, instanceName, err = s.addInstanceCreateMode(targetLXDHosts, req)
		if err != nil {
			return nil, err
		}
	}

	i, _, err := host.Client.GetInstance(instanceName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve instance information: %+v", err)
	}

	return &pb.AddInstanceResponse{
		CloudId:      i.Name,
		ShoesType:    "lxd",
		IpAddress:    "",
		ResourceType: req.ResourceType,
	}, nil
}

func (s *ShoesLXDMultiServer) addInstancePoolMode(targets []lxdclient.LXDHost, req *pb.AddInstanceRequest) (*lxdclient.LXDHost, string, error) {
	host, instanceName, found := findInstanceByJob(targets, req.RunnerName)
	if !found {
		resourceTypeName := datastore.UnmarshalResourceTypePb(req.ResourceType).String()
		retried := 0
		for {
			var err error
			host, instanceName, err = allocatePooledInstance(targets, resourceTypeName, req.ImageAlias, s.overCommitPercent, req.RunnerName)
			if err != nil {
				if retried < 10 {
					retried++
					log.Printf("AddInstance failed allocating instance (retrying %d...): %+v", retried, err)
					time.Sleep(1 * time.Second)
					continue
				} else {
					return nil, "", status.Errorf(codes.Internal, "can not allocate instance")
				}
			}
			break
		}
	}
	log.Printf("AddInstance uses instance %q in %q", instanceName, host.HostConfig.LxdHost)
	client := host.Client

	err := unfreezeInstance(client, instanceName)
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to unfreeze instance: %+v", err)
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

func (s *ShoesLXDMultiServer) addInstanceCreateMode(targetLXDHosts []lxdclient.LXDHost, req *pb.AddInstanceRequest) (*lxdclient.LXDHost, string, error) {
	instanceName := req.RunnerName

	instanceSource, err := parseAlias(req.ImageAlias)
	if err != nil {
		return nil, "", status.Errorf(codes.InvalidArgument, "failed to parse image alias: %+v", err)
	}

	host, err := s.isExistInstance(targetLXDHosts, instanceName)
	if err != nil && !errors.Is(err, ErrInstanceIsNotFound) {
		return nil, "", status.Errorf(codes.Internal, "failed to get instance: %+v", err)
	}

	if errors.Is(err, ErrInstanceIsNotFound) {
		host, err = s.scheduleHost(targetLXDHosts)
		if err != nil {
			return nil, "", status.Errorf(codes.InvalidArgument, "failed to schedule host: %+v", err)
		}
		log.Printf("AddInstance scheduled host: %s\n", host.HostConfig.LxdHost)

		reqInstance := api.InstancesPost{
			InstancePut: api.InstancePut{
				Config:  s.getInstanceConfig(req.SetupScript, req.ResourceType),
				Devices: s.getInstanceDevices(),
			},
			Name:   instanceName,
			Source: *instanceSource,
		}

		op, err := host.Client.CreateInstance(reqInstance)
		if err != nil {
			return nil, "", status.Errorf(codes.Internal, "failed to create instance: %+v", err)
		}
		if err := op.Wait(); err != nil {
			return nil, "", status.Errorf(codes.Internal, "failed to wait creating instance: %+v", err)
		}
	}

	reqState := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}
	op, err := host.Client.UpdateInstanceState(instanceName, reqState, "")
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to start instance: %+v", err)
	}
	if err := op.Wait(); err != nil && !strings.EqualFold(err.Error(), "The instance is already running") {
		return nil, "", status.Errorf(codes.Internal, "failed to wait starting instance: %+v", err)
	}

	return host, instanceName, nil
}

func (s *ShoesLXDMultiServer) getInstanceConfig(setupScript string, rt myshoespb.ResourceType) map[string]string {
	rawLXCConfig := `lxc.apparmor.profile = unconfined
lxc.cgroup.devices.allow = a
lxc.cap.drop=`

	instanceConfig := map[string]string{
		"security.nesting":    "true",
		"security.privileged": "true",
		"raw.lxc":             rawLXCConfig,
		"user.user-data":      setupScript,
	}

	if mapping, ok := s.resourceMapping[rt]; ok {
		instanceConfig["limits.cpu"] = strconv.Itoa(mapping.CPUCore)
		instanceConfig["limits.memory"] = mapping.Memory
	}

	return instanceConfig
}

func (s *ShoesLXDMultiServer) getInstanceDevices() map[string]map[string]string {
	instanceDevices := map[string]map[string]string{
		"kmsg": {
			"path":   "/dev/kmsg",
			"source": "/dev/kmsg",
			"type":   "unix-char",
		},
	}

	return instanceDevices
}

type targetHost struct {
	host     lxdclient.LXDHost
	resource lxdclient.Resource

	percentOverCommit uint64
}

func (s *ShoesLXDMultiServer) scheduleHost(targetLXDHosts []lxdclient.LXDHost) (*lxdclient.LXDHost, error) {
	targets, err := getResources(targetLXDHosts)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
	}

	target, err := schedule(targets, s.overCommitPercent)
	if err != nil {
		return nil, fmt.Errorf("failed to schedule: %w", err)
	}
	return &(target.host), nil
}

func getResources(targetLXDHosts []lxdclient.LXDHost) ([]targetHost, error) {
	var targets []targetHost

	eg := errgroup.Group{}
	mu := sync.Mutex{}

	for _, t := range targetLXDHosts {
		t := t
		eg.Go(func() error {
			resources, err := lxdclient.GetResource(t.HostConfig)
			if err != nil {
				log.Printf("failed to get resource (host: %s): %+v\n", t.HostConfig.LxdHost, err)
				return nil
			}

			mu.Lock()
			targets = append(targets, targetHost{
				host:              t,
				resource:          *resources,
				percentOverCommit: lxdclient.GetCPUOverCommitPercent(*resources),
			})
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
	}

	return targets, nil
}

var (
	// ErrNoValidHost is not valid host in targets
	ErrNoValidHost = fmt.Errorf("no valid host in targets")
)

func schedule(targets []targetHost, limitOverCommit uint64) (*targetHost, error) {
	var schedulableTargets []targetHost
	for _, target := range targets {
		if target.percentOverCommit < limitOverCommit {
			schedulableTargets = append(schedulableTargets, target)
		} else {
			log.Printf("%s is percentage of over-commit is high. ignore (now: %d, limit: %d)", target.host.HostConfig.LxdHost, target.percentOverCommit, limitOverCommit)
		}
	}
	if len(schedulableTargets) == 0 {
		return nil, ErrNoValidHost
	}

	// 1. use lowest over-commit instance
	// 2. check limit of over-commit
	sort.SliceStable(schedulableTargets, func(i, j int) bool {
		// lowest percentOverCommit is first
		return schedulableTargets[i].percentOverCommit < schedulableTargets[j].percentOverCommit
	})

	index := rand.Intn(len(schedulableTargets))
	return &schedulableTargets[index], nil
}

// parseAlias parse user input
func parseAlias(input string) (*api.InstanceSource, error) {
	if strings.EqualFold(input, "") {
		// default value is ubuntu:bionic
		return &api.InstanceSource{
			Type: "image",
			Properties: map[string]string{
				"os":      "ubuntu",
				"release": "bionic",
			},
		}, nil
	}

	if strings.HasPrefix(input, "http") {
		// https://<FQDN or IP>:8443/<alias>
		u, err := url.Parse(input)
		if err != nil {
			return nil, fmt.Errorf("failed to parse alias: %w", err)
		}

		urlImageServer := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		alias := strings.TrimPrefix(u.Path, "/")

		return &api.InstanceSource{
			Type:   "image",
			Mode:   "pull",
			Server: urlImageServer,
			Alias:  alias,
		}, nil
	}

	return &api.InstanceSource{
		Type:  "image",
		Alias: input,
	}, nil
}
