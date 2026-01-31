package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
)

// PasskeyAuthenticationExample demonstrates passkey (WebAuthn/FIDO2) authentication
func PasskeyAuthenticationExample() {
	// Setup database connection
	db, _ := sql.Open("postgres", "postgres://user:pass@localhost/db")

	// Create passkey provider
	passkeyProvider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:     "example.com",         // Your domain
		RPName:   "Example Application", // Display name
		RPOrigin: "https://example.com", // Expected origin
		Timeout:  60000,                 // 60 seconds
	})

	// Create authenticator with passkey support
	// Option 1: Pass during creation
	_ = NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{
		PasskeyProvider: passkeyProvider,
	})

	// Option 2: Use WithPasskey method
	auth := NewDatabaseAuthenticator(db).WithPasskey(passkeyProvider)

	ctx := context.Background()

	// === REGISTRATION FLOW ===

	// Step 1: Begin registration
	regOptions, _ := auth.BeginPasskeyRegistration(ctx, PasskeyBeginRegistrationRequest{
		UserID:      1,
		Username:    "alice",
		DisplayName: "Alice Smith",
	})

	// Send regOptions to client as JSON
	// Client will call navigator.credentials.create() with these options
	_ = regOptions

	// Step 2: Complete registration (after client returns credential)
	// This would come from the client's navigator.credentials.create() response
	clientResponse := PasskeyRegistrationResponse{
		ID:    "base64-credential-id",
		RawID: []byte("raw-credential-id"),
		Type:  "public-key",
		Response: PasskeyAuthenticatorAttestationResponse{
			ClientDataJSON:    []byte("..."),
			AttestationObject: []byte("..."),
		},
		Transports: []string{"internal"},
	}

	credential, _ := auth.CompletePasskeyRegistration(ctx, PasskeyRegisterRequest{
		UserID:            1,
		Response:          clientResponse,
		ExpectedChallenge: regOptions.Challenge,
		CredentialName:    "My iPhone",
	})

	fmt.Printf("Registered credential: %s\n", credential.ID)

	// === AUTHENTICATION FLOW ===

	// Step 1: Begin authentication
	authOptions, _ := auth.BeginPasskeyAuthentication(ctx, PasskeyBeginAuthenticationRequest{
		Username: "alice", // Optional - omit for resident key flow
	})

	// Send authOptions to client as JSON
	// Client will call navigator.credentials.get() with these options
	_ = authOptions

	// Step 2: Complete authentication (after client returns assertion)
	// This would come from the client's navigator.credentials.get() response
	clientAssertion := PasskeyAuthenticationResponse{
		ID:    "base64-credential-id",
		RawID: []byte("raw-credential-id"),
		Type:  "public-key",
		Response: PasskeyAuthenticatorAssertionResponse{
			ClientDataJSON:    []byte("..."),
			AuthenticatorData: []byte("..."),
			Signature:         []byte("..."),
		},
	}

	loginResponse, _ := auth.LoginWithPasskey(ctx, PasskeyLoginRequest{
		Response:          clientAssertion,
		ExpectedChallenge: authOptions.Challenge,
		Claims: map[string]any{
			"ip_address": "192.168.1.1",
			"user_agent": "Mozilla/5.0...",
		},
	})

	fmt.Printf("Logged in user: %s with token: %s\n",
		loginResponse.User.UserName, loginResponse.Token)

	// === CREDENTIAL MANAGEMENT ===

	// Get all credentials for a user
	credentials, _ := auth.GetPasskeyCredentials(ctx, 1)
	for i := range credentials {
		fmt.Printf("Credential: %s (created: %s, last used: %s)\n",
			credentials[i].Name, credentials[i].CreatedAt, credentials[i].LastUsedAt)
	}

	// Update credential name
	_ = auth.UpdatePasskeyCredentialName(ctx, 1, credential.ID, "My New iPhone")

	// Delete credential
	_ = auth.DeletePasskeyCredential(ctx, 1, credential.ID)
}

