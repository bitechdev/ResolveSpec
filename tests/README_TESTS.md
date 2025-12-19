# Testing Guide

This document provides a comprehensive guide to running tests for the ResolveSpec project.

## Table of Contents

- [Quick Start](#quick-start)
- [Unit Tests](#unit-tests)
- [Integration Tests](#integration-tests)
- [Test Coverage](#test-coverage)
- [CI/CD](#cicd)

## Quick Start

### Run All Unit Tests

```bash
# Simple
go test ./pkg/resolvespec ./pkg/restheadspec -v

# With coverage
make test-unit
```

### Run Integration Tests (Automated with Docker)

```bash
# Easiest way - handles Docker automatically
./scripts/run-integration-tests.sh

# Or with make
make test-integration-docker

# Run specific package
./scripts/run-integration-tests.sh resolvespec
./scripts/run-integration-tests.sh restheadspec
```

### Run All Tests

```bash
make test
```

## Unit Tests

Unit tests are located alongside the source files with `_test.go` suffix and **do not** require a database.

### Test Structure

```
pkg/
├── resolvespec/
│   ├── context.go
│   ├── context_test.go       # Unit tests
│   ├── handler.go
│   ├── handler_test.go        # Unit tests
│   ├── hooks.go
│   ├── hooks_test.go          # Unit tests
│   └── integration_test.go    # Integration tests
└── restheadspec/
    ├── context.go
    ├── context_test.go        # Unit tests
    └── integration_test.go    # Integration tests
```

### Coverage Report

#### Current Coverage

- **pkg/resolvespec**: 11.2% (improved from 0%)
- **pkg/restheadspec**: 12.5% (improved from 10.5%)

#### What's Tested

##### pkg/resolvespec
- ✅ Context operations (WithSchema, GetEntity, etc.)
- ✅ Hook registry (register, execute, clear)
- ✅ Handler initialization
- ✅ Utility functions (parseModelName, buildRoutePath, toSnakeCase)
- ✅ Column type detection
- ✅ Table name parsing

##### pkg/restheadspec
- ✅ Context operations including options
- ✅ Hook system
- ✅ Header parsing and decoding
- ✅ Query parameter parsing
- ✅ Nested relation detection
- ✅ Row number operations

### Running Specific Tests

```bash
# Run specific test function
go test ./pkg/resolvespec -run TestHookRegistry -v

# Run tests matching pattern
go test ./pkg/resolvespec -run "TestHook.*" -v

# Run with coverage
go test ./pkg/resolvespec -cover

# Generate HTML coverage report
go test ./pkg/resolvespec -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Integration Tests

Integration tests require a PostgreSQL database and use the `// +build integration` tag.

### Prerequisites

**Option 1: Docker (Recommended)**
- Docker and Docker Compose installed

**Option 2: Manual PostgreSQL**
- PostgreSQL 12+ installed and running
- Create test databases manually (see below)

### Setup with Podman

1. **Start PostgreSQL**:
   ```bash
   make docker-up
   # or
   podman compose up -d postgres-test
   ```

2. **Run Tests**:
   ```bash
   # Automated (recommended)
   ./scripts/run-integration-tests.sh

   # Manual
   go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v
   ```

3. **Stop PostgreSQL**:
   ```bash
   make docker-down
   # or
   podman compose down
   ```

### Setup without Podman

1. **Create Databases**:
   ```sql
   CREATE DATABASE resolvespec_test;
   CREATE DATABASE restheadspec_test;
   ```

2. **Set Environment Variable**:
   ```bash
   export TEST_DATABASE_URL="host=localhost user=postgres password=yourpass dbname=resolvespec_test port=5432 sslmode=disable"
   ```

3. **Run Tests**:
   ```bash
   go test -tags=integration ./pkg/resolvespec -v

   # For restheadspec, update dbname in TEST_DATABASE_URL
   export TEST_DATABASE_URL="host=localhost user=postgres password=yourpass dbname=restheadspec_test port=5432 sslmode=disable"
   go test -tags=integration ./pkg/restheadspec -v
   ```

### Integration Test Coverage

#### pkg/resolvespec (7 tests)
- ✅ Create operation
- ✅ Read operation with pagination
- ✅ Read with filters (age > 25)
- ✅ Update operation
- ✅ Delete operation
- ✅ Metadata retrieval
- ✅ Read with relationship preloading

#### pkg/restheadspec (10 tests)
- ✅ GET all records
- ✅ GET with header-based filters
- ✅ GET with pagination (limit/offset)
- ✅ GET with sorting
- ✅ GET with column selection
- ✅ GET with relationship preloading
- ✅ Metadata endpoint
- ✅ OPTIONS/CORS handling
- ✅ Query params override headers
- ✅ GET single record by ID

## Test Coverage

### Generate Coverage Reports

```bash
# Unit test coverage
make coverage

# Integration test coverage
make coverage-integration

# Both
make coverage && make coverage-integration
```

Coverage reports are generated as HTML files:
- `coverage.html` - Unit tests
- `coverage-integration.html` - Integration tests

### View Coverage

```bash
# Open in browser
open coverage.html  # macOS
xdg-open coverage.html  # Linux
start coverage.html  # Windows
```

## Makefile Commands

```bash
make help                    # Show all available commands
make test-unit              # Run unit tests
make test-integration       # Run integration tests (requires PostgreSQL)
make test                   # Run all tests
make docker-up              # Start PostgreSQL
make docker-down            # Stop PostgreSQL
make test-integration-docker # Full automated integration test
make clean                  # Clean up Docker volumes
make coverage               # Generate unit test coverage
make coverage-integration   # Generate integration test coverage
```

## CI/CD

### GitHub Actions Example

See `INTEGRATION_TESTS.md` for a complete GitHub Actions workflow example.

Key points:
- Use PostgreSQL service container
- Run unit tests first (faster)
- Run integration tests separately
- Generate coverage reports
- Upload coverage to codecov/coveralls

### GitLab CI Example

```yaml
stages:
  - test

unit-tests:
  stage: test
  script:
    - go test ./pkg/resolvespec ./pkg/restheadspec -v -cover

integration-tests:
  stage: test
  services:
    - postgres:15
  variables:
    POSTGRES_DB: resolvespec_test
    POSTGRES_USER: postgres
    POSTGRES_PASSWORD: postgres
    TEST_DATABASE_URL: "host=postgres user=postgres password=postgres dbname=resolvespec_test port=5432 sslmode=disable"
  script:
    - go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v
```

## Troubleshooting

### Tests Won't Run

**Problem**: `go test` finds no tests
**Solution**: Make sure you're using the `-tags=integration` flag for integration tests

```bash
# Wrong (for integration tests)
go test ./pkg/resolvespec -v

# Correct
go test -tags=integration ./pkg/resolvespec -v
```

### Database Connection Failed

**Problem**: "connection refused" or "database does not exist"

**Solutions**:
1. Check PostgreSQL is running: `podman compose ps`
2. Verify databases exist: `podman compose exec postgres-test psql -U postgres -l`
3. Check environment variable: `echo $TEST_DATABASE_URL`
4. Recreate databases: `make clean && make docker-up`

### Permission Denied on Script

**Problem**: `./scripts/run-integration-tests.sh: Permission denied`

**Solution**:
```bash
chmod +x scripts/run-integration-tests.sh
```

### Tests Pass Locally but Fail in CI

**Possible causes**:
1. Different PostgreSQL version
2. Missing environment variables
3. Timezone differences
4. Race conditions (use `-race` flag to detect)

```bash
go test -race -tags=integration ./pkg/resolvespec -v
```

## Best Practices

1. **Run unit tests frequently** - They're fast and catch most issues
2. **Run integration tests before commits** - Ensures DB operations work
3. **Keep tests independent** - Each test should clean up after itself
4. **Use descriptive test names** - `TestIntegration_GetUsersWithFilters` vs `TestGet`
5. **Test error cases** - Not just the happy path
6. **Mock external dependencies** - Use interfaces for testability
7. **Maintain test data** - Keep test fixtures small and focused

## Test Data

Integration tests use these models:
- **TestUser**: id, name, email, age, active, posts[]
- **TestPost**: id, user_id, title, content, published, comments[]
- **TestComment**: id, post_id, content

Sample test data:
- 3 users (John Doe, Jane Smith, Bob Johnson)
- 3 posts (2 by John, 1 by Jane)
- 3 comments (2 on first post, 1 on second)

## Performance

- **Unit tests**: ~0.003s per package
- **Integration tests**: ~0.5-2s per package (depends on database)
- **Total**: <10 seconds for all tests

## Additional Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Table Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [GORM Testing](https://gorm.io/docs/testing.html)
- [Integration Tests Guide](./INTEGRATION_TESTS.md)
