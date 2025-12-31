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

release-version: ## Create and push a release with specific version (use: make release-version VERSION=v1.2.3 or make release-version to auto-increment)
	@if [ -z "$(VERSION)" ]; then \
		latest_tag=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
		echo "No VERSION specified. Last version: $$latest_tag"; \
		version_num=$$(echo "$$latest_tag" | sed 's/^v//'); \
		major=$$(echo "$$version_num" | cut -d. -f1); \
		minor=$$(echo "$$version_num" | cut -d. -f2); \
		patch=$$(echo "$$version_num" | cut -d. -f3); \
		new_patch=$$((patch + 1)); \
		version="v$$major.$$minor.$$new_patch"; \
		echo "Auto-incrementing to: $$version"; \
	else \
		version="$(VERSION)"; \
		if ! echo "$$version" | grep -q "^v"; then \
			version="v$$version"; \
		fi; \
	fi; \
	echo "Creating release: $$version"; \
	latest_tag=$$(git describe --tags --abbrev=0 2>/dev/null || echo ""); \
	if [ -z "$$latest_tag" ]; then \
		commit_logs=$$(git log --pretty=format:"- %s" --no-merges); \
	else \
		commit_logs=$$(git log "$${latest_tag}..HEAD" --pretty=format:"- %s" --no-merges); \
	fi; \
	if [ -z "$$commit_logs" ]; then \
		tag_message="Release $$version"; \
	else \
		tag_message="Release $$version\n\n$$commit_logs"; \
	fi; \
	git tag -a "$$version" -m "$$tag_message"; \
	git push origin "$$version"; \
	echo "Tag $$version created and pushed to remote repository."


lint: ## Run linter
	@echo "Running linter..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run --config=.golangci.json; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

lintfix: ## Run linter
	@echo "Running linter..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run --config=.golangci.json --fix; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi


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
