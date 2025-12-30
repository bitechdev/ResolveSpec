# Error Tracking

This package provides error tracking integration for ResolveSpec, with built-in support for Sentry.

## Features

- **Provider Interface**: Flexible design supporting multiple error tracking backends
- **Sentry Integration**: Full-featured Sentry support with automatic error, warning, and panic tracking
- **Automatic Logger Integration**: All `logger.Error()` and `logger.Warn()` calls are automatically sent to the error tracker
- **Panic Tracking**: Automatic panic capture with stack traces
- **NoOp Provider**: Zero-overhead when error tracking is disabled

## Configuration

Add error tracking configuration to your config file:

```yaml
error_tracking:
  enabled: true
  provider: "sentry"  # Currently supports: "sentry" or "noop"
  dsn: "https://your-sentry-dsn@sentry.io/project-id"
  environment: "production"  # e.g., production, staging, development
  release: "v1.0.0"  # Your application version
  debug: false
  sample_rate: 1.0  # Error sample rate (0.0-1.0)
  traces_sample_rate: 0.1  # Traces sample rate (0.0-1.0)
```

## Usage

### Initialization

Initialize error tracking in your application startup:

```go
package main

import (
    "github.com/bitechdev/ResolveSpec/pkg/config"
    "github.com/bitechdev/ResolveSpec/pkg/errortracking"
    "github.com/bitechdev/ResolveSpec/pkg/logger"
)

func main() {
    // Load your configuration
    cfg := config.Config{
        ErrorTracking: config.ErrorTrackingConfig{
            Enabled:     true,
            Provider:    "sentry",
            DSN:         "https://your-sentry-dsn@sentry.io/project-id",
            Environment: "production",
            Release:     "v1.0.0",
            SampleRate:  1.0,
        },
    }

    // Initialize logger
    logger.Init(false)

    // Initialize error tracking
    provider, err := errortracking.NewProviderFromConfig(cfg.ErrorTracking)
    if err != nil {
        logger.Error("Failed to initialize error tracking: %v", err)
    } else {
        logger.InitErrorTracking(provider)
    }

    // Your application code...

    // Cleanup on shutdown
    defer logger.CloseErrorTracking()
}
```

### Automatic Tracking

Once initialized, all logger errors and warnings are automatically sent to the error tracker:

```go
// This will be logged AND sent to Sentry
logger.Error("Database connection failed: %v", err)

// This will also be logged AND sent to Sentry
logger.Warn("Cache miss for key: %s", key)
```

### Panic Tracking

Panics are automatically captured when using the logger's panic handlers:

```go
// Using CatchPanic
defer logger.CatchPanic("MyFunction")()

// Using CatchPanicCallback
defer logger.CatchPanicCallback("MyFunction", func(err any) {
    // Custom cleanup
})()

// Using HandlePanic
defer func() {
    if r := recover(); r != nil {
        err = logger.HandlePanic("MyMethod", r)
    }
}()
```

### Manual Tracking

You can also use the provider directly for custom error tracking:

```go
import (
    "context"
    "github.com/bitechdev/ResolveSpec/pkg/errortracking"
    "github.com/bitechdev/ResolveSpec/pkg/logger"
)

func someFunction() {
    tracker := logger.GetErrorTracker()
    if tracker != nil {
        // Capture an error
        tracker.CaptureError(context.Background(), err, errortracking.SeverityError, map[string]interface{}{
            "user_id": userID,
            "request_id": requestID,
        })

        // Capture a message
        tracker.CaptureMessage(context.Background(), "Important event occurred", errortracking.SeverityInfo, map[string]interface{}{
            "event_type": "user_signup",
        })

        // Capture a panic
        tracker.CapturePanic(context.Background(), recovered, stackTrace, map[string]interface{}{
            "context": "background_job",
        })
    }
}
```

## Severity Levels

The package supports the following severity levels:

- `SeverityError`: For errors that should be tracked and investigated
- `SeverityWarning`: For warnings that may indicate potential issues
- `SeverityInfo`: For informational messages
- `SeverityDebug`: For debug-level information

```
