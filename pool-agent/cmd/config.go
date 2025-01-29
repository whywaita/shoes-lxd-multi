package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config is config for pool agent from toml.
type Config struct {
	ResourceTypesMap ResourceTypesMap          `toml:"resource_types_map"`
	ConfigPerImage   map[string]ConfigPerImage `toml:"config"`

	// For backward compatibility, use first version config.
	ImageAlias          string              `toml:"image_alias"`
	ResourceTypesCounts ResourceTypesCounts `toml:"resource_types_counts"`
}

// ConfigPerImage is config map per image.
type ConfigPerImage struct {
	ImageAlias          string              `toml:"image_alias"`
	ResourceTypesCounts ResourceTypesCounts `toml:"resource_types_counts"`
}

type resourceType struct {
	CPUCore int    `toml:"cpu"`
	Memory  string `toml:"memory"`
}

// ResourceTypesMap is resource configuration for pool mode.
type ResourceTypesMap map[string]resourceType

// ResourceTypesCounts is counts of instance by resource types.
type ResourceTypesCounts map[string]int

// LoadConfig loads config from configPath
func LoadConfig() (*Config, error) {
	c, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config from file: %w", err)
	}

	// For backward compatibility, use old format if unset config of image.
	if c.ConfigPerImage == nil {
		slog.Warn("config is not set, use old format")
		if c.ImageAlias == "" {
			return nil, fmt.Errorf("image_alias for old format is not set")
		}

		c.ConfigPerImage = map[string]ConfigPerImage{
			"default": {
				ImageAlias:          c.ImageAlias,
				ResourceTypesCounts: c.ResourceTypesCounts,
			},
		}
	}

	return c, nil
}

func loadConfig() (*Config, error) {
	f, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed read config file: %w", err)
	}
	var s Config
	if err := toml.Unmarshal(f, &s); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	return &s, nil
}

//// LoadImageAlias loads image alias from environment variable "LXD_MULTI_IMAGE_ALIAS".
//func LoadImageAlias() (string, api.InstanceSource, error) {
//	env := os.Getenv("LXD_MULTI_IMAGE_ALIAS")
//	if env == "" {
//		return "", api.InstanceSource{}, fmt.Errorf("LXD_MULTI_IMAGE_ALIAS is not set")
//	}
//	source, err := slm.ParseAlias(env)
//	if err != nil {
//		return "", api.InstanceSource{}, fmt.Errorf("parse LXD_MULTI_IMAGE_ALIAS: %w", err)
//	}
//	return env, *source, nil
//}

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
