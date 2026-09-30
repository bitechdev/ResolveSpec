# Audit: `pkg/cache`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/cache` |
| **Files** | `cache.go` (76), `cache_manager.go` (167), `provider.go` (65), `provider_memory.go` (342), `provider_memcache.go` (284), `provider_redis.go` (269), `example_usage.go` (266), `cache_test.go` (69) |
| **Audit date** | 2026-09-29 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names all attacker-controlled |
| **Depth** | deep (hot package) |

## Summary

`pkg/cache` is a thin `Provider` abstraction over three backends. Two things
make it much more security-relevant than a cache normally is:

1. **It stores authentication state.** `pkg/security/providers.go:398-402` caches
   `UserContext` under the key `fmt.Sprintf("auth:session:%s", token)` — the raw
   bearer token from the `Authorization` header. So **cache keys are directly
   attacker-controlled**, the cached value is an authorization decision, and
   `DeleteByPattern` is the only session-revocation mechanism.
2. **It is never explicitly initialized.** Nothing in `pkg/` calls
   `Initialize`/`UseMemory`/`UseRedis`/`UseMemcache`; every consumer reaches the
   cache through `GetDefaultCache()` (`cache.go:48`), which lazily constructs a
   `MemoryProvider` **on the request path** without synchronization.

The most serious findings are: a cache-write failure being converted into an
authentication failure (`GetOrSet`, `cache_manager.go:126`), session revocation
that silently cannot work on memcache, an unsynchronized lazy singleton read by
concurrent HTTP handlers, an unbounded `tagToKeys` index that `MaxSize` does not
cover, and `MemoryProvider.Get` taking the **write** lock on every cache hit.

There is **no `recover()` anywhere in this package** and no logging at all —
grep for `recover()` and `logger.` across all 1 538 lines returns zero hits.
Every error is either returned to the caller or discarded with `_ =`.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **Critical** | security | Cache-write failure is returned as an error from `GetOrSet`, so a cache outage becomes a total authentication outage |
| 2 | **Critical** | security | Session revocation (`DeleteByPattern`) is unimplemented on memcache and returns an error — revoked sessions keep authenticating |
| 3 | **High** | locking | `defaultCache` is an unsynchronized lazy singleton read/written from concurrent request handlers |
| 4 | **High** | security / slowness | `tagToKeys` is unbounded and leaked by six paths; `MaxSize` bounds only `items` |
| 5 | **High** | locking / slowness | `MemoryProvider.Get` acquires the **write** lock on every hit |
| 6 | **High** | security | `DeleteByPattern` pattern syntax differs per provider (Go regexp vs Redis glob vs error); memory matches unanchored |
| 7 | **High** | security | Raw session token embedded in the `"key not found: %s"` error string |
| 8 | **High** | security | No single-flight: concurrent misses on one key run N loaders (auth stampede against the session stored procedure) |
| 9 | **Medium** | security | Attacker-controlled keys break memcache's 250-byte/no-whitespace key rule; error is swallowed as a miss, then fails the write |
| 10 | **Medium** | correctness | TOCTOU in `MemoryProvider.Get`: expired-item path deletes unconditionally after dropping the read lock |
| 11 | **Medium** | correctness | `MemoryProvider.Close()` sets `items = nil`; a subsequent `Set` panics and nothing recovers |
| 12 | **Medium** | security | Memcache tag index is non-atomic read-modify-write and shares the key namespace; lost updates silently drop keys from invalidation |
| 13 | **Medium** | security | `Clear()` maps to `FlushAll()` / `FlushDB()` — wipes the whole shared server/DB |
| 14 | **Medium** | correctness | `MemcacheProvider.Close()` is a no-op justified by a false comment; gomemcache **does** have `Close()` |
| 15 | **Medium** | security | Redis and memcache have no TLS option at all; `AUTH` password and cached `UserContext` cross the wire in cleartext |
| 16 | **Medium** | correctness | `Cache.Remember` returns a different Go type on hit vs miss |
| 17 | **Medium** | slowness | `Get`/`Exists` swallow **all** backend errors as a cache miss — a degraded backend is invisible and stampedes the DB |
| 18 | **Low** | slowness | `evictOne` is an O(n) scan under the write lock, run per insertion at capacity |
| 19 | **Low** | correctness | `CleanExpired` is dead code; there is no janitor, so expired items are only reclaimed on access |
| 20 | **Low** | security | `RedisProvider.Stats` returns the raw `INFO` output in `ProviderStats["info"]` |
| 21 | **Low** | correctness | `ctx` is accepted and completely ignored by the memcache provider |
| 22 | **Low** | correctness | `MaxSize <= 0` disables eviction entirely — an unbounded in-memory cache |
| 23 | **Low** | correctness | Provider constructors mutate the caller's config struct |
| 24 | **Low** | correctness | No defensive copy of `[]byte` on `Set`/`Get` in the memory provider |
| 25 | **Low** | hygiene | `example_usage.go` ships `log.Fatal` calls in a library package |

## Resolution status (2026-09-30)

- **#1** — Fixed: cache-write failure is logged and ignored in `GetOrSet`/`Remember`
- **#2** — Not fixed here: needs `pkg/security` changes (tag-based revocation); memcache `DeleteByPattern` still errors (now documented)
- **#3** — Fixed: `atomic.Pointer` + CAS lazy init; displaced provider closed on `Initialize`/`Use*` (not on `SetDefaultCache`, where the caller owns it)
- **#4** — Fixed: single `removeLocked` used by every removal path; `Clear` resets the index
- **#5** — Fixed: `Get` runs under `RLock`; access counters are atomics
- **#6** — Deferred (pattern language is an API decision). Memory now compiles the regexp before locking; Redis `SCAN COUNT` is 500
- **#7** — Fixed: `ErrNotFound` sentinel, no key in the error. `pkg/security` still keys on the raw token (out of scope)
- **#8** — Deferred (needs singleflight dependency)
- **#9** — Fixed in the memcache provider: keys are namespaced (`k:`) and hashed when over 200 bytes or illegal
- **#10** — Fixed: re-check under the write lock before deleting
- **#11** — Fixed: `Close` sets a closed flag; writes return `ErrClosed`
- **#12** — Fixed: CAS retry loop, bounded tag list (5000), errors returned, 30-day expiry rule, user keys namespaced. Failed tag indexing rolls back the value
- **#13** — Fixed: `Clear` on Redis/Memcache returns `ErrFlushNotAllowed` unless `AllowFlush` is set (behaviour change)
- **#14** — Fixed: `Close` calls `client.Close()`
- **#15** — Deferred (new TLS config fields)
- **#16** — Documented only: warning on `Remember`; signature unchanged
- **#17** — Partly fixed: Redis/Memcache `Get` now log backend errors; the `Provider` interface is unchanged so it is still reported as a miss
- **#18** — Not fixed: still an O(n) scan (comment fixed in `Stats`)
- **#19** — Fixed: janitor goroutine, `Options.CleanupInterval` (default 1m), stopped by `Close`
- **#20** — Fixed: only allowlisted counters are exposed; `Hits`/`Misses` populated
- **#21** — Partly fixed: memcache methods check `ctx.Err()` first; in-flight calls are still bounded only by `Timeout`
- **#22** — Fixed: `MaxSize` 0 means 10000; negative means unbounded
- **#23** — Fixed: constructors copy config and `Options`
- **#24** — Fixed: memory provider copies on `Set` and `Get`
- **#25** — Fixed: `example_usage.go` excluded from the build with `//go:build ignore`
- Tests: `pkg/cache/hardening_test.go` (run with `-race`).

