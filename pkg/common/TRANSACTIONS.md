# Request Transactions (cheatsheet)

Every DB statement and every DB-touching hook of one request runs on one transaction. Hooks never get the pool.

## Rules
- `hookCtx.Tx` is always the open transaction (never `h.db`), except `BeforeHandle`, which runs before any tx and must not touch the DB.
- `OnTxBegin` fires once, first, in every tx the handler opens (incl. the second short tx).
- `OnTxBegin` error or abort: rollback, client gets a generic error, nothing leaked.
- Begin or commit failure: generic error (`transaction_error` / "Transaction failed" in websocketspec, mqttspec, funcspec).
- Transaction-local state (`set_config(..., true)`, RLS GUCs) is only visible on that tx. Set it in `OnTxBegin`.

## Transactions per operation
| Operation | Tx 1 | Tx 2 (short, after commit) |
|---|---|---|
| read | `BeforeRead`, count, scan, `AfterRead`* | restheadspec: `AfterRead` |
| create | `Before*`, insert | re-fetch, `BeforeScan`, `AfterCreate` |
| update | `Before*`, select, update, `AfterUpdate`† | re-fetch (+ `AfterUpdate` where noted) |
| delete (single/batch) | `BeforeDelete`, select, delete, `AfterDelete` | none |
| funcspec query | `BeforeQuery*`, `BeforeSQLExec`, SQL, `After*` | `BeforeResponse` |

\* resolvespec, websocketspec, mqttspec, resolvemcp. restheadspec runs `AfterRead` in tx 2.
† restheadspec, websocketspec, mqttspec run `AfterUpdate` in tx 2. resolvespec, resolvemcp run it in tx 1.

resolvespec has no tx 2 for create: `AfterCreate` runs in tx 1, once per record (per item in a batch), after the insert and re-fetch. Its `AfterRead` gets the scanned slice (single and list reads) and may mask in place; a failing `AfterRead` fails the read (fail closed).

Tx 2 exists so the re-fetch sees trigger changes from the committed write.

## Per spec
| Spec | `OnTxBegin` | Helper |
|---|---|---|
| resolvespec, restheadspec, websocketspec, resolvemcp, funcspec | own `HookType` = `common.TxHookName` | `Handler.runInTx` |
| mqttspec | re-exports `websocketspec.OnTxBegin` | `Handler.runInTx` |

- `common.RunRequestTx(ctx, db, TxContext, onBegin, body)`: open tx, `SetTx`, `onBegin`, `body`.
- `common.TxContext`: `SetTx(tx)`; implemented by each spec's `HookContext`.

## RLS / transaction settings (pkg/security)
- `SecurityList.SetTxSettings(fn)`: `fn(SecurityContext) (map[string]string, error)`; nil disables.
- Every spec's `RegisterSecurityHooks` registers `OnTxBegin` → `security.StampTxSettings`. `fn` is read per call, so set order does not matter.
- Stamps via `set_config(name, value, true)` in name order, before any other SQL.
- Fail closed: `fn` error, invalid name, or non-Postgres driver with a non-empty map aborts the tx.
- Name: dotted identifier (`ns.name`). Value is hex-encoded in SQL, never inlined.
- Low level: `security.ApplyTxSettings(secCtx, tx, map)`.

## Test notes
- sqlmock + `SetMaxOpenConns(1)`: any pool use inside an open tx blocks and fails.
- restheadspec model-based updates/reads need the bun adapter.
