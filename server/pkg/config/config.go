package config

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
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

	EnvLogLevel = "LXD_MULTI_LOG_LEVEL"

	EnvLXDImageAliasMapping = "LXD_MULTI_IMAGE_ALIAS_MAPPING"

	// EnvLXDImageAlias is old environment variable for image alias, for backward compatibility
	EnvLXDImageAlias = "LXD_MULTI_IMAGE_ALIAS"

	// EnvLXDSchedulerAddress is scheduler server address
	EnvLXDSchedulerAddress = "LXD_MULTI_SCHEDULER_ADDRESS"
)

// Mapping is resource mapping
type Mapping struct {
	ResourceTypeName string `json:"resource_type_name"`
	CPUCore          int    `json:"cpu"`
	Memory           string `json:"memory"`
}

// Load load config from Environment values
func Load() (*HostConfigMap, map[myshoespb.ResourceType]Mapping, map[string]string, int64, int, uint64, *slog.Level, string, error) {
	hostConfigs, err := LoadHostConfigs()
	if err != nil {
		return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to load host config: %w", err)
	}

	envMappingJSON := os.Getenv(EnvLXDResourceTypeMapping)
	var m map[myshoespb.ResourceType]Mapping
	if envMappingJSON != "" {
		m, err = readResourceTypeMapping(envMappingJSON)
		if err != nil {
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to read %s: %w", EnvLXDResourceTypeMapping, err)
		}
	}

	envImageAliasJSON := os.Getenv(EnvLXDImageAliasMapping)
	var imageAliasMap map[string]string
	if envImageAliasJSON != "" {
		if err := json.Unmarshal([]byte(envImageAliasJSON), &imageAliasMap); err != nil {
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to unmarshal JSON: %w", err)
		}
		if _, ok := imageAliasMap["default"]; !ok {
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("default image alias is required, actual: %v", imageAliasMap)
		}
	} else {
		imageAlias := os.Getenv(EnvLXDImageAlias)
		if imageAlias == "" {
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("%s or %s is required", EnvLXDImageAliasMapping, EnvLXDImageAlias)
		}
		imageAliasMap = map[string]string{"default": imageAlias}
	}

	envPeriodSec := os.Getenv(EnvLXDResourceCachePeriodSec)
	var periodSec int64
	if envPeriodSec == "" {
		periodSec = 10
	} else {
		periodSec, err = strconv.ParseInt(envPeriodSec, 10, 64)
		if err != nil {
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to parse %s, need to uint: %w", EnvOverCommit, err)
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
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to parse %s, need to int: %w", EnvPort, err)
		}
	}

	envOCP := os.Getenv(EnvOverCommit)
	var overCommitPercent uint64
	if envOCP == "" {
		overCommitPercent = 100
	} else {
		overCommitPercent, err = strconv.ParseUint(envOCP, 10, 64)
		if err != nil {
			return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to parse %s, need to uint: %w", EnvOverCommit, err)
		}
	}
	log.Printf("overCommitPercent: %d\n", overCommitPercent)

	var inLogLevel string
	var level slog.Level
	if os.Getenv(EnvLogLevel) == "" {
		inLogLevel = "INFO"
	}
	if err := level.UnmarshalText([]byte(inLogLevel)); err != nil {
		return nil, nil, nil, 0, -1, 0, nil, "", fmt.Errorf("failed to parse log level (%s): %w", inLogLevel, err)
	}

	schedulerAddress := os.Getenv(EnvLXDSchedulerAddress)
	if schedulerAddress == "" {
		slog.Warn("scheduler address is not set, using default scheduler (very simple scheduler)")
	}

	return hostConfigs, m, imageAliasMap, periodSec, port, overCommitPercent, &level, schedulerAddress, nil
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
