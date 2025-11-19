package api

import (
	"bytes"
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

// bufferCloser is a wrapper around bytes.Buffer that implements io.WriteCloser
type bufferCloser struct {
	*bytes.Buffer
}

func (bc *bufferCloser) Close() error {
	return nil
}

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

	// Prepare stdout/stderr buffers for capturing exec output
	stdout := &bufferCloser{Buffer: &bytes.Buffer{}}
	stderr := &bufferCloser{Buffer: &bytes.Buffer{}}

	op, err := client.ExecInstance(instanceName, api.InstanceExecPost{
		Command: []string{
			"systemd-run",
			"--unit", "myshoes-setup",
			"--property", "After=multi-user.target",
			"--property", "StandardOutput=journal+console",
			"--property", fmt.Sprintf("ExecStartPre=/usr/bin/hostname %s", req.RunnerName), // set hostname inside instance. caution: we don't use hostnamectl because dbus is not response in environment of high load
			"--property", fmt.Sprintf("ExecStartPre=/bin/sh -c 'echo 127.0.1.1 %s >> /etc/hosts'", req.RunnerName),
			scriptFilename,
		},
	}, &lxd.InstanceExecArgs{
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to execute setup script: %+v", err)
	}
	if err := op.Wait(); err != nil {
		return nil, "", status.Errorf(codes.Internal, "failed to wait executing setup script: %+v", err)
	}

	// Get command exit code, logging stdout/stderr if non-zero
	if op.Get().Metadata["return"] == nil || op.Get().Metadata["return"].(int32) != 0 {
		l.Error("Setup script failed", "stdout", stdout.String(), "stderr", stderr.String(), "exitCode", op.Get().Metadata["return"])
		return nil, "", status.Errorf(codes.Internal, "failed to execute setup script: exit code %v", op.Get().Metadata["return"])
	}

	return host, instanceName, nil
}

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
