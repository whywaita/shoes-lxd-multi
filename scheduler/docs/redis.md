## Redis Data Layout & TTLs

This project stores runtime state in Redis using simple key/value pairs. All values are JSON.

### Keys and Value Formats

- **LXD resources**
  - **Key pattern:** `shoes:resource:<lxdAPIAddress>`
  - **Value:** JSON object describing the node and its resources  
    **Example:**
    ```json
    {
      "Hostname": "example-host",
      "Resource": {
        "CPUTotal": 8,
        "CPUUsed": 2,
        "MemoryTotal": 16384,
        "MemoryUsed": 4096,
        "Instances": ["instance1", "instance2"]
      }
    }
    ```

- **Scheduled resources**
  - **Key pattern:** `shoes:resource:scheduled:<lxdAPIAddress>`
  - **Value:** JSON array of scheduled resource reservations  
    **Example:**
    ```json
    [
      { "cpu": 1, "memory": 1024, "time": "2023-10-01T12:34:56Z" },
      { "cpu": 2, "memory": 2048, "time": "2023-10-01T12:35:00Z" }
    ]
    ```

- **Locks**
  - **Key pattern:** `shoes:resource:<lxdAPIAddress>:lock`
  - **Value:** string `"locked"` with a TTL set  
  - Used to prevent concurrent updates on the same resource.

### TTL Settings

- **LXD resources:** 24 hours  
  Defined as `ResourceTTL` in `storage_redis.go`. Applied in `SetResource`.

- **Scheduled resources:** 2 minutes  
  Defined as `ScheduledResourceTTL` in `scheduler.go`. Applied in `storeScheduledResource`.

- **Locks:** 30 seconds  
  Defined as `LockTTL` in `storage_redis.go`. Applied in `TryLock`.

These TTLs ensure data freshness and limit stale entries in Redis.