---

### 1. Critical — a cache-write failure is an authentication failure

`pkg/cache/cache_manager.go:112-141`:

```go
func (c *Cache) GetOrSet(ctx context.Context, key string, dest interface{}, ttl time.Duration, loader func() (interface{}, error)) error {
	err := c.Get(ctx, key, dest)
	if err == nil {
		return nil
	}

	value, err := loader()
	if err != nil {
		return fmt.Errorf("loader failed: %w", err)
	}

	// Store in cache
	if err := c.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("failed to cache value: %w", err)   // <-- line 127
	}
	...
}
```

The loader has already succeeded — the authoritative value is in hand — but a
failure to *cache* it aborts the whole call. The only consumer of `GetOrSet` is
authentication, `pkg/security/providers.go:398-444`:

```go
cacheKey := fmt.Sprintf("auth:session:%s", token)

var userCtx UserContext
err := a.cache.GetOrSet(r.Context(), cacheKey, &userCtx, a.cacheTTL, func() (any, error) {
	// ... queries the session stored procedure, returns &user on success
})

if err != nil {
	lastErr = err
	continue // Try next token
}
```

Any error — including "failed to cache value" — is treated as *this token is not
valid*, and after the token loop the request is rejected.

**Failure scenario.** Redis is configured and becomes unreachable (restart,
failover, network partition, `maxmemory` reached with `noeviction`).
`RedisProvider.Get` swallows the error and reports a miss
(`provider_redis.go:91-93`), the loader runs and the database confirms the
session is valid, then `RedisProvider.Set` returns the connection error and
`GetOrSet` returns it. **Every request from every user is now rejected with an
authentication error**, even though both the database and the sessions are
healthy. A cache is supposed to be a latency optimization; here it is a hard
dependency of the auth path, and its failure mode is total outage. The same
applies to `maxmemory` pressure, which an attacker can induce (see finding 4).

**Recommendation.** A cache-write failure must be non-fatal. Log it and return
the loaded value:

```go
if err := c.Set(ctx, key, value, ttl); err != nil {
	logger.Warn("cache: failed to store key (continuing uncached): %v", ctx, err)
}
```

Separately, `pkg/security` should not conflate "cache layer failed" with
"credential rejected"; the loader's own error is the only one that should fail
authentication. Consider having `GetOrSet` return the loaded value plus a
non-fatal cache error, or wrap cache errors in a sentinel the caller can test
with `errors.Is`.

---

### 2. Critical — session revocation silently cannot work on memcache

`pkg/security/providers.go:463-479` is the only session-revocation path:

```go
func (a *DatabaseAuthenticator) ClearCache(token string) error {
	ctx := context.Background()
	if token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", token)
		return a.cache.Delete(ctx, cacheKey)
	}
	// Clear all auth cache entries
	return a.cache.DeleteByPattern(ctx, "auth:session:*")
}

func (a *DatabaseAuthenticator) ClearUserCache(userID int) error {
	ctx := context.Background()
	pattern := "auth:session:*"
	return a.cache.DeleteByPattern(ctx, pattern)
}
```

`pkg/cache/provider_memcache.go:249-254`:

```go
// DeleteByPattern removes all keys matching the pattern.
// Note: Memcache does not support pattern-based deletion natively.
// This is a no-op for memcache and returns an error.
func (m *MemcacheProvider) DeleteByPattern(ctx context.Context, pattern string) error {
	return fmt.Errorf("pattern-based deletion is not supported by Memcache")
}
```

**Failure scenario.** A deployment uses memcache (`cache.provider: memcache`).
An account is compromised; an operator disables the user or the sessions are
revoked in the database, and the application calls `ClearUserCache(userID)`.
That returns an error and **removes nothing**. The attacker's cached
`UserContext` continues to authenticate every request for the full
`a.cacheTTL` — the database is never consulted again during that window
(`GetOrSet` short-circuits on a cache hit). Whether the operator even learns
this failed depends entirely on whether the caller checks the returned error;
`ClearUserCache` is also broken for a second reason — it ignores `userID` and
would have revoked every session in the process.

Note also that `ClearUserCache`'s pattern is *not* user-scoped, so even on Redis
and memory it is a global logout, not a per-user one. That direction is at least
fail-safe.

**Recommendation.** Revocation must not depend on a capability the provider may
not have. Options, in order of preference:

- Tag every session entry (`SetWithTags` with tags `auth:session`,
  `auth:user:<id>`) and revoke with `DeleteByTag`, which all three providers
  implement. This also makes `ClearUserCache` actually per-user.
- Keep a short `cacheTTL` (seconds, not minutes) so the revocation window is
  bounded regardless.
- Make `DeleteByPattern`'s unsupported case loud: have `pkg/security` refuse to
  start, or fall back to `Clear`, when the configured provider cannot revoke.
- At minimum, log at error level when a revocation call fails.

---

### 3. High — `defaultCache` is an unsynchronized lazy singleton on the request path

`pkg/cache/cache.go:9-62`:

```go
var (
	defaultCache *Cache
)

func Initialize(provider Provider) {
	defaultCache = NewCache(provider)
}

func UseMemory(opts *Options) error {
	provider := NewMemoryProvider(opts)
	defaultCache = NewCache(provider)
	return nil
}
// ... UseRedis (:32), UseMemcache (:42) likewise

func GetDefaultCache() *Cache {
	if defaultCache == nil {
		_ = UseMemory(&Options{
			DefaultTTL: 5 * time.Minute,
			MaxSize:    10000,
		})
	}
	return defaultCache
}

func SetDefaultCache(cache *Cache) {
	defaultCache = cache
}
```

Six functions write `defaultCache` and `GetDefaultCache` both reads and writes
it, with no mutex, no `sync.Once` and no `atomic.Pointer`. `GetDefaultCache` is
called **from HTTP request handlers**: `pkg/restheadspec/handler.go:833`
(`cache.GetDefaultCache().Get(ctx, cacheKey, cachedTotalData)`),
`pkg/restheadspec/cache_helpers.go:109` and `:118`, and the equivalent
`pkg/resolvespec` paths.

Nothing in `pkg/` ever calls `Initialize` or `Use*`, so in a default deployment
**the first traffic to arrive is what initializes the cache**, concurrently.

**Failure scenario.** Two requests arrive simultaneously on a cold process.
Both observe `defaultCache == nil`, both run `UseMemory`, each constructing its
own `MemoryProvider`. One assignment wins. Request A writes its query total into
the provider that loses and is immediately garbage — so the entry is
unreachable, the cache reports a permanent miss for it, and the count is
recomputed from the database on every subsequent request. Worse, if this races
with an application's explicit `UseRedis` during startup, the Redis provider can
be clobbered by the lazy memory provider (or vice versa) and **the process
silently runs on the wrong backend**, which for `pkg/security` means session
cache entries that no other process shares and that `ClearCache` on another
instance can never reach.

