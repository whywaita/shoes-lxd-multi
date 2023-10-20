package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/lxd/shared/api"
	"github.com/pelletier/go-toml/v2"
)

// ConfigMap is config map for pool agent.
type ConfigMap map[string]struct {
	ImageAlias   string         `toml:"image_alias"`
	ResouceTypes []ResourceType `toml:"resource_types"`
	CertPath     string         `toml:"cert_path"`
	KeyPath      string         `toml:"key_path"`
}

// ResourceType is resource configuration for pool mode.
type ResourceType struct {
	Name string `toml:"name"`

	CPUCore int    `toml:"cpu"`
	Memory  string `toml:"memory"`

	PoolCount int `toml:"count"`
}

// LoadConfig LoadConfig loads config from configPath
func LoadConfig() (ConfigMap, error) {
	f, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed read config file: %w", err)
	}
	var s ConfigMap
	if err := toml.Unmarshal(f, &s); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	return s, nil
}

// ParseImageAlias parses LXD image alias.
func ParseImageAlias(s string) (api.InstanceSource, error) {
	if s == "" {
		// default value is ubuntu:bionic
		return api.InstanceSource{
			Type: "image",
			Properties: map[string]string{
				"os":      "ubuntu",
				"release": "bionic",
			},
		}, nil
	}

	if !strings.HasPrefix(s, "http") {
		return api.InstanceSource{
			Type:  "image",
			Alias: s,
		}, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return api.InstanceSource{}, fmt.Errorf("parse url: %w", err)
	}
	return api.InstanceSource{
		Type:   "image",
		Mode:   "pull",
		Server: fmt.Sprintf("%s://%s", u.Scheme, u.Host),
		Alias:  strings.TrimPrefix(u.Path, "/"),
	}, nil
}

// LoadImageAlias loads image alias from environment variable "LXD_MULTI_IMAGE_ALIAS".
func LoadImageAlias() (string, api.InstanceSource, error) {
	env := os.Getenv("LXD_MULTI_IMAGE_ALIAS")
	if env == "" {
		return "", api.InstanceSource{}, fmt.Errorf("LXD_MULTI_IMAGE_ALIAS is not set")
	}
	source, err := ParseImageAlias(env)
	if err != nil {
		return "", api.InstanceSource{}, fmt.Errorf("parse LXD_MULTI_IMAGE_ALIAS: %w", err)
	}
	return env, source, nil
}

// LoadParams loads parameters for pool agent.
func LoadParams() (checkInterval time.Duration, concurrentCreateLimit int64, waitIdleTime time.Duration, zombieAllowTime time.Duration, err error) {
	checkInterval, err = loadDurationEnv("LXD_MULTI_CHECK_INTERVAL", 2*time.Second)
	if err != nil {
		return
	}
	waitIdleTime, err = loadDurationEnv("LXD_MULTI_WAIT_IDLE_TIME", 5*time.Second)
	if err != nil {
		return
	}
	zombieAllowTime, err = loadDurationEnv("LXD_MULTI_ZOMBIE_ALLOW_TIME", 5*time.Minute)
	if err != nil {
		return
	}

	if env := os.Getenv("LXD_MULTI_CONCURRENT_CREATE_LIMIT"); env != "" {
		concurrentCreateLimit, err = strconv.ParseInt(env, 10, 64)
		if err != nil {
			return
		}
	} else {
		concurrentCreateLimit = 3
	}

	return
}

func loadDurationEnv(name string, def time.Duration) (time.Duration, error) {
	env := os.Getenv(name)
	if env == "" {
		return def, nil
	}
	d, err := time.ParseDuration(env)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return d, nil
}
