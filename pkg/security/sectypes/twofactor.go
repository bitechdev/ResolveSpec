package sectypes

// TwoFactorSecret contains 2FA setup information
type TwoFactorSecret struct {
	Secret      string   `json:"secret"`       // Base32 encoded secret
	QRCodeURL   string   `json:"qr_code_url"`  // URL for QR code generation
	BackupCodes []string `json:"backup_codes"` // One-time backup codes
	Issuer      string   `json:"issuer"`       // Application name
	AccountName string   `json:"account_name"` // User identifier (email/username)
}