This is also a genuine data race on the pointer word: unsynchronized
read/write of `defaultCache`, which `go test -race` would report immediately.
See `_CROSS-CUTTING.audit.md` — `-race` is never run in this repo, and
`pkg/cache` is not in the tested package set.

Note the same pattern exists in `pkg/security/providers.go:145`
(`cacheInstance = cache.GetDefaultCache()`), which at least happens at
construction time.

**Recommendation.** Guard the global with `sync.RWMutex` or store it in an
`atomic.Pointer[Cache]`, and make the lazy default a `sync.Once`:

```go
var (
	defaultCache atomic.Pointer[Cache]
	defaultOnce  sync.Once
)

func GetDefaultCache() *Cache {
	if c := defaultCache.Load(); c != nil {
		return c
	}
	defaultOnce.Do(func() {
		defaultCache.CompareAndSwap(nil, NewCache(NewMemoryProvider(&Options{
			DefaultTTL: 5 * time.Minute, MaxSize: 10000,
		})))
	})
	return defaultCache.Load()
}
```

Also: every replacement path drops the previous provider **without closing it**,
so `UseRedis` after a lazy `UseMemory` (or two `UseRedis` calls) leaks the old
provider's connection pool and, for Redis, its background goroutines. Close the
old provider on swap.

---

### 4. High — `tagToKeys` is unbounded; `MaxSize` bounds only `items`

`MemoryProvider` holds two maps (`provider_memory.go:30-37`):

```go
type MemoryProvider struct {
	mu        sync.RWMutex
	items     map[string]*memoryItem
	tagToKeys map[string]map[string]struct{} // tag -> set of keys
	options   *Options
	...
}
```

`MaxSize` is checked only against `len(m.items)` (`:105`, `:135`). Six paths
remove entries from `items` **without** removing them from `tagToKeys`:

| Path | Line | Cleans `tagToKeys`? |
|---|---|---|
| `Get` — expired-item delete | `:70` | no |
| `Set` — overwrites a tagged key | `:111` | no (and drops `Tags`, so the entry becomes unreachable for cleanup) |
| `evictOne` — expired scan | `:313` | no |
| `evictOne` — LRU victim | `:324` | no |
| `DeleteByPattern` | `:242` | no |
| `Clear` | `:254` | no — `m.tagToKeys` is never reset |
| `CleanExpired` | `:336` | no |

Only `Delete` (`:178-187`) and `SetWithTags` (`:141-151`) maintain it, and
`DeleteByTag` (`:226`) drops one whole tag.

Tags come from `pkg/restheadspec/cache_helpers.go:99-105`:

```go
func buildCacheTags(schema, tableName string) []string {
	return []string{
		fmt.Sprintf("schema:%s", strings.ToLower(schema)),
		fmt.Sprintf("table:%s", strings.ToLower(tableName)),
	}
}
```

and keys from `buildExtendedQueryCacheKey` (`:43-85`) — a SHA-256 of the full
query shape, including filters, sort, `customWhere`, `customOr`, `customJoin`,
expand and cursors.

**Failure scenario.** An attacker issues `GET /api/public/orders?...` in a loop,
varying one filter value each time. Every request produces a distinct SHA-256
key, `setQueryTotalCache` stores it under the tags `schema:public` and
`table:orders`, and `tagToKeys["table:orders"][key]` gains a member. Once
`items` reaches `MaxSize` (10 000 by default), `evictOne` starts discarding
items — but **never** the corresponding `tagToKeys` members. `items` stays
capped at 10 000; `tagToKeys["table:orders"]` grows by one 64-character key per
request, forever. At roughly 100 bytes per map entry, a few million requests —
easily reachable at modest rate — costs hundreds of megabytes of heap that
nothing will ever reclaim, because `Clear()` does not reset the map and
`CleanExpired` does not touch it. This is a **memory-exhaustion DoS driven
purely by query-string variation**, and it is cheap for the attacker: the
expensive part (the actual count query) can be avoided by hitting a table whose
count is trivial.

The leak also breaks invalidation correctness: `DeleteByTag` iterates a key set
full of keys that no longer exist, and a plain `Set` over a previously tagged
key leaves that key in the tag index while clearing its `Tags` — so the item can
be deleted by a tag it no longer claims.

**Recommendation.** Factor tag maintenance into a single private helper and call
it from every removal path:

```go
// caller must hold m.mu for writing
func (m *MemoryProvider) removeLocked(key string) {
	if item, ok := m.items[key]; ok {
		for _, tag := range item.Tags {
			if ks := m.tagToKeys[tag]; ks != nil {
				delete(ks, key)
				if len(ks) == 0 {
					delete(m.tagToKeys, tag)
				}
			}
		}
	}
	delete(m.items, key)
}
```

Use it in `Get`'s expired path, `Set` (before overwrite), `evictOne`,
`DeleteByPattern` and `CleanExpired`; reset `m.tagToKeys` in `Clear`; and
account `len(m.tagToKeys)` (or total members) against an explicit bound.
Independently, cap the number of distinct tags and the members per tag.

---

### 5. High — `MemoryProvider.Get` takes the write lock on every hit

`provider_memory.go:56-88`:

```go
func (m *MemoryProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	// First try with read lock for fast path
	m.mu.RLock()
	item, exists := m.items[key]
	...
	value := item.Value
	m.mu.RUnlock()

	// Update access tracking with write lock
	m.mu.Lock()
	item.LastAccess = time.Now()
	item.HitCount++
	m.mu.Unlock()

	m.hits.Add(1)
	return value, true
}
```

The comment promises a read-lock fast path, but every **successful** lookup ends
in an exclusive lock to bump two bookkeeping fields. The `RWMutex` therefore
provides no read concurrency at all on the hot path, and Go's `RWMutex` blocks
*new* readers once a writer is waiting — so a burst of concurrent hits
degenerates into a fully serialized queue with two lock handoffs per operation.

**Failure scenario.** The session cache is the default `MemoryProvider`. Under
concurrent load, every authenticated request performs
`GetOrSet` → `Get` → cache hit → exclusive lock. With hundreds of in-flight
requests the mutex becomes the throughput ceiling for the entire API, and
because the write lock is taken *after* the read lock is released, each hit pays
two full lock acquisitions. Adding `HitCount` to a per-item `atomic.Int64`
would make this free; as written, the cache that exists to reduce latency is the
serialization point.

A secondary defect: between `RUnlock` at `:78` and `Lock` at `:81` the item may
have been deleted or replaced, so the code can mutate an orphaned struct. Harmless
but confirms the bookkeeping does not need the lock.

**Recommendation.** Make the counters lock-free and drop the write lock:

```go
type memoryItem struct {
	Value      []byte
	Expiration time.Time
	lastAccess atomic.Int64 // unix nanos
	hitCount   atomic.Int64
	Tags       []string
}
```

Then the whole `Get` runs under `RLock`. If exact LRU ordering matters, consider
an approximate clock (update `lastAccess` only if it is more than a second
stale) or a sharded map to cut contention.

---

### 6. High — `DeleteByPattern` has three incompatible pattern languages

The interface (`provider.go:29-31`) says only "Pattern syntax depends on the
provider implementation", and the three implementations diverge completely:

