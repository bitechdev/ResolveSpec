# Audit: `pkg/dbmanager`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/dbmanager` (+ `providers/`) |
| **Files** | `config.go` (489), `connection.go` (722), `manager.go` (401), `metrics.go` (136), `errors.go` (82), `factory.go` (67), `providers/postgres.go` (231), `providers/postgres_listener.go` (401), `providers/sqlite.go` (216), `providers/mongodb.go` (214), `providers/mssql.go` (184), `providers/existing_db.go` (111), `providers/provider.go` (89); tests `factory_test.go` (369), `manager_test.go` (290), `providers/existing_db_test.go` (194), `providers/postgres_listener_example_test.go` (229) |
| **Audit date** | 2026-09-30 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names all attacker-controlled |
| **Depth** | deep (hot package; every request's DB handle comes from here). Several findings were checked with a throw-away probe test against SQLite, and the probe was deleted afterwards |

## Summary

`pkg/dbmanager` owns every database pool in the process. It wraps a
`*sql.DB` (or a `mongo.Client`) in a `sqlConnection` and hands out lazily-built
`*bun.DB`, `*gorm.DB`, raw `*sql.DB` and `common.Database` adapters over it. A
background health checker pings each connection every 15 s, and it can
**reconnect**, which closes the pool and opens a new one.

This audit was started to answer one question: **"why does a database
connection that has been idle for a while become unusable?"** Several defects
in this package combine to give exactly that symptom. They are findings 1–5,
and the [Idle-connection failure chain](#idle-connection-failure-chain) section
below puts them together.

The root design problem is that **`Reconnect` destroys the shared `*sql.DB`**.
`*sql.DB` is already a self-healing pool: it throws away bad connections and
dials new ones. So "reconnecting" a pool is almost never needed, and here it
has a large blast radius. Every `*bun.DB`, `*gorm.DB` and `*sql.DB` handed out
before the reconnect now points at a closed pool, and it stays closed. Only the
`common.Database` adapters carry a factory that can re-fetch a handle, and even
they only use it on a subset of code paths (see `common.audit.md` finding 5).
Those adapter factories also *trigger* `Reconnect` themselves, so one stale
handle closes the pool for everyone else. `Reconnect` isn't atomic, so
concurrent callers turn this into a storm.

The other major theme is **missing client-side deadlines**. `QueryTimeout` is
only ever sent to the server as `statement_timeout`, which does nothing when
the TCP peer has vanished. No `context.WithTimeout` is applied to request
queries, and pgx's dialer sets no `TCP_USER_TIMEOUT`. So the first query on a
pooled connection whose peer silently disappeared (NAT/firewall idle drop,
failover, a pgbouncer restart) can block for minutes. One Close path does this
while holding the connection's write lock, which stalls every request.

## Findings

| # | Severity | Axis | Finding | Status |
|---|---|---|---|---|
| 1 | **Critical** | locking / availability | `Reconnect` closes the shared `*sql.DB`, so every `*bun.DB` / `*gorm.DB` / `*sql.DB` handed out earlier is permanently dead ("sql: database is closed") | Fixed |
| 2 | **High** | locking | Adapter reconnect factories call `Reconnect` on the *shared* connection, and `Reconnect` is not atomic, so one stale handle starts a reconnect storm that repeatedly closes the pool under in-flight requests | Fixed |
| 3 | **High** | slowness / locking | `sqlConnection.HealthCheck` holds the write lock across a network ping for up to 5 s; every `Bun()`/`GORM()`/`Native()`/`Database()`/`Stats()` call blocks for that time | Fixed |
| 4 | **High** | slowness | No client-side query deadline and no `TCP_USER_TIMEOUT`: a query on a silently-dead idle socket blocks for minutes (up to about 15 min); `QueryTimeout` is server-side only, and is forced to at least 2 min | Fixed |
| 5 | **High** | locking / slowness | `PostgresListener.Close` runs `UNLISTEN` with `context.Background()` while `sqlConnection.mu` (write), `PostgresProvider.mu` and `listener.mu` are all held; on a dead socket this freezes every request for minutes | Fixed |
| 6 | **High** | locking / leak | `PostgresListener.Connect` starts a new goroutine pair on every (re)connect; the old pair keeps running, so two loops call `WaitForNotification` on one `pgx.Conn` concurrently, which triggers more reconnects | Fixed |
| 7 | **High** | panic handling | `Connect → Close → Connect → Close` panics with "close of closed channel"; after the first cycle the health checker also exits immediately and silently | Fixed |
| 8 | **Medium** | availability | SQLite: `:memory:` with a 25-connection pool gives every connection its own empty database, and `ConnMaxIdleTime` then silently discards data; `busy_timeout` / WAL pragmas are applied to only one pooled connection | Fixed |
| 9 | **Medium** | availability | Partial failure in `sqlConnection.Close` leaves `connected=true` over a closed pool; partial failure in `Manager.Connect` leaks the connections already opened | Fixed |
| 10 | **Medium** | security | DSN builders concatenate unescaped credentials (postgres key=value, mssql/mongo URLs); `sslmode` defaults to `disable` | Fixed |
| 11 | **Medium** | config | Several config knobs are ignored or impossible to turn off: `EnableAutoReconnect`, `HealthCheckInterval`, `RetryAttempts`/`RetryDelay`/`RetryMaxDelay`, SQLite `_timeout`, and `statement_timeout` when a DSN is given | Fixed |
| 12 | **Medium** | locking | `Manager.Connect` holds `m.mu` across every network dial (up to 3 retries × `ConnectTimeout` per connection) | Fixed |
| 13 | **Low** | observability | `PublishMetrics` / `RecordReconnectAttempt` are never called, so all dbmanager metrics are permanently zero; `*_total` metrics are gauges | Fixed |
| 14 | **Low** | correctness | `Bun()`/`GORM()` do not check `connected`; `getNativeAdapter` uses `PgSQLAdapter` for SQLite and MSSQL; `ExistingDBProvider` applies no pool settings and closes the caller's DB | Fixed (partly, see notes) |
| 15 | **Low** | logging | `Close` / `performHealthCheck` pass key-value pairs to the printf-style logger, which produces `%!(EXTRA ...)` output; `ResetInstance` discards the close error | Fixed |

## Remediation status

Implemented 2026-09-30. `go build ./...` and `go test -race ./pkg/dbmanager/...`
pass. The Postgres behaviour was also verified against a live server (tests are
skipped unless `PG_LIVE=1` / `PG_RESTART_DIR` is set).

**Design decisions taken**
- No automatic reconnect. Adapter factories and the health checker never close
  the pool; they only re-fetch the current handle. `*sql.DB` replaces bad
  connections itself. `EnableAutoReconnect` is deprecated and ignored.
- `Reconnect` is atomic (one critical section) and operator-only. On PostgreSQL
  it goes through a custom `driver.Connector` (`providers/pgconnector.go`): it
  bumps a generation, stale pooled connections are discarded, and the `*sql.DB`
  is never closed, so held Bun/GORM/`*sql.DB` handles keep working. Other
  providers still close and reopen.
- Client-side deadlines are applied at the driver level rather than in the
  adapters (a `context.WithTimeout` around a query is cancelled before the
  caller has read the rows).

**Per finding**
1. Fixed. Postgres refresh keeps the pool; explicit `Reconnect` on other
   providers still invalidates handles (documented in the README).
2. Fixed. Adapter factories no longer call `Reconnect`; `Reconnect` is a single
   critical section under `lifecycleMu` + `mu`.
3. Fixed. The ping runs without `mu`; `lifecycleMu` (read) only keeps
   `Close`/`Reconnect` from tearing the provider down mid-ping. Same for Mongo.
4. Fixed. TCP keepalive and `TCP_USER_TIMEOUT` (30 s, Linux) via `DialFunc`;
   the reuse-time liveness ping is capped at 5 s; `statement_timeout` is set as
   a runtime parameter so it also applies to a supplied DSN; the 2-minute floor
   on `QueryTimeout` is removed. `SetConnMaxIdleTime` tuning remains a
   configuration matter (documented in the README).
5. Fixed. Listener `Close` sends no `UNLISTEN`, closes with a 2 s bound, and
   holds no lock across network I/O.
6. Fixed. Background goroutines start once (`sync.Once`); reconnect dials a
   replacement, re-`LISTEN`s, then swaps it in; sleeps honour `ctx.Done()`.
   Additionally, all use of the single `pgx.Conn` is serialised (`connMu`, 500 ms
   notification poll), fixing "conn busy" from `Listen`/`Unlisten`/`Notify`, and
   old connections are closed under `connMu` (a race found by the live test).
7. Fixed. Stop channel is created per start, guarded by `healthMu`; `Close` is
   idempotent; `Connect` is idempotent.
8. Fixed. `:memory:` is pinned to one connection with no idle/lifetime limits;
   `busy_timeout`/WAL are `_pragma` DSN parameters; `_timeout` and the dead
   reconnect code are removed.
9. Fixed. `Close` always marks disconnected and returns joined errors;
   `PostgresProvider.Close` closes the pool even if the listener fails;
   `Manager.Connect` closes connections it opened when a later one fails.
10. Fixed. Postgres, MSSQL and Mongo DSNs are built as escaped URLs; default
    `sslmode` is now `prefer` (was `disable`).
11. Fixed. Retry settings reach every provider; a negative
    `HealthCheckInterval` disables the health checker; `EnableAutoReconnect`
    deprecated; `statement_timeout` applies with a supplied DSN.
12. Fixed. `Manager.Connect` dials outside `m.mu` and publishes results under it.
13. Fixed. `PublishMetrics` runs on each health-check tick, `Reconnect` records
    `RecordReconnectAttempt`, and the wait/closed metrics are true counters
    (delta-tracked).
14. Partly fixed. `Bun()`/`GORM()` check `connected`; Mongo no longer maps
    `MaxIdleConns` to `MinPoolSize`. `ExistingDBProvider`: `Close` is now a no-op
    that logs a warning (the caller owns the `*sql.DB`; the connection's `Close`
    also skips `bun.DB.Close`), and `Reconnect` only pings. Pool settings are
    still not applied to a caller-owned pool. The `getNativeAdapter` claim was
    stale: the adapter already receives the driver name; the three duplicate
    cases were merged. Mongo `Stats()` is still empty.
15. Fixed. Printf-style logger calls corrected; `ResetInstance` logs the close
    error. Unscrubbed driver errors in Sentry (X8) are not addressed here.

**Behaviour changes**
- Removed tests that closed the pool from outside and expected an adapter to
  swap in a new one (three adapter tests, and the health-check reconnect test,
  now asserting it never reconnects).
- `sslmode` default `prefer`; `NewConnectionFromDB` connections are no longer
  closed by the manager.

**Regression tests added:** `lifecycle_test.go` (double Connect/Close cycle,
idempotent Connect, concurrent Reconnect, adapter factory leaves pool open,
accessors not blocked by health check, Close marks disconnected, existing-DB
Reconnect/Close leave the caller's pool open), `config_dsn_test.go`,
`providers/pgconnector_test.go`, `pg_live_test.go` (refresh keeps handles,
listener Listen/Notify) and `restart_live_test.go` (server crash and restart).

---

## Idle-connection failure chain

This is how findings 1–5 combine into "the connection sat idle and then could
not be used":

1. The app is idle. A NAT, firewall, load balancer or pgbouncer silently drops
   the idle TCP flows. No FIN or RST reaches the process.
2. The next request takes a pooled connection. pgx's `ResetSession` pings it
   because it has been idle for more than 1 s, and that ping uses the request
   ctx, **which has no deadline** (finding 4). The write goes into the kernel
   buffer and the read blocks until TCP retransmission gives up, which can
   take minutes.
   Meanwhile the health checker's 5 s ping times out and holds `c.mu`
   **exclusively** for the whole time (finding 3), so every request trying to
   get a handle queues behind it.
3. Eventually something returns "sql: database is closed" or
   `ErrConnectionClosed`. That can be an adapter that hit a closed pool, or a
   partial `Close` (finding 9). An adapter's `dbFactory` or the health checker
   then calls `Reconnect` (finding 2).
4. `Reconnect` closes the `*sql.DB` (finding 1). If the Postgres listener has
   subscriptions, `Close` first sends `UNLISTEN` on its own dead socket with no
   deadline, still holding the write lock (finding 5), which freezes the
   process again.
5. When the reconnect completes, every handle captured before it is
   permanently broken. That includes the `*gorm.DB` given to
   `resolvespec.NewHandlerWithGORM` in `cmd/testserver/main.go:142,56`, any
   `*bun.DB` passed to `NewHandlerWithBun`, and every Bun `NewSelect`/`NewInsert`
   path. **From this point on, every request that goes through those handles
   fails until the process is restarted.** Concurrent failures run their own
   `Reconnect`s, and each one closes the pool the previous one just opened
   (finding 2).

### Fix order for this symptom

1. **Stop closing the pool to recover from connection errors.** Remove
   `WithDBFactory(c.reopen*ForAdapter)` → `Reconnect`, and remove the
   health-check → `Reconnect` path for SQL providers. `*sql.DB` already discards
   bad connections (`driver.ErrBadConn`, `ResetSession`,
   `SetConnMaxIdleTime`/`SetConnMaxLifetime`). Keep `Reconnect` for explicit
   operator use only, and make it atomic (finding 2).
2. Give every request a deadline. Wrap the request ctx in
   `context.WithTimeout(ctx, QueryTimeout)` in the adapters, or at the handler
   boundary.
3. Set `SetConnMaxIdleTime` **below** the shortest idle timeout of any
   middlebox (typically 60–240 s for cloud NATs and LBs) so idle connections
   are recycled before they can be dropped silently. Also set TCP keepalive and
   `TCP_USER_TIMEOUT` through a custom `pgconn.Config.DialFunc`.
4. Ping without the write lock (finding 3), and give the listener's `Close`
   bounded ctxs (finding 5).

---

### 1. Critical — `Reconnect` kills every previously issued handle

`connection.go:129-160` (`Close`) and `connection.go:187-192` (`Reconnect`):

```go
func (c *sqlConnection) Close() error {
	c.mu.Lock()
	...
	if c.bunDB != nil {
		if err := c.bunDB.Close(); err != nil {   // closes the shared *sql.DB
	...
	if err := c.provider.Close(); err != nil {    // closes it again (idempotent)
	...
	c.nativeDB = nil
	c.bunDB = nil
	c.gormDB = nil
	c.bunAdapter = nil
	...
}

func (c *sqlConnection) Reconnect(ctx context.Context) error {
	if err := c.Close(); err != nil {
		return err
	}
	return c.Connect(ctx)
}
```

`Bun()`, `GORM()` and `Native()` return the handle itself, and callers keep
it: every spec package has a `NewHandlerWithGORM(*gorm.DB)` /
`NewHandlerWithBun(*bun.DB)` constructor, and `cmd/testserver/main.go:142` does
exactly this. After `Reconnect`, the cached fields are nilled, a new pool is
built, and the handles the callers hold point at a `*sql.DB` whose `closed`
flag is set forever.

Verified with a probe: I obtained `conn.GORM()`, called `conn.Reconnect(ctx)`,
then ran a query through the old handle. It returned
`sql: database is closed`, and a fresh `conn.GORM()` worked.

The comment in `manager.go:371-374` shows the authors already knew about this
("forcing Close()+Connect() here invalidates any cached ORM wrappers and callers
that still hold the old handle"). Their mitigation was to narrow *when* the
health checker reconnects. But the adapters' own `dbFactory` still reconnects
unconditionally (finding 2).

**Failure scenario.** Any event that triggers a reconnect turns every
long-lived handler into a permanent 500 generator: a single adapter query hitting
"database is closed", or a health check returning `ErrConnectionClosed`. The
process does not recover without a restart. The same thing happens after a
normal `Manager.Close()` + `Connect()` in tests or hot-reload code.

**Recommendation.** Treat the `*sql.DB` as immortal for the life of the
`sqlConnection`. Don't close it to "reconnect": `database/sql` already replaces
broken connections. If a real re-dial is ever needed (for example after
changing credentials), build the new pool, atomically swap it in, and close the
old one only after a grace period. Give the handles returned by
`Bun()`/`GORM()`/`Native()` stable identity; one way is a `driver.Connector`
that indirects to the current pool.

---

### 2. High — Adapter-triggered, non-atomic `Reconnect` causes a reconnect storm

`connection.go:362-397` and `connection.go:431/474/517-525`:

```go
func (c *sqlConnection) reconnectForAdapter() error {
	...
	return c.Reconnect(ctx)            // Close() then Connect(): two separate lock scopes
}
...
	WithDBFactory(c.reopenBunForAdapter).
```

The adapters (`pkg/common/adapters/database/bun.go:131`, `gorm.go`,
`pgsql.go`) call `dbFactory` whenever an operation returns an error that
matches `"sql: database is closed"`. So:

- **One stale handle closes the pool for everyone.** If an adapter holds a
  `*sql.DB` from before a previous reconnect, its first query fails with
  "database is closed". Its factory then calls `c.Reconnect`, which closes the
  *current, healthy* pool that every other adapter and request is using right
  now.
- **`Reconnect` isn't atomic.** `Close` and `Connect` each take `c.mu`
  separately. Under N concurrent failures, one goroutine closes and reconnects
  while the others either close the brand-new pool again or fail with
  `already connected`. The probe used 20 concurrent `Reconnect`s: 9 returned
  "already connected", and every successful reconnect closed the pool the
  previous winner had just handed to its adapter. Each of those adapters then
  sees "database is closed" on its next query, and the cycle continues.

**Failure scenario.** A burst of traffic arrives just after a reconnect. Each
in-flight request whose adapter still holds the old pool triggers another
`Reconnect`, and each of those closes the pool that the previous request
reopened. The service flaps until traffic stops.

**Recommendation.** Remove the adapter → `Reconnect` path (see finding 1). If
it is kept, make `Reconnect` a single critical section, and add a generation
counter: a caller that saw generation N only reconnects if the current
generation is still N; otherwise it just re-fetches the handle.

---

### 3. High — Health check holds the write lock across a network ping

`connection.go:163-185`:

```go
func (c *sqlConnection) HealthCheck(ctx context.Context) error {
	c.mu.Lock()                    // exclusive
	defer c.mu.Unlock()
	...
	if err := c.provider.HealthCheck(ctx); err != nil {   // PingContext, 5 s timeout
```

Every handle accessor takes `c.mu.RLock()` first (`connection.go:199, 238, 271,
308, 335, 403, 441, 484`). While the health checker (every 15 s, `manager.go:348`)
is pinging, **every request that needs a DB handle waits**. On a healthy
network this is a few ms. On a dead idle socket it's the full 5 s ping timeout
(`providers/postgres.go:155`, inside a 10 s outer ctx).

Verified with a probe: while `c.mu` was held, `conn.Bun()` blocked for the whole
hold (200 ms in the test).

**Failure scenario.** A network blip or a silently dropped idle connection
makes the ping hang. Every 15 s the whole API pauses for up to 5 s. This fits
reports of "idle, then slow or unusable".

**Recommendation.** Snapshot `provider` under `RLock`, release the lock, ping,
then take the lock only to write `healthCheckStatus` / `lastHealthCheck`. Better
still, keep the status in an `atomic.Value`.

---

### 4. High — No client-side query deadline; `QueryTimeout` is server-side only and floored at 2 min

`config.go:223-228`:

```go
if cc.QueryTimeout == 0 {
	cc.QueryTimeout = 2 * time.Minute
} else if cc.QueryTimeout < 2*time.Minute {
	cc.QueryTimeout = 2 * time.Minute
}
```

`config.go:331-335` turns this into `statement_timeout=<ms>` in the Postgres DSN,
and it only does that when the DSN is *built*. A user-supplied `DSN` gets no
timeout at all. Nothing anywhere in the request path wraps ctx in a deadline.
`pkg/config`'s `query_timeout: 30s` default is silently raised to 2 min.

`statement_timeout` is enforced by the **server**, so it only helps if the
server is reachable. On a silently dropped connection:

- pgconn's default dialer is `&net.Dialer{}`: Go's default keepalive (15 s idle,
  15 s interval, 9 probes) and **no `TCP_USER_TIMEOUT`**.
- Once a query has been written, there is unacknowledged data, so keepalive does
  not apply. The socket then waits for TCP retransmission to give up
  (`tcp_retries2`), which takes about 15 min on Linux defaults.
- `database/sql` calls pgx's `ResetSession`, which pings a connection that has
  been idle for more than 1 s. That ping uses the **request ctx**, so with no
  deadline it blocks just as long.

**Failure scenario.** An idle period longer than the NAT or LB idle timeout
causes the next request to hang for minutes rather than failing fast and being
retried on a fresh connection. With `MaxOpenConns` = 25, 25 such requests
exhaust the pool and every later request blocks on `db.conn()`.

**Recommendation.**
- Apply `context.WithTimeout(ctx, QueryTimeout)` in the adapters, or in a
  handler middleware.
- Remove the 2-minute floor, and honour the configured value.
- Set `SetConnMaxIdleTime` below the middlebox idle timeout.
- Configure `pgconn.Config.DialFunc` with a `net.Dialer` that has `KeepAlive`
  set and a `Control` func setting `TCP_USER_TIMEOUT` (for example 30 s).
- Apply `statement_timeout` through `RuntimeParams` so it also works with a
  supplied DSN.

---

### 5. High — Listener `Close` does unbounded network I/O under three locks

`providers/postgres_listener.go:216-244`, reached from
`providers/postgres.go:116-126`, which is reached from `connection.go:147`:

```go
// sqlConnection.Close holds c.mu (write)
//   PostgresProvider.Close holds p.mu
//     PostgresListener.Close holds l.mu:
for channel := range l.channels {
	_, _ = l.conn.Exec(context.Background(), fmt.Sprintf("UNLISTEN %s", ...))
}
err := l.conn.Close(context.Background())
```

If the listener's socket is dead, and it usually is in the situation that
triggers a reconnect, each `UNLISTEN` waits for a reply that never comes. This
is the same unbounded wait as in finding 4, and `c.mu` is held **for writing**
the whole time. Every request blocks. `bunDB` has already been closed at this
point, so there is no fallback either.

Also, if `listener.Close` returns an error, `PostgresProvider.Close` returns
early. `sqlConnection.Close` then returns with `connected=true` over a closed
pool (finding 9).

**Failure scenario.** An app with any `LISTEN` subscription hits a network
partition. The health checker or an adapter calls `Reconnect`, and the process
stops serving database requests for as long as the kernel takes to kill the
socket.

**Recommendation.** Skip `UNLISTEN` entirely, because closing the connection
drops all subscriptions server-side. Close with `context.WithTimeout(…, 2*time.Second)`.
Don't do network I/O while holding `l.mu`, and don't close the listener
inside `sqlConnection.Close`'s write lock.

---

### 6. High — Listener leaks a goroutine pair per reconnect, and they race on one `pgx.Conn`

`providers/postgres_listener.go:48-120` (Connect), `257-324` (handleNotifications),
`326-370` (handleReconnection).

`Connect()` ends by starting `go l.handleNotifications()` and
`go l.handleReconnection()`. `handleReconnection` responds to a reconnect
signal by calling `l.Connect(ctx)`, which starts **another** pair. The old pair
keeps running on the same `l.ctx`. After N reconnects there are N+1
notification loops. Each one snapshots `l.conn` and calls
`conn.WaitForNotification`. `pgx.Conn` is **not** safe for concurrent use, so
the second caller gets a "conn busy" error. That error isn't a timeout, so it
sends another reconnect signal, which adds another pair.

`handleReconnection` also waits with `time.Sleep(5 * time.Second)` instead of
selecting on `l.ctx.Done()`, so `Close` can't interrupt it. And `Listen` runs
`l.conn.Exec(LISTEN …)` while holding `l.mu`, which blocks `handleReconnection`
for as long as that Exec takes.

Once the parent `PostgresProvider` is closed (for example by any `Reconnect`,
finding 1), subscribers holding the old `*PostgresListener` get
"listener is closed" forever. Nothing re-subscribes them on the new provider.

**Failure scenario.** A flaky network causes a few listener reconnects. The
goroutine count grows without bound, notifications are delivered twice or
dropped, and CPU rises because of the busy/reconnect spiral.

**Recommendation.** Start the goroutines once, in the constructor or the first
`Connect`. Have `handleReconnection` dial a new conn without calling the public
`Connect`. Guard `WaitForNotification` so only one loop owns the conn. Replace
`time.Sleep` with `select { case <-time.After(d): case <-l.ctx.Done(): }`.

---

### 7. High — Second `Close` panics; health checker silently dead after first cycle

`manager.go:119, 313-345`:

```go
stopChan: make(chan struct{}),   // created once, in the constructor
...
func (m *connectionManager) stopHealthChecker() {
	if m.healthTicker != nil {
		m.healthTicker.Stop()
		close(m.stopChan)          // never recreated
		m.wg.Wait()
		m.healthTicker = nil
	}
}
```

After `Connect → Close`, `stopChan` is closed. A second `Connect` calls
`startHealthChecker`, which creates a new ticker and goroutine. That goroutine's
`select` sees the closed `stopChan` right away and **exits**, so health
checking is silently off. A second `Close` finds `healthTicker != nil` and
calls `close(m.stopChan)` again, which **panics**: `close of closed channel`.
`startHealthChecker` and `stopHealthChecker` also read and write `healthTicker`
without `m.mu` held (`Close` calls `stopHealthChecker` before locking), so a
concurrent `Connect`/`Close` pair is a data race.

Calling `Connect` twice without `Close` also leaks: `m.connections[name] = conn`
overwrites the previous connection without closing it.

**Failure scenario.** Anything that cycles the manager can crash the process
during shutdown: graceful restart, config hot-reload, or test suites using
`ResetInstance`.

**Recommendation.** Create `stopChan` in `startHealthChecker`. Guard both
functions with `m.mu`, or a dedicated mutex. Make `Connect` idempotent, or have
it close existing connections first.

---

### 8. Medium — SQLite: in-memory data loss and per-connection pragmas

`providers/sqlite.go:54-90`, `config.go:140-141, 202-204`:

- `ManagerConfig.ApplyDefaults` always gives `MaxOpenConns` a value (25), so the
  "SQLite works best with MaxOpenConns=1" branch at `sqlite.go:60` never runs.
  The probe reported `MaxOpenConnections=25`.
- With `:memory:` (the documented test setup), each pooled connection opens its
  **own** private database. The probe created a table on one connection, and a
  second connection reported `no such table: t`. `ConnMaxIdleTime` (default
  5 min) then closes idle connections and their data with them.
- `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout` are `Exec`'d once on
  whichever pooled connection runs them. `busy_timeout` is per-connection, so
  the other 24 get `database is locked` immediately under write contention.
- `BuildDSN` adds `?_timeout=<ms>` (`config.go:347-351`), but
  `glebarez/go-sqlite` only recognises `_pragma`, `_txlock` and `_time_format`,
  so this parameter is silently ignored.
- `SQLiteProvider.reconnectDB` (`sqlite.go:165`) needs a `dbFactory` that
  nothing ever sets, so it is dead code.

**Recommendation.** For SQLite, force `MaxOpenConns=1` for `:memory:` (or use
`file::memory:?cache=shared`), and never set an idle timeout there. Pass the
pragmas in the DSN (`_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)`) so
every connection gets them.

---

### 9. Medium — Partial-failure states in `Close` and `Connect`

- `connection.go:137-149`: if `bunDB.Close()` or `provider.Close()` fails, for
  example because the listener's Close failed (finding 5), `Close` returns
  early with `connected = true` and the pool already closed. Every accessor then
  returns a handle to a closed pool until someone calls `Close` again.
- `manager.go:197-231`: if connection *k* of *n* fails to connect, `Connect`
  returns an error. Connections 1…k-1 stay open but are never stored in
  `m.connections`, so `Close` can't reach them and they leak.

**Recommendation.** In `Close`, mark the connection disconnected and nil the
fields regardless of errors, and return a joined error. In `Connect`, close any
connections opened so far when a later one fails.

---

### 10. Medium — DSN builders don't escape credentials; TLS off by default

`config.go` `buildPostgresDSN` / `buildMSSQLDSN` / `buildMongoDSN` use
`fmt.Sprintf` with raw `User`/`Password`/`Database` values:

- Postgres key=value format: a password containing a space or `'` breaks
  parsing. A password like `x sslmode=disable` *overrides earlier parameters*.
- MSSQL and Mongo URLs: `@`, `:`, `/`, `?` or `&` in the password corrupt the
  URL. They need `url.QueryEscape` / `url.UserPassword`.
- `sslmode` defaults to `disable` (`config.go:322-325`); see
  `_CROSS-CUTTING.audit.md` X6.

These values come from config, not from clients, so this isn't directly
exploitable by the threat model. It is a correctness and hardening problem,
and it becomes a security problem wherever DSN parts come from a tenant or
operator UI.

**Recommendation.** Build the Postgres DSN as a URL with `url.URL{User: url.UserPassword(...)}`,
or quote key=value values properly. Default `sslmode` to `prefer` or `require`.

---

### 11. Medium — Config knobs that are ignored or cannot be disabled

- `config.go:161-168`: `HealthCheckInterval == 0` and
  `EnableAutoReconnect == false` are both treated as "unset" and replaced with
  the defaults (15 s, `true`). **Auto-reconnect, the trigger for findings 1–2,
  cannot be switched off from config.**
- `RetryAttempts`, `RetryDelay` and `RetryMaxDelay` are defaulted and copied,
  but no provider reads them. Every provider hardcodes `retryAttempts := 3`
  and `retryDelay := 1 * time.Second`.
- `statement_timeout` is only added when the DSN is built (finding 4), and
  SQLite `_timeout` is ignored by the driver (finding 8).

**Recommendation.** Use `*bool` / `*time.Duration`, or an explicit
`Disable…` flag, for the values that can legitimately be zero or false. Wire
the retry settings into the providers, or delete them.

---

### 12. Medium — `Manager.Connect` holds the manager lock across network dials

`manager.go:197-231` holds `m.mu` (write) while dialing every configured
connection, each with up to 3 attempts, backoff, and `ConnectTimeout`.
`GetConnection`, `HealthCheck`, `Stats` and the health checker all wait
behind it. That's harmless at startup, but it serialises the whole manager if
`Connect` is ever called at runtime (hot-reload, lazy init).

**Recommendation.** Dial outside the lock, then lock only to publish the
results into `m.connections`.

---

### 13. Low — dbmanager metrics are never published

`metrics.go` defines Prometheus collectors plus `PublishMetrics` and
`RecordReconnectAttempt`. A grep over the repository finds **no callers** of
either. The connection-pool gauges (open, in-use, idle, wait count) are exactly
what would have shown the idle-connection problem, and they are always zero.
The `*_total` names are registered as gauges, not counters.

**Recommendation.** Call `PublishMetrics` from the health-check tick, call
`RecordReconnectAttempt` from `Reconnect`, and make the totals counters.

---

### 14. Low — Assorted correctness issues

- `Native()` checks `c.connected` (`connection.go:214`); `Bun()` and `GORM()`
  don't. After a partial `Close` they can build ORM wrappers over a nil or
  closed DB.
- `getNativeAdapter` (`connection.go:500-525`) wraps SQLite and MSSQL in
  `PgSQLAdapter`, which quotes and builds SQL in Postgres dialect.
- `ExistingDBProvider` (`NewConnectionFromDB`) applies no pool settings and no
  idle or lifetime limits, and its `Close` closes the caller's `*sql.DB`.
- `MongoProvider` uses `MaxIdleConns` as `MinPoolSize`, and `Stats()` returns an
  empty struct.

---

### 15. Low — Logging defects

- `manager.go:247, 367-369, 378-380` call `logger.Error("…", "name", name, "error", err)`.
  `pkg/logger` is printf-style, so these print `%!(EXTRA string=name, …)`, and
  the error text is buried in exactly the log lines needed during an outage.
- `ResetInstance` discards the error from `Close`.
- Connection errors wrap driver errors that can include the DSN host and user.
  Together with `_CROSS-CUTTING.audit.md` X8, they reach Sentry unscrubbed.

---

## Test coverage

`manager_test.go` and `factory_test.go` cover construction and config defaults.
Nothing tests `Reconnect` while handles are held, concurrent `Reconnect`, a
`Connect`/`Close` cycle run twice, health-check lock hold time, or listener
reconnection. Each of findings 1, 2, 3, 6 and 7 can be reproduced with a short
SQLite-backed test (the probes used for this audit took about 20 lines each).
Add them as regression tests when the fixes land, and run them with `-race`
(`_CROSS-CUTTING.audit.md` X1).
