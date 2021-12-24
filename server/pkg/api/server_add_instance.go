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

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"

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
	instanceName := req.RunnerName

	instanceSource, err := parseAlias(req.ImageAlias)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse image alias: %+v", err)
	}

	targetLXDHosts, err := s.validateTargetHosts(req.TargetHosts)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to validate target hosts: %+v", err)
	}

	host := s.isExistInstance(targetLXDHosts, instanceName)
	var client lxd.InstanceServer
	if host == nil {
		host, err := s.scheduleHost(targetLXDHosts)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to schedule host: %+v", err)
		}
		log.Printf("AddInstance scheduled host: %s\n", host.HostConfig.LxdHost)

		reqInstance := api.InstancesPost{
			InstancePut: api.InstancePut{
				Config: s.getInstanceConfig(req.SetupScript, req.ResourceType),
			},
			Name:   instanceName,
			Source: *instanceSource,
		}

		client = host.Client
		op, err := client.CreateInstance(reqInstance)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create instance: %+v", err)
		}
		if err := op.Wait(); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to wait creating instance: %+v", err)
		}
	} else {
		client = host.Client
	}

	reqState := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}
	op, err := client.UpdateInstanceState(instanceName, reqState, "")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start instance: %+v", err)
	}
	if err := op.Wait(); err != nil && !strings.EqualFold(err.Error(), "The instance is already running") {
		return nil, status.Errorf(codes.Internal, "failed to wait starting instance: %+v", err)
	}

	i, _, err := client.GetInstance(instanceName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve instance information: %+v", err)
	}

	return &pb.AddInstanceResponse{
		CloudId:   i.Name,
		ShoesType: "lxd",
		IpAddress: "",
	}, nil
}

func (s *ShoesLXDMultiServer) getInstanceConfig(setupScript string, rt pb.ResourceType) map[string]string {
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

type targetHost struct {
	host     lxdclient.LXDHost
	resource lxdclient.Resource

	percentOverCommit uint64
}

func (s *ShoesLXDMultiServer) scheduleHost(targetLXDHosts []lxdclient.LXDHost) (*lxdclient.LXDHost, error) {
	var targets []targetHost

	for _, t := range targetLXDHosts {
		resources, err := lxdclient.GetResource(t.HostConfig)
		if err != nil {
			if errors.Is(err, lxdclient.ErrTimeoutConnectLXD) {
				continue
			}

			return nil, fmt.Errorf("failed to get resource (host: %s): %w", t.HostConfig.LxdHost, err)
		}

		targets = append(targets, targetHost{
			host:              t,
			resource:          *resources,
			percentOverCommit: lxdclient.GetCPUOverCommitPercent(*resources),
		})
	}
	target, err := schedule(targets, s.overCommitPercent)
	if err != nil {
		return nil, fmt.Errorf("failed to schedule: %w", err)
	}
	return &(target.host), nil
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
