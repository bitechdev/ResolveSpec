# Integration Tests

This document describes how to run integration tests for ResolveSpec packages with a PostgreSQL database.

## Overview

Integration tests validate the full functionality of both `pkg/resolvespec` and `pkg/restheadspec` packages with an actual PostgreSQL database. These tests cover:

- CRUD operations (Create, Read, Update, Delete)
- Filtering and sorting
- Pagination
- Column selection
- Relationship preloading
- Metadata generation
- Query parameter parsing
- CORS handling

## Prerequisites

- Go 1.19 or later
- PostgreSQL 12 or later
- Podman and Podman Compose (optional, for easy setup)

## Quick Start with Podman

### 1. Start PostgreSQL with Podman Compose

```bash
podman compose up -d postgres-test
```

This starts a PostgreSQL container with the following default settings:
- Host: localhost
- Port: 5432
- User: postgres
- Password: postgres
- Databases: resolvespec_test, restheadspec_test

### 2. Run Integration Tests

```bash
# Run all integration tests
go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v

# Run only resolvespec integration tests
go test -tags=integration ./pkg/resolvespec -v

# Run only restheadspec integration tests
go test -tags=integration ./pkg/restheadspec -v
```

### 3. Stop PostgreSQL

```bash
podman compose down
```

## Manual PostgreSQL Setup

If you prefer to use an existing PostgreSQL installation:

### 1. Create Test Databases

```sql
CREATE DATABASE resolvespec_test;
CREATE DATABASE restheadspec_test;
```

### 2. Set Environment Variable

```bash
# For resolvespec tests
export TEST_DATABASE_URL="host=localhost user=postgres password=yourpassword dbname=resolvespec_test port=5432 sslmode=disable"

# For restheadspec tests (uses same env var with different dbname)
export TEST_DATABASE_URL="host=localhost user=postgres password=yourpassword dbname=restheadspec_test port=5432 sslmode=disable"
```

### 3. Run Tests

```bash
go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v
```

## Test Coverage

### pkg/resolvespec Integration Tests

| Test | Description |
|------|-------------|
| `TestIntegration_CreateOperation` | Tests creating new records via API |
| `TestIntegration_ReadOperation` | Tests reading all records with pagination |
| `TestIntegration_ReadWithFilters` | Tests filtering records (e.g., age > 25) |
| `TestIntegration_UpdateOperation` | Tests updating existing records |
| `TestIntegration_DeleteOperation` | Tests deleting records |
| `TestIntegration_MetadataOperation` | Tests retrieving table metadata |
| `TestIntegration_ReadWithPreload` | Tests eager loading relationships |

### pkg/restheadspec Integration Tests

| Test | Description |
|------|-------------|
| `TestIntegration_GetAllUsers` | Tests GET request to retrieve all records |
| `TestIntegration_GetUsersWithFilters` | Tests header-based filtering |
| `TestIntegration_GetUsersWithPagination` | Tests limit/offset pagination |
| `TestIntegration_GetUsersWithSorting` | Tests sorting by column |
| `TestIntegration_GetUsersWithColumnsSelection` | Tests selecting specific columns |
| `TestIntegration_GetUsersWithPreload` | Tests relationship preloading |
| `TestIntegration_GetMetadata` | Tests metadata endpoint |
| `TestIntegration_OptionsRequest` | Tests OPTIONS/CORS handling |
| `TestIntegration_QueryParamsOverHeaders` | Tests query param precedence |
| `TestIntegration_GetSingleRecord` | Tests retrieving single record by ID |

## Test Data

Integration tests use the following test models:

### TestUser
```go
type TestUser struct {
    ID        uint
    Name      string
    Email     string (unique)
    Age       int
    Active    bool
    CreatedAt time.Time
    Posts     []TestPost
}
```

### TestPost
```go
type TestPost struct {
    ID        uint
    UserID    uint
    Title     string
    Content   string
    Published bool
    CreatedAt time.Time
    User      *TestUser
    Comments  []TestComment
}
```

### TestComment
```go
type TestComment struct {
    ID        uint
    PostID    uint
    Content   string
    CreatedAt time.Time
    Post      *TestPost
}
```

## Troubleshooting

### Connection Refused

If you see "connection refused" errors:

1. Check that PostgreSQL is running:
   ```bash
   podman compose ps
   ```

2. Verify connection parameters:
   ```bash
   psql -h localhost -U postgres -d resolvespec_test
   ```

3. Check firewall settings if using remote PostgreSQL

### Permission Denied

Ensure the PostgreSQL user has necessary permissions:

```sql
GRANT ALL PRIVILEGES ON DATABASE resolvespec_test TO postgres;
GRANT ALL PRIVILEGES ON DATABASE restheadspec_test TO postgres;
```

### Tests Fail with "relation does not exist"

The tests automatically run migrations, but if you encounter this error:

1. Ensure your DATABASE_URL environment variable is correct
2. Check that the database exists
3. Verify the user has CREATE TABLE permissions

### Clean Database Between Runs

Each test automatically cleans up its data using `TRUNCATE`. If you need a fresh database:

```bash
# Stop and remove containers (removes data)
podman compose down -v

# Restart
podman compose up -d postgres-test
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Integration Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: resolvespec_test
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 5432:5432

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run integration tests
        env:
          TEST_DATABASE_URL: "host=localhost user=postgres password=postgres dbname=resolvespec_test port=5432 sslmode=disable"
        run: |
          go test -tags=integration ./pkg/resolvespec -v
          go test -tags=integration ./pkg/restheadspec -v
```

## Performance Considerations

- Integration tests are slower than unit tests due to database I/O
- Each test sets up and tears down test data
- Consider running integration tests separately from unit tests in CI/CD
- Use connection pooling for better performance

## Best Practices

1. **Isolation**: Each test cleans up its data using TRUNCATE
2. **Independent**: Tests don't depend on each other's state
3. **Idempotent**: Tests can be run multiple times safely
4. **Fast Setup**: Migrations run automatically
5. **Flexible**: Works with any PostgreSQL instance via environment variables

## Additional Resources

- [PostgreSQL Docker Image](https://hub.docker.com/_/postgres)
- [GORM Documentation](https://gorm.io/)
- [Testing in Go](https://golang.org/doc/tutorial/add-a-test)
