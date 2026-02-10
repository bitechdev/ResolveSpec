# ResolveSpec Query Features Examples

This document provides examples of using the advanced query features in ResolveSpec, including OR logic filters, Custom Operators, and FetchRowNumber.

## OR Logic in Filters (SearchOr)

### Basic OR Filter Example

Find all users with status "active" OR "pending":

```json
POST /users
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      },
      {
        "column": "status",
        "operator": "eq",
        "value": "pending",
        "logic_operator": "OR"
      }
    ]
  }
}
```

### Combined AND/OR Filters

Find users with (status="active" OR status="pending") AND age >= 18:

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      },
      {
        "column": "status",
        "operator": "eq",
        "value": "pending",
        "logic_operator": "OR"
      },
      {
        "column": "age",
        "operator": "gte",
        "value": 18
      }
    ]
  }
}
```

**SQL Generated:** `WHERE (status = 'active' OR status = 'pending') AND age >= 18`

**Important Notes:**
- By default, filters use AND logic
- Consecutive filters with `"logic_operator": "OR"` are automatically grouped with parentheses
- This grouping ensures OR conditions don't interfere with AND conditions
- You don't need to specify `"logic_operator": "AND"` as it's the default

### Multiple OR Groups

You can have multiple separate OR groups:

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      },
      {
        "column": "status",
        "operator": "eq",
        "value": "pending",
        "logic_operator": "OR"
      },
      {
        "column": "priority",
        "operator": "eq",
        "value": "high"
      },
      {
        "column": "priority",
        "operator": "eq",
        "value": "urgent",
        "logic_operator": "OR"
      }
    ]
  }
}
```

**SQL Generated:** `WHERE (status = 'active' OR status = 'pending') AND (priority = 'high' OR priority = 'urgent')`

## Custom Operators

### Simple Custom SQL Condition

Filter by email domain using custom SQL:

```json
{
  "operation": "read",
  "options": {
    "customOperators": [
      {
        "name": "company_emails",
        "sql": "email LIKE '%@company.com'"
      }
    ]
  }
}
```

### Multiple Custom Operators

Combine multiple custom SQL conditions:

```json
{
  "operation": "read",
  "options": {
    "customOperators": [
      {
        "name": "recent_active",
        "sql": "last_login > NOW() - INTERVAL '30 days'"
      },
      {
        "name": "high_score",
        "sql": "score > 1000"
      }
    ]
  }
}
```

### Complex Custom Operator

Use complex SQL expressions:

```json
{
  "operation": "read",
  "options": {
    "customOperators": [
      {
        "name": "priority_users",
        "sql": "(subscription = 'premium' AND points > 500) OR (subscription = 'enterprise')"
      }
    ]
  }
}
```

### Combining Custom Operators with Regular Filters

Mix custom operators with standard filters:

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "country",
        "operator": "eq",
        "value": "USA"
      }
    ],
    "customOperators": [
      {
        "name": "active_last_month",
        "sql": "last_activity > NOW() - INTERVAL '1 month'"
      }
    ]
  }
}
```

## Row Numbers

### Two Ways to Get Row Numbers

There are two different features for row numbers:

1. **`fetch_row_number`** - Get the position of ONE specific record in a sorted/filtered set
2. **`RowNumber` field in models** - Automatically number all records in the response

### 1. FetchRowNumber - Get Position of Specific Record

Get the rank/position of a specific user in a leaderboard. **Important:** When `fetch_row_number` is specified, the response contains **ONLY that specific record**, not all records.

```json
{
  "operation": "read",
  "options": {
    "sort": [
      {
        "column": "score",
        "direction": "desc"
      }
    ],
    "fetch_row_number": "12345"
  }
}
```

**Response - Contains ONLY the specified user:**
```json
{
  "success": true,
  "data": {
    "id": 12345,
    "name": "Alice Smith",
    "score": 9850,
    "level": 42
  },
  "metadata": {
    "total": 10000,
    "count": 1,
    "filtered": 10000,
    "row_number": 42
  }
}
```

**Result:** User "12345" is ranked #42 out of 10,000 users. The response includes only Alice's data, not the other 9,999 users.

### Row Number with Filters

Find position within a filtered subset (e.g., "What's my rank in my country?"):

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "country",
        "operator": "eq",
        "value": "USA"
      },
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      }
    ],
    "sort": [
      {
        "column": "score",
        "direction": "desc"
      }
    ],
    "fetch_row_number": "12345"
  }
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": 12345,
    "name": "Bob Johnson",
    "country": "USA",
    "score": 7200,
    "status": "active"
  },
  "metadata": {
    "total": 2500,
    "count": 1,
    "filtered": 2500,
    "row_number": 156
  }
}
```

**Result:** Bob is ranked #156 out of 2,500 active USA users. Only Bob's record is returned.

### 2. RowNumber Field - Auto-Number All Records

If your model has a `RowNumber int64` field, restheadspec will automatically populate it for paginated results.

**Model Definition:**
```go
type Player struct {
    ID        int64  `json:"id"`
    Name      string `json:"name"`
    Score     int64  `json:"score"`
    RowNumber int64  `json:"row_number"` // Will be auto-populated
}
```

**Request (with pagination):**
```json
{
  "operation": "read",
  "options": {
    "sort": [{"column": "score", "direction": "desc"}],
    "limit": 10,
    "offset": 20
  }
}
```

