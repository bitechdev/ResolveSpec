package security

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// TxSettingsFunc returns the transaction-local settings (e.g. RLS GUCs such as
// "app.user_id") to stamp on a transaction. It runs once per transaction, at
// OnTxBegin, before any other SQL. Returning an error rolls the transaction back.
type TxSettingsFunc func(secCtx SecurityContext) (map[string]string, error)

// settingNameRE matches a custom GUC name: two or more dot-separated identifiers.
var settingNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)

// SetTxSettings sets the function that provides transaction-local settings for
// every transaction opened by a spec that registered its security hooks with this
// list. Pass nil to disable. May be called before or after RegisterSecurityHooks.
func (m *SecurityList) SetTxSettings(fn TxSettingsFunc) {
	m.txSettingsMu.Lock()
	defer m.txSettingsMu.Unlock()
	m.txSettings = fn
}

// TxSettings returns the configured TxSettingsFunc, or nil.
func (m *SecurityList) TxSettings() TxSettingsFunc {
	m.txSettingsMu.RLock()
	defer m.txSettingsMu.RUnlock()
	return m.txSettings
}

// StampTxSettings runs the list's TxSettingsFunc and applies the result to tx as
// transaction-local settings (set_config(name, value, true)). No-op when no
// function is configured or it returns no settings. tx must be the transaction
// itself, never the pool: the settings are lost on any other connection.
func StampTxSettings(secCtx SecurityContext, list *SecurityList, tx common.Database) error {
	if list == nil {
		return nil
	}
	fn := list.TxSettings()
	if fn == nil {
		return nil
	}
	settings, err := fn(secCtx)
	if err != nil {
		return err
	}
	return ApplyTxSettings(secCtx, tx, settings)
}

// ApplyTxSettings sets each entry as a transaction-local setting on tx, in name
// order. Postgres only; any other driver with a non-empty map is an error so a
// missing RLS stamp fails closed.
func ApplyTxSettings(secCtx SecurityContext, tx common.Database, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}
	if tx == nil {
		return fmt.Errorf("tx settings: no transaction")
	}
	if drv := tx.DriverName(); drv != "postgres" && drv != "pgsql" {
		return fmt.Errorf("tx settings: unsupported driver %q", drv)
	}
	names := make([]string, 0, len(settings))
	for name := range settings {
		if !settingNameRE.MatchString(name) {
			return fmt.Errorf("tx settings: invalid setting name %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// The value is hex-encoded so it needs no quoting and cannot be read as a
		// bind placeholder by any adapter.
		query := fmt.Sprintf("SELECT set_config('%s', convert_from(decode('%s', 'hex'), 'UTF8'), true)",
			name, hex.EncodeToString([]byte(settings[name])))
		if _, err := tx.Exec(secCtx.GetContext(), query); err != nil {
			return fmt.Errorf("tx settings: set %s: %w", name, err)
		}
	}
	return nil
}
