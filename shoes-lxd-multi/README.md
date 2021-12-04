# shoes-provider

shoes-provider implementation for shoes-lxd-multi

## Setup

- `LXD_MULTI_TARGET_HOSTS`: List of target hosts
    - Set endpoint of LXD API e.g.) `https://192.0.2.100:8443`
    - Require same value in `host` from Server-side

```bash
[
  "https://192.0.2.100:8443",
  "https://192.0.2.101:8443",
  "https://192.0.2.102:8443",
  ...  
]
```

- `LXD_MULTI_SERVER_ENDPOINT`: Endpoint of Server-side Application 