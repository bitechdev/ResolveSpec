.PHONY: test test-unit test-integration docker-up docker-down clean

# Run all unit tests
test-unit:
	@echo "Running unit tests..."
	@go test ./pkg/resolvespec ./pkg/restheadspec -v -cover

# Run all integration tests (requires PostgreSQL)
test-integration:
	@echo "Running integration tests..."
	@go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v

# Run all tests (unit + integration)
test: test-unit test-integration

# Start PostgreSQL for integration tests
docker-up:
	@echo "Starting PostgreSQL container..."
	@podman compose up -d postgres-test
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 5
	@echo "PostgreSQL is ready!"

# Stop PostgreSQL container
docker-down:
	@echo "Stopping PostgreSQL container..."
	@podman compose down

# Clean up Docker volumes and test data
clean:
	@echo "Cleaning up..."
	@podman compose down -v
	@echo "Cleanup complete!"

# Run integration tests with Docker (full workflow)
test-integration-docker: docker-up
	@echo "Running integration tests with Docker..."
	@go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v
	@$(MAKE) docker-down

# Check test coverage
coverage:
	@echo "Generating coverage report..."
	@go test ./pkg/resolvespec ./pkg/restheadspec -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run integration tests coverage
coverage-integration:
	@echo "Generating integration test coverage report..."
	@go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -coverprofile=coverage-integration.out
	@go tool cover -html=coverage-integration.out -o coverage-integration.html
	@echo "Integration coverage report generated: coverage-integration.html"

help:
	@echo "Available targets:"
	@echo "  test-unit              - Run unit tests"
	@echo "  test-integration       - Run integration tests (requires PostgreSQL)"
	@echo "  test                   - Run all tests"
	@echo "  docker-up              - Start PostgreSQL container"
	@echo "  docker-down            - Stop PostgreSQL container"
	@echo "  test-integration-docker - Run integration tests with Docker (automated)"
	@echo "  clean                  - Clean up Docker volumes"
	@echo "  coverage               - Generate unit test coverage report"
	@echo "  coverage-integration   - Generate integration test coverage report"
	@echo "  help                   - Show this help message"
