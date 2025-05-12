package cmd_test

import (
	"slices"
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
	s.agent.Client.(*mockLxdClient).On("GetInstances", api.InstanceTypeAny).Return([]api.Instance{
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
		imageKey         string
		want             bool
	}{
		{
			name:             "typeA - focal",
			resourceTypeName: "typeA",
			imageKey:         "focal",
			want:             false,
		},
		{
			name:             "typeB - focal",
			resourceTypeName: "typeB",
			imageKey:         "focal",
			want:             false,
		},
		{
			name:             "typeC - focal",
			resourceTypeName: "typeC",
			imageKey:         "focal",
			want:             true,
		},
		{
			name:             "typeD - focal - not configured",
			resourceTypeName: "typeD",
			imageKey:         "focal",
			want:             true,
		},
	}
	instances, err := s.agent.Client.GetInstances(api.InstanceTypeAny)
	s.Require().NoError(err)
	for _, tt := range tests {
		disabledResourceTypes := []string{}
		_, ok := s.agent.CalculateCreateCount(instances, tt.resourceTypeName, tt.imageKey)
		if !ok {
			disabledResourceTypes = append(disabledResourceTypes, tt.resourceTypeName)
		}

		s.Run(tt.name, func() {
			toDeleteInstances := s.agent.CalculateToDeleteInstances(instances, disabledResourceTypes, tt.imageKey)
			s.Assert().Equal(tt.want, slices.ContainsFunc(toDeleteInstances, func(instance api.Instance) bool {
				return tt.resourceTypeName == instance.Config[cmd.ConfigKeyResourceType] && s.agent.Image[tt.imageKey].Config.ImageAlias == instance.Config[cmd.ConfigKeyImageAlias]
			}))
		})
	}
}

func (s *AgentSuite) TestCollectResourceTypes() {
	instances, err := s.agent.Client.GetInstances(api.InstanceTypeAny)
	s.Require().NoError(err)

	resourceTypes := s.agent.CollectResourceTypes(instances)

	tests := []struct {
		name     string
		resource string
	}{
		{
			name:     "typeA",
			resource: "typeA",
		},
		{
			name:     "typeD - only in actual instances",
			resource: "typeD",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Assert().Contains(resourceTypes, tt.resource)
		})
	}
}

func (s *AgentSuite) TestCalculateCreateCount() {
	tests := []struct {
		name             string
		resourceTypeName string
		imageKey         string
		want1            int
		want2            bool
	}{
		{
			name:             "typeA - focal not enough",
			resourceTypeName: "typeA",
			imageKey:         "focal",
			want1:            1,
			want2:            true,
		},
		{
			name:             "typeB - focal already created",
			resourceTypeName: "typeB",
			imageKey:         "focal",
			want1:            0,
			want2:            true,
		},
		{
			name:             "typeC - focal disabled",
			resourceTypeName: "typeC",
			imageKey:         "focal",
			want1:            0,
			want2:            false,
		},
		{
			name:             "typeA - noble not enough",
			resourceTypeName: "typeA",
			imageKey:         "noble",
			want1:            2,
			want2:            true,
		},
		{
			name:             "typeB - noble disabled",
			resourceTypeName: "typeB",
			imageKey:         "noble",
			want1:            0,
			want2:            false,
		},
		{
			name:             "typeC - noble not enough",
			resourceTypeName: "typeC",
			imageKey:         "noble",
			want1:            1,
			want2:            true,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			instances, err := s.agent.Client.GetInstances(api.InstanceTypeAny)
			s.Require().NoError(err)
			count, ok := s.agent.CalculateCreateCount(instances, tt.resourceTypeName, tt.imageKey)
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
