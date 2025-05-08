package cmd_test

import (
	"testing"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"github.com/whywaita/shoes-lxd-multi/pool-agent/cmd"
)

type AgentSuite struct {
	suite.Suite
	agent *cmd.Agent
}

type mockLxdClient struct {
	mock.Mock
	lxd.InstanceServer
}

func (m *mockLxdClient) GetInstances(instanceType api.InstanceType) ([]api.Instance, error) {
	args := m.Called(instanceType)
	return args.Get(0).([]api.Instance), args.Error(1)
}

func (s *AgentSuite) SetupSuite() {
	s.agent = &cmd.Agent{
		Client: &mockLxdClient{},
		Image: map[string]*cmd.Image{
			"focal": {
				InstanceSource: api.InstanceSource{},
				Config: cmd.ConfigPerImage{
					ImageAlias:          "ubuntu:focal",
					ResourceTypesCounts: map[string]int{"typeA": 2, "typeB": 1, "typeC": 0},
				},
				Status: cmd.ImageStatus{
					CreatingInstances: map[string]cmd.Instances{},
					CurrentImage: struct {
						Hash      string
						CreatedAt time.Time
					}{},
				},
			},
			"noble": {
				InstanceSource: api.InstanceSource{},
				Config: cmd.ConfigPerImage{
					ImageAlias:          "ubuntu:noble",
					ResourceTypesCounts: map[string]int{"typeA": 2, "typeB": 0, "typeC": 1},
				},
				Status: cmd.ImageStatus{
					CreatingInstances: map[string]cmd.Instances{},
					CurrentImage: struct {
						Hash      string
						CreatedAt time.Time
					}{},
				},
			},
		},
	}
	s.agent.Client.(*mockLxdClient).On("GetInstances", api.InstanceTypeContainer).Return([]api.Instance{
		{
			Name:       "available_stock_running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Config: map[string]string{
					cmd.ConfigKeyResourceType: "typeA",
					cmd.ConfigKeyImageAlias:   "ubuntu:focal",
					cmd.ConfigKeyRunnerName:   "runner1",
				},
			},
		},
		{
			Name:       "available_stock_frozen",
			StatusCode: api.Frozen,
			InstancePut: api.InstancePut{
				Config: map[string]string{
					cmd.ConfigKeyResourceType: "typeB",
					cmd.ConfigKeyImageAlias:   "ubuntu:focal",
					cmd.ConfigKeyRunnerName:   "",
				},
			},
		},
		{
			Name:       "broken_running",
			StatusCode: api.Running,
			InstancePut: api.InstancePut{
				Config: map[string]string{
					cmd.ConfigKeyResourceType: "typeC",
					cmd.ConfigKeyImageAlias:   "ubuntu:focal",
					cmd.ConfigKeyRunnerName:   "", // correctly running container should contain runner name
				},
			},
		},
		{
			Name:       "disabled_stock_frozen",
			StatusCode: api.Frozen,
			InstancePut: api.InstancePut{
				Config: map[string]string{
					cmd.ConfigKeyResourceType: "typeD",
					cmd.ConfigKeyImageAlias:   "ubuntu:focal",
					cmd.ConfigKeyRunnerName:   "",
				},
			},
		},
	}, nil)
}

func (s *AgentSuite) TestCalculateToDeleteInstances() {
	tests := []struct {
		name             string
		resourceTypeName string
		version          string
		want             bool
	}{
		{
			name:             "typeA - focal",
			resourceTypeName: "typeA",
			version:          "focal",
			want:             false,
		},
		{
			name:             "typeB - focal",
			resourceTypeName: "typeB",
			version:          "focal",
			want:             false,
		},
		{
			name:             "typeC - focal",
			resourceTypeName: "typeC",
			version:          "focal",
			want:             true,
		},
		{
			name:             "typeD - focal - not configured",
			resourceTypeName: "typeD",
			version:          "focal",
			want:             true,
		},
	}
	instances, err := s.agent.Client.GetInstances(api.InstanceTypeContainer)
	s.Require().NoError(err)
	for _, tt := range tests {
		disabledResourceTypes := []string{}
		_, ok := s.agent.CalculateCreateCount(instances, tt.resourceTypeName, tt.version)
		if !ok {
			disabledResourceTypes = append(disabledResourceTypes, tt.resourceTypeName)
		}
		s.Run(tt.name, func() {
			toDelete := s.agent.CalculateToDeleteInstances(instances, disabledResourceTypes, tt.version)
			s.Equal(tt.want, len(toDelete) > 0)
		})
	}
}

func (s *AgentSuite) TestCalculateCreateCount() {
	tests := []struct {
		name             string
		resourceTypeName string
		version          string
		want1            int
		want2            bool
	}{
		{
			name:             "typeA - focal not enough",
			resourceTypeName: "typeA",
			version:          "focal",
			want1:            1,
			want2:            true,
		},
		{
			name:             "typeB - focal already created",
			resourceTypeName: "typeB",
			version:          "focal",
			want1:            0,
			want2:            true,
		},
		{
			name:             "typeC - focal disabled",
			resourceTypeName: "typeC",
			version:          "focal",
			want1:            0,
			want2:            false,
		},
		{
			name:             "typeA - noble not enough",
			resourceTypeName: "typeA",
			version:          "noble",
			want1:            2,
			want2:            true,
		},
		{
			name:             "typeB - noble disabled",
			resourceTypeName: "typeB",
			version:          "noble",
			want1:            0,
			want2:            false,
		},
		{
			name:             "typeC - noble not enough",
			resourceTypeName: "typeC",
			version:          "noble",
			want1:            1,
			want2:            true,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			instances, err := s.agent.Client.GetInstances(api.InstanceTypeContainer)
			s.Require().NoError(err)
			count, ok := s.agent.CalculateCreateCount(instances, tt.resourceTypeName, tt.version)
			s.Equal(tt.want1, count)
			s.Equal(tt.want2, ok)
		})
	}
}

func TestIsPooledInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		instance         api.Instance
		resourceTypeName string
		imageAlias       string
		want             bool
	}{
		{
			name: "Status not pooled",
			instance: api.Instance{
				StatusCode: api.Stopped,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "ubuntu:focal",
						cmd.ConfigKeyRunnerName:   "",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "ubuntu:focal",
			want:             false,
		},
		{
			name: "Resource type mismatch",
			instance: api.Instance{
				StatusCode: api.Running,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeB",
						cmd.ConfigKeyImageAlias:   "ubuntu:focal",
						cmd.ConfigKeyRunnerName:   "runner1",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "ubuntu:focal",
			want:             false,
		},
		{
			name: "Image alias mismatch",
			instance: api.Instance{
				StatusCode: api.Frozen,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "ubuntu:noble",
						cmd.ConfigKeyRunnerName:   "",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "ubuntu:focal",
			want:             false,
		},
		{
			name: "All matched - Running",
			instance: api.Instance{
				StatusCode: api.Running,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "ubuntu:focal",
						cmd.ConfigKeyRunnerName:   "runner1",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "ubuntu:focal",
			want:             true,
		},
		{
			name: "All matched - Frozen",
			instance: api.Instance{
				StatusCode: api.Frozen,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "ubuntu:focal",
						cmd.ConfigKeyRunnerName:   "",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "ubuntu:focal",
			want:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.IsPooledInstance(tt.instance, tt.resourceTypeName, tt.imageAlias)
			if got != tt.want {
				t.Errorf("isPooledInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(AgentSuite))
}
