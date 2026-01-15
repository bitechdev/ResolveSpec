# RestHeadSpec Headers Documentation

RestHeadSpec provides a comprehensive header-based REST API where all query options are passed via HTTP headers instead of request body. This document describes all supported headers and their usage.

## Overview

RestHeadSpec uses HTTP headers for:
- Field selection
- Filtering and searching
- Joins and relationship loading
- Sorting and pagination
- Advanced query features
- Response formatting
- Transaction control

### Header Naming Convention

All headers support **optional identifiers** at the end to allow multiple instances of the same header type. This is useful when you need to specify multiple related filters or options.

**Examples:**
```
# Standard header
x-preload: employees

# Headers with identifiers (both work the same)
x-preload-main: employees
x-preload-secondary: department
x-preload-1: projects
```

The system uses `strings.HasPrefix()` to match headers, so any suffix after the header name is ignored for matching purposes. This allows you to:
- Add descriptive identifiers: `x-sort-primary`, `x-sort-fallback`
- Add numeric identifiers: `x-fieldfilter-status-1`, `x-fieldfilter-status-2`
- Organize related headers: `x-preload-employee-data`, `x-preload-department-info`

## Header Categories

### 1. Field Selection

#### `x-select-fields`
Specify which columns to include in the response.

**Format:** Comma-separated list of column names
```
x-select-fields: id,name,email,created_at
```

#### `x-not-select-fields`
Specify which columns to exclude from the response.

**Format:** Comma-separated list of column names
```
x-not-select-fields: password,internal_notes
```

#### `x-clean-json`
Remove null and empty fields from the response.

**Format:** Boolean (true/false)
```
x-clean-json: true
```

---

### 2. Filtering & Search

#### `x-fieldfilter-{colname}`
Exact match filter on a specific column.

**Format:** `x-fieldfilter-{columnName}: {value}`
```
x-fieldfilter-status: active
x-fieldfilter-department_id: dept123
```

#### `x-searchfilter-{colname}`
Fuzzy search (ILIKE) on a specific column.

**Format:** `x-searchfilter-{columnName}: {searchTerm}`
```
x-searchfilter-name: john
x-searchfilter-description: website
```
This will match any records where the column contains the search term (case-insensitive).

#### `x-searchop-{operator}-{colname}`
Search with specific operators (AND logic).

**Supported Operators:**
- `contains` - Contains substring (case-insensitive)
- `beginswith` / `startswith` - Starts with (case-insensitive)
- `endswith` - Ends with (case-insensitive)
- `equals` / `eq` - Exact match
- `notequals` / `neq` / `ne` - Not equal
- `greaterthan` / `gt` - Greater than
- `lessthan` / `lt` - Less than
- `greaterthanorequal` / `gte` / `ge` - Greater than or equal
- `lessthanorequal` / `lte` / `le` - Less than or equal
- `between` - Between two values, **exclusive** (> val1 AND < val2) - format: `value1,value2`
- `betweeninclusive` - Between two values, **inclusive** (>= val1 AND <= val2) - format: `value1,value2`
- `in` - In a list of values - format: `value1,value2,value3`
- `empty` / `isnull` / `null` - Is NULL or empty string
- `notempty` / `isnotnull` / `notnull` - Is NOT NULL and not empty string

**Type-Aware Features:**
- Text searches use case-insensitive matching (ILIKE with citext cast)
- Numeric comparisons work with integers, floats, and decimals
- Date/time comparisons handle timestamps correctly
- JSON field support for structured data

**Examples:**
```
# Text search (case-insensitive)
x-searchop-contains-name: smith

# Numeric comparison
x-searchop-gt-age: 25
x-searchop-gte-salary: 50000

# Date range (exclusive)
x-searchop-between-created_at: 2024-01-01,2024-12-31

# Date range (inclusive)
x-searchop-betweeninclusive-birth_date: 1990-01-01,2000-12-31

# List matching
x-searchop-in-status: active,pending,review

# NULL checks
x-searchop-empty-deleted_at: true
x-searchop-notempty-email: true
```

