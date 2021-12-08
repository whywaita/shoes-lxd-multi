package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hashicorp/go-plugin"
	pb "github.com/whywaita/myshoes/api/proto"
	shoeslxdpb "github.com/whywaita/shoes-lxd-multi/proto.go"
	"google.golang.org/grpc"
)

const (
	// EnvTargetHosts is list of target host
	EnvTargetHosts = "LXD_MULTI_TARGET_HOSTS"
	// EnvServerEndpoint is endpoint of server
	EnvServerEndpoint = "LXD_MULTI_SERVER_ENDPOINT"

	// EnvLXDImageAlias is alias in lxd
	EnvLXDImageAlias = "LXD_MULTI_IMAGE_ALIAS"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	handshake := plugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "SHOES_PLUGIN_MAGIC_COOKIE",
		MagicCookieValue: "are_you_a_shoes?",
	}
	pluginMap := map[string]plugin.Plugin{
		"shoes_grpc": &LXDMultiPlugin{},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshake,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})

	return nil
}

// LXDMultiPlugin is plugin for lxd multi node
type LXDMultiPlugin struct {
	plugin.Plugin
}

func loadConfig() ([]string, string, string, error) {
	var targetHosts []string
	envTargetHosts := os.Getenv(EnvTargetHosts)
	if err := json.Unmarshal([]byte(envTargetHosts), &targetHosts); err != nil {
		return nil, "", "", fmt.Errorf("failed to unmarshal JSON from %s: %w", envTargetHosts, err)
	}

	envServerEndpoint := os.Getenv(EnvServerEndpoint)
	if envServerEndpoint == "" {
		return nil, "", "", fmt.Errorf("must set %s", EnvServerEndpoint)
	}

	alias := os.Getenv(EnvLXDImageAlias)
	if alias == "" {
		alias = "ubuntu:bionic"
	}

	return targetHosts, envServerEndpoint, alias, nil
}

// GRPCServer is implement gRPC Server.
func (l *LXDMultiPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	targetHosts, serverEndpoint, imageAlias, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	grpcConn, err := grpc.Dial(
		serverEndpoint,
		grpc.WithInsecure(),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("failed to dial to server: %w", err)
	}
	client := NewClient(targetHosts, grpcConn, imageAlias)
	pb.RegisterShoesServer(s, client)
	return nil

}

// GRPCClient is implement gRPC client.
// This function is not have client, so return nil
func (l *LXDMultiPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return nil, nil
}

// Client is client of shoes-lxd-multi/server
type Client struct {
	pb.UnimplementedShoesServer

	targetHosts []string
	conn        *grpc.ClientConn
	imageAlias  string
}

// NewClient create Client
func NewClient(targetHosts []string, conn *grpc.ClientConn, imageAlias string) *Client {
	return &Client{
		targetHosts: targetHosts,
		conn:        conn,
		imageAlias:  imageAlias,
	}
}

// AddInstance add a lxd instance.
func (l Client) AddInstance(ctx context.Context, req *pb.AddInstanceRequest) (*pb.AddInstanceResponse, error) {
	slClient := shoeslxdpb.NewShoesLXDMultiClient(l.conn)
	slReq := &shoeslxdpb.AddInstanceRequest{
		RunnerName:   req.RunnerName,
		SetupScript:  req.SetupScript,
		ResourceType: shoeslxdpb.ResourceTypeToShoesLXDMultiPb(req.ResourceType),
		TargetHosts:  l.targetHosts,
		ImageAlias:   l.imageAlias,
	}

	slResp, err := slClient.AddInstance(ctx, slReq)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to AddInstance: %+v", err)
	}
	return &pb.AddInstanceResponse{
		CloudId:   slResp.CloudId,
		ShoesType: slResp.ShoesType,
		IpAddress: slResp.IpAddress,
	}, nil
}

// DeleteInstance delete a lxd instance.
func (l Client) DeleteInstance(ctx context.Context, req *pb.DeleteInstanceRequest) (*pb.DeleteInstanceResponse, error) {
	slClient := shoeslxdpb.NewShoesLXDMultiClient(l.conn)
	slReq := &shoeslxdpb.DeleteInstanceRequest{
		CloudId:     req.CloudId,
		TargetHosts: l.targetHosts,
	}

	if _, err := slClient.DeleteInstance(ctx, slReq); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to DeleteInstance: %+v", err)
	}
	return &pb.DeleteInstanceResponse{}, nil
}
