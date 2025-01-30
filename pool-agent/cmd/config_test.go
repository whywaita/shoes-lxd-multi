package cmd_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/whywaita/shoes-lxd-multi/pool-agent/cmd"
)

func Test_LoadConfig(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		name  string
		input string
		want  *cmd.Config
	}{
		{
			name: "ok - multiple os release",
			input: `[resource_types_map.large]
cpu = 2
memory = "2GB"
[resource_types_map.xlarge]
cpu = 4
memory = "4GB"
[resource_types_map.2xlarge]
cpu = 8
memory = "8GB"
[resource_types_map.3xlarge]
cpu = 12
memory = "12GB"
[config.noble]
image_alias = "https://image-server.example.com:8443/myshoes-noble"
[config.noble.resource_types_counts]
large = 9
xlarge = 2
2xlarge = 2
3xlarge = 2
[config.focal]
image_alias = "https://image-server.example.com:8443/myshoes-focal"
[config.focal.resource_types_counts]
large = 9
xlarge = 2
2xlarge = 2
3xlarge = 2`,
			want: &cmd.Config{
				ResourceTypesMap: cmd.ResourceTypesMap{
					"large": {
						CPUCore: 2,
						Memory:  "2GB",
					},
					"xlarge": {
						CPUCore: 4,
						Memory:  "4GB",
					},
					"2xlarge": {
						CPUCore: 8,
						Memory:  "8GB",
					},
					"3xlarge": {
						CPUCore: 12,
						Memory:  "12GB",
					},
				},
				ConfigPerImage: map[string]cmd.ConfigPerImage{
					"noble": {
						ImageAlias: "https://image-server.example.com:8443/myshoes-noble",
						ResourceTypesCounts: cmd.ResourceTypesCounts{
							"large":   9,
							"xlarge":  2,
							"2xlarge": 2,
							"3xlarge": 2,
						},
					},
					"focal": {
						ImageAlias: "https://image-server.example.com:8443/myshoes-focal",
						ResourceTypesCounts: cmd.ResourceTypesCounts{
							"large":   9,
							"xlarge":  2,
							"2xlarge": 2,
							"3xlarge": 2,
						},
					},
				},
			},
		},
		{
			name: "ok - support backward compatibility",
			input: `image_alias = "https://image-server.example.com:8443/myshoes-focal"
[[resource_types_map]]
name = "large"
cpu = 2
memory = "2GB"
[[resource_types_map]]
name = "xlarge"
cpu = 4
memory = "4GB"
[[resource_types_map]]
name = "2xlarge"
cpu = 8
memory = "8GB"
[[resource_types_map]]
name = "3xlarge"
cpu = 12
memory = "12GB"
[resource_types_counts]
large = 27
xlarge = 0
2xlarge = 2
3xlarge = 1`,
			want: &cmd.Config{
				ResourceTypesMap: cmd.ResourceTypesMap{
					"large": {
						CPUCore: 2,
						Memory:  "2GB",
					},
					"xlarge": {
						CPUCore: 4,
						Memory:  "4GB",
					},
					"2xlarge": {
						CPUCore: 8,
						Memory:  "8GB",
					},
					"3xlarge": {
						CPUCore: 12,
						Memory:  "12GB",
					},
				},
				ConfigPerImage: map[string]cmd.ConfigPerImage{
					"default": {
						ImageAlias: "https://image-server.example.com:8443/myshoes-focal",
						ResourceTypesCounts: cmd.ResourceTypesCounts{
							"large":   27,
							"xlarge":  0,
							"2xlarge": 2,
							"3xlarge": 1,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := cmd.LoadConfig([]byte(tt.input))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}
