package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/lxd/shared/api"
)

type ResourceType struct {
	Name string `json:"name"`

	CPUCore int    `json:"cpu"`
	Memory  string `json:"memory"`

	PoolCount int `json:"count"`
}

func LoadResourceTypes() ([]ResourceType, error) {
	env := os.Getenv("LXD_MULTI_RESOURCE_TYPES")
	if env == "" {
		return nil, fmt.Errorf("LXD_MULTI_RESOURCE_TYPES is not set")
	}
	var s []ResourceType
	if err := json.Unmarshal([]byte(env), &s); err != nil {
		return nil, fmt.Errorf("parse LXD_MULTI_RESOURCE_TYPES: %w", err)
	}
	return s, nil
}

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
