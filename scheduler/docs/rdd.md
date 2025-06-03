# Requirements Definition Document (RDD)

## Component Name: shoes-lxd-multi-scheduler

## 1. Overview

This component provides centralized scheduling logic for the [whywaita/shoes-lxd-multi](https://github.com/whywaita/shoes-lxd-multi) system, which manages multiple LXD servers. The scheduler aims to ensure load-balanced and conflict-free container placement by delegating scheduling responsibility to an external, stateless service.

## 2. Background and Motivation

Currently, each `server` process independently performs scheduling logic, which introduces several issues:

* **Conflict**: Multiple processes may schedule to the same node simultaneously.
* **Suboptimal load balancing**: Local decision-making does not ensure global efficiency.
* **Tight coupling**: Scheduling logic is embedded within each server, making maintenance and upgrades difficult.

By introducing an external scheduler, we achieve:

* Centralized decision-making
* Better load distribution across nodes
* Improved extensibility for future enhancements

## 3. Scheduler Specifications

### 3.1 Scheduling Algorithm (Initial)

The scheduler selects the node **with the fewest total allocated CPU cores**.
The goal is to evenly distribute CPU usage across nodes.

* Inputs include available CPU, memory, and allocated CPU (from LXD API).
* Instance resource requirements must be satisfied (e.g., 2 CPU, 2048 MB memory).

### 3.2 API Design

#### Endpoint

* **HTTP Method**: `POST`
* **Path**: `/schedule`

#### Request Format

```json
{
  "instance_requirements": {
    "cpu": 2,
    "memory": 2048
  }
}
```

#### Response Format

```json
{
  "host": "node-1"
}
```

#### Error Response Example

```json
{
  "error": "no suitable node found"
}
```

### 3.3 Stateless Architecture

The scheduler is designed to be stateless. All decisions are made based solely on input data provided in each HTTP request. Future versions may support state sharing via Redis for high availability.

### 3.4 Configuration

The scheduler can be configured via an environment variables.

- `LXD_MULTI_SCHEDULER_PORT`: Port on which the scheduler listens (default: `8080`)
- `LXD_MULTI_HOSTS`: JSON array of LXD server addresses. It's a same in `server` process.

## 4. Technical Specifications

* **Language**: Go
* **Framework**: `net/http`
* **Data Format**: JSON
* **Logging**: `log/slog`
* **Test Coverage**: Unit and integration tests included

## 5. Deployment

* Distributed as a single static Go binary
* Docker image provided
* Configuration file support (optional)

## 6. Extensibility

Planned future enhancements include:

* Pluggable scheduling strategies
* Redis-based state coordination for HA
* Authentication and access control (e.g., API keys, mTLS)