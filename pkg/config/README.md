# ResolveSpec Configuration System

A centralized configuration system with support for multiple configuration sources: config files (YAML, TOML, JSON), environment variables, and programmatic configuration.

## Features

- **Multiple Config Sources**: Config files, environment variables, and code
- **Priority Order**: Environment variables > Config file > Defaults
- **Multiple Formats**: YAML, TOML, JSON supported
- **Type Safety**: Strongly-typed configuration structs
- **Sensible Defaults**: Works out of the box with reasonable defaults

## Quick Start

### Basic Usage

```go
import "github.com/heinhel/ResolveSpec/pkg/config"

// Create a new config manager
mgr := config.NewManager()

// Load configuration from file and environment
if err := mgr.Load(); err != nil {
    log.Fatal(err)
}

// Get the complete configuration
cfg, err := mgr.GetConfig()
if err != nil {
    log.Fatal(err)
}

// Use the configuration
fmt.Println("Server address:", cfg.Server.Addr)
```

### Custom Configuration Paths

```go
mgr := config.NewManagerWithOptions(
    config.WithConfigFile("/path/to/config.yaml"),
    config.WithEnvPrefix("MYAPP"),
)
```

## Configuration Sources

### 1. Config Files

Place a `config.yaml` file in one of these locations:
- Current directory (`.`)
- `./config/`
- `/etc/resolvespec/`
- `$HOME/.resolvespec/`

Example `config.yaml`:

```yaml
server:
  addr: ":8080"
  shutdown_timeout: 30s

tracing:
  enabled: true
  service_name: "my-service"

cache:
  provider: "redis"
  redis:
    host: "localhost"
    port: 6379
```

### 2. Environment Variables

All configuration can be set via environment variables with the `RESOLVESPEC_` prefix:

```bash
export RESOLVESPEC_SERVER_ADDR=":9090"
export RESOLVESPEC_TRACING_ENABLED=true
export RESOLVESPEC_CACHE_PROVIDER=redis
export RESOLVESPEC_CACHE_REDIS_HOST=localhost
```

Nested configuration uses underscores:
- `server.addr` → `RESOLVESPEC_SERVER_ADDR`
- `cache.redis.host` → `RESOLVESPEC_CACHE_REDIS_HOST`

### 3. Programmatic Configuration

```go
mgr := config.NewManager()
mgr.Set("server.addr", ":9090")
mgr.Set("tracing.enabled", true)

cfg, _ := mgr.GetConfig()
```

## Configuration Options

### Server Configuration

```yaml
server:
  addr: ":8080"              # Server address
  shutdown_timeout: 30s       # Graceful shutdown timeout
  drain_timeout: 25s          # Connection drain timeout
  read_timeout: 10s           # HTTP read timeout
  write_timeout: 10s          # HTTP write timeout
  idle_timeout: 120s          # HTTP idle timeout
```

### Tracing Configuration

```yaml
tracing:
  enabled: false                                  # Enable/disable tracing
  service_name: "resolvespec"                     # Service name
  service_version: "1.0.0"                        # Service version
  endpoint: "http://localhost:4318/v1/traces"     # OTLP endpoint
```

### Cache Configuration

```yaml
cache:
  provider: "memory"  # Options: memory, redis, memcache

  redis:
    host: "localhost"
    port: 6379
    password: ""
    db: 0

  memcache:
    servers:
      - "localhost:11211"
    max_idle_conns: 10
    timeout: 100ms
```

### Logger Configuration

```yaml
logger:
  dev: false    # Development mode (human-readable output)
  path: ""      # Log file path (empty = stdout)
```

### Middleware Configuration

```yaml
middleware:
  rate_limit_rps: 100.0       # Requests per second
  rate_limit_burst: 200        # Burst size
  max_request_size: 10485760   # Max request size in bytes (10MB)
```

### CORS Configuration

```yaml
cors:
  allowed_origins:
    - "*"
  allowed_methods:
    - "GET"
    - "POST"
    - "PUT"
    - "DELETE"
    - "OPTIONS"
  allowed_headers:
    - "*"
  max_age: 3600
```

### Database Configuration

```yaml
database:
  url: "host=localhost user=postgres password=postgres dbname=mydb port=5432 sslmode=disable"
```

## Priority and Overrides

Configuration sources are applied in this order (highest priority first):

1. **Environment Variables** (highest priority)
2. **Config File**
3. **Defaults** (lowest priority)

This allows you to:
- Set defaults in code
- Override with a config file
- Override specific values with environment variables

## Examples

### Production Setup

```yaml
# config.yaml
server:
  addr: ":8080"

tracing:
  enabled: true
  service_name: "myapi"
  endpoint: "http://jaeger:4318/v1/traces"

cache:
  provider: "redis"
  redis:
    host: "redis"
    port: 6379
    password: "${REDIS_PASSWORD}"

logger:
  dev: false
  path: "/var/log/myapi/app.log"
```

### Development Setup

```bash
# Use environment variables for development
export RESOLVESPEC_LOGGER_DEV=true
export RESOLVESPEC_TRACING_ENABLED=false
export RESOLVESPEC_CACHE_PROVIDER=memory
```

### Testing Setup

```go
// Override config for tests
mgr := config.NewManager()
mgr.Set("cache.provider", "memory")
mgr.Set("database.url", testDBURL)

cfg, _ := mgr.GetConfig()
```

## Best Practices

1. **Use config files for base configuration** - Define your standard settings
2. **Use environment variables for secrets** - Never commit passwords/tokens
3. **Use environment variables for deployment-specific values** - Different per environment
4. **Keep defaults sensible** - Application should work with minimal configuration
5. **Document your configuration** - Comment your config.yaml files

## Integration with ResolveSpec Components

The configuration system integrates seamlessly with ResolveSpec components:

```go
cfg, _ := config.NewManager().Load().GetConfig()

// Server
srv := server.NewGracefulServer(server.Config{
    Addr:            cfg.Server.Addr,
    ShutdownTimeout: cfg.Server.ShutdownTimeout,
    // ... other fields
})

// Tracing
if cfg.Tracing.Enabled {
    tracer := tracing.Init(tracing.Config{
        ServiceName:    cfg.Tracing.ServiceName,
        ServiceVersion: cfg.Tracing.ServiceVersion,
        Endpoint:       cfg.Tracing.Endpoint,
    })
    defer tracer.Shutdown(context.Background())
}

// Cache
var cacheProvider cache.Provider
switch cfg.Cache.Provider {
case "redis":
    cacheProvider = cache.NewRedisProvider(cfg.Cache.Redis.Host, cfg.Cache.Redis.Port, ...)
case "memcache":
    cacheProvider = cache.NewMemcacheProvider(cfg.Cache.Memcache.Servers, ...)
default:
    cacheProvider = cache.NewMemoryProvider()
}

// Logger
logger.Init(cfg.Logger.Dev)
if cfg.Logger.Path != "" {
    logger.UpdateLoggerPath(cfg.Logger.Path, cfg.Logger.Dev)
}
```
