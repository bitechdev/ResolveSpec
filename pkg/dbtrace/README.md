# dbtrace

Per-request DB call counting + pool logging. Off by default.

## Enable
| Config (`db_trace.*`) | Env | Meaning |
|---|---|---|
| `enabled` | `RESOLVESPEC_DB_TRACE_ENABLED` | per-request logging |
| `min_calls` (5) | `..._MIN_CALLS` | log if tx+pooled+raw >= N |
| `min_duration` (0) | `..._MIN_DURATION` | or request took >= D |
| `pool_log` | `..._POOL_LOG` | log pool stats on each dbmanager metrics publish |

Wire: `dbtrace.Configure(dbtrace.FromConfig(cfg.DBTrace))` and wrap handlers with `dbtrace.Middleware` (outside the auth middleware).

## Log fields
- `tx` transactions begun · `tx_queries` adapter queries inside `RunInTransaction` (share the tx connection)
- `pooled` adapter queries outside a tx (each takes a pool connection)
- `raw` direct `*sql.DB` calls, with kinds: `auth.session`, `auth.activity`, `security.column`, `security.row`, `probe.pg_proc` (lookup `ModeAuto` only), `keystore.validate`
- Connections used ≈ `tx + pooled + raw`

## Pool log
`dbtrace pool <name>: open in_use idle max opened=+N waits=+N wait_time=+D` — `opened`/`waits` are deltas since last publish.

## Limits
- tx attribution is per request, assumes sequential use of a request's context
- `auth.activity` runs detached after the response: not in the request's log line
- `BeginTx`/`CommitTx` (manual tx) not counted; only `RunInTransaction`
- Raw counters cover the hot paths listed above only (not login/OAuth/passkey/TOTP)
