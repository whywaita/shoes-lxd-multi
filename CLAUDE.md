# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Architecture

shoes-lxd-multi is a [myshoes](https://github.com/whywaita/myshoes) provider for [LXD](https://linuxcontainers.org/lxd/) in multi-server environments. The project consists of four main components:

### Core Components

1. **shoes-lxd-multi** (`/shoes-lxd-multi/`): The main shoes-provider plugin that integrates with myshoes
2. **server** (`/server/`): gRPC server that manages LXD instances across multiple hosts
3. **pool-agent** (`/pool-agent/`): Stadium agent for pool mode that pre-creates containers
4. **scheduler** (`/scheduler/`): Resource scheduler with Redis-based shared storage

### Communication Flow

```
myshoes → shoes-lxd-multi → server (gRPC) → LXD hosts
                                ↓
pool-agent → LXD (creates container pools)
                                ↓
scheduler → Redis (shared resource state)
```

### Key Dependencies

- **LXD Client**: Uses `github.com/lxc/lxd` for container management
- **gRPC**: Inter-service communication via protocol buffers (`/proto/`)
- **Prometheus**: Metrics collection in server and scheduler
- **Redis**: Shared storage backend for scheduler
- **TOML**: Configuration format for pool-agent

## Development Commands

### Building Components

Each component is a separate Go module. Build from component directories:

```bash
# Build server
cd server && go build -o server ./main.go

# Build scheduler  
cd scheduler && go build -o scheduler ./main.go

# Build pool-agent
cd pool-agent && go build -o pool-agent ./main.go

# Build shoes-provider
cd shoes-lxd-multi && go build -o shoes-lxd-multi ./main.go
```

### Testing

Run tests for each component:

```bash
# Test all components
cd server && go test -v ./...
cd scheduler && go test -v ./...  
cd pool-agent && go test -v ./...
cd shoes-lxd-multi && go test -v ./...

# Run linting (uses staticcheck)
cd <component> && staticcheck ./...

# Run go vet
cd <component> && go vet ./...
```

### Protocol Buffers

Regenerate gRPC code when modifying `.proto` files:

```bash
./proto.sh
```

This script:
- Generates Go code from `.proto` files in `/proto/`
- Outputs to `/proto.go/` module
- Requires `protoc` with Go plugins installed

### Docker

Build Docker images for server and scheduler:

```bash
cd server && docker build .
cd scheduler && docker build .
```

## Configuration

### Server Configuration

- **LXD_MULTI_HOSTS**: JSON array of LXD host configurations with client certificates
- **LXD_MULTI_RESOURCE_TYPE_MAPPING**: JSON mapping of resource types to CPU/memory
- **LXD_MULTI_IMAGE_ALIAS_MAPPING**: JSON mapping of image names to aliases
- **LXD_MULTI_PORT**: gRPC server port (default: 8080)
- **LXD_MULTI_OVER_COMMIT_PERCENT**: CPU over-commit percentage (default: 100)
- **LXD_MULTI_SCHEDULER_ADDRESS**: Scheduler service URL (optional, enables intelligent host selection)

### Pool Agent Configuration

- Uses TOML configuration file (default: `/etc/pool-agent/config.toml`)
- Defines resource types and image configurations per Ubuntu version
- Environment variables: `LXD_MULTI_CHECK_INTERVAL`, `LXD_MULTI_WAIT_IDLE_TIME`, `LXD_MULTI_ZOMBIE_ALLOW_TIME`

### Scheduler Configuration

- **REDIS_ADDR**: Redis server address (default: localhost:6379)
- **LXD_MULTI_SCHEDULER_FETCH_INTERVAL_SECOND**: Resource fetch interval
- **PORT**: HTTP server port (default: 8080)

## Metrics

Both server and scheduler expose Prometheus metrics on port 9090 (`/metrics` endpoint). Pool-agent writes metrics to node_exporter textfile collector format.