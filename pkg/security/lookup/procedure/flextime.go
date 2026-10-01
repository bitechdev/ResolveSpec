package procedure

import (
	"encoding/json"
	"strings"
	"time"
)

// normalizeTimes rewrites the zone-less timestamps the procedures emit (Postgres `timestamp`
// columns serialise as "2026-01-02T03:04:05.123456") as UTC RFC 3339, so the standard
// time.Time decoder accepts them. It handles a JSON object or an array of objects and only
// touches string fields whose name ends in "_at" or is "expiry". Anything else, including
// input that is not valid JSON, is returned unchanged.
func normalizeTimes(raw []byte) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	switch x := v.(type) {
	case map[string]any:
		fixTimes(x)
	case []any:
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				fixTimes(m)
			}
		}
	default:
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

func fixTimes(m map[string]any) {
	for k, v := range m {
		s, ok := v.(string)
		if !ok || (!strings.HasSuffix(k, "_at") && k != "expiry") {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
			continue
		}
		for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"} {
			if t, err := time.Parse(layout, s); err == nil {
				m[k] = t.UTC().Format(time.RFC3339Nano)
				break
			}
		}
	}
}