#### `x-searchor-{operator}-{colname}`
Same as `x-searchop` but with OR logic instead of AND.

```
x-searchor-eq-status: active
x-searchor-eq-status: pending
```

#### `x-searchand-{operator}-{colname}`
Explicit AND logic (same as `x-searchop`).

```
x-searchand-gte-age: 18
x-searchand-lte-age: 65
```

#### `x-searchcols`
Specify columns for "all" search operations.

**Format:** Comma-separated list
```
x-searchcols: name,email,description
```

#### `x-custom-sql-w`
Raw SQL WHERE clause with AND condition.

**Format:** SQL WHERE clause (without the WHERE keyword)
```
x-custom-sql-w: status = 'active' AND created_at > '2024-01-01'
```

⚠️ **Warning:** Use with caution - ensure proper SQL injection prevention.

#### `x-custom-sql-or`
Raw SQL WHERE clause with OR condition.

**Format:** SQL WHERE clause
```
x-custom-sql-or: status = 'archived' OR is_deleted = true
```

---

### 3. Joins & Relations

#### `x-preload`
Preload related tables using the ORM's preload functionality.

**Format:** `RelationName:field1,field2` or `RelationName`

Multiple relations can be specified using multiple headers or by separating with `|`

**Examples:**
```
# Preload all fields from employees relation
x-preload: employees

# Preload specific fields from employees
x-preload: employees:id,first_name,last_name,email

# Multiple preloads using pipe separator
x-preload: employees:id,name|department:id,name

# Multiple preloads using separate headers with identifiers
x-preload-1: employees:id,first_name,last_name
x-preload-2: department:id,name
x-preload-related: projects:id,name,status
```

#### `x-expand`
LEFT JOIN related tables and expand results inline.

**Format:** Same as `x-preload`

```
x-expand: department:id,name,code
```

**Note:** Currently, expand falls back to preload behavior. Full JOIN expansion is planned for future implementation.

#### `x-custom-sql-join`
Custom SQL JOIN clauses for joining tables in queries.

**Format:** SQL JOIN clause or multiple clauses separated by `|`

**Single JOIN:**
```
x-custom-sql-join: LEFT JOIN departments d ON d.id = employees.department_id
```

**Multiple JOINs:**
```
x-custom-sql-join: LEFT JOIN departments d ON d.id = e.dept_id | INNER JOIN roles r ON r.id = e.role_id
```

**Features:**
- Supports any type of JOIN (INNER, LEFT, RIGHT, FULL, CROSS)
- Multiple JOINs can be specified using the pipe `|` separator
- JOINs are sanitized for security
- Can be specified via headers or query parameters
- **Table aliases are automatically extracted and allowed for filtering and sorting**

**Using Join Aliases in Filters and Sorts:**

When you specify a custom SQL join with an alias, you can use that alias in your filter and sort parameters:

```
# Join with alias
x-custom-sql-join: LEFT JOIN departments d ON d.id = employees.department_id

# Sort by joined table column
x-sort: d.name,employees.id

# Filter by joined table column
x-searchop-eq-d.name: Engineering
```

The system automatically:
1. Extracts the alias from the JOIN clause (e.g., `d` from `departments d`)
2. Validates that prefixed columns (like `d.name`) refer to valid join aliases
3. Allows these prefixed columns in filters and sorts

---

### 4. Sorting & Pagination

#### `x-sort`
Sort results by one or more columns.

**Format:** Comma-separated list with optional `+` (ASC) or `-` (DESC) prefix

```
# Single column ascending (default)
x-sort: name

# Single column descending
x-sort: -created_at

# Multiple columns
x-sort: +department,- created_at,name

# Equivalent to: ORDER BY department ASC, created_at DESC, name ASC
```

#### `x-limit`
Limit the number of records returned.

**Format:** Integer
```
x-limit: 50
```