| Provider | Line | Semantics |
|---|---|---|
| memory | `provider_memory.go:235-244` | `regexp.Compile` + **unanchored** `MatchString` |
| redis | `provider_redis.go:197` | `SCAN MATCH` — Redis glob |
| memcache | `provider_memcache.go:252-254` | always an error |

```go
// memory
re, err := regexp.Compile(pattern)
if err != nil {
	return fmt.Errorf("invalid pattern: %w", err)
}
for key := range m.items {
	if re.MatchString(key) {
		delete(m.items, key)
	}
}
```

The one caller, `pkg/security/providers.go:470`, passes `"auth:session:*"` —
a Redis glob. Interpreted as a Go regexp that is `auth:session` followed by zero
or more `:`, matched unanchored, so it happens to match the intended keys (and
any key merely *containing* `auth:session`). It works by coincidence, not design.

**Failure scenario.** Two ways this bites:

- **Over-deletion.** Because matching is unanchored, a glob like `user:*` becomes
  the regexp `user:*` = `user` + zero-or-more colons, which matches *any* key
  containing `user` — including `auth:session:<token>` if a token happens to
  contain that substring. Conversely a caller who writes a glob such as
  `*` gets `regexp.Compile("*")` → `error parsing regexp: missing argument to
  repetition operator`, i.e. a silent no-op invalidation where the author
  expected a full flush. Stale authorization data continues to be served.
- **Attacker-influenced regexp.** `regexp.Compile` runs on the caller's string
  **while holding the write lock** (`:232-238`), so if any future caller derives
  a pattern from request input, an attacker both controls the compiled program
  and blocks every other cache operation for its duration. Go's RE2 has no
  catastrophic backtracking, but compilation of a large pattern is not free and
  the lock is held across it.

Redis's side has its own cost: `r.client.Scan(ctx, 0, pattern, 0)` with
`count = 0` leaves the server at its default `COUNT 10`, so revoking sessions
walks the entire keyspace in ~10-key increments — thousands of round trips on a
large DB, executed synchronously inside `ClearCache`.

**Recommendation.** Define one pattern language in the interface — a glob is the
right choice, since it is the one Redis supports natively — and implement it for
memory with `path.Match` (or an explicit anchored translation to regexp),
compiled **before** taking the lock. Reject patterns the provider cannot honour
with a typed `ErrUnsupported` so callers can branch. Pass a sensible `COUNT`
(e.g. 500) to `SCAN`. Better still, replace pattern deletion with tag deletion
at the one call site (see finding 2).

---

### 7. High — the session token is embedded in an error string

`cache_manager.go:22-43`:

```go
func (c *Cache) Get(ctx context.Context, key string, dest interface{}) error {
	data, exists := c.provider.Get(ctx, key)
	if !exists {
		return fmt.Errorf("key not found: %s", key)
	}
	...
}

func (c *Cache) GetBytes(ctx context.Context, key string) ([]byte, error) {
	data, exists := c.provider.Get(ctx, key)
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return data, nil
}
```

The key for the session cache is `"auth:session:" + token` — the raw bearer
credential. Every cache miss therefore allocates an error whose text contains a
live secret. `GetOrSet` discards it, but this is a public API on a `*Cache` that
`pkg/security` holds directly, and `pkg/security/keystore_database.go:232` calls
`ks.cache.Get` on an API-key cache too.

**Failure scenario.** Any caller that does `logger.Error("cache lookup failed: %v", err)`
publishes the bearer token to the application log **and**, per
`audit/pkg/logger.audit.md` finding 2, forwards it verbatim to Sentry, where it
is retained by a third party with no scrubbing (`pkg/errortracking` has no
`BeforeSend` hook). A token in a log aggregator is a replayable credential for
the whole `cacheTTL` — longer, if the log outlives the session. Note that this
requires only one careless `%v` at a call site; the package is handing out the
loaded weapon.

**Recommendation.** Never interpolate cache keys into errors. Use a package
sentinel and let the caller decide what is safe to log:

```go
var ErrNotFound = errors.New("cache: key not found")
...
if !exists {
	return ErrNotFound
}
```

Callers then use `errors.Is(err, cache.ErrNotFound)` instead of matching on
strings, which also fixes the miss/error conflation noted in finding 17. If a
key must appear in diagnostics, log a truncated hash of it. Separately,
`pkg/security` should key the cache on a SHA-256 of the token rather than the
token itself, exactly as `keystore_database.go` already does for API keys
(`keystoreCacheKey(hash)`, `:287`).

---

### 8. High — no single-flight: concurrent misses run N loaders

`GetOrSet` (`cache_manager.go:112`) and `Remember` (`:145`) both do
check → load → store with nothing serializing concurrent callers on the same
key.

**Failure scenario (auth).** A client opens 200 connections with the same fresh
session token. All 200 miss the cache, all 200 enter the loader, and all 200
execute the session stored procedure
(`SELECT p_success, p_error, p_user::text FROM <session_fn>($1, $2)`,
`pkg/security/providers.go:413-415`) concurrently. Each success then also spawns
`go a.updateSessionActivity(...)` (`:447`), i.e. 200 more goroutines each issuing
a database write. The cache provides no protection at all for the first
round-trip, and under sustained concurrency where request arrival outpaces query
latency it never catches up: throughput is bounded by the database, not the
cache. A single valid credential is enough to drive this — no privilege needed.

**Failure scenario (query totals).** The same shape applies to
`restheadspec/handler.go:833`: a burst of identical expensive `COUNT(*)` queries
all miss together and all hit the database.

This is the classic cache stampede / thundering herd, and it is the reason
single-flight exists.

**Recommendation.** Wrap the loader in `golang.org/x/sync/singleflight`, which is
already an indirect dependency of most Go service stacks:

```go
type Cache struct {
	provider Provider
	sf       singleflight.Group
}

func (c *Cache) GetOrSet(ctx context.Context, key string, dest any, ttl time.Duration, loader func() (any, error)) error {
	if err := c.Get(ctx, key, dest); err == nil {
		return nil
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		// re-check under the flight, then load and store
		...
	})
	...
}
```

Note the group must be keyed per-`Cache`, and `Forget` should be called on
loader error so a failure is not shared beyond the in-flight set. For the auth
path specifically, also bound `updateSessionActivity` — an unbounded `go` per
request is its own DoS vector (raised again in the `pkg/security` audit).

---

### 9. Medium — attacker-controlled keys violate memcache's key rules

gomemcache enforces the protocol limits (verified in
`gomemcache@v0.0.0-20260422231931-4d751bb6e37c/memcache.go:58-91`):

```go
// ErrMalformedKey is returned when an invalid key is used.
// Keys must be at maximum 250 bytes long and not
// contain whitespace or control characters.
ErrMalformedKey = errors.New("malformed: key is too long or contains invalid characters")

func legalKey(key string) bool {
	if len(key) > 250 {
		return false
	}
	...
}
```

The session cache key is `"auth:session:" + token` with `token` taken from the
`Authorization` header, so its length and byte content are chosen by the client.

**Failure scenario.** A deployment uses memcache and issues JWT session tokens,
which routinely exceed 238 bytes. `MemcacheProvider.Get` receives
`ErrMalformedKey` and — per finding 17 — reports it as a plain cache miss
(`provider_memcache.go:80-82`). The loader runs, the database validates the
session, and then `MemcacheProvider.Set` returns `ErrMalformedKey`, which
finding 1 converts into an authentication failure. **Every user with a long
token is permanently unable to authenticate**, and the logs show nothing but a
generic "failed to cache value". A client can also trigger this deliberately
with a token containing a space to probe the backend.

