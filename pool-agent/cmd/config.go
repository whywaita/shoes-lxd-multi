package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/lxc/lxd/shared/api"
	slm "github.com/whywaita/shoes-lxd-multi/server/pkg/api"
	"github.com/pelletier/go-toml/v2"
)

// Config is config map for pool agent.
type Config struct {
	ImageAlias          string              `toml:"image_alias"`
	ResourceTypesMap    []ResourceTypesMap  `toml:"resource_types_map"`
	ResourceTypesCounts ResourceTypesCounts `toml:"resource_types_counts"`
}

// ResourceTypesMap is resource configuration for pool mode.
type ResourceTypesMap struct {
	Name string `toml:"name"`

	CPUCore int    `toml:"cpu"`
	Memory  string `toml:"memory"`
}

// ResourceTypesCounts is counts for resouce types.
type ResourceTypesCounts map[string]int

// LoadConfig LoadConfig loads config from configPath
func LoadConfig() (Config, error) {
	f, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("failed read config file: %w", err)
	}
	var s Config
	if err := toml.Unmarshal(f, &s); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	return s, nil
}

// LoadImageAlias loads image alias from environment variable "LXD_MULTI_IMAGE_ALIAS".
func LoadImageAlias() (string, api.InstanceSource, error) {
	env := os.Getenv("LXD_MULTI_IMAGE_ALIAS")
	if env == "" {
		return "", api.InstanceSource{}, fmt.Errorf("LXD_MULTI_IMAGE_ALIAS is not set")
	}
	source, err := slm.ParseAlias(env)
	if err != nil {
		return "", api.InstanceSource{}, fmt.Errorf("parse LXD_MULTI_IMAGE_ALIAS: %w", err)
	}
	return env, *source, nil
}

// LoadParams loads parameters for pool agent.
func LoadParams() (time.Duration, time.Duration, time.Duration, error) {
	checkInterval, err := loadDurationEnv("LXD_MULTI_CHECK_INTERVAL", 5*time.Second)
	if err != nil {
		return 0, 0, 0, err
	}
	waitIdleTime, err := loadDurationEnv("LXD_MULTI_WAIT_IDLE_TIME", 5*time.Second)
	if err != nil {
		return 0, 0, 0, err
	}
	zombieAllowTime, err := loadDurationEnv("LXD_MULTI_ZOMBIE_ALLOW_TIME", 5*time.Minute)
	if err != nil {
		return 0, 0, 0, err
	}

	return checkInterval, waitIdleTime, zombieAllowTime, nil
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
