package lookup

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// settingNameRE matches a custom GUC name: two or more dot-separated identifiers.
var settingNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)

// ApplyTxSettings sets each entry as a transaction-local setting on tx, in name
// order. Postgres only; any other driver with a non-empty map is an error so a
// missing RLS stamp fails closed.
func ApplyTxSettings(ctx context.Context, tx common.Database, settings map[string]string) error {
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
		if _, err := tx.Exec(ctx, query); err != nil {
			return fmt.Errorf("tx settings: set %s: %w", name, err)
		}
	}
	return nil
}
