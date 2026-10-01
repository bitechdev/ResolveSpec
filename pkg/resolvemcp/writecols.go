package resolvemcp

import (
	"reflect"
	"sort"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// maxKeyEcho caps how much of a rejected client key is echoed back in an error.
const maxKeyEcho = 64

// writeColumns validates the keys of a create/update payload against the model and returns the
// values keyed by database column name. A key may be the json name or the column name of a
// writable field (case-insensitive); relations, scan-only and unexported fields are not
// writable. Unknown keys are rejected rather than dropped, so a client learns that a write
// did not take effect, and no client-chosen identifier reaches SQL.
func writeColumns(model interface{}, data map[string]interface{}) (map[string]interface{}, error) {
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice) {
		modelType = modelType.Elem()
	}
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return nil, errInternal
	}

	accepted := make(map[string]string)
	for jsonKey, col := range reflection.BuildJSONToDBColumnMap(modelType) {
		accepted[strings.ToLower(jsonKey)] = col
		accepted[strings.ToLower(col)] = col
	}

	out := make(map[string]interface{}, len(data))
	var unknown []string
	for key, value := range data {
		col, ok := accepted[strings.ToLower(key)]
		if !ok {
			if len(key) > maxKeyEcho {
				key = key[:maxKeyEcho] + "..."
			}
			unknown = append(unknown, key)
			continue
		}
		if _, dup := out[col]; dup {
			return nil, invalidArg("column %q given more than once", col)
		}
		out[col] = value
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, invalidArg("unknown or read-only fields: %s", strings.Join(unknown, ", "))
	}
	return out, nil
}
