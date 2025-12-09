# Automatic Relation Loading Strategies

## Overview

**NEW:** The database adapters now **automatically** choose the optimal loading strategy by inspecting your model's relationship tags!

Simply use `PreloadRelation()` and the system automatically:
- Detects relationship type from Bun/GORM tags
- Uses **JOIN** for many-to-one and one-to-one (efficient, no duplication)
- Uses **separate query** for one-to-many and many-to-many (avoids duplication)

## How It Works

```go
// Just write this - the system handles the rest!
db.NewSelect().
    Model(&links).
    PreloadRelation("Provider").  // ✓ Auto-detects belongs-to → uses JOIN
    PreloadRelation("Tags").      // ✓ Auto-detects has-many → uses separate query
    Scan(ctx, &links)
```

### Detection Logic

The system inspects your model's struct tags:

**Bun models:**
```go
type Link struct {
    Provider   *Provider `bun:"rel:belongs-to"`   // → Detected: belongs-to → JOIN
    Tags       []Tag     `bun:"rel:has-many"`     // → Detected: has-many → Separate query
}
```

**GORM models:**
```go
type Link struct {
    ProviderID int
    Provider   *Provider `gorm:"foreignKey:ProviderID"`  // → Detected: belongs-to → JOIN
    Tags       []Tag     `gorm:"many2many:link_tags"`    // → Detected: many-to-many → Separate query
}
```

**Type inference (fallback):**
- `[]Type` (slice) → has-many → Separate query
- `*Type` (pointer) → belongs-to → JOIN
- `Type` (struct) → belongs-to → JOIN

### What Gets Logged

Enable debug logging to see strategy selection:

```go
bunAdapter.EnableQueryDebug()
```

**Output:**
```
DEBUG: PreloadRelation 'Provider' detected as: belongs-to
INFO:  Using JOIN strategy for belongs-to relation 'Provider'
DEBUG: PreloadRelation 'Links' detected as: has-many
DEBUG: Using separate query for has-many relation 'Links'
```

## Relationship Types

| Bun Tag | GORM Pattern | Field Type | Strategy | Why |
|---------|--------------|------------|----------|-----|
| `rel:has-many` | Slice field | `[]Type` | Separate Query | Avoids duplicating parent data |
| `rel:belongs-to` | `foreignKey:` | `*Type` | JOIN | Single parent, no duplication |
| `rel:has-one` | Single pointer | `*Type` | JOIN | One-to-one, no duplication |
| `rel:many-to-many` | `many2many:` | `[]Type` | Separate Query | Complex join, avoid cartesian |

## Manual Override

If you need to force a specific strategy, use `JoinRelation()`:

```go
// Force JOIN even for has-many (not recommended)
db.NewSelect().
    Model(&providers).
    JoinRelation("Links").  // Explicitly use JOIN
    Scan(ctx, &providers)
```

## Examples

### Automatic Strategy Selection (Recommended)

```go
// Example 1: Loading parent provider for each link
// System detects belongs-to → uses JOIN automatically
db.NewSelect().
    Model(&links).
    PreloadRelation("Provider", func(q common.SelectQuery) common.SelectQuery {
        return q.Where("active = ?", true)
    }).
    Scan(ctx, &links)

// Generated SQL: Single query with JOIN
// SELECT links.*, providers.*
// FROM links
// LEFT JOIN providers ON links.provider_id = providers.id
// WHERE providers.active = true

// Example 2: Loading child links for each provider
// System detects has-many → uses separate query automatically
db.NewSelect().
    Model(&providers).
    PreloadRelation("Links", func(q common.SelectQuery) common.SelectQuery {
        return q.Where("active = ?", true)
    }).
    Scan(ctx, &providers)

// Generated SQL: Two queries
// Query 1: SELECT * FROM providers
// Query 2: SELECT * FROM links
//          WHERE provider_id IN (1, 2, 3, ...)
//          AND active = true
```

### Mixed Relationships

```go
type Order struct {
    ID         int
    CustomerID int
    Customer   *Customer `bun:"rel:belongs-to"`    // JOIN
    Items      []Item    `bun:"rel:has-many"`      // Separate
    Invoice    *Invoice  `bun:"rel:has-one"`       // JOIN
}

// All three handled optimally!
db.NewSelect().
    Model(&orders).
    PreloadRelation("Customer").  // → JOIN (many-to-one)
    PreloadRelation("Items").     // → Separate (one-to-many)
    PreloadRelation("Invoice").   // → JOIN (one-to-one)
    Scan(ctx, &orders)
```

## Performance Benefits

### Before (Manual Strategy Selection)

```go
// You had to remember which to use:
.PreloadRelation("Provider")  // Should I use PreloadRelation or JoinRelation?
.PreloadRelation("Links")     // Which is more efficient here?
```

### After (Automatic Selection)

```go
// Just use PreloadRelation everywhere:
.PreloadRelation("Provider")  // ✓ System uses JOIN automatically
.PreloadRelation("Links")     // ✓ System uses separate query automatically
```

## Migration Guide

**No changes needed!** If you're already using `PreloadRelation()`, it now automatically optimizes:

```go
// Before: Always used separate query
.PreloadRelation("Provider")  // Inefficient: extra round trip

// After: Automatic optimization
.PreloadRelation("Provider")  // ✓ Now uses JOIN automatically!
```

## Implementation Details

### Supported Bun Tags
- `rel:has-many` → Separate query
- `rel:belongs-to` → JOIN
- `rel:has-one` → JOIN
- `rel:many-to-many` or `rel:m2m` → Separate query

### Supported GORM Patterns
- `many2many:` tag → Separate query
- `foreignKey:` tag → JOIN (belongs-to)
- `[]Type` slice without many2many → Separate query (has-many)
- `*Type` pointer with foreignKey → JOIN (belongs-to)
- `*Type` pointer without foreignKey → JOIN (has-one)

### Fallback Behavior
- `[]Type` (slice) → Separate query (safe default for collections)
- `*Type` or `Type` (single) → JOIN (safe default for single relations)
- Unknown → Separate query (safest default)

## Debugging

To see strategy selection in action:

```go
// Enable debug logging
bunAdapter.EnableQueryDebug()  // or gormAdapter.EnableQueryDebug()

// Run your query
db.NewSelect().
    Model(&records).
    PreloadRelation("RelationName").
    Scan(ctx, &records)

// Check logs for:
// - "PreloadRelation 'X' detected as: belongs-to"
// - "Using JOIN strategy for belongs-to relation 'X'"
// - Actual SQL queries executed
```

## Best Practices

1. **Use PreloadRelation() for everything** - Let the system optimize
2. **Define proper relationship tags** - Ensures correct detection
3. **Only use JoinRelation() for overrides** - When you know better than auto-detection
4. **Enable debug logging during development** - Verify optimal strategies are chosen
5. **Trust the system** - It's designed to choose correctly based on relationship type
