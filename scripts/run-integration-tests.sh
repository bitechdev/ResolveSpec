#!/bin/bash

# Script to run integration tests with automatic PostgreSQL setup
# Usage: ./scripts/run-integration-tests.sh [package]
#   package: optional, can be "resolvespec", "restheadspec", or omit for both

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ResolveSpec Integration Tests ===${NC}\n"

# Check if docker-compose is available
if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}Error: docker-compose is not installed${NC}"
    echo "Please install docker-compose or run PostgreSQL manually"
    echo "See INTEGRATION_TESTS.md for details"
    exit 1
fi

# Clean up any existing containers and networks from previous runs
echo -e "${YELLOW}Cleaning up existing containers and networks...${NC}"
docker-compose down -v 2>/dev/null || true

# Start PostgreSQL
echo -e "${YELLOW}Starting PostgreSQL...${NC}"
docker-compose up -d postgres-test

# Wait for PostgreSQL to be ready
echo -e "${YELLOW}Waiting for PostgreSQL to be ready...${NC}"
max_attempts=30
attempt=0

while ! docker-compose exec -T postgres-test pg_isready -U postgres > /dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ $attempt -ge $max_attempts ]; then
        echo -e "${RED}Error: PostgreSQL failed to start after ${max_attempts} seconds${NC}"
        docker-compose logs postgres-test
        docker-compose down
        exit 1
    fi
    sleep 1
    echo -n "."
done

echo -e "\n${GREEN}PostgreSQL is ready!${NC}\n"

# Create test databases
echo -e "${YELLOW}Creating test databases...${NC}"
docker-compose exec -T postgres-test psql -U postgres -c "CREATE DATABASE resolvespec_test;" 2>/dev/null || echo "  resolvespec_test already exists"
docker-compose exec -T postgres-test psql -U postgres -c "CREATE DATABASE restheadspec_test;" 2>/dev/null || echo "  restheadspec_test already exists"
echo -e "${GREEN}Test databases ready!${NC}\n"

# Determine which tests to run
PACKAGE=""
if [ "$1" == "resolvespec" ]; then
    PACKAGE="./pkg/resolvespec"
    echo -e "${YELLOW}Running resolvespec integration tests...${NC}\n"
elif [ "$1" == "restheadspec" ]; then
    PACKAGE="./pkg/restheadspec"
    echo -e "${YELLOW}Running restheadspec integration tests...${NC}\n"
else
    PACKAGE="./pkg/resolvespec ./pkg/restheadspec"
    echo -e "${YELLOW}Running all integration tests...${NC}\n"
fi

# Run tests
if go test -tags=integration $PACKAGE -v; then
    echo -e "\n${GREEN}✓ All integration tests passed!${NC}"
    EXIT_CODE=0
else
    echo -e "\n${RED}✗ Some integration tests failed${NC}"
    EXIT_CODE=1
fi

# Cleanup
echo -e "\n${YELLOW}Stopping PostgreSQL...${NC}"
docker-compose down

exit $EXIT_CODE
