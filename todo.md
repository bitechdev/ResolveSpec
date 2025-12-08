# ResolveSpec - TODO List

This document tracks incomplete features and improvements for the ResolveSpec project.

## Core Features to Implement

### 1. Column Selection and Filtering for Preloads
**Location:** `pkg/resolvespec/handler.go:730`
**Status:** Not Implemented
**Description:** Currently, preloads are applied without any column selection or filtering. This feature would allow clients to:
- Select specific columns for preloaded relationships
- Apply filters to preloaded data
- Reduce payload size and improve performance

**Current Limitation:**
```go
// For now, we'll preload without conditions
// TODO: Implement column selection and filtering for preloads
// This requires a more sophisticated approach with callbacks or query builders
query = query.Preload(relationFieldName)
```

**Required Implementation:**
- Add support for column selection in preloaded relationships
- Implement filtering conditions for preloaded data
- Design a callback or query builder approach that works across different ORMs

---

### 2. Recursive JSON Cleaning
**Location:** `pkg/restheadspec/handler.go:796`
**Status:** Partially Implemented (Simplified)
**Description:** The current `cleanJSON` function returns data as-is without recursively removing null and empty fields from nested structures.

**Current Limitation:**
```go
// This is a simplified implementation
// A full implementation would recursively clean nested structures
// For now, we'll return the data as-is
// TODO: Implement recursive cleaning
return data
```

**Required Implementation:**
- Recursively traverse nested structures (maps, slices, structs)
- Remove null values
- Remove empty objects and arrays
- Handle edge cases (circular references, pointers, etc.)

---

### 3. Custom SQL Join Support
**Location:** `pkg/restheadspec/headers.go:159`
**Status:** Not Implemented
**Description:** Support for custom SQL joins via the `X-Custom-SQL-Join` header is currently logged but not executed.

**Current Limitation:**
```go
case strings.HasPrefix(normalizedKey, "x-custom-sql-join"):
    // TODO: Implement custom SQL join
    logger.Debug("Custom SQL join not yet implemented: %s", decodedValue)
```

**Required Implementation:**
- Parse custom SQL join expressions from headers
- Apply joins to the query builder
- Ensure security (SQL injection prevention)
- Support for different join types (INNER, LEFT, RIGHT, FULL)
- Works across different database adapters (GORM, Bun)

---

### 4. Proper Condition Handling for Bun Preloads
**Location:** `pkg/common/adapters/database/bun.go:202`
**Status:** Partially Implemented
**Description:** The Bun adapter's `Preload` method currently ignores conditions passed to it.

**Current Limitation:**
```go
func (b *BunSelectQuery) Preload(relation string, conditions ...interface{}) common.SelectQuery {
    // Bun uses Relation() method for preloading
    // For now, we'll just pass the relation name without conditions
    // TODO: Implement proper condition handling for Bun
    b.query = b.query.Relation(relation)
    return b
}
```

**Required Implementation:**
- Properly handle condition parameters in Bun's Relation() method
- Support filtering on preloaded relationships
- Ensure compatibility with GORM's condition syntax where possible
- Test with various condition types

---

## Code Quality Improvements

### 5. Modernize Go Type Declarations
**Location:** `pkg/common/types.go:5, 42, 64, 79`
**Status:** Pending
**Priority:** Low
**Description:** Replace legacy `interface{}` with modern `any` type alias (Go 1.18+).

**Affected Lines:**
- Line 5: Function parameter or return type
- Line 42: Function parameter or return type
- Line 64: Function parameter or return type
- Line 79: Function parameter or return type

**Benefits:**
- More modern and idiomatic Go code
- Better readability
- Aligns with current Go best practices

---

### 6. Pre / Post select/update/delete query in transaction.
- This will allow us to set a user before doing a select
- When making changes, we can have the trigger fire with the correct user.
- Maybe wrap the handleRead,Update,Create,Delete handlers in a transaction with context that can abort when the request is cancelled or a configurable timeout is reached.

### 7. 

## Additional Considerations

### Documentation
- Ensure all new features are documented in README.md
- Update examples to showcase new functionality
- Add migration notes if any breaking changes are introduced

### Testing
- Add unit tests for each new feature
- Add integration tests for database adapter compatibility
- Ensure backward compatibility is maintained

### Performance
- Profile preload performance with column selection and filtering
- Optimize recursive JSON cleaning for large payloads
- Benchmark custom SQL join performance


### 8.

1. **Test Coverage**: Increase from 20% to 70%+
   - Add integration tests for CRUD operations
   - Add unit tests for security providers
   - Add concurrency tests for model registry

2. **Security Enhancements**:
   - Add request size limits
   - Configure CORS properly
   - Implement input sanitization beyond SQL

3. **Configuration Management**:
   - Centralized config system
   - Environment-based configuration

4. **Graceful Shutdown**:
   - Implement shutdown coordination
   - Drain in-flight requests


---

## Priority Ranking

1. **High Priority**
   - Column Selection and Filtering for Preloads (#1)
   - Proper Condition Handling for Bun Preloads (#4)

2. **Medium Priority**
   - Custom SQL Join Support (#3)
   - Recursive JSON Cleaning (#2)

3. **Low Priority**
   - Modernize Go Type Declarations (#5)

---



**Last Updated:** 2025-11-07
