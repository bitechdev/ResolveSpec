# ResolveSpec - TODO List

This document tracks incomplete features and improvements for the ResolveSpec project.

## In Progress

### Database Layer

- [x] SQLite schema translation (schema.table → schema_table)
- [x] Driver name normalization across adapters
- [x] Database Connection Manager (dbmanager) package

### Documentation

- [x] Add dbmanager to README
- [x] Add WebSocketSpec to top-level intro
- [x] Add MQTTSpec to top-level intro
- [x] Remove migration sections from README
- [ ] Complete API reference documentation
- [ ] Add examples for all supported databases

## Planned Features

### ResolveSpec JS Client Implementation & Testing

1. **ResolveSpec Client API (resolvespec-js)**
   - [x] Core API implementation (read, create, update, delete, getMetadata)
   - [ ] Unit tests for API functions
   - [ ] Integration tests with server
   - [ ] Error handling and edge cases

2. **HeaderSpec Client API (resolvespec-js)**
   - [ ] Client API implementation
   - [ ] Unit tests
   - [ ] Integration tests with server

3. **FunctionSpec Client API (resolvespec-js)**
   - [ ] Client API implementation
   - [ ] Unit tests
   - [ ] Integration tests with server

4. **WebSocketSpec Client API (resolvespec-js)**
   - [x] WebSocketClient class implementation (read, create, update, delete, meta, subscribe, unsubscribe)
   - [ ] Unit tests for WebSocketClient
   - [ ] Connection handling tests
   - [ ] Subscription tests
   - [ ] Integration tests with server

5. **resolvespec-js Testing Infrastructure**
   - [ ] Set up test framework (Jest or Vitest)
   - [ ] Configure test coverage reporting
   - [ ] Add test utilities and mocks
   - [ ] Create test documentation

### ResolveSpec Python Client Implementation & Testing

See [`resolvespec-python/todo.md`](./resolvespec-python/todo.md) for detailed Python client implementation tasks.

### Core Functionality

1. **Enhanced Preload Filtering**
   - [ ] Column selection for nested preloads
   - [ ] Advanced filtering conditions for relations
   - [ ] Performance optimization for deep nesting

2. **Advanced Query Features**
   - [ ] Custom SQL join support
   - [ ] Computed column improvements
   - [ ] Recursive query support

3. **Testing & Quality**
   - [ ] Increase test coverage to 70%+
   - [ ] Add integration tests for all ORMs
   - [ ] Add concurrency tests for thread safety
   - [ ] Performance benchmarks

### Infrastructure

- [ ] Improved error handling and reporting
- [ ] Enhanced logging capabilities
- [ ] Additional monitoring metrics
- [ ] Performance profiling tools

## Documentation Tasks

- [ ] Complete API reference
- [ ] Add troubleshooting guides
- [ ] Create architecture diagrams
- [ ] Expand database adapter documentation

## Known Issues

- [ ] Long preload alias names may exceed PostgreSQL identifier limit
- [ ] Some edge cases in computed column handling

---

**Last Updated:** 2026-02-07
**Updated:** Added resolvespec-js client testing and implementation tasks
