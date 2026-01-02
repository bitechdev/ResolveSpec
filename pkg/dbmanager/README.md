# Database Connection Manager (dbmanager)

A comprehensive database connection manager for Go that provides centralized management of multiple named database connections with support for PostgreSQL, SQLite, MSSQL, and MongoDB.

## Features

- **Multiple Named Connections**: Manage multiple database connections with names like `primary`, `analytics`, `cache-db`
- **Multi-Database Support**: PostgreSQL, SQLite, Microsoft SQL Server, and MongoDB
- **Multi-ORM Access**: Each SQL connection provides access through:
  - **Bun ORM** - Modern, lightweight ORM
  - **GORM** - Popular Go ORM
  - **Native** - Standard library `*sql.DB`
  - All three share the same underlying connection pool
- **Configuration-Driven**: YAML configuration with Viper integration
- **Production-Ready Features**:
  - Automatic health checks and reconnection
  - Prometheus metrics
  - Connection pooling with configurable limits
  - Retry logic with exponential backoff
  - Graceful shutdown
  - OpenTelemetry tracing support

## Installation

```bash
go get github.com/bitechdev/ResolveSpec/pkg/dbmanager
```

## Quick Start

### 1. Configuration

Create a configuration file (e.g., `config.yaml`):

```yaml
dbmanager:
  default_connection: "primary"

  # Global connection pool defaults
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 30m
  conn_max_idle_time: 5m

  # Retry configuration
  retry_attempts: 3
  retry_delay: 1s
  retry_max_delay: 10s

  # Health checks
  health_check_interval: 30s
  enable_auto_reconnect: true

  connections:
    # Primary PostgreSQL connection
    primary:
      type: postgres
      host: localhost
      port: 5432
      user: myuser
      password: mypassword
      database: myapp
      sslmode: disable
      default_orm: bun
      enable_metrics: true
      enable_tracing: true
      enable_logging: true

    # Read replica for analytics
    analytics:
      type: postgres
      dsn: "postgres://readonly:pass@analytics:5432/analytics"
      default_orm: bun
      enable_metrics: true

    # SQLite cache
    cache-db:
      type: sqlite
      filepath: /var/lib/app/cache.db
      max_open_conns: 1

    # MongoDB for documents
    documents:
      type: mongodb
      host: localhost
      port: 27017
      database: documents
      user: mongouser
      password: mongopass
      auth_source: admin
      enable_metrics: true
```

### 2. Initialize Manager

```go
package main

import (
    "context"
    "log"

    "github.com/bitechdev/ResolveSpec/pkg/config"
    "github.com/bitechdev/ResolveSpec/pkg/dbmanager"
)

func main() {
    // Load configuration
    cfgMgr := config.NewManager()
    if err := cfgMgr.Load(); err != nil {
        log.Fatal(err)
    }
    cfg, _ := cfgMgr.GetConfig()

    // Create database manager
    mgr, err := dbmanager.NewManager(cfg.DBManager)
    if err != nil {
        log.Fatal(err)
    }
    defer mgr.Close()

    // Connect all databases
    ctx := context.Background()
    if err := mgr.Connect(ctx); err != nil {
        log.Fatal(err)
    }

    // Your application code here...
}
```

### 3. Use Database Connections

#### Get Default Database

```go
// Get the default database (as configured common.Database interface)
db, err := mgr.GetDefaultDatabase()
if err != nil {
    log.Fatal(err)
}

// Use it with any query
var users []User
err = db.NewSelect().
    Model(&users).
    Where("active = ?", true).
    Scan(ctx, &users)
```

#### Get Named Connection with Specific ORM

```go
// Get primary connection
primary, err := mgr.Get("primary")
if err != nil {
    log.Fatal(err)
}

// Use with Bun
bunDB, err := primary.Bun()
if err != nil {
    log.Fatal(err)
}
err = bunDB.NewSelect().Model(&users).Scan(ctx)

// Use with GORM (same underlying connection!)
gormDB, err := primary.GORM()
if err != nil {
    log.Fatal(err)
}
gormDB.Where("active = ?", true).Find(&users)

// Use native *sql.DB
nativeDB, err := primary.Native()
if err != nil {
    log.Fatal(err)
}
rows, err := nativeDB.QueryContext(ctx, "SELECT * FROM users WHERE active = $1", true)
```

#### Use MongoDB

```go
// Get MongoDB connection
docs, err := mgr.Get("documents")
if err != nil {
    log.Fatal(err)
}

mongoClient, err := docs.MongoDB()
if err != nil {
    log.Fatal(err)
}

collection := mongoClient.Database("documents").Collection("articles")
// Use MongoDB driver...
```

#### Change Default Database

