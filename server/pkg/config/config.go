package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	myshoespb "github.com/whywaita/myshoes/api/proto.go"
	"github.com/whywaita/myshoes/pkg/datastore"
)

const (
	// EnvLXDHosts is json of lxd hosts
	EnvLXDHosts = "LXD_MULTI_HOSTS"

	// EnvLXDResourceTypeMapping is mapping resource in lxd
	EnvLXDResourceTypeMapping = "LXD_MULTI_RESOURCE_TYPE_MAPPING"
	// EnvLXDResourceCachePeriodSec is period of setting LXD resource cache
	EnvLXDResourceCachePeriodSec = "LXD_MULTI_RESOURCE_CACHE_PERIOD_SEC"
	// EnvPort will listen port
	EnvPort = "LXD_MULTI_PORT"
	// EnvOverCommit will set percent of over commit in CPU
	EnvOverCommit = "LXD_MULTI_OVER_COMMIT_PERCENT"
	// EnvMode will set running mode
	EnvMode = "LXD_MULTI_MODE"
)

// Mapping is resource mapping
type Mapping struct {
	ResourceTypeName string `json:"resource_type_name"`
	CPUCore          int    `json:"cpu"`
	Memory           string `json:"memory"`
}

// Load load config from Environment values
func Load() (*HostConfigMap, map[myshoespb.ResourceType]Mapping, int64, int, uint64, bool, error) {
	hostConfigs, err := loadHostConfigs()
	if err != nil {
		return nil, nil, 0, -1, 0, false, fmt.Errorf("failed to load host config: %w", err)
	}

	envMappingJSON := os.Getenv(EnvLXDResourceTypeMapping)
	var m map[myshoespb.ResourceType]Mapping
	if envMappingJSON != "" {
		m, err = readResourceTypeMapping(envMappingJSON)
		if err != nil {
			return nil, nil, 0, -1, 0, false, fmt.Errorf("failed to read %s: %w", EnvLXDResourceTypeMapping, err)
		}
	}

	envPeriodSec := os.Getenv(EnvLXDResourceCachePeriodSec)
	var periodSec int64
	if envPeriodSec == "" {
		periodSec = 10
	} else {
		periodSec, err = strconv.ParseInt(envPeriodSec, 10, 64)
		if err != nil {
			return nil, nil, 0, -1, 0, false, fmt.Errorf("failed to parse %s, need to uint: %w", EnvOverCommit, err)
		}
	}
	log.Printf("periodSec: %d\n", periodSec)

	envPort := os.Getenv(EnvPort)
	var port int
	if envPort == "" {
		port = 8080
	} else {
		port, err = strconv.Atoi(envPort)
		if err != nil {
			return nil, nil, 0, -1, 0, false, fmt.Errorf("failed to parse %s, need to int: %w", EnvPort, err)
		}
	}

	envOCP := os.Getenv(EnvOverCommit)
	var overCommitPercent uint64
	if envOCP == "" {
		overCommitPercent = 100
	} else {
		overCommitPercent, err = strconv.ParseUint(envOCP, 10, 64)
		if err != nil {
			return nil, nil, 0, -1, 0, false, fmt.Errorf("failed to parse %s, need to uint: %w", EnvOverCommit, err)
		}
	}
	log.Printf("overCommitPercent: %d\n", overCommitPercent)

	var poolMode bool
	switch os.Getenv(EnvMode) {
	case "", "create":
		poolMode = false
	case "pool":
		poolMode = true
	default:
		return nil, nil, 0, -1, 0, false, fmt.Errorf(`unknown mode %q (expected "create" or "pool")`, os.Getenv(EnvMode))
	}

	return hostConfigs, m, periodSec, port, overCommitPercent, poolMode, nil
}

func readResourceTypeMapping(env string) (map[myshoespb.ResourceType]Mapping, error) {
	var mapping []Mapping
	if err := json.Unmarshal([]byte(env), &mapping); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	r := map[myshoespb.ResourceType]Mapping{}
	for _, m := range mapping {
		rt := datastore.UnmarshalResourceType(m.ResourceTypeName)
		if rt == datastore.ResourceTypeUnknown {
			return nil, fmt.Errorf("%s is invalid resource type", m.ResourceTypeName)
		}
		r[rt.ToPb()] = m
	}

	return r, nil
}
