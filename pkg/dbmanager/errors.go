package dbmanager

import (
	"errors"
	"fmt"
)

// Common errors
var (
	// ErrConnectionNotFound is returned when a connection with the given name doesn't exist
	ErrConnectionNotFound = errors.New("connection not found")

	// ErrInvalidConfiguration is returned when the configuration is invalid
	ErrInvalidConfiguration = errors.New("invalid configuration")

	// ErrConnectionClosed is returned when attempting to use a closed connection
	ErrConnectionClosed = errors.New("connection is closed")

	// ErrNotSQLDatabase is returned when attempting SQL operations on a non-SQL database
	ErrNotSQLDatabase = errors.New("not a SQL database")

	// ErrNotMongoDB is returned when attempting MongoDB operations on a non-MongoDB connection
	ErrNotMongoDB = errors.New("not a MongoDB connection")

	// ErrUnsupportedDatabase is returned when the database type is not supported
	ErrUnsupportedDatabase = errors.New("unsupported database type")

	// ErrNoDefaultConnection is returned when no default connection is configured
	ErrNoDefaultConnection = errors.New("no default connection configured")

	// ErrAlreadyConnected is returned when attempting to connect an already connected connection
	ErrAlreadyConnected = errors.New("already connected")
)

// ConnectionError wraps errors that occur during connection operations
type ConnectionError struct {
	Name      string
	Operation string
	Err       error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("connection '%s' %s: %v", e.Name, e.Operation, e.Err)
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// NewConnectionError creates a new ConnectionError
func NewConnectionError(name, operation string, err error) *ConnectionError {
	return &ConnectionError{
		Name:      name,
		Operation: operation,
		Err:       err,
	}
}

// ConfigurationError wraps configuration-related errors
type ConfigurationError struct {
	Field string
	Err   error
}

func (e *ConfigurationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("configuration error in field '%s': %v", e.Field, e.Err)
	}
	return fmt.Sprintf("configuration error: %v", e.Err)
}

func (e *ConfigurationError) Unwrap() error {
	return e.Err
}

// NewConfigurationError creates a new ConfigurationError
func NewConfigurationError(field string, err error) *ConfigurationError {
	return &ConfigurationError{
		Field: field,
		Err:   err,
	}
}