**Response - RowNumber automatically set:**
```json
{
  "success": true,
  "data": [
    {
      "id": 456,
      "name": "Player21",
      "score": 8900,
      "row_number": 21
    },
    {
      "id": 789,
      "name": "Player22",
      "score": 8850,
      "row_number": 22
    },
    {
      "id": 123,
      "name": "Player23",
      "score": 8800,
      "row_number": 23
    }
    // ... records 24-30 ...
  ]
}
```

**How It Works:**
- `row_number = offset + index + 1` (1-based)
- With offset=20, first record gets row_number=21
- With offset=20, second record gets row_number=22
- Perfect for displaying "Rank" in paginated tables

**Use Case:** Displaying leaderboards with rank numbers:
```
Rank | Player    | Score
-----|-----------|-------
21   | Player21  | 8900
22   | Player22  | 8850
23   | Player23  | 8800
```

**Note:** This feature is available in all three packages: resolvespec, restheadspec, and websocketspec.

### When to Use Each Feature

| Feature | Use Case | Returns | Performance |
|---------|----------|---------|-------------|
| `fetch_row_number` | "What's my rank?" | 1 record with position | Fast - 1 record |
| `RowNumber` field | "Show top 10 with ranks" | Many records numbered | Fast - simple math |

**Combined Example - Full Leaderboard UI:**

```javascript
// Request 1: Get current user's rank
const userRank = await api.read({
  fetch_row_number: currentUserId,
  sort: [{column: "score", direction: "desc"}]
});
// Returns: {id: 123, name: "You", score: 7500, row_number: 156}

// Request 2: Get top 10 with rank numbers
const top10 = await api.read({
  sort: [{column: "score", direction: "desc"}],
  limit: 10,
  offset: 0
});
// Returns: [{row_number: 1, ...}, {row_number: 2, ...}, ...]

// Display:
// "Your Rank: #156"
// "Top Players:"
// "#1 - Alice - 9999"
// "#2 - Bob - 9876"
// ...
```

## Complete Example: Advanced Query

Combine all features for a complex query:

```json
{
  "operation": "read",
  "options": {
    "columns": ["id", "name", "email", "score", "status"],
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      },
      {
        "column": "status",
        "operator": "eq",
        "value": "trial",
        "logic_operator": "OR"
      },
      {
        "column": "score",
        "operator": "gte",
        "value": 100
      }
    ],
    "customOperators": [
      {
        "name": "recent_activity",
        "sql": "last_login > NOW() - INTERVAL '7 days'"
      },
      {
        "name": "verified_email",
        "sql": "email_verified = true"
      }
    ],
    "sort": [
      {
        "column": "score",
        "direction": "desc"
      },
      {
        "column": "created_at",
        "direction": "asc"
      }
    ],
    "fetch_row_number": "12345",
    "limit": 50,
    "offset": 0
  }
}
```

This query:
- Selects specific columns
- Filters for users with status "active" OR "trial"
- AND score >= 100
- Applies custom SQL conditions for recent activity and verified emails
- Sorts by score (descending) then creation date (ascending)
- Returns the row number of user "12345" in this filtered/sorted set
- Returns 50 records starting from the first one

## Use Cases

### 1. Leaderboards - Get Current User's Rank

Get the current user's position and data (returns only their record):

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "game_id",
        "operator": "eq",
        "value": "game123"
      }
    ],
    "sort": [
      {
        "column": "score",
        "direction": "desc"
      }
    ],
    "fetch_row_number": "current_user_id"
  }
}
```

**Tip:** For full leaderboards, make two requests:
1. One with `fetch_row_number` to get user's rank
2. One with `limit` and `offset` to get top players list

### 2. Multi-Status Search

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "order_status",
        "operator": "eq",
        "value": "pending"
      },
      {
        "column": "order_status",
        "operator": "eq",
        "value": "processing",
        "logic_operator": "OR"
      },
      {
        "column": "order_status",
        "operator": "eq",
        "value": "shipped",
        "logic_operator": "OR"
      }
    ]
  }
}
```

### 3. Advanced Date Filtering

```json
{
  "operation": "read",
  "options": {
    "customOperators": [
      {
        "name": "this_month",
        "sql": "created_at >= DATE_TRUNC('month', CURRENT_DATE)"
      },
      {
        "name": "business_hours",
        "sql": "EXTRACT(HOUR FROM created_at) BETWEEN 9 AND 17"
      }
    ]
  }
}
```

## Security Considerations

**Warning:** Custom operators allow raw SQL, which can be a security risk if not properly handled:

1. **Never** directly interpolate user input into custom operator SQL
2. Always validate and sanitize custom operator SQL on the backend
3. Consider using a whitelist of allowed custom operators
4. Use prepared statements or parameterized queries when possible
5. Implement proper authorization checks before executing queries

Example of safe custom operator handling in Go:

```go
// Whitelist of allowed custom operators
allowedOperators := map[string]string{
    "recent_week": "created_at > NOW() - INTERVAL '7 days'",
    "active_users": "status = 'active' AND last_login > NOW() - INTERVAL '30 days'",
    "premium_only": "subscription_level = 'premium'",
}

// Validate custom operators from request
for _, op := range req.Options.CustomOperators {
    if sql, ok := allowedOperators[op.Name]; ok {
        op.SQL = sql  // Use whitelisted SQL
    } else {
        return errors.New("custom operator not allowed: " + op.Name)
    }
}
```
