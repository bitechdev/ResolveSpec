# Testing Quick Start

## ⚡ 30-Second Start

```bash
# Unit tests (no setup required)
go test ./pkg/resolvespec ./pkg/restheadspec -v

# Integration tests (automated)
./scripts/run-integration-tests.sh
```

## 📋 Common Commands

| What You Want | Command |
|---------------|---------|
| Run unit tests | `make test-unit` |
| Run integration tests | `./scripts/run-integration-tests.sh` |
| Run all tests | `make test` |
| Coverage report | `make coverage` |
| Start PostgreSQL | `make docker-up` |
| Stop PostgreSQL | `make docker-down` |
| See all commands | `make help` |

## 📊 Current Test Coverage

- **pkg/resolvespec**: 11.2% (28 unit + 7 integration tests)
- **pkg/restheadspec**: 12.5% (50 unit + 10 integration tests)
- **Total**: 95 tests

## 🧪 Test Types

### Unit Tests (Fast, No Database)
Test individual functions and components in isolation.

```bash
go test ./pkg/resolvespec -v
go test ./pkg/restheadspec -v
```

### Integration Tests (Requires PostgreSQL)
Test full API operations with real database.

```bash
# Automated (recommended)
./scripts/run-integration-tests.sh

# Manual
make docker-up
go test -tags=integration ./pkg/resolvespec -v
make docker-down
```

## 🔍 Run Specific Tests

```bash
# Run a specific test function
go test ./pkg/resolvespec -run TestHookRegistry -v

# Run tests matching a pattern
go test ./pkg/resolvespec -run "TestHook.*" -v

# Run integration test for specific feature
go test -tags=integration ./pkg/restheadspec -run TestIntegration_GetUsersWithFilters -v
```

## 📈 Coverage Reports

```bash
# Generate HTML coverage report
make coverage

# View in terminal
go test ./pkg/resolvespec -cover
```

## 🐛 Troubleshooting

| Problem | Solution |
|---------|----------|
| "No tests found" | Use `-tags=integration` for integration tests |
| "Connection refused" | Run `make docker-up` to start PostgreSQL |
| "Permission denied" | Run `chmod +x scripts/run-integration-tests.sh` |
| Tests fail randomly | Use `-race` flag to detect race conditions |

## 📚 Full Documentation

- **Complete Guide**: [README_TESTS.md](./README_TESTS.md)
- **Integration Details**: [INTEGRATION_TESTS.md](./INTEGRATION_TESTS.md)
- **All Commands**: `make help`

## 🎯 What Gets Tested?

### pkg/resolvespec
- ✅ Context operations
- ✅ Hook system
- ✅ CRUD operations (Create, Read, Update, Delete)
- ✅ Filtering and sorting
- ✅ Relationship preloading
- ✅ Metadata generation

### pkg/restheadspec
- ✅ Header-based API operations
- ✅ Query parameter parsing
- ✅ Pagination (limit/offset)
- ✅ Column selection
- ✅ CORS handling
- ✅ Sorting by columns

## 🚀 CI/CD

GitHub Actions workflow is ready at `.github/workflows/tests.yml`

Tests run automatically on:
- Push to main/develop branches
- Pull requests

## 💡 Tips

1. **Run unit tests frequently** - They're fast (< 1 second)
2. **Run integration tests before commits** - Catches DB issues
3. **Use `make test-integration-docker`** - Handles everything automatically
4. **Check coverage reports** - Identify untested code
5. **Use `-v` flag** - See detailed test output

## 🎓 Next Steps

1. Run unit tests: `make test-unit`
2. Try integration tests: `./scripts/run-integration-tests.sh`
3. Generate coverage: `make coverage`
4. Read full guide: `README_TESTS.md`

---

**Need Help?** Check [README_TESTS.md](./README_TESTS.md) for detailed instructions.
