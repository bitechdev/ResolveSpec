package security

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// WriteDataContext is implemented by security contexts that expose the
// create/update payload of the operation in flight.
type WriteDataContext interface {
	GetData() interface{}
	SetData(interface{})
}

// isWriteOperation reports whether the operation writes columns.
func isWriteOperation(operation string) bool {
	return operation == "create" || operation == "update"
}

// cachedColumnRules returns the cached column rules for the user/table and
// whether they were loaded. It never calls the provider, so it is safe inside
// a transaction.
func (m *SecurityList) cachedColumnRules(userID int, schema, table string) ([]ColumnSecurity, bool) {
	m.ColumnSecurityMutex.RLock()
	defer m.ColumnSecurityMutex.RUnlock()
	rules, ok := m.ColumnSecurity[fmt.Sprintf("%s.%s@%d", schema, table, userID)]
	return rules, ok && rules != nil
}

// blockedWriteColumns returns the lower-cased top-level column names that a
// "hide" or "mask" rule removes from write payloads.
func blockedWriteColumns(rules []ColumnSecurity) map[string]struct{} {
	blocked := make(map[string]struct{})
	for i := range rules {
		r := &rules[i]
		if !strings.EqualFold(r.Accesstype, "hide") && !strings.EqualFold(r.Accesstype, "mask") {
			continue
		}
		if len(r.Path) != 1 {
			continue // nested paths address JSON sub-values, not columns
		}
		blocked[strings.ToLower(r.Path[0])] = struct{}{}
	}
	return blocked
}

// modelColumnAliases maps each lower-cased field/column/JSON name of the model
// to all of its lower-cased names, so a rule on "name" also blocks its column.
func modelColumnAliases(model interface{}) map[string][]string {
	aliases := make(map[string][]string)
	if model == nil {
		return aliases
	}
	v := reflect.ValueOf(model)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		if v.Kind() == reflect.Pointer && v.IsNil() {
			v = reflect.New(v.Type().Elem())
		}
		if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
			v = reflect.New(v.Type().Elem()).Elem()
			continue
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return aliases
	}
	for _, c := range reflection.GetModelColumnDetail(v) {
		names := []string{strings.ToLower(c.Name), strings.ToLower(c.SQLName)}
		for _, n := range names {
			if n != "" {
				aliases[n] = names
			}
		}
	}
	return aliases
}

// stripBlocked removes blocked keys from one payload map in place.
func stripBlocked(m map[string]interface{}, blocked map[string]struct{}, aliases map[string][]string) []string {
	var dropped []string
	for key := range m {
		lk := strings.ToLower(key)
		hit := false
		if _, ok := blocked[lk]; ok {
			hit = true
		} else {
			for _, a := range aliases[lk] {
				if _, ok := blocked[a]; ok {
					hit = true
					break
				}
			}
		}
		if hit {
			delete(m, key)
			dropped = append(dropped, key)
		}
	}
	return dropped
}

// stripPayload strips blocked keys from a map, []map or []interface{} payload.
func stripPayload(data interface{}, blocked map[string]struct{}, aliases map[string][]string) (dropped []string) {
	switch d := data.(type) {
	case map[string]interface{}:
		dropped = stripBlocked(d, blocked, aliases)
	case []map[string]interface{}:
		for _, m := range d {
			dropped = append(dropped, stripBlocked(m, blocked, aliases)...)
		}
	case []interface{}:
		for _, e := range d {
			dropped = append(dropped, stripPayload(e, blocked, aliases)...)
		}
	}
	return dropped
}

// ApplyWriteColumnSecurity removes columns the user may not see (column
// security "hide" or "mask") from the create/update payload in place, so a
// hidden or masked column can never be written. It only reads the rules cache
// (see PreloadSecurityRules) and never queries the provider, so it is safe
// inside the transaction. Models with security disabled are skipped. Without
// a loaded rule set for a known user it fails closed.
func ApplyWriteColumnSecurity(secCtx SecurityContext, securityList *SecurityList) error {
	userID, ok := secCtx.GetUserID()
	if !ok || securityList == nil || IsModelSecurityDisabled(secCtx) {
		return nil
	}
	dc, ok := secCtx.(WriteDataContext)
	if !ok {
		return fmt.Errorf("column security: write payload not accessible for %s.%s", secCtx.GetSchema(), secCtx.GetEntity())
	}
	data := dc.GetData()
	if data == nil {
		return nil
	}

	rules, loaded := securityList.cachedColumnRules(userID, secCtx.GetSchema(), secCtx.GetEntity())
	if !loaded {
		return fmt.Errorf("column security rules not loaded for %s.%s", secCtx.GetSchema(), secCtx.GetEntity())
	}
	blocked := blockedWriteColumns(rules)
	if len(blocked) == 0 {
		return nil
	}

	dropped := stripPayload(data, blocked, modelColumnAliases(secCtx.GetModel()))
	if len(dropped) > 0 {
		logger.Warn("Column security: dropped write to hidden/masked columns %v on %s.%s (user %d)",
			dropped, secCtx.GetSchema(), secCtx.GetEntity(), userID)
	}
	return nil
}
