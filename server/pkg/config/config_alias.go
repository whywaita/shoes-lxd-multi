package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/lxc/lxd/shared/api"
)

// parseAlias parse user input
func parseAlias(input string) (*api.InstanceSource, error) {
	if strings.EqualFold(input, "") {
		// default value is ubuntu:bionic
		return &api.InstanceSource{
			Type: "image",
			Properties: map[string]string{
				"os":      "ubuntu",
				"release": "bionic",
			},
		}, nil
	}

	if strings.HasPrefix(input, "http") {
		// https://<FQDN or IP>:8443/<alias>
		u, err := url.Parse(input)
		if err != nil {
			return nil, fmt.Errorf("failed to parse alias: %w", err)
		}

		urlImageServer := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		alias := strings.TrimPrefix(u.Path, "/")

		return &api.InstanceSource{
			Type:   "image",
			Mode:   "pull",
			Server: urlImageServer,
			Alias:  alias,
		}, nil
	}

	return &api.InstanceSource{
		Type:  "image",
		Alias: input,
	}, nil
}