**Recommendation.** Hash keys inside the provider so key length and charset are
bounded regardless of caller input — e.g. `sha256` hex of the key when it
exceeds 200 bytes or contains illegal bytes, with a fixed prefix. And, as in
finding 7, `pkg/security` should hash the token before it ever becomes a key.

---

### 10. Medium — TOCTOU when deleting an expired item

`provider_memory.go:66-74`:

```go
if item.isExpired() {
	m.mu.RUnlock()
	// Upgrade to write lock to delete expired item
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	m.misses.Add(1)
	return nil, false
}
```

Go's `RWMutex` has no lock upgrade, so the read lock is genuinely released and
the write lock separately acquired. The `delete` is then **unconditional** — it
does not re-check that the entry still exists or is still the expired one.

**Failure scenario.** Goroutine A reads an expired entry for key `K` and drops
the read lock. Goroutine B takes the write lock and `Set`s a fresh value for
`K`. Goroutine A now takes the write lock and deletes B's fresh entry. B has no
idea; the value it believes it cached is gone, and the next reader recomputes
it. In the auth path this means a just-validated session is dropped immediately,
forcing another stored-procedure call — and under load this can repeat, since the
interleaving recurs whenever expiry and refresh coincide, which is exactly when
traffic for that key is highest.

**Recommendation.** Re-check under the write lock and delete only the same
entry:

```go
m.mu.Lock()
if cur, ok := m.items[key]; ok && cur == item {
	m.removeLocked(key)   // see finding 4
}
m.mu.Unlock()
```

---

### 11. Medium — `Close()` makes the provider panic on next use

`provider_memory.go:273-280`:

```go
func (m *MemoryProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = nil
	return nil
}
```

Reads of a nil map are fine, but `Set`/`SetWithTags` do
`m.items[key] = &memoryItem{...}` (`:111`, `:154`), which on a nil map panics
with `assignment to entry in nil map`.

**Failure scenario.** A graceful-shutdown handler calls `cache.Close()` while
requests are still draining — the normal ordering, since `pkg/config` defaults
`servers.drain_timeout` to 25s. The next in-flight request reaches
`setQueryTotalCache` and the process panics **while holding `m.mu`**. There is
no `recover()` anywhere in `pkg/cache`, so this unwinds into whatever the caller
has; if the HTTP server's panic handler recovers it, the mutex is never
unlocked and **every subsequent cache operation blocks forever** — the process
is alive, serving, and permanently wedged on the first cache access. That is a
worse outcome than crashing.

**Recommendation.** Mark the provider closed instead of destroying the map, and
return an error from every method afterwards:

```go
type MemoryProvider struct {
	...
	closed bool
}

func (m *MemoryProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.items = make(map[string]*memoryItem)
	m.tagToKeys = make(map[string]map[string]struct{})
	return nil
}
```

with `if m.closed { return ErrClosed }` at the top of each mutating method.
More generally, the package should use `defer logger.CatchPanicCallback(...)` at
its exported boundaries so a panic inside a lock is reported rather than
silently converted into a deadlock — but note `audit/pkg/logger.audit.md`
finding 4 on `CatchPanic` swallowing unconditionally; here the panic should be
logged and **re-raised** or converted to an error, not absorbed.

---

### 12. Medium — the memcache tag index is a lost-update machine

`provider_memcache.go:136-170` maintains, for each tag, a JSON array of keys:

```go
for _, tag := range tags {
	tagKey := fmt.Sprintf("cache:tag:%s", tag)

	// Get existing keys for this tag
	var keys []string
	if item, err := m.client.Get(tagKey); err == nil {
		_ = json.Unmarshal(item.Value, &keys)
	}

	// Add current key if not already present
	found := false
	for _, k := range keys { ... }
	if !found {
		keys = append(keys, key)
	}

	keysData, err := json.Marshal(keys)
	if err != nil {
		continue
	}

	tagItem := &memcache.Item{
		Key:        tagKey,
		Value:      keysData,
		Expiration: expiration + 3600, // Give tag lists longer TTL
	}
	_ = m.client.Set(tagItem)
}
```

Four distinct defects in fifteen lines:

1. **Non-atomic read-modify-write.** `Get` then `Set` with no CAS, across a
   network, for a value that every concurrent writer of the same tag touches.
2. **Unbounded growth.** The list only ever grows within its TTL, and every
   writer re-reads and re-writes the whole array. `Delete` (`:190-200`) rewrites
   it too.
3. **1 MB item limit.** Once the array exceeds memcached's default item size the
   `Set` fails — and the error is discarded by `_ =`.
4. **`expiration + 3600` can cross memcached's 30-day boundary.** The protocol
   treats an expiry value above 2 592 000 as an **absolute Unix timestamp**. A
   caller passing a 30-day TTL yields `2592000 + 3600 = 2595600`, which
   memcached reads as 1970-01-31 — already past, so the tag list is dead on
   arrival. `int32(ttl.Seconds())` at `:108` also truncates for very large TTLs
   and, for a negative TTL, expires the item immediately.

**Failure scenario.** Two requests cache different query totals for the same
table concurrently. Both read `cache:tag:table:orders` and see `[k1]`; one
writes `[k1,k2]`, the other writes `[k1,k3]`. The second write wins and `k2` is
**no longer in the tag index**. A later `POST` to that table calls
`invalidateCacheForTags` → `DeleteByTag("table:orders")`, which deletes `k1` and
`k3` but not `k2`. `k2` continues to serve a **stale row count from before the
write** for its full TTL. Under concurrency this is not an edge case — it is the
normal outcome, and lost updates accumulate. When the list crosses 1 MB the
failure becomes total: no further keys are indexed and nothing is logged.

Additionally, the prefixes `cache:tag:` and `cache:tags:` (`:128`, `:138`,
`:179`, `:185`, `:219`, `:239`) share the key namespace with ordinary cache
keys. No consumer currently writes keys under that prefix — the restheadspec
keys are `query_total:<sha256>` and the security keys `auth:session:*` — but
nothing enforces it, and a consumer that ever allows an attacker-derived key
beginning `cache:tag:` could forge or destroy the invalidation index (mass
invalidation → database load, or suppressed invalidation → stale authorization
data served).

**Recommendation.** Use `CompareAndSwap` (gomemcache exposes it) in a bounded
retry loop, cap the list length, honour the 30-day rule, and stop discarding
errors:

```go
func memcacheExpiry(ttl time.Duration) int32 {
	secs := int64(ttl.Seconds())
	if secs < 0 { secs = 0 }
	if secs > 2592000 {           // >30d must be an absolute timestamp
		return int32(time.Now().Add(ttl).Unix())
	}
	return int32(secs)
}
```

Given how weak tag support is on memcache, the honest alternative is to return
`ErrUnsupported` from `SetWithTags`/`DeleteByTag` and force callers to pick a
provider that can do it, rather than offering invalidation that silently misses
keys. Namespace the index keys under a prefix that ordinary keys cannot reach
(e.g. by prefixing all user keys with `k:`).

---

