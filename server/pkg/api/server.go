package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	myshoespb "github.com/whywaita/myshoes/api/proto.go"
	pb "github.com/whywaita/shoes-lxd-multi/proto.go"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
	"google.golang.org/grpc"
)

// ShoesLXDMultiServer implement gRPC server
type ShoesLXDMultiServer struct {
	pb.UnimplementedShoesLXDMultiServer

	hostConfigs     *config.HostConfigMap
	resourceMapping map[myshoespb.ResourceType]config.Mapping
	imageAliasMap   map[string]string

	overCommitPercent uint64

	schedulerClient *SchedulerClient

	mu sync.Mutex
}

// New create gRPC server
func New(hostConfigs *config.HostConfigMap, mapping map[myshoespb.ResourceType]config.Mapping, imageAliasMap map[string]string, overCommitPercent uint64, schedulerAddress string) (*ShoesLXDMultiServer, error) {
	var schedulerClient *SchedulerClient
	if schedulerAddress != "" {
		schedulerClient = NewSchedulerClient(schedulerAddress)
	}

	return &ShoesLXDMultiServer{
		hostConfigs:       hostConfigs,
		resourceMapping:   mapping,
		overCommitPercent: overCommitPercent,
		mu:                sync.Mutex{},
		imageAliasMap:     imageAliasMap,
		schedulerClient:   schedulerClient,
	}, nil
}

// Run run gRPC server
func (s *ShoesLXDMultiServer) Run(listenPort int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	slog.Info("start listen", "port", listenPort)

	grpcServer := grpc.NewServer()
	pb.RegisterShoesLXDMultiServer(grpcServer, s)

	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC: %w", err)
	}
	return nil
}

func (s *ShoesLXDMultiServer) validateTargetHosts(ctx context.Context, targetHosts []string, logger *slog.Logger) ([]*lxdclient.LXDHost, error) {
	var hostConfigs []config.HostConfig

	for _, target := range targetHosts {
		l := logger.With("target", target)
		host, err := s.hostConfigs.Load(target)
		if err != nil {
			l.Warn("ignore host in target", "err", err.Error())
			continue
		}

		hostConfigs = append(hostConfigs, *host)
	}

	if len(hostConfigs) == 0 {
		return nil, fmt.Errorf("valid target host is not found")
	}

	targetLXDHosts, _, err := lxdclient.ConnectLXDs(ctx, hostConfigs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect LXD: %w", err)
	}

	if len(targetLXDHosts) == 0 {
		return nil, fmt.Errorf("all target host can't connect")
	}

	return targetLXDHosts, nil
}