// PasskeyHTTPHandlersExample shows HTTP handlers for passkey authentication
func PasskeyHTTPHandlersExample(auth *DatabaseAuthenticator) {
	// Store challenges in session/cache in production
	challenges := make(map[string][]byte)

	// Begin registration endpoint
	http.HandleFunc("/api/passkey/register/begin", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID      int    `json:"user_id"`
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		options, err := auth.BeginPasskeyRegistration(r.Context(), PasskeyBeginRegistrationRequest{
			UserID:      req.UserID,
			Username:    req.Username,
			DisplayName: req.DisplayName,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Store challenge for verification (use session ID as key in production)
		sessionID := "session-123"
		challenges[sessionID] = options.Challenge

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(options)
	})

	// Complete registration endpoint
	http.HandleFunc("/api/passkey/register/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID         int                         `json:"user_id"`
			Response       PasskeyRegistrationResponse `json:"response"`
			CredentialName string                      `json:"credential_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Get stored challenge (from session in production)
		sessionID := "session-123"
		challenge := challenges[sessionID]
		delete(challenges, sessionID)

		credential, err := auth.CompletePasskeyRegistration(r.Context(), PasskeyRegisterRequest{
			UserID:            req.UserID,
			Response:          req.Response,
			ExpectedChallenge: challenge,
			CredentialName:    req.CredentialName,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(credential)
	})

	// Begin authentication endpoint
	http.HandleFunc("/api/passkey/login/begin", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"` // Optional
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		options, err := auth.BeginPasskeyAuthentication(r.Context(), PasskeyBeginAuthenticationRequest{
			Username: req.Username,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Store challenge for verification (use session ID as key in production)
		sessionID := "session-456"
		challenges[sessionID] = options.Challenge

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(options)
	})

	// Complete authentication endpoint
	http.HandleFunc("/api/passkey/login/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Response PasskeyAuthenticationResponse `json:"response"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Get stored challenge (from session in production)
		sessionID := "session-456"
		challenge := challenges[sessionID]
		delete(challenges, sessionID)

		loginResponse, err := auth.LoginWithPasskey(r.Context(), PasskeyLoginRequest{
			Response:          req.Response,
			ExpectedChallenge: challenge,
			Claims: map[string]any{
				"ip_address": r.RemoteAddr,
				"user_agent": r.UserAgent(),
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Set session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    loginResponse.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loginResponse)
	})

	// List credentials endpoint
	http.HandleFunc("/api/passkey/credentials", func(w http.ResponseWriter, r *http.Request) {
		// Get user from authenticated session
		userCtx, err := auth.Authenticate(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		credentials, err := auth.GetPasskeyCredentials(r.Context(), userCtx.UserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(credentials)
	})

	// Delete credential endpoint
	http.HandleFunc("/api/passkey/credentials/delete", func(w http.ResponseWriter, r *http.Request) {
		userCtx, err := auth.Authenticate(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			CredentialID string `json:"credential_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		err = auth.DeletePasskeyCredential(r.Context(), userCtx.UserID, req.CredentialID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
}

// PasskeyClientSideExample shows the client-side JavaScript code needed
func PasskeyClientSideExample() string {
	return `
// === CLIENT-SIDE JAVASCRIPT FOR PASSKEY AUTHENTICATION ===

// Helper function to convert base64 to ArrayBuffer
function base64ToArrayBuffer(base64) {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes.buffer;
}

// Helper function to convert ArrayBuffer to base64
function arrayBufferToBase64(buffer) {
    const bytes = new Uint8Array(buffer);
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
}

// === REGISTRATION ===

async function registerPasskey(userId, username, displayName) {
    // Step 1: Get registration options from server
    const optionsResponse = await fetch('/api/passkey/register/begin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_id: userId, username, display_name: displayName })
    });
    const options = await optionsResponse.json();
    
    // Convert base64 strings to ArrayBuffers
    options.challenge = base64ToArrayBuffer(options.challenge);
    options.user.id = base64ToArrayBuffer(options.user.id);
    if (options.excludeCredentials) {
        options.excludeCredentials = options.excludeCredentials.map(cred => ({
            ...cred,
            id: base64ToArrayBuffer(cred.id)
        }));
    }
    
    // Step 2: Create credential using WebAuthn API
    const credential = await navigator.credentials.create({
        publicKey: options
    });
    
    // Step 3: Send credential to server
    const credentialResponse = {
        id: credential.id,
        rawId: arrayBufferToBase64(credential.rawId),
        type: credential.type,
        response: {
            clientDataJSON: arrayBufferToBase64(credential.response.clientDataJSON),
            attestationObject: arrayBufferToBase64(credential.response.attestationObject)
        },
        transports: credential.response.getTransports ? credential.response.getTransports() : []
    };
    
    const completeResponse = await fetch('/api/passkey/register/complete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            user_id: userId,
            response: credentialResponse,
            credential_name: 'My Device'
        })
    });
    
    return await completeResponse.json();
}

// === AUTHENTICATION ===

async function loginWithPasskey(username) {
    // Step 1: Get authentication options from server
    const optionsResponse = await fetch('/api/passkey/login/begin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username })
    });
    const options = await optionsResponse.json();
    
    // Convert base64 strings to ArrayBuffers
    options.challenge = base64ToArrayBuffer(options.challenge);
    if (options.allowCredentials) {
        options.allowCredentials = options.allowCredentials.map(cred => ({
            ...cred,
            id: base64ToArrayBuffer(cred.id)
        }));
    }
    
    // Step 2: Get credential using WebAuthn API
    const credential = await navigator.credentials.get({
        publicKey: options
    });
    
    // Step 3: Send assertion to server
    const assertionResponse = {
        id: credential.id,
        rawId: arrayBufferToBase64(credential.rawId),
        type: credential.type,
        response: {
            clientDataJSON: arrayBufferToBase64(credential.response.clientDataJSON),
            authenticatorData: arrayBufferToBase64(credential.response.authenticatorData),
            signature: arrayBufferToBase64(credential.response.signature),
            userHandle: credential.response.userHandle ? arrayBufferToBase64(credential.response.userHandle) : null
        }
    };
    
    const loginResponse = await fetch('/api/passkey/login/complete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ response: assertionResponse })
    });
    
    return await loginResponse.json();
}

// === USAGE ===

// Register a new passkey
document.getElementById('register-btn').addEventListener('click', async () => {
    try {
        const result = await registerPasskey(1, 'alice', 'Alice Smith');
        console.log('Passkey registered:', result);
    } catch (error) {
        console.error('Registration failed:', error);
    }
});

// Login with passkey
document.getElementById('login-btn').addEventListener('click', async () => {
    try {
        const result = await loginWithPasskey('alice');
        console.log('Logged in:', result);
    } catch (error) {
        console.error('Login failed:', error);
    }
});
`
}
