# Resource Scheduling and Management

## Overview

The scheduler component manages resource allocation across LXD hosts. It uses Redis for storing both actual resource usage and scheduled resource allocations.

## Key Components

1. **LXDResourceManager**: Manages the collection and storage of actual resource usage from LXD hosts
2. **Scheduler**: Implements the scheduling algorithm to select hosts for new workloads
3. **RedisStorage**: Provides Redis-based persistence for resources and scheduled resources

## Resource Tracking

Resources are tracked in two categories:

1. **Actual Resources**: Current resource usage of each LXD host
   - Updated periodically by the `LXDResourceManager`
   - Stored in Redis with keys like `shoes:resource:{lxd_api_address}`
   - TTL: 24 hours

2. **Scheduled Resources**: Resources that have been allocated by the scheduler but not yet provisioned
   - Created during the scheduling process
   - Stored in Redis with keys like `shoes:resource:scheduled:{lxd_api_address}`
   - TTL: 2 minutes
   - Filtered out after 1 minute when making scheduling decisions

## Cleanup Process

To prevent resource leaks and stale data:

1. **Automatic Expiration**: Redis TTL ensures resources expire after their configured lifetime
2. **Active Cleanup**: `cleanupScheduledResources` runs every 30 seconds to:
   - Remove stale scheduled resource entries
   - Update scheduled resource entries by removing individual stale allocations
   - Delete completely expired scheduled resource entries

## Locking Mechanism

A distributed locking mechanism prevents race conditions:

1. The scheduler attempts to acquire a lock on a host before finalizing allocation
2. Locks are implemented as Redis keys with a 30-second TTL
3. Lock keys follow the pattern `shoes:resource:{lxd_api_address}:lock`

## Scheduling Algorithm

The scheduling algorithm selects hosts based on:

1. Minimum CPU usage
2. Sufficient available resources (considering both actual and scheduled allocations)
3. Fewest running instances
4. Random selection among equally suitable hosts
