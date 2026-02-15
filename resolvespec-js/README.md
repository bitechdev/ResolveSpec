# ResolveSpec JS

TypeScript client library for ResolveSpec APIs. Supports body-based REST, header-based REST, and WebSocket protocols.

## Install

```bash
pnpm add @warkypublic/resolvespec-js
```

## Clients

| Client | Protocol | Singleton Factory |
| --- | --- | --- |
| `ResolveSpecClient` | REST (body-based) | `getResolveSpecClient(config)` |
| `HeaderSpecClient` | REST (header-based) | `getHeaderSpecClient(config)` |
| `WebSocketClient` | WebSocket | `getWebSocketClient(config)` |

All clients use the class pattern. Singleton factories return cached instances keyed by URL.

## REST Client (Body-Based)

Options sent in JSON request body. Maps to Go `pkg/resolvespec`.

```typescript
import { ResolveSpecClient, getResolveSpecClient } from '@warkypublic/resolvespec-js';

// Class instantiation
const client = new ResolveSpecClient({ baseUrl: 'http://localhost:3000', token: 'your-token' });

// Or singleton factory (returns cached instance per baseUrl)
const client = getResolveSpecClient({ baseUrl: 'http://localhost:3000', token: 'your-token' });

// Read with filters, sort, pagination
const result = await client.read('public', 'users', undefined, {
  columns: ['id', 'name', 'email'],
  filters: [{ column: 'status', operator: 'eq', value: 'active' }],
  sort: [{ column: 'name', direction: 'asc' }],
  limit: 10,
  offset: 0,
  preload: [{ relation: 'Posts', columns: ['id', 'title'] }],
});

// Read by ID
const user = await client.read('public', 'users', 42);

// Create
const created = await client.create('public', 'users', { name: 'New User' });

// Update
await client.update('public', 'users', { name: 'Updated' }, 42);

// Delete
await client.delete('public', 'users', 42);

// Metadata
const meta = await client.getMetadata('public', 'users');
```

## HeaderSpec Client (Header-Based)

Options sent via HTTP headers. Maps to Go `pkg/restheadspec`.

```typescript
import { HeaderSpecClient, getHeaderSpecClient } from '@warkypublic/resolvespec-js';

const client = new HeaderSpecClient({ baseUrl: 'http://localhost:3000', token: 'your-token' });
// Or: const client = getHeaderSpecClient({ baseUrl: 'http://localhost:3000', token: 'your-token' });

// GET with options as headers
const result = await client.read('public', 'users', undefined, {
  columns: ['id', 'name'],
  filters: [
    { column: 'status', operator: 'eq', value: 'active' },
    { column: 'age', operator: 'gte', value: 18, logic_operator: 'AND' },
  ],
  sort: [{ column: 'name', direction: 'asc' }],
  limit: 50,
  preload: [{ relation: 'Department', columns: ['id', 'name'] }],
});

// POST create
await client.create('public', 'users', { name: 'New User' });

// PUT update
await client.update('public', 'users', '42', { name: 'Updated' });

// DELETE
await client.delete('public', 'users', '42');
```

### Header Mapping

| Header | Options Field | Format |
| --- | --- | --- |
| `X-Select-Fields` | `columns` | comma-separated |
| `X-Not-Select-Fields` | `omit_columns` | comma-separated |
| `X-FieldFilter-{col}` | `filters` (eq, AND) | value |
| `X-SearchOp-{op}-{col}` | `filters` (AND) | value |
| `X-SearchOr-{op}-{col}` | `filters` (OR) | value |
| `X-Sort` | `sort` | `+col` asc, `-col` desc |
| `X-Limit` / `X-Offset` | `limit` / `offset` | number |
| `X-Cursor-Forward` | `cursor_forward` | string |
| `X-Cursor-Backward` | `cursor_backward` | string |
| `X-Preload` | `preload` | `Rel:col1,col2` pipe-separated |
| `X-Fetch-RowNumber` | `fetch_row_number` | string |
| `X-CQL-SEL-{col}` | `computedColumns` | expression |
| `X-Custom-SQL-W` | `customOperators` | SQL AND-joined |

### Utility Functions

```typescript
import { buildHeaders, encodeHeaderValue, decodeHeaderValue } from '@warkypublic/resolvespec-js';

const headers = buildHeaders({ columns: ['id', 'name'], limit: 10 });
// => { 'X-Select-Fields': 'id,name', 'X-Limit': '10' }

const encoded = encodeHeaderValue('complex value');  // 'ZIP_...'
const decoded = decodeHeaderValue(encoded);           // 'complex value'
```

## WebSocket Client

Real-time CRUD with subscriptions. Maps to Go `pkg/websocketspec`.

```typescript
import { WebSocketClient, getWebSocketClient } from '@warkypublic/resolvespec-js';

const ws = new WebSocketClient({
  url: 'ws://localhost:8080/ws',
  reconnect: true,
  heartbeatInterval: 30000,
});
// Or: const ws = getWebSocketClient({ url: 'ws://localhost:8080/ws' });

await ws.connect();

// CRUD
const users = await ws.read('users', { schema: 'public', limit: 10 });
const created = await ws.create('users', { name: 'New' }, { schema: 'public' });
await ws.update('users', '1', { name: 'Updated' });
await ws.delete('users', '1');

// Subscribe to changes
const subId = await ws.subscribe('users', (notification) => {
  console.log(notification.operation, notification.data);
});

// Unsubscribe
await ws.unsubscribe(subId);

// Events
ws.on('connect', () => console.log('connected'));
ws.on('disconnect', () => console.log('disconnected'));
ws.on('error', (err) => console.error(err));

ws.disconnect();
```

## Types

All types align with Go `pkg/common/types.go`.

### Key Types

```typescript
interface Options {
  columns?: string[];
  omit_columns?: string[];
  filters?: FilterOption[];
  sort?: SortOption[];
  limit?: number;
  offset?: number;
  preload?: PreloadOption[];
  customOperators?: CustomOperator[];
  computedColumns?: ComputedColumn[];
  parameters?: Parameter[];
  cursor_forward?: string;
  cursor_backward?: string;
  fetch_row_number?: string;
}

interface FilterOption {
  column: string;
  operator: Operator | string;
  value: any;
  logic_operator?: 'AND' | 'OR';
}

// Operators: eq, neq, gt, gte, lt, lte, like, ilike, in,
//            contains, startswith, endswith, between,
//            between_inclusive, is_null, is_not_null

interface APIResponse<T> {
  success: boolean;
  data: T;
  metadata?: Metadata;
  error?: APIError;
}
```

## Build

```bash
pnpm install
pnpm run build    # dist/index.js (ES) + dist/index.cjs (CJS) + .d.ts
pnpm run test     # vitest
pnpm run lint     # eslint
```

## License

MIT
