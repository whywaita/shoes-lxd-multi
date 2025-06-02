# Server

Server-side implementation for shoes-lxd-multi

## Setup

- `LXD_MULTI_HOSTS`

```json
[
  {
    "host": "https://192.0.2.100:8443",
    "client_cert": "./node1/client.crt",
    "client_key": "./node1/client.key"
  },
  ...
]
```

### Optional values

- `LXD_MULTI_RESOURCE_TYPE_MAPPING`
    - mapping `resource_type` and CPU / Memory.
    - need JSON format. keys is `resource_type_name`, `cpu`, `memory`.
    - e.g.) `[{"resource_type_name": "nano", "cpu": 1, "memory": "1GB"}, {"resource_type_name": "micro", "cpu": 2, "memory": "2GB"}]`
    - become no limit if not set resource_type.
- `LXD_MULTI_PORT`
    - Port of listen gRPC Server
    - default: `8080`
- `LXD_MULTI_OVER_COMMIT_PERCENT`
    - Percent of able over commit in CPU
    - default: `100`
- `LXD_MULTI_RESOURCE_CACHE_PERIOD_SEC`
    - Period of cache resource in seconds
    - default: `10`
- `LXD_MULTI_LOG_LEVEL`
    - Log level (`debug`, `info`, `warn`, `error`, `fatal`, `panic`) will set to `log/slog.Level`
    - default: `info`
- `LXD_MULTI_IMAGE_ALIAS_MAPPING`
    - default: fallback to `LXD_MULTI_IMAGE_ALIAS`, for backward compatibility.
    - must be in JSON format as `{"<image_name>": "<image_alias>"}`.
        - `default` must be set.
    - e.g.) `{"default": "2004", "latest": "2404", "2004": "ubuntu:focal", "2404": "ubuntu:noble"}`
        - supports recursive resolution
            - in this case:
                - `default` is `2004`
                - `2004` resolves to `ubuntu:focal`
                - `latest` is `2404`, which resolves to `ubuntu:noble`
- `LXD_MULTI_IMAGE_ALIAS`
    - moved from shoes-lxd-multi plugin, for backward compatibility.
    - set runner image alias
    - default: `ubuntu:bionic`
    - e.g.) for remote image server: `https://192.0.2.110:8443/ubuntu-custom`


## Note
LXD Server can't use `zfs` in storageclass if use `--privileged`. ref: https://discuss.linuxcontainers.org/t/docker-with-overlay-driver-in-lxd-cluster-not-working/9243

We recommend using `btrfs`.