### 13. Medium — `Clear()` flushes the entire shared server

```go
// provider_memcache.go:257-259
func (m *MemcacheProvider) Clear(ctx context.Context) error {
	return m.client.FlushAll()
}

// provider_redis.go:228-230
func (r *RedisProvider) Clear(ctx context.Context) error {
	return r.client.FlushDB(ctx).Err()
}
```

Neither is scoped to this application's keys. `FlushAll` wipes every key on
every configured memcached server; `FlushDB` wipes the whole logical Redis DB —
and `RedisConfig.DB` defaults to `0`, which is also where `pkg/config` puts the
event broker (`event_broker.redis.db: 0`, `pkg/config/manager.go:265`) and its
`resolvespec:events` stream.

**Failure scenario.** An operator or an admin endpoint calls `cache.Clear()` to
drop stale entries. On Redis with the default config this also deletes the event
broker's stream and consumer-group state, so queued events are lost and
consumers fail; on memcached it evicts every other tenant sharing that server.
There is no confirmation, no scoping, and the method is one call away from any
consumer holding the `*Cache`.

**Recommendation.** Implement `Clear` as a scoped delete over the application's
own key prefix (`SCAN`+`DEL` for Redis), require an explicitly opted-in
"destructive flush" flag to use `FlushDB`/`FlushAll`, and document that the
cache must not share a Redis DB with the event broker. Consider adding a
mandatory `KeyPrefix` to `Options` so scoping is always possible.

---

### 14. Medium — `MemcacheProvider.Close()` is a no-op based on a false comment

`provider_memcache.go:267-271`:

```go
func (m *MemcacheProvider) Close() error {
	// Memcache client doesn't have a close method
	return nil
}
```

This is factually wrong for the pinned dependency: gomemcache
`v0.0.0-20260422231931-4d751bb6e37c` exposes `func (c *Client) Close() error` at
`memcache.go:836`. Its documented behaviour is to close all currently-open idle
connections (the client stays usable afterwards), which is exactly what a
provider `Close()` should be releasing.

**Failure scenario.** Every provider replacement (finding 3) and every
`cache.Close()` leaves the memcache client's idle connections — up to
`MaxIdleConns` per server — established. In a process that reconfigures the
cache, or in tests that construct providers repeatedly, file descriptors
accumulate until `accept`/`dial` starts failing with `too many open files`,
which manifests as unrelated failures elsewhere in the process.

**Recommendation.** `return m.client.Close()`. Also add `Close` handling to the
swap paths in `cache.go` per finding 3.

---

### 15. Medium — no TLS option for Redis or memcache

`RedisConfig` (`provider_redis.go:17-36`) has `Host`, `Port`, `Password`, `DB`,
`PoolSize`, `Options` — and nothing else. `redis.Options` supports `TLSConfig`,
but the constructor never sets it:

```go
client := redis.NewClient(&redis.Options{
	Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
	Password: config.Password,
	DB:       config.DB,
	PoolSize: config.PoolSize,
})
```

`MemcacheConfig` (`:18-31`) likewise has no transport security, and the
memcached protocol has no in-band auth here at all.

**Failure scenario.** The cached values are `UserContext` objects — identity and
authorization data — and the `AUTH` password is sent in cleartext on the first
command of every new connection. Anything able to observe the path between the
service and Redis (a shared VPC, a misconfigured security group, a compromised
sidecar, a managed Redis reached over the public internet) can read session
contents and steal the Redis password, then write forged `auth:session:*` entries
directly. Writing a crafted `UserContext` into the cache is a **complete
authentication bypass**: `GetOrSet` returns it on a hit and never consults the
database.

This is the same theme as `pkg/config`'s `sslmode: disable` default
(`pkg/config/manager.go:242`) — see `audit/pkg/config.audit.md` finding 3.

**Recommendation.** Add TLS configuration to both configs and plumb it through:

```go
type RedisConfig struct {
	...
	TLS               bool
	TLSSkipVerify     bool   // must default false
	TLSCACertFile     string
}
```

and set `redis.Options.TLSConfig` accordingly. Since the cache holds
authentication material, treat encrypted transport as the default and require an
explicit opt-out. For memcache, prefer Redis for this workload or terminate TLS
with a local proxy (stunnel/envoy) and document it.

---

### 16. Medium — `Remember` returns a different type on hit vs miss

`cache_manager.go:145-167`:

```go
func (c *Cache) Remember(ctx context.Context, key string, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	data, err := c.GetBytes(ctx, key)
	if err == nil {
		var result interface{}
		if err := json.Unmarshal(data, &result); err == nil {
			return result, nil       // <-- map[string]interface{} / float64 / ...
		}
	}

	value, err := loader()
	...
	return value, nil                // <-- whatever the loader returned
}
```

On a hit the value comes back as generic JSON (`map[string]interface{}`,
`[]interface{}`, `float64`, `string`); on a miss it is the loader's concrete Go
type.

**Failure scenario.** A caller writes the natural thing:

```go
v, err := c.Remember(ctx, key, ttl, func() (any, error) { return loadUser(id) })
u := v.(*User)      // panics on every cache hit
```

This passes every test run against a cold cache and panics in production as soon
as the cache warms — the worst possible failure timing. `Remember` has no
callers in `pkg/` today, so this is latent, but it is a trap laid for the next
consumer and there is no `recover()` in the package to contain it.

**Recommendation.** Either delete `Remember` in favour of `GetOrSet` (which
takes a typed `dest`), or give it the same contract:

```go
func (c *Cache) Remember(ctx context.Context, key string, dest any, ttl time.Duration, loader func() (any, error)) error
```

If the generic-return shape must stay, document it loudly and name it
`RememberAny`.

---

### 17. Medium — `Get`/`Exists` swallow every backend error as a cache miss

```go
// provider_redis.go:86-95
val, err := r.client.Get(ctx, key).Bytes()
if err == redis.Nil {
	return nil, false
}
if err != nil {
	return nil, false     // network error, WRONGTYPE, auth failure, timeout...
}

// provider_memcache.go:75-84 — same shape
// provider_memcache.go:262-265
func (m *MemcacheProvider) Exists(ctx context.Context, key string) bool {
	_, err := m.client.Get(key)
	return err == nil
}
```

The `Provider.Get` signature `([]byte, bool)` cannot express "I don't know", so
every failure is indistinguishable from an absent key. Nothing is logged —
`pkg/cache` imports no logger.

**Failure scenario.** Redis develops packet loss, or memcached is restarted, or
`maxmemory` is hit. Every lookup reports a miss, so every request falls through
to the database: the count query at `restheadspec/handler.go:857` and the session
stored procedure at `providers.go:413`. Load multiplies by the cache hit ratio
in an instant — typically 10–100× — and the database becomes the next thing to
fall over. Meanwhile the metrics say "cache miss", the logs say nothing, and the
`Stats` endpoint reports a plausible-looking miss count, so the actual cause is
invisible during the incident. Combined with finding 1, the subsequent `Set`
failure also rejects every request, so the symptom presented to operators is
"authentication is broken" with no mention of Redis.

**Recommendation.** Widen the interface to `Get(ctx, key) ([]byte, bool, error)`
(or keep the two-value form and add `GetE`), and at minimum log at warn level
with a rate limit inside each provider:

```go
if err != nil && err != redis.Nil {
	logger.Warn("cache: redis GET failed for prefix %s: %v", ctx, keyPrefix(key), err)
	return nil, false
}
```

Note `keyPrefix` rather than `key`, per finding 7. Feed a
`cache_errors_total{provider}` counter into `pkg/metrics` so a degraded backend
is alertable — the `Provider` interface there already has `RecordCacheHit`/
`RecordCacheMiss` but no error counter.

---

### 18. Low — `evictOne` is an O(n) scan under the write lock

`provider_memory.go:306-326`:

```go
func (m *MemoryProvider) evictOne() {
	var oldestKey string
	var oldestTime time.Time

	for key, item := range m.items {
		if item.isExpired() {
			delete(m.items, key)
			return
		}
		if oldestKey == "" || item.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.LastAccess
		}
	}
	if oldestKey != "" {
		delete(m.items, oldestKey)
	}
}
```

Called from `Set` (`:107`) and `SetWithTags` (`:137`) whenever the cache is at
capacity — i.e. on **every insertion** in steady state — while the write lock is
held. With the default `MaxSize: 10000` that is a 10 000-entry map walk plus
10 000 `time.Time` comparisons per write, blocking all readers (which, per
finding 5, includes every cache hit).

`Stats` (`:283-304`) has the same O(n) shape under `RLock`, and its comment
"Clean expired items first" is wrong — it holds a **read** lock and only counts.