#### `x-offset`
Skip a number of records (offset-based pagination).

**Format:** Integer
```
x-offset: 100
```

#### `x-cursor-forward`
Cursor-based pagination (forward).

**Format:** Cursor string
```
x-cursor-forward: eyJpZCI6MTIzfQ==
```

⚠️ **Note:** Not yet fully implemented.

#### `x-cursor-backward`
Cursor-based pagination (backward).

**Format:** Cursor string
```
x-cursor-backward: eyJpZCI6MTIzfQ==
```

⚠️ **Note:** Not yet fully implemented.

---

### 5. Advanced Features

#### `x-advsql-{colname}`
Advanced SQL expression for a specific column.

**Format:** `x-advsql-{columnName}: {SQLExpression}`
```
x-advsql-full_name: CONCAT(first_name, ' ', last_name)
x-advsql-age_years: EXTRACT(YEAR FROM AGE(birth_date))
```

⚠️ **Note:** Not yet fully implemented in query execution.

#### `x-cql-sel-{colname}`
Computed Query Language - custom SQL expressions aliased as columns.

**Format:** `x-cql-sel-{aliasName}: {SQLExpression}`
```
x-cql-sel-employee_count: COUNT(employees.id)
x-cql-sel-total_revenue: SUM(orders.amount)
```

⚠️ **Note:** Not yet fully implemented in query execution.

#### `x-distinct`
Apply DISTINCT to the query.

**Format:** Boolean (true/false)
```
x-distinct: true
```

⚠️ **Note:** Implementation depends on ORM adapter support.

#### `x-skipcount`
Skip counting total records (performance optimization).

**Format:** Boolean (true/false)
```
x-skipcount: true
```

When enabled, the total count will be -1 in the response metadata.

#### `x-skipcache`
Bypass query cache (if caching is implemented).

**Format:** Boolean (true/false)
```
x-skipcache: true
```

#### `x-fetch-rownumber`
Get the row number of a specific record in the result set.

**Format:** Record identifier
```
x-fetch-rownumber: record123
```

⚠️ **Note:** Not yet implemented.

#### `x-pkrow`
Similar to `x-fetch-rownumber` - get row number by primary key.

**Format:** Primary key value
```
x-pkrow: 123
```

⚠️ **Note:** Not yet implemented.

---

### 6. Response Format

#### `x-simpleapi`
Return simple format (just the data array).

**Format:** Presence of header activates it
```
x-simpleapi: true
```

**Response Format:**
```json
[
  { "id": 1, "name": "John" },
  { "id": 2, "name": "Jane" }
]
```

#### `x-detailapi`
Return detailed format with metadata (default).

**Format:** Presence of header activates it
```
x-detailapi: true
```

**Response Format:**
```json
{
  "success": true,
  "data": [...],
  "metadata": {
    "total": 100,
    "filtered": 100,
    "limit": 50,
    "offset": 0
  }
}
```

#### `x-syncfusion`
Format response for Syncfusion UI components.

**Format:** Presence of header activates it
```
x-syncfusion: true
```

**Response Format:**
```json
{
  "result": [...],
  "count": 100
}
```

---

### 7. Transaction Control

#### `x-transaction-atomic`
Use atomic transactions for write operations.

**Format:** Boolean (true/false)
```
x-transaction-atomic: true
```

Ensures that all write operations in the request succeed or fail together.

---

## Base64 Encoding

Headers support base64 encoding for complex values. Use one of these prefixes:

- `ZIP_` - Base64 encoded value
- `__` - Base64 encoded value (double underscore)

**Example:**
```
# Plain value
x-custom-sql-w: status = 'active'

# Base64 encoded (same value)
x-custom-sql-w: ZIP_c3RhdHVzID0gJ2FjdGl2ZSc=
```

---

## Complete Examples

### Example 1: Basic Query

