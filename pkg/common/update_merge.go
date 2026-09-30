package common

// MergeUpdateValues merges the incoming request values into the existing
// record map (in place) and returns it.
//
// Every key present in incoming overwrites the existing value, including empty
// strings and explicit nulls, so clients can clear a column by sending "" or
// null. Keys absent from incoming are left untouched.
//
// When disallowNulls is true, nil values are skipped and the existing value is
// kept. Empty strings are still applied.
func MergeUpdateValues(existing, incoming map[string]interface{}, disallowNulls bool) map[string]interface{} {
	if existing == nil {
		existing = make(map[string]interface{}, len(incoming))
	}
	for key, newValue := range incoming {
		if newValue == nil && disallowNulls {
			continue
		}
		existing[key] = newValue
	}
	return existing
}
