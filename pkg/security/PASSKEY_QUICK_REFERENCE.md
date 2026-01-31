# Passkey Authentication Quick Reference

## Overview
Passkey authentication (WebAuthn/FIDO2) is now integrated into the DatabaseAuthenticator. This provides passwordless authentication using biometrics, security keys, or device credentials.

## Setup

### Database Schema
Run the passkey SQL schema (in database_schema.sql):
- Creates `user_passkey_credentials` table
- Adds stored procedures for passkey operations

### Go Code
```go
// Create passkey provider
passkeyProvider := security.NewDatabasePasskeyProvider(db, 
    security.DatabasePasskeyProviderOptions{
        RPID:     "example.com",
        RPName:   "Example App",
        RPOrigin: "https://example.com",
        Timeout:  60000,
    })

// Create authenticator with passkey support
auth := security.NewDatabaseAuthenticatorWithOptions(db, 
    security.DatabaseAuthenticatorOptions{
        PasskeyProvider: passkeyProvider,
    })

// Or add passkey to existing authenticator
auth = security.NewDatabaseAuthenticator(db).WithPasskey(passkeyProvider)
```

## Registration Flow

### Backend - Step 1: Begin Registration
```go
options, err := auth.BeginPasskeyRegistration(ctx, 
    security.PasskeyBeginRegistrationRequest{
        UserID:      1,
        Username:    "alice",
        DisplayName: "Alice Smith",
    })
// Send options to client as JSON
```

### Frontend - Step 2: Create Credential
```javascript
// Convert options from server
options.challenge = base64ToArrayBuffer(options.challenge);
options.user.id = base64ToArrayBuffer(options.user.id);

// Create credential
const credential = await navigator.credentials.create({
    publicKey: options
});

// Send credential back to server
```

### Backend - Step 3: Complete Registration
```go
credential, err := auth.CompletePasskeyRegistration(ctx, 
    security.PasskeyRegisterRequest{
        UserID:            1,
        Response:          clientResponse,
        ExpectedChallenge: storedChallenge,
        CredentialName:    "My iPhone",
    })
```

## Authentication Flow

### Backend - Step 1: Begin Authentication
```go
options, err := auth.BeginPasskeyAuthentication(ctx,
    security.PasskeyBeginAuthenticationRequest{
        Username: "alice", // Optional for resident key
    })
// Send options to client as JSON
```

### Frontend - Step 2: Get Credential
```javascript
// Convert options from server
options.challenge = base64ToArrayBuffer(options.challenge);

// Get credential
const credential = await navigator.credentials.get({
    publicKey: options
});

// Send assertion back to server
```

### Backend - Step 3: Complete Authentication
```go
loginResponse, err := auth.LoginWithPasskey(ctx,
    security.PasskeyLoginRequest{
        Response:          clientAssertion,
        ExpectedChallenge: storedChallenge,
        Claims: map[string]any{
            "ip_address": "192.168.1.1",
            "user_agent": "Mozilla/5.0...",
        },
    })
// Returns session token and user info
```

## Credential Management

### List Credentials
```go
credentials, err := auth.GetPasskeyCredentials(ctx, userID)
```

### Update Credential Name
```go
err := auth.UpdatePasskeyCredentialName(ctx, userID, credentialID, "New Name")
```

### Delete Credential
```go
err := auth.DeletePasskeyCredential(ctx, userID, credentialID)
```

## HTTP Endpoints Example

### POST /api/passkey/register/begin
Request: `{user_id, username, display_name}`
Response: PasskeyRegistrationOptions

### POST /api/passkey/register/complete
Request: `{user_id, response, credential_name}`
Response: PasskeyCredential

### POST /api/passkey/login/begin
Request: `{username}` (optional)
Response: PasskeyAuthenticationOptions

### POST /api/passkey/login/complete
Request: `{response}`
Response: LoginResponse with session token

### GET /api/passkey/credentials
Response: Array of PasskeyCredential

### DELETE /api/passkey/credentials/{id}
Request: `{credential_id}`
Response: 204 No Content

## Database Stored Procedures

- `resolvespec_passkey_store_credential` - Store new credential
- `resolvespec_passkey_get_credential` - Get credential by ID
- `resolvespec_passkey_get_user_credentials` - Get all user credentials
- `resolvespec_passkey_update_counter` - Update sign counter (clone detection)
- `resolvespec_passkey_delete_credential` - Delete credential
- `resolvespec_passkey_update_name` - Update credential name
- `resolvespec_passkey_get_credentials_by_username` - Get credentials for login

## Security Features

- **Clone Detection**: Sign counter validation detects credential cloning
- **Attestation Support**: Stores attestation type (none, indirect, direct)
- **Transport Options**: Tracks authenticator transports (usb, nfc, ble, internal)
- **Backup State**: Tracks if credential is backed up/synced
- **User Verification**: Supports preferred/required user verification

## Important Notes

1. **WebAuthn Library**: Current implementation is simplified. For production, use a proper WebAuthn library like `github.com/go-webauthn/webauthn` for full verification.

2. **Challenge Storage**: Store challenges securely in session/cache. Never expose challenges to client beyond initial request.

3. **HTTPS Required**: Passkeys only work over HTTPS (except localhost).

4. **Browser Support**: Check browser compatibility for WebAuthn API.

5. **Relying Party ID**: Must match your domain exactly.

## Client-Side Helper Functions

```javascript
function base64ToArrayBuffer(base64) {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes.buffer;
}

function arrayBufferToBase64(buffer) {
    const bytes = new Uint8Array(buffer);
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
}
```

## Testing

Run tests: `go test -v ./pkg/security -run Passkey`

All passkey functionality includes comprehensive tests using sqlmock.
