# Pool Agent

Stadium Agent for pool mode

## Setup

configuration file format is toml

```toml
["https://127.0.0.1:8443"]
    cert_path = "cert.pem"
    key_path = "key.pem"
    [["https://127.0.0.1:8443".resource_types]]
    name = "nano"
    cpu = 1
    memory = "1GB"
    count = 3
    [["https://127.0.0.1:8443".resource_types]]
    name = "micro"
    cpu = 2
    memory = "2GB"
    count = 1
```

- `LXD_MULTI_IMAGE_ALIAS`
  - Image to pool

### Optional values

- `LXD_MULTI_CHECK_INTERVAL`
    - Interval to check instances
    - default: `2s`
- `LXD_MULTI_CONCURRENT_CREATE_LIMIT`
    - Limit concurrency for creating instance
    - default: `3`
- `LXD_MULTI_WAIT_IDLE_TIME`
    - Duration to wait instance idle after `systemctl is-system-running --wait`
    - default: `5s`
- `LXD_MULTI_ZOMBIE_ALLOW_TIME`
    - Duration to delete zombie instances after instance created
    - default: `5m`
