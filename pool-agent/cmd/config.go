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
}

type ConfigOld struct {
	ImageAlias          string              `toml:"image_alias"`
	ResourceTypesCounts ResourceTypesCounts `toml:"resource_types_counts"`
	ResourceTypeMap     []resourceTypeOld   `toml:"resource_types_map"`
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

type resourceTypeOld struct {
	Name    string `toml:"name"`
	CPUCore int    `toml:"cpu"`
	Memory  string `toml:"memory"`
}

// ResourceTypesCounts is counts of instance by resource types.
type ResourceTypesCounts map[string]int

// LoadConfig loads config from configPath
func LoadConfig(body []byte) (*Config, error) {
	c, err := loadConfig(body)
	if err != nil {
		// For backward compatibility, use old format if unset config of image.
		cOld, errOld := loadConfigOld(body)
		if errOld != nil {
			slog.Warn("load config (old format) from file", slog.String("err", errOld.Error()))
			return nil, fmt.Errorf("load config from file: %w", err)
		}

		slog.Warn("config is not set, use old format")
		c = cOld.toConfig()
	}

	return c, nil
}

func loadConfig(body []byte) (*Config, error) {
	var s Config
	if err := toml.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	return &s, nil
}

func loadConfigOld(body []byte) (*ConfigOld, error) {
	var s ConfigOld
	if err := toml.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	return &s, nil
}

func (co *ConfigOld) toConfig() *Config {
	r := make(map[string]ConfigPerImage, 1)
	r["default"] = ConfigPerImage{
		ImageAlias:          co.ImageAlias,
		ResourceTypesCounts: co.ResourceTypesCounts,
	}

	m := make(ResourceTypesMap, len(co.ResourceTypeMap))
	for _, rt := range co.ResourceTypeMap {
		m[rt.Name] = resourceType{
			CPUCore: rt.CPUCore,
			Memory:  rt.Memory,
		}
	}

	return &Config{
		ResourceTypesMap: m,
		ConfigPerImage:   r,
	}
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
