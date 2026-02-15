# ResolveSpec JS - Implementation Plan

TypeScript client library for ResolveSpec, RestHeaderSpec, WebSocket and MQTT APIs.

---

## Status

| Phase | Description | Status |
|-------|-------------|--------|
| 0 | Restructure into folders | Done |
| 1 | Fix types (align with Go) | Done |
| 2 | Fix REST client | Done |
| 3 | Build config | Done |
| 4 | Tests | Done |
| 5 | HeaderSpec client | Done |
| 6 | MQTT client | Planned |
| 6.5 | Unified class pattern + singleton factories | Done |
| 7 | Response cache (TTL) | Planned |
| 8 | TanStack Query integration | Planned |
| 9 | React Hooks | Planned |

**Build:** `dist/index.js` (ES) + `dist/index.cjs` (CJS) + `.d.ts` declarations
**Tests:** 65 passing (common: 10, resolvespec: 13, websocketspec: 15, headerspec: 27)

---

## Folder Structure

```
src/
├── common/
│   ├── types.ts             # Core types aligned with Go pkg/common/types.go
│   └── index.ts
├── resolvespec/
│   ├── client.ts            # ResolveSpecClient class + createResolveSpecClient singleton
│   └── index.ts
├── headerspec/
│   ├── client.ts            # HeaderSpecClient class + createHeaderSpecClient singleton + buildHeaders utility
│   └── index.ts
├── websocketspec/
│   ├── types.ts             # WS-specific types (WSMessage, WSOptions, etc.)
│   ├── client.ts            # WebSocketClient class + createWebSocketClient singleton
│   └── index.ts
├── mqttspec/                # Future
│   ├── types.ts
│   ├── client.ts
│   └── index.ts
├── __tests__/
│   ├── common.test.ts
│   ├── resolvespec.test.ts
│   ├── headerspec.test.ts
│   └── websocketspec.test.ts
└── index.ts                 # Root barrel export
```

---

## Type Alignment with Go

Types in `src/common/types.ts` match `pkg/common/types.go`:

- **Operator**: `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `like`, `ilike`, `in`, `contains`, `startswith`, `endswith`, `between`, `between_inclusive`, `is_null`, `is_not_null`
- **FilterOption**: `column`, `operator`, `value`, `logic_operator` (AND/OR)
- **Options**: `columns`, `omit_columns`, `filters`, `sort`, `limit`, `offset`, `preload`, `customOperators`, `computedColumns`, `parameters`, `cursor_forward`, `cursor_backward`, `fetch_row_number`
- **PreloadOption**: `relation`, `table_name`, `columns`, `omit_columns`, `sort`, `filters`, `where`, `limit`, `offset`, `updatable`, `recursive`, `computed_ql`, `primary_key`, `related_key`, `foreign_key`, `recursive_child_key`, `sql_joins`, `join_aliases`
- **Parameter**: `name`, `value`, `sequence?`
- **Metadata**: `total`, `count`, `filtered`, `limit`, `offset`, `row_number?`
- **APIError**: `code`, `message`, `details?`, `detail?`

---

## HeaderSpec Header Mapping

Maps Options to HTTP headers per Go `restheadspec/headers.go`:

| Header | Options field | Format |
|--------|--------------|--------|
| `X-Select-Fields` | `columns` | comma-separated |
| `X-Not-Select-Fields` | `omit_columns` | comma-separated |
| `X-FieldFilter-{col}` | `filters` (eq, AND) | value |
| `X-SearchOp-{op}-{col}` | `filters` (AND) | value |
| `X-SearchOr-{op}-{col}` | `filters` (OR) | value |
| `X-Sort` | `sort` | `+col` (asc), `-col` (desc) |
| `X-Limit` | `limit` | number |
| `X-Offset` | `offset` | number |
| `X-Cursor-Forward` | `cursor_forward` | string |
| `X-Cursor-Backward` | `cursor_backward` | string |
| `X-Preload` | `preload` | `Rel:col1,col2` pipe-separated |
| `X-Fetch-RowNumber` | `fetch_row_number` | string |
| `X-CQL-SEL-{col}` | `computedColumns` | expression |
| `X-Custom-SQL-W` | `customOperators` | SQL AND-joined |

Complex values use `ZIP_` + base64 encoding.
HTTP methods: GET=read, POST=create, PUT=update, DELETE=delete.

---

## Build & Test

```bash
pnpm install
pnpm run build    # vite library mode → dist/
pnpm run test     # vitest
pnpm run lint     # eslint
```

**Config files:** `tsconfig.json` (ES2020, strict, bundler), `vite.config.ts` (lib mode, dts via vite-plugin-dts)
**Externals:** `uuid`, `semver`

---

## Remaining Work

- **Phase 6 — MQTT Client**: Topic-based CRUD over MQTT (optional/future)
- **Phase 7 — Cache**: In-memory response cache with TTL, key = URL + options hash, auto-invalidation on CUD, `skipCache` flag
- **Phase 8 — TanStack Query Integration**: Query/mutation hooks wrapping each client, query key factories, automatic cache invalidation
- **Phase 9 — React Hooks**: `useResolveSpec`, `useHeaderSpec`, `useWebSocket` hooks with provider context, loading/error states
- ESLint config may need updating for new folder structure

---

## Reference Files

| Purpose | Path |
|---------|------|
| Go types (source of truth) | `pkg/common/types.go` |
| Go REST handler | `pkg/resolvespec/handler.go` |
| Go HeaderSpec handler | `pkg/restheadspec/handler.go` |
| Go HeaderSpec header parsing | `pkg/restheadspec/headers.go` |
| Go test models | `pkg/testmodels/business.go` |
| Go tests | `tests/crud_test.go` |