```go
// Switch to analytics database as default
err := mgr.SetDefaultDatabase("analytics")
if err != nil {
    log.Fatal(err)
}

// Now GetDefaultDatabase() returns the analytics connection
db, _ := mgr.GetDefaultDatabase()
```

## Configuration Reference

### Manager Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default_connection` | string | "" | Name of the default connection |
| `connections` | map | {} | Map of connection name to ConnectionConfig |
| `max_open_conns` | int | 25 | Global default for max open connections |
| `max_idle_conns` | int | 5 | Global default for max idle connections |
| `conn_max_lifetime` | duration | 30m | Global default for connection max lifetime |
| `conn_max_idle_time` | duration | 5m | Global default for connection max idle time |
| `retry_attempts` | int | 3 | Number of connection retry attempts |
| `retry_delay` | duration | 1s | Initial retry delay |
| `retry_max_delay` | duration | 10s | Maximum retry delay |
| `health_check_interval` | duration | 30s | Interval between health checks |
| `enable_auto_reconnect` | bool | true | Auto-reconnect on health check failure |

### Connection Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique connection name |
| `type` | string | Database type: `postgres`, `sqlite`, `mssql`, `mongodb` |
| `dsn` | string | Complete connection string (overrides individual params) |
| `host` | string | Database host |
| `port` | int | Database port |
| `user` | string | Username |
| `password` | string | Password |
| `database` | string | Database name |
| `sslmode` | string | SSL mode (postgres/mssql): `disable`, `require`, etc. |
| `schema` | string | Default schema (postgres/mssql) |
| `filepath` | string | File path (sqlite only) |
| `auth_source` | string | Auth source (mongodb) |
| `replica_set` | string | Replica set name (mongodb) |
| `read_preference` | string | Read preference (mongodb): `primary`, `secondary`, etc. |
| `max_open_conns` | int | Override global max open connections |
| `max_idle_conns` | int | Override global max idle connections |
| `conn_max_lifetime` | duration | Override global connection max lifetime |
| `conn_max_idle_time` | duration | Override global connection max idle time |
| `connect_timeout` | duration | Connection timeout (default: 10s) |
| `query_timeout` | duration | Query timeout (default: 30s) |
| `enable_tracing` | bool | Enable OpenTelemetry tracing |
| `enable_metrics` | bool | Enable Prometheus metrics |
| `enable_logging` | bool | Enable connection logging |
| `default_orm` | string | Default ORM for Database(): `bun`, `gorm`, `native` |
| `tags` | map[string]string | Custom tags for filtering/organization |

## Advanced Usage

### Health Checks

```go
// Manual health check
if err := mgr.HealthCheck(ctx); err != nil {
    log.Printf("Health check failed: %v", err)
}

// Per-connection health check
primary, _ := mgr.Get("primary")
if err := primary.HealthCheck(ctx); err != nil {
    log.Printf("Primary connection unhealthy: %v", err)

    // Manual reconnect
    if err := primary.Reconnect(ctx); err != nil {
        log.Printf("Reconnection failed: %v", err)
    }
}
```

### Connection Statistics

```go
// Get overall statistics
stats := mgr.Stats()
fmt.Printf("Total connections: %d\n", stats.TotalConnections)
fmt.Printf("Healthy: %d, Unhealthy: %d\n", stats.HealthyCount, stats.UnhealthyCount)

// Per-connection stats
for name, connStats := range stats.ConnectionStats {
    fmt.Printf("%s: %d open, %d in use, %d idle\n",
        name,
        connStats.OpenConnections,
        connStats.InUse,
        connStats.Idle)
}

// Individual connection stats
primary, _ := mgr.Get("primary")
stats := primary.Stats()
fmt.Printf("Wait count: %d, Wait duration: %v\n",
    stats.WaitCount,
    stats.WaitDuration)
```

### Prometheus Metrics

The package automatically exports Prometheus metrics:

- `dbmanager_connections_total` - Total configured connections by type
- `dbmanager_connection_status` - Connection health status (1=healthy, 0=unhealthy)
- `dbmanager_connection_pool_size` - Connection pool statistics by state
- `dbmanager_connection_wait_count` - Times connections waited for availability
- `dbmanager_connection_wait_duration_seconds` - Total wait duration
- `dbmanager_health_check_duration_seconds` - Health check execution time
- `dbmanager_reconnect_attempts_total` - Reconnection attempts and results
- `dbmanager_connection_lifetime_closed_total` - Connections closed due to max lifetime
- `dbmanager_connection_idle_closed_total` - Connections closed due to max idle time

Metrics are automatically updated during health checks. To manually publish metrics:

```go
if mgr, ok := mgr.(*connectionManager); ok {
    mgr.PublishMetrics()
}
```

## Architecture

### Single Connection Pool, Multiple ORMs

A key design principle is that Bun, GORM, and Native all wrap the **same underlying `*sql.DB`** connection pool:

```
┌─────────────────────────────────────┐
│        SQL Connection               │
├─────────────────────────────────────┤
│  ┌─────────┐  ┌──────┐  ┌────────┐ │
│  │   Bun   │  │ GORM │  │ Native │ │
│  └────┬────┘  └───┬──┘  └───┬────┘ │
│       │           │         │      │
│       └───────────┴─────────┘      │
│              *sql.DB                │
│         (single pool)               │
└─────────────────────────────────────┘
```

**Benefits:**
- No connection duplication
- Consistent pool limits across all ORMs
- Unified connection statistics
- Lower resource usage

### Provider Pattern

Each database type has a dedicated provider:

- **PostgresProvider** - Uses `pgx` driver
- **SQLiteProvider** - Uses `glebarez/sqlite` (pure Go)
- **MSSQLProvider** - Uses `go-mssqldb`
- **MongoProvider** - Uses official `mongo-driver`

Providers handle:
- Connection establishment with retry logic
- Health checking
- Connection statistics
- Connection cleanup

## Best Practices

1. **Use Named Connections**: Be explicit about which database you're accessing
   ```go
   primary, _ := mgr.Get("primary")    // Good
   db, _ := mgr.GetDefaultDatabase()   // Risky if default changes
   ```

2. **Configure Connection Pools**: Tune based on your workload
   ```yaml
   connections:
     primary:
       max_open_conns: 100  # High traffic API
       max_idle_conns: 25
     analytics:
       max_open_conns: 10   # Background analytics
       max_idle_conns: 2
   ```

3. **Enable Health Checks**: Catch connection issues early
   ```yaml
   health_check_interval: 30s
   enable_auto_reconnect: true
   ```

4. **Use Appropriate ORM**: Choose based on your needs
   - **Bun**: Modern, fast, type-safe - recommended for new code
   - **GORM**: Mature, feature-rich - good for existing GORM code
   - **Native**: Maximum control - use for performance-critical queries

5. **Monitor Metrics**: Watch connection pool utilization
   - If `wait_count` is high, increase `max_open_conns`
   - If `idle` is always high, decrease `max_idle_conns`

## Troubleshooting

### Connection Failures

If connections fail to establish:

1. Check configuration:
   ```bash
   # Test connection manually
   psql -h localhost -U myuser -d myapp
   ```

2. Enable logging:
   ```yaml
   connections:
     primary:
       enable_logging: true
   ```

3. Check retry attempts:
   ```yaml
   retry_attempts: 5  # Increase retries
   retry_max_delay: 30s
   ```

### Pool Exhaustion

If you see "too many connections" errors:

1. Increase pool size:
   ```yaml
   max_open_conns: 50  # Increase from default 25
   ```

2. Reduce connection lifetime:
   ```yaml
   conn_max_lifetime: 15m  # Recycle faster
   ```

3. Monitor wait stats:
   ```go
   stats := primary.Stats()
   if stats.WaitCount > 1000 {
       log.Warn("High connection wait count")
   }
   ```

### MongoDB vs SQL Confusion

MongoDB connections don't support SQL ORMs:

```go
docs, _ := mgr.Get("documents")

// ✓ Correct
mongoClient, _ := docs.MongoDB()

// ✗ Error: ErrNotSQLDatabase
bunDB, err := docs.Bun()  // Won't work!
```

SQL connections don't support MongoDB:

```go
primary, _ := mgr.Get("primary")

// ✓ Correct
bunDB, _ := primary.Bun()

// ✗ Error: ErrNotMongoDB
mongoClient, err := primary.MongoDB()  // Won't work!
```

## Migration Guide

### From Raw `database/sql`

Before:
```go
db, err := sql.Open("postgres", dsn)
defer db.Close()

rows, err := db.Query("SELECT * FROM users")
```

After:
```go
mgr, _ := dbmanager.NewManager(cfg.DBManager)
mgr.Connect(ctx)
defer mgr.Close()

primary, _ := mgr.Get("primary")
nativeDB, _ := primary.Native()

rows, err := nativeDB.Query("SELECT * FROM users")
```

### From Direct Bun/GORM

Before:
```go
sqldb, _ := sql.Open("pgx", dsn)
bunDB := bun.NewDB(sqldb, pgdialect.New())

var users []User
bunDB.NewSelect().Model(&users).Scan(ctx)
```

After:
```go
mgr, _ := dbmanager.NewManager(cfg.DBManager)
mgr.Connect(ctx)

primary, _ := mgr.Get("primary")
bunDB, _ := primary.Bun()

var users []User
bunDB.NewSelect().Model(&users).Scan(ctx)
```

## License

Same as the parent project.

## Contributing

Contributions are welcome! Please submit issues and pull requests to the main repository.
