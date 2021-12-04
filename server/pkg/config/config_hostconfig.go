package config

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// HostConfigMap is mapping of HostConfig
type HostConfigMap struct {
	s sync.Map
}

var ErrHostNotFound = fmt.Errorf("host config is not found")

func NewHostConfigMap() *HostConfigMap {
	return &HostConfigMap{}
}

func (s *HostConfigMap) Store(lxdAPIAddress string, hostConfig HostConfig) {
	s.s.Store(lxdAPIAddress, hostConfig)
}

func (s *HostConfigMap) Load(lxdAPIAddress string) (*HostConfig, error) {
	v, ok := s.s.Load(lxdAPIAddress)
	if !ok {
		return nil, ErrHostNotFound
	}

	t, ok := v.(HostConfig)
	if !ok {
		return nil, fmt.Errorf("invalid type in storad")
	}

	return &t, nil
}

func (s *HostConfigMap) Range(f func(key string, value HostConfig) bool) {
	s.s.Range(func(key, value interface{}) bool {
		k := key.(string)
		v := value.(HostConfig)

		return f(k, v)
	})
}

// HostConfig is config of lxd host
type HostConfig struct {
	Cert tls.Certificate

	LxdHostName   string
	LxdHost       string
	LxdClientCert string
	LxdClientKey  string
}

func loadHostConfigs() (*HostConfigMap, error) {
	type multiNode struct {
		IPAddress  string `json:"host"`
		ClientCert string `json:"client_cert"`
		ClientKey  string `json:"client_key"`
	}

	multiNodeJSON := os.Getenv(EnvLXDHosts)
	var mn []multiNode

	if err := json.Unmarshal([]byte(multiNodeJSON), &mn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s: %w", EnvLXDHosts, err)
	}

	hostConfigs := NewHostConfigMap()
	for _, node := range mn {
		host, err := newHostConfig(node.IPAddress, node.ClientCert, node.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create hostConfig: %w", err)
		}

		hostConfigs.Store(node.IPAddress, *host)
	}

	return hostConfigs, nil
}

func newHostConfig(ip, pathCert, pathKey string) (*HostConfig, error) {
	var host HostConfig

	host.LxdHost = ip

	lxdClientCert, err := os.ReadFile(pathCert)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", pathCert, err)
	}
	lxdClientKey, err := os.ReadFile(pathKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", pathKey, err)
	}

	host.LxdClientCert = string(lxdClientCert)
	host.LxdClientKey = string(lxdClientKey)

	return &host, nil
}
