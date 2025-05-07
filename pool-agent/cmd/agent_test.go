package cmd_test

import (
	"testing"

	"github.com/lxc/lxd/shared/api"
	"github.com/whywaita/shoes-lxd-multi/pool-agent/cmd"
)

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
						cmd.ConfigKeyImageAlias:   "image1",
						cmd.ConfigKeyRunnerName:   "",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "image1",
			want:             false,
		},
		{
			name: "Resource type mismatch",
			instance: api.Instance{
				StatusCode: api.Running,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeB",
						cmd.ConfigKeyImageAlias:   "image1",
						cmd.ConfigKeyRunnerName:   "runner1",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "image1",
			want:             false,
		},
		{
			name: "Image alias mismatch",
			instance: api.Instance{
				StatusCode: api.Frozen,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "image2",
						cmd.ConfigKeyRunnerName:   "",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "image1",
			want:             false,
		},
		{
			name: "All matched - Running",
			instance: api.Instance{
				StatusCode: api.Running,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "image1",
						cmd.ConfigKeyRunnerName:   "runner1",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "image1",
			want:             true,
		},
		{
			name: "All matched - Frozen",
			instance: api.Instance{
				StatusCode: api.Frozen,
				InstancePut: api.InstancePut{
					Config: map[string]string{
						cmd.ConfigKeyResourceType: "typeA",
						cmd.ConfigKeyImageAlias:   "image1",
						cmd.ConfigKeyRunnerName:   "",
					},
				},
			},
			resourceTypeName: "typeA",
			imageAlias:       "image1",
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
