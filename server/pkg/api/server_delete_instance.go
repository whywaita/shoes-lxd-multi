package api

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/lxc/lxd/shared/api"
	pb "github.com/whywaita/shoes-lxd-multi/proto.go"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DeleteInstance delete instance to LXD server
func (s *ShoesLXDMultiServer) DeleteInstance(ctx context.Context, req *pb.DeleteInstanceRequest) (*pb.DeleteInstanceResponse, error) {
	slog.Info("DeleteInstance", "req", req)
	l := slog.With("method", "DeleteInstance")
	instanceName := req.CloudId
	l = l.With("instanceName", instanceName)
	targetLXDHosts, err := s.validateTargetHosts(ctx, req.TargetHosts, l)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to validate target hosts: %+v", err)
	}

	host, err := s.isExistInstance(targetLXDHosts, instanceName, l)
	if err != nil {
		switch {
		case errors.Is(err, ErrInstanceIsNotFound):
			return nil, status.Errorf(codes.NotFound, "failed to found worker that has %s", instanceName)
		default:
			return nil, status.Errorf(codes.Internal, "failed to found worker that has %s", instanceName)
		}
	}

	l = l.With("host", host.HostConfig.LxdHost)

	l.Info("will stop instance")
	client := host.Client
	hostAddr := host.HostConfig.LxdHost
	reqState := api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}
	timer := metric.NewLXDAPITimer(hostAddr, "UpdateInstanceState")
	op, err := client.UpdateInstanceState(instanceName, reqState, "")
	timer.ObserveDuration(err)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to stop instance: %+v", err)
	}
	if err := op.Wait(); err != nil && !strings.EqualFold(err.Error(), "The instance is already stopped") {
		return nil, status.Errorf(codes.Internal, "failed to wait stopping instance: %+v", err)
	}

	l.Info("will delete instance")
	timer = metric.NewLXDAPITimer(hostAddr, "DeleteInstance")
	op, err = client.DeleteInstance(instanceName)
	timer.ObserveDuration(err)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete instance: %+v", err)
	}
	if err := op.Wait(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to wait deleting instance: %+v", err)
	}

	l.Info("Success DeleteInstance")

	return &pb.DeleteInstanceResponse{}, nil
}
