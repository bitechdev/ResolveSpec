package security

import (
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Shared data types live in sectypes so that lookup and the sub packages can use
// them without importing this package. They are aliased here permanently, so
// security.X and sectypes.X are identical types.

type (
	UserContext                             = sectypes.UserContext
	LoginRequest                            = sectypes.LoginRequest
	RegisterRequest                         = sectypes.RegisterRequest
	LoginResponse                           = sectypes.LoginResponse
	LogoutRequest                           = sectypes.LogoutRequest
	PasswordResetRequest                    = sectypes.PasswordResetRequest
	PasswordResetResponse                   = sectypes.PasswordResetResponse
	PasswordResetCompleteRequest            = sectypes.PasswordResetCompleteRequest
	KeyType                                 = sectypes.KeyType
	UserKey                                 = sectypes.UserKey
	CreateKeyRequest                        = sectypes.CreateKeyRequest
	CreateKeyResponse                       = sectypes.CreateKeyResponse
	OAuthServerClient                       = sectypes.OAuthServerClient
	OAuthCode                               = sectypes.OAuthCode
	OAuthTokenInfo                          = sectypes.OAuthTokenInfo
	PasskeyCredential                       = sectypes.PasskeyCredential
	PasskeyRegistrationOptions              = sectypes.PasskeyRegistrationOptions
	PasskeyAuthenticationOptions            = sectypes.PasskeyAuthenticationOptions
	PasskeyRelyingParty                     = sectypes.PasskeyRelyingParty
	PasskeyUser                             = sectypes.PasskeyUser
	PasskeyCredentialParam                  = sectypes.PasskeyCredentialParam
	PasskeyCredentialDescriptor             = sectypes.PasskeyCredentialDescriptor
	PasskeyAuthenticatorSelection           = sectypes.PasskeyAuthenticatorSelection
	PasskeyRegistrationResponse             = sectypes.PasskeyRegistrationResponse
	PasskeyAuthenticatorAttestationResponse = sectypes.PasskeyAuthenticatorAttestationResponse
	PasskeyAuthenticationResponse           = sectypes.PasskeyAuthenticationResponse
	PasskeyAuthenticatorAssertionResponse   = sectypes.PasskeyAuthenticatorAssertionResponse
	TwoFactorSecret                         = sectypes.TwoFactorSecret
	ColumnSecurity                          = sectypes.ColumnSecurity
	RowSecurity                             = sectypes.RowSecurity
)

const (
	KeyTypeJWTSecret  = sectypes.KeyTypeJWTSecret
	KeyTypeHeaderAPI  = sectypes.KeyTypeHeaderAPI
	KeyTypeOAuth2     = sectypes.KeyTypeOAuth2
	KeyTypeGenericAPI = sectypes.KeyTypeGenericAPI
)

// errInvalidAPIKey is the single error API-key login returns for unknown, expired, inactive
// and wrong-type keys.
var errInvalidAPIKey = lookup.ErrInvalidAPIKey
