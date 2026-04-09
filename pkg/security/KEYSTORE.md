# Keystore

Per-user named auth keys with pluggable storage. Each user can hold multiple keys of different types — JWT secrets, header API keys, OAuth2 client credentials, or generic API keys. Keys are identified by a human-readable name ("CI deploy", "mobile app") and can carry scopes and arbitrary metadata.

## Key types

| Constant | Value | Use case |
|---|---|---|
| `KeyTypeJWTSecret` | `jwt_secret` | Per-user JWT signing secret |
| `KeyTypeHeaderAPI` | `header_api` | Static API key sent in a request header |
| `KeyTypeOAuth2` | `oauth2` | OAuth2 client credentials |
| `KeyTypeGenericAPI` | `api` | General-purpose application key |

## Storage backends

### ConfigKeyStore

In-memory store seeded from a static list. Suitable for a small, fixed set of service-account keys loaded from a config file. Keys created at runtime via `CreateKey` are held in memory and lost on restart.

```go
// Pre-load keys from config (KeyHash = SHA-256 hex of the raw key)
store := security.NewConfigKeyStore([]security.UserKey{
    {
        UserID:  1,
        KeyType: security.KeyTypeGenericAPI,
        KeyHash: "e3b0c44298fc1c149afb...", // sha256(rawKey)
        Name:    "CI deploy",
        Scopes:  []string{"deploy"},
        IsActive: true,
    },
})
```

### DatabaseKeyStore

Backed by PostgreSQL stored procedures. Supports optional caching (default 2-minute TTL). Apply `keystore_schema.sql` before use.

```go
db, _ := sql.Open("postgres", dsn)

store := security.NewDatabaseKeyStore(db)

// With options
store = security.NewDatabaseKeyStore(db, security.DatabaseKeyStoreOptions{
    CacheTTL: 5 * time.Minute,
    SQLNames: &security.KeyStoreSQLNames{
        ValidateKey: "myapp_keystore_validate", // override one procedure name
    },
})
```

## Managing keys

```go
ctx := context.Background()

// Create — raw key returned once; store it securely
resp, err := store.CreateKey(ctx, security.CreateKeyRequest{
    UserID:  42,
    KeyType: security.KeyTypeGenericAPI,
    Name:    "mobile app",
    Scopes:  []string{"read", "write"},
})
fmt.Println(resp.RawKey) // only shown here; hashed internally

// List
keys, err := store.GetUserKeys(ctx, 42, "") // "" = all types
keys, err  = store.GetUserKeys(ctx, 42, security.KeyTypeGenericAPI)

// Revoke
err = store.DeleteKey(ctx, 42, resp.Key.ID)

// Validate (used by authenticators internally)
key, err := store.ValidateKey(ctx, rawKey, "")
```

## HTTP authentication

`KeyStoreAuthenticator` wraps any `KeyStore` and implements the `Authenticator` interface. It is drop-in compatible with `DatabaseAuthenticator` and works in `CompositeSecurityProvider`.

Keys are extracted from the request in this order:

1. `Authorization: Bearer <key>`
2. `Authorization: ApiKey <key>`
3. `X-API-Key: <key>`

```go
auth := security.NewKeyStoreAuthenticator(store, "") // "" = accept any key type
// Restrict to a specific type:
auth = security.NewKeyStoreAuthenticator(store, security.KeyTypeGenericAPI)
```

Plug it into a handler:

```go
handler := resolvespec.NewHandler(db, registry,
    resolvespec.WithAuthenticator(auth),
)
```

`Login` and `Logout` return an error — key lifecycle is managed through `KeyStore` directly.

On successful validation the request context receives a `UserContext` where:

- `UserID` — from the key
- `Roles` — the key's `Scopes`
- `Claims["key_type"]` — key type string
- `Claims["key_name"]` — key name

## Database setup

Apply `keystore_schema.sql` to your PostgreSQL database. It requires the `users` table from the main `database_schema.sql`.

```sql
\i pkg/security/keystore_schema.sql
```

This creates:

- `user_keys` table with indexes on `user_id`, `key_hash`, and `key_type`
- `resolvespec_keystore_get_user_keys(p_user_id, p_key_type)`
- `resolvespec_keystore_create_key(p_request jsonb)`
- `resolvespec_keystore_delete_key(p_user_id, p_key_id)`
- `resolvespec_keystore_validate_key(p_key_hash, p_key_type)`

### Custom procedure names

```go
store := security.NewDatabaseKeyStore(db, security.DatabaseKeyStoreOptions{
    SQLNames: &security.KeyStoreSQLNames{
        GetUserKeys: "myschema_get_keys",
        CreateKey:   "myschema_create_key",
        DeleteKey:   "myschema_delete_key",
        ValidateKey: "myschema_validate_key",
    },
})

// Validate names at startup
names := &security.KeyStoreSQLNames{
    GetUserKeys: "myschema_get_keys",
    // ...
}
if err := security.ValidateKeyStoreSQLNames(names); err != nil {
    log.Fatal(err)
}
```

## Security notes

- Raw keys are never stored. Only the SHA-256 hex digest is persisted.
- The raw key is generated with `crypto/rand` (32 bytes, base64url-encoded) and returned exactly once in `CreateKeyResponse.RawKey`.
- Hash comparisons in `ConfigKeyStore` use `crypto/subtle.ConstantTimeCompare` to prevent timing side-channels.
- `DeleteKey` performs a soft delete (`is_active = false`). The `DatabaseKeyStore` invalidates the cache entry immediately, but due to the cache TTL a revoked key may authenticate for up to `CacheTTL` (default 2 minutes) in a distributed environment. Set `CacheTTL: 0` to disable caching if immediate revocation is required.
