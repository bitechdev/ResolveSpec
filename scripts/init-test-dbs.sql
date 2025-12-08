-- Create test databases for integration tests
CREATE DATABASE resolvespec_test;
CREATE DATABASE restheadspec_test;

-- Grant all privileges to postgres user
GRANT ALL PRIVILEGES ON DATABASE resolvespec_test TO postgres;
GRANT ALL PRIVILEGES ON DATABASE restheadspec_test TO postgres;