```http
GET /api/employees HTTP/1.1
Host: example.com
x-select-fields: id,first_name,last_name,email,department_id
x-preload: department:id,name
x-searchfilter-name: john
x-searchop-gte-created_at: 2024-01-01
x-sort: -created_at,+last_name
x-limit: 50
x-offset: 0
x-skipcount: false
x-detailapi: true
```

### Example 2: Complex Query with Multiple Filters and Preloads

```http
GET /api/employees HTTP/1.1
Host: example.com
x-select-fields-main: id,first_name,last_name,email,department_id,manager_id
x-preload-1: department:id,name,code
x-preload-2: manager:id,first_name,last_name
x-preload-3: projects:id,name,status
x-fieldfilter-status-1: active
x-searchop-gte-created_at-filter1: 2024-01-01
x-searchop-lt-created_at-filter2: 2024-12-31
x-searchfilter-name-query: smith
x-sort-primary: -created_at
x-sort-secondary: +last_name
x-limit-page: 100
x-offset-page: 0
x-detailapi: true
```

**Note:** The identifiers after the header names (like `-main`, `-1`, `-filter1`, etc.) are optional and help organize multiple headers of the same type. Both approaches work:

```http
# Without identifiers
x-preload: employees
x-preload: department

# With identifiers (more organized)
x-preload-1: employees
x-preload-2: department
```

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "id": "emp1",
      "first_name": "John",
      "last_name": "Doe",
      "email": "john@example.com",
      "department_id": "dept1",
      "department": {
        "id": "dept1",
        "name": "Engineering"
      }
    }
  ],
  "metadata": {
    "total": 1,
    "filtered": 1,
    "limit": 50,
    "offset": 0
  }
}
```

---

## HTTP Method Mapping

- `GET /{schema}/{entity}` - List all records
- `GET /{schema}/{entity}/{id}` - Get single record
- `POST /{schema}/{entity}` - Create record(s)
- `PUT /{schema}/{entity}/{id}` - Update record
- `PATCH /{schema}/{entity}/{id}` - Partial update
- `DELETE /{schema}/{entity}/{id}` - Delete record
- `GET /{schema}/{entity}/metadata` - Get table metadata

---

## Implementation Status

✅ **Implemented:**
- Field selection (select/omit columns)
- Filtering (field filters, search filters, operators)
- Preloading relations
- Sorting and pagination
- Skip count optimization
- Response format options
- Base64 decoding

⚠️ **Partially Implemented:**
- Expand (currently falls back to preload)
- DISTINCT (depends on ORM adapter)

🚧 **Planned:**
- Advanced SQL expressions (advsql, cql-sel)
- Custom SQL joins
- Cursor pagination
- Row number fetching
- Full expand with JOIN
- Query caching control

---

## Security Considerations

1. **SQL Injection**: Custom SQL headers (`x-custom-sql-*`) should be properly sanitized or restricted to trusted users only.

2. **Query Complexity**: Consider implementing query complexity limits to prevent resource exhaustion.

3. **Authentication**: Implement proper authentication and authorization checks before processing requests.

4. **Rate Limiting**: Apply rate limiting to prevent abuse.

5. **Field Restrictions**: Consider implementing field-level permissions to restrict access to sensitive columns.

---

## Performance Tips

1. Use `x-skipcount: true` for large datasets when you don't need the total count
2. Select only needed columns with `x-select-fields`
3. Use preload wisely - only load relations you need
4. Implement proper database indexes for filtered and sorted columns
5. Consider pagination for large result sets

---

## Migration from ResolveSpec

RestHeadSpec is an alternative to ResolveSpec that uses headers instead of request body for options:

**ResolveSpec (body-based):**
```json
POST /api/departments
{
  "operation": "read",
  "options": {
    "preload": [{"relation": "employees"}],
    "filters": [{"column": "status", "operator": "eq", "value": "active"}],
    "limit": 50
  }
}
```

**RestHeadSpec (header-based):**
```http
GET /api/departments
x-preload: employees
x-fieldfilter-status: active
x-limit: 50
```

Both implementations share the same core handler logic and database adapters.