**Recommendation.** Use a proper LRU (an intrusive doubly-linked list beside the
map, as `hashicorp/golang-lru` or `container/list` gives you) for O(1) eviction,
or sample k random entries and evict the oldest of those (Redis's approach) if
approximate LRU is acceptable. Maintain a counter for `Stats` instead of
walking. Fix the misleading comment.

---

### 19. Low — `CleanExpired` is dead code; there is no janitor

`provider_memory.go:329` `CleanExpired` has no callers anywhere in the
repository (verified by grep — the only hits are its own declaration and
comment). No goroutine sweeps expirations.

**Failure scenario.** Expired entries are reclaimed only when someone looks them
up (`Get`, `:66`) or when `evictOne` happens to walk past one. A workload whose
keys are written once and never re-read — which is exactly the
`query_total:<sha256>` pattern, since a distinct filter combination is usually
requested once — retains every expired entry until `MaxSize` forces eviction.
The cache therefore sits permanently at its maximum footprint holding mostly
dead data, and the LRU scan in finding 18 walks those dead entries on every
insertion. With `MaxSize <= 0` (finding 22) nothing reclaims them at all.

**Recommendation.** Start a janitor goroutine from `NewMemoryProvider` with a
configurable interval, and stop it in `Close()`:

```go
func NewMemoryProvider(opts *Options) *MemoryProvider {
	m := &MemoryProvider{...; done: make(chan struct{})}
	go m.janitor(opts.CleanupInterval)   // default e.g. 1 minute
	return m
}
```

Guard the goroutine with `defer logger.CatchPanicCallback("cache.janitor", ...)`
so a panic in the sweep is reported rather than killing the process silently.

---

### 20. Low — `RedisProvider.Stats` returns the raw `INFO` output

`provider_redis.go:247-268`:

```go
info, err := r.client.Info(ctx, "stats", "keyspace").Result()
...
stats := &CacheStats{
	Keys:         dbSize,
	ProviderType: "redis",
	ProviderStats: map[string]any{
		"info": info,
	},
}
```

`CacheStats.ProviderStats` is `json:"provider_stats,omitempty"` — i.e. designed
to be serialized. The `stats` and `keyspace` sections include the Redis version,
uptime, connected-client counts, keyspace hit/miss totals, eviction and
expiration counters, and per-database key counts.

**Failure scenario.** `cache.GetStats` (`cache.go:65`) has no callers today, but
it is an obvious thing to wire to a `/health` or `/admin/stats` endpoint. Doing
so exposes infrastructure fingerprinting to any client that reaches it, and the
keyspace counters leak activity volume. It is an information-disclosure
primitive waiting for a route.

**Recommendation.** Parse `INFO` into a small allowlisted set of numeric fields
(`keyspace_hits`, `keyspace_misses`, `evicted_keys`, `expired_keys`) and
populate `CacheStats.Hits`/`Misses` from them rather than passing the blob
through. If the raw text is wanted for debugging, gate it behind an explicit
debug flag and never include it in a response body.

---

### 21. Low — the memcache provider ignores `ctx` entirely

Every method on `MemcacheProvider` accepts `ctx context.Context` and none uses
it (`:75`, `:87`, `:103`, `:177`, `:218`, `:252`, `:257`, `:262`, `:275`). The
only timeout is the client-wide `config.Timeout` (default 1s, `:49-51`).

**Failure scenario.** A client disconnects or the request deadline expires; the
handler's context is cancelled, but the cache call continues to completion. In
`SetWithTags` that is one `Set` plus, per tag, a `Get` and a `Set` — so a
two-tag write is five sequential round trips, each able to consume the full 1s
client timeout, all after the caller has given up. Under load-shedding
conditions the server keeps doing work for requests nobody is waiting for, which
is precisely when it can least afford to.

**Recommendation.** gomemcache's API is context-free, so either wrap each call
with a `select` on `ctx.Done()` and a goroutine, or switch to a
context-aware client. At minimum, check `ctx.Err()` at the top of each method
and return early, and document that `config.Timeout` is the real bound.

---

### 22. Low — `MaxSize <= 0` silently disables eviction

`provider_memory.go:105` and `:135` both guard with `if m.options.MaxSize > 0 && ...`.
A caller who constructs `&Options{DefaultTTL: time.Minute}` — as
`NewRedisProvider` (`:59`) and `NewMemcacheProvider` (`:54`) do for their own
defaults, and as any hand-written config easily does — gets an **unbounded**
in-memory cache. Combined with findings 4 and 19, memory then grows with
attacker-controlled query variation until the process is OOM-killed.

**Recommendation.** Treat `MaxSize <= 0` as "use the default" (10 000) rather
than "unlimited", and require an explicit sentinel such as `MaxSize: -1` to
opt into unbounded. Validate `Options` in `NewMemoryProvider` and log the
effective values once at startup.

---

### 23. Low — constructors mutate the caller's config struct

`NewMemcacheProvider` writes `config.Servers`, `config.MaxIdleConns`,
`config.Timeout` and `config.Options` (`:41-57`); `NewRedisProvider` writes
`config.Host`, `config.Port`, `config.PoolSize`, `config.Options` (`:48-62`).
Both also assign into the `config == nil` replacement, which is at least local.

**Failure scenario.** A caller holds one config struct and constructs two
providers from it (a common test pattern, or a primary/replica setup). The second
construction sees the first one's defaults already applied, so "zero means
default" no longer holds and an intentional later change is silently ignored.
Callers reasonably assume a constructor does not write to their arguments.

**Recommendation.** Copy first: `cfg := *config` (plus a copy of `Options` if it
is non-nil, since it is a pointer), then apply defaults to the local copy.

---

### 24. Low — no defensive copy of `[]byte` in the memory provider

`Set` stores the caller's slice directly (`provider_memory.go:112`) and `Get`
returns the stored slice directly (`:77`, `:87`). Both share backing memory with
the caller.

**Failure scenario.** Two requests `GetBytes` the same key and receive the same
underlying array. If either mutates it in place — or if a caller reuses a buffer
it passed to `SetBytes` — the cached value changes for everyone, with no lock
held and no copy. For the session cache that means one request's scratch buffer
can rewrite another user's cached `UserContext`. Today every consumer goes
through `json.Marshal`/`json.Unmarshal` in `cache_manager.go`, which allocates
fresh slices, so this is latent rather than live; the `SetBytes`/`GetBytes`
API (`:56`, `:37`) exposes it directly to any future caller.

**Recommendation.** Copy on both sides in `MemoryProvider` — the Redis and
memcache providers get copies for free because the data crosses a socket, so
this also removes a behavioural difference between providers:

```go
buf := make([]byte, len(value))
copy(buf, value)
```

Document the ownership rule on the `Provider` interface either way.

---

### 25. Low — `example_usage.go` is a library file full of `log.Fatal`

`pkg/cache/example_usage.go` (266 lines) is compiled into the package and calls
`log.Fatal` at seventeen sites (`:18`, `:36`, `:44`, `:57`, `:64`, `:83`, `:96`,
`:103`, `:112`, `:127`, `:146`, `:154`, `:168`, `:187`, `:199`, `:224`, `:245`).
`log.Fatal` calls `os.Exit(1)`.

**Failure scenario.** These are exported functions (`ExampleInMemoryCache`,
`ExampleRedisCache`, `ExampleMemcacheCache`, …) with no `_test.go` suffix, so
they are part of the package's public API. Anything that calls one — a
misremembered name, a code-completion accident, a copied snippet — can terminate
the host process on a cache error, bypassing every panic handler and graceful
shutdown path. They also drag `log` into the package's dependency set while the
package deliberately imports no logger.

**Recommendation.** Move the file to `example_usage_test.go` (Go's testable-example
convention, which also makes them compile-checked and runnable), or to a
`_examples/` directory outside the package. Replace `log.Fatal` with returned
errors.

---

## What looks right

- `Provider` (`provider.go:9-44`) is a clean, minimal interface; the three
  backends are genuinely swappable and the package has no import cycle problems
  (it imports nothing from `ResolveSpec` at all).
- `hits`/`misses` use `atomic.Int64` (`provider_memory.go:35-36`) and are read
  with `Load()` in `Stats`, so the counters themselves are race-free.
- `MemoryProvider.Delete` (`:173-191`) and `SetWithTags` (`:141-151`) do maintain
  `tagToKeys` correctly, including deleting the tag entry when its key set
  empties — the bug in finding 4 is the *other* paths not doing the same.
- `DeleteByTag` (`:194-228`) correctly handles multi-tag items: it strips only
  the invalidated tag and keeps the item alive if other tags remain.
- `RedisProvider.DeleteByPattern` (`:196-225`) batches `DEL` in groups of 100
  rather than buffering an unbounded pipeline, and checks `iter.Err()`.
- `RedisProvider.Close` (`:242-244`) correctly delegates to `client.Close()`.
- Both network providers verify connectivity at construction
  (`provider_redis.go:75`, `provider_memcache.go:64`) and return an error rather
  than deferring the failure to the first request — though the Redis `Ping` uses
  a 5s blocking timeout on a `context.Background()`, which delays startup.
- Cache keys for query totals are SHA-256 hashes of the query shape
  (`restheadspec/cache_helpers.go:88-92`), not raw user input — which is why the
  key-injection risk in findings 9 and 12 is confined to the `pkg/security`
  session path.
- `pkg/security/keystore_database.go:287` already keys on a hash
  (`keystoreCacheKey(hash)`), which is the pattern `providers.go:398` should
  follow.

## Suggested follow-up

Ordered by value:

1. **Make cache failures non-fatal in the auth path** (findings 1, 2, 9). This
   is the single highest-value change: a cache problem should never be able to
   reject a valid credential, and a revocation that cannot be honoured must be
   loud.
2. **Key the session cache on `sha256(token)`** (findings 7, 9, 12) in
   `pkg/security/providers.go:318`, `:398`, `:466`. Removes attacker control of
   cache keys, bounds their length, and keeps the credential out of error
   strings.
3. **Replace `DeleteByPattern`-based revocation with `DeleteByTag`**
   (findings 2, 6), and make `ClearUserCache` actually scoped to the user.
4. **Fix the `defaultCache` race** (finding 3) with `atomic.Pointer` +
   `sync.Once`, and close the displaced provider on swap (finding 14).
5. **Add `go test -race ./pkg/cache/...` to CI.** Findings 3 and 10 are
   detectable in minutes with a small concurrent test; see
   `_CROSS-CUTTING.audit.md`. `pkg/cache` currently has 69 lines of tests — two
   functions, `TestSetDefaultCache` (`cache_test.go:9`) and
   `TestGetDefaultCacheInitialization` (`:50`) — and is not in the package list
   that `Makefile`/`.github/workflows/tests.yml` run.
6. **Centralize tag-index maintenance in `MemoryProvider`** (finding 4) and reset
   it in `Clear`; add an eviction path that cannot leak.
7. **Make `MemoryProvider.Get` read-only** (finding 5) by moving `LastAccess`
   and `HitCount` to atomics.
8. **Add single-flight to `GetOrSet`** (finding 8).
9. **Add TLS to `RedisConfig`/`MemcacheConfig`** (finding 15) and default it on.
10. **Give the package a logger and error metrics** (finding 17). Right now
    `pkg/cache` cannot tell anyone that anything went wrong: 1 538 lines, zero
    log statements, zero `recover()`, and eight `_ =` error discards outside the
    examples file — seven of them in `provider_memcache.go`, precisely where the
    tag index breaks (finding 12).
11. **Scope `Clear`** (finding 13) so it cannot flush the event broker's Redis DB.

## Cross-references

- `audit/pkg/logger.audit.md` — finding 2 (messages forwarded to Sentry
  unscrubbed) is what makes finding 7 here dangerous; finding 4 (`CatchPanic`
  swallows) is why findings 11/16/19 should not simply add `CatchPanic`.
- `audit/pkg/config.audit.md` — finding 3 (insecure transport defaults) is the
  same theme as finding 15; `cache.redis.*` defaults live at
  `pkg/config/manager.go:195-202` and expose no TLS field.
- `audit/pkg/security.audit.md` — findings 1, 2, 7, 8, 9 all land in
  `pkg/security/providers.go`; the unbounded `go a.updateSessionActivity(...)`
  per request (`:447`) is raised there.
- `audit/pkg/restheadspec.audit.md` — the query-total cache path
  (`handler.go:829-860`) and tag-based invalidation (`:1477`, `:1711`, `:1785`,
  `:1859`, `:1919`, `:2024`) are the other consumer; error returns from
  `invalidateCacheForTags` are the invalidation-failure signal.
- `audit/pkg/metrics.audit.md` — `Provider` has `RecordCacheHit`/
  `RecordCacheMiss`/`UpdateCacheSize` but no cache-error counter, and
  `pkg/cache` calls none of them.
- `audit/pkg/_CROSS-CUTTING.audit.md` — no `-race` in CI; only
  `pkg/resolvespec` and `pkg/restheadspec` are tested.
