package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	pb "github.com/whywaita/shoes-lxd-multi/proto.go"

	"github.com/whywaita/myshoes/pkg/datastore"
)

const (
	// EnvLXDHosts is json of lxd hosts
	EnvLXDHosts = "LXD_MULTI_HOSTS"

	// EnvLXDResourceTypeMapping is mapping resource in lxd
	EnvLXDResourceTypeMapping = "LXD_MULTI_RESOURCE_TYPE_MAPPING"
	// EnvPort will listen port
	EnvPort = "LXD_MULTI_PORT"
	// EnvOverCommit will set percent of over commit in CPU
	EnvOverCommit = "LXD_MULTI_OVER_COMMIT_PERCENT"
)

// Mapping is resource mapping
type Mapping struct {
	ResourceTypeName string `json:"resource_type_name"`
	CPUCore          int    `json:"cpu"`
	Memory           string `json:"memory"`
}

// Load load config from Environment values
func Load() (*HostConfigMap, map[pb.ResourceType]Mapping, int, uint64, error) {
	hostConfigs, err := loadHostConfigs()
	if err != nil {
		return nil, nil, -1, 0, fmt.Errorf("failed to load host config: %w", err)
	}

	envMappingJSON := os.Getenv(EnvLXDResourceTypeMapping)
	var m map[pb.ResourceType]Mapping
	if envMappingJSON != "" {
		m, err = readResourceTypeMapping(envMappingJSON)
		if err != nil {
			return nil, nil, -1, 0, fmt.Errorf("failed to read %s: %w", EnvLXDResourceTypeMapping, err)
		}
	}

	envPort := os.Getenv(EnvPort)
	var port int
	if envPort == "" {
		port = 8080
	} else {
		port, err = strconv.Atoi(envPort)
		if err != nil {
			return nil, nil, -1, 0, fmt.Errorf("failed to parse %s, need to int: %w", EnvPort, err)
		}
	}

	envOCP := os.Getenv(EnvOverCommit)
	var overCommitPercent uint64
	if envOCP == "" {
		overCommitPercent = 100
	} else {
		overCommitPercent, err = strconv.ParseUint(envOCP, 10, 64)
		if err != nil {
			return nil, nil, -1, 0, fmt.Errorf("failed to parse %s, need to uint: %w", EnvOverCommit, err)
		}
	}

	return hostConfigs, m, port, overCommitPercent, nil
}

func readResourceTypeMapping(env string) (map[pb.ResourceType]Mapping, error) {
	var mapping []Mapping
	if err := json.Unmarshal([]byte(env), &mapping); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	r := map[pb.ResourceType]Mapping{}
	for _, m := range mapping {
		rt := datastore.UnmarshalResourceType(m.ResourceTypeName)
		if rt == datastore.ResourceTypeUnknown {
			return nil, fmt.Errorf("%s is invalid resource type", m.ResourceTypeName)
		}

		r[toPb(rt)] = m
	}

	return r, nil
}

func toPb(in datastore.ResourceType) pb.ResourceType {
	switch in {
	case datastore.ResourceTypeNano:
		return pb.ResourceType_Nano
	case datastore.ResourceTypeMicro:
		return pb.ResourceType_Micro
	case datastore.ResourceTypeSmall:
		return pb.ResourceType_Small
	case datastore.ResourceTypeMedium:
		return pb.ResourceType_Medium
	case datastore.ResourceTypeLarge:
		return pb.ResourceType_Large
	case datastore.ResourceTypeXLarge:
		return pb.ResourceType_XLarge
	case datastore.ResourceType2XLarge:
		return pb.ResourceType_XLarge2
	case datastore.ResourceType3XLarge:
		return pb.ResourceType_XLarge3
	case datastore.ResourceType4XLarge:
		return pb.ResourceType_XLarge4
	}

	return pb.ResourceType_Unknown
}
