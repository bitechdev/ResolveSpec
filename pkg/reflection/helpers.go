package reflection

import "reflect"

func Len(v any) int {
	val := reflect.ValueOf(v)
	valKind := val.Kind()

	if valKind == reflect.Ptr {
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String, reflect.Chan:
		return val.Len()
	default:
		return 0
	}
}

// ExtractTableNameOnly extracts the table name from a fully qualified table reference.
// It removes any schema prefix (e.g., "schema.table" -> "table") and truncates at
// the first delimiter (comma, space, tab, or newline). If the input contains multiple
// dots, it returns everything after the last dot up to the first delimiter.
func ExtractTableNameOnly(fullName string) string {
	// First, split by dot to remove schema prefix if present
	lastDotIndex := -1
	for i, char := range fullName {
		if char == '.' {
			lastDotIndex = i
		}
	}

	// Start from after the last dot (or from beginning if no dot)
	startIndex := 0
	if lastDotIndex != -1 {
		startIndex = lastDotIndex + 1
	}

	// Now find the end (first delimiter after the table name)
	for i := startIndex; i < len(fullName); i++ {
		char := rune(fullName[i])
		if char == ',' || char == ' ' || char == '\t' || char == '\n' {
			return fullName[startIndex:i]
		}
	}

	return fullName[startIndex:]
}

// GetPointerElement returns the element type if the provided reflect.Type is a pointer.
// If the type is a slice of pointers, it returns the element type of the pointer within the slice.
// If neither condition is met, it returns the original type.
func GetPointerElement(v reflect.Type) reflect.Type {
	if v.Kind() == reflect.Ptr {
		return v.Elem()
	}
	if v.Kind() == reflect.Slice && v.Elem().Kind() == reflect.Ptr {
		subElem := v.Elem()
		if subElem.Elem().Kind() == reflect.Ptr {
			return subElem.Elem().Elem()
		}
		return v.Elem()
	}
	return v
}
