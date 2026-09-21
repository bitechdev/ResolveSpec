package spectypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// CIString is a string that stores, scans, and returns its value exactly as
// given (no case normalization), but compares case-insensitively via Equal
// and EqualString. Use it as a bun model field type for columns (e.g.
// citext, or codes matched case-insensitively) where you want Go-side
// case-insensitive comparisons without forcing the stored/returned value to
// a particular case.
type CIString string

// Value implements driver.Valuer. The value is passed through unchanged.
func (s CIString) Value() (driver.Value, error) {
	return string(s), nil
}

// Scan implements sql.Scanner. The value is stored unchanged.
func (s *CIString) Scan(value any) error {
	switch v := value.(type) {
	case string:
		*s = CIString(v)
	case []byte:
		*s = CIString(v)
	case nil:
		*s = ""
	default:
		return fmt.Errorf("cannot scan %T into CIString", value)
	}
	return nil
}

// String implements fmt.Stringer.
func (s CIString) String() string { return string(s) }

// Equal reports whether s and other are equal, ignoring case.
func (s CIString) Equal(other CIString) bool {
	return strings.EqualFold(string(s), string(other))
}

// EqualString reports whether s equals other, ignoring case.
func (s CIString) EqualString(other string) bool {
	return strings.EqualFold(string(s), other)
}

// Compare returns -1, 0, or +1 if s is less than, equal to, or greater than
// other, ignoring case. Useful with slices.SortFunc or similar.
func (s CIString) Compare(other CIString) int {
	return strings.Compare(strings.ToLower(string(s)), strings.ToLower(string(other)))
}

// Less reports whether s sorts before other, ignoring case. Suitable for
// sort.Slice or slices.SortFunc comparisons.
func (s CIString) Less(other CIString) bool {
	return s.Compare(other) < 0
}

// LCString is a string that always stores, scans, and returns as lowercase.
// Use it as a bun model field type for columns that must be normalized to
// lowercase (e.g. codes, slugs, emails) rather than merely compared
// case-insensitively; see CIString if the original case must be preserved.
type LCString string

// Value implements driver.Valuer, always lowercase.
func (s LCString) Value() (driver.Value, error) {
	return strings.ToLower(string(s)), nil
}

// Scan implements sql.Scanner, always lowercase.
func (s *LCString) Scan(value any) error {
	switch v := value.(type) {
	case string:
		*s = LCString(strings.ToLower(v))
	case []byte:
		*s = LCString(strings.ToLower(string(v)))
	case nil:
		*s = ""
	default:
		return fmt.Errorf("cannot scan %T into LCString", value)
	}
	return nil
}

// String implements fmt.Stringer, always lowercase.
func (s LCString) String() string { return strings.ToLower(string(s)) }

// Equal reports whether s and other are equal (case-insensitively, since
// both normalize to lowercase).
func (s LCString) Equal(other LCString) bool {
	return s.String() == other.String()
}

// EqualString reports whether s equals other, ignoring case.
func (s LCString) EqualString(other string) bool {
	return s.String() == strings.ToLower(other)
}

// MarshalJSON implements json.Marshaler, always lowercase. Needed because
// encoding/json marshals a bare string-kind type as-is and does not call
// Value/String, so a value constructed directly (not scanned from the DB)
// would otherwise serialize with its original case.
func (s LCString) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ToLower(string(s)))
}

// UnmarshalJSON implements json.Unmarshaler, always lowercase.
func (s *LCString) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	*s = LCString(strings.ToLower(str))
	return nil
}

// UCString is a string that always stores, scans, and returns as uppercase.
// Use it as a bun model field type for columns that must be normalized to
// uppercase (e.g. table prefix codes) rather than merely compared
// case-insensitively; see CIString if the original case must be preserved.
type UCString string

// Value implements driver.Valuer, always uppercase.
func (s UCString) Value() (driver.Value, error) {
	return strings.ToUpper(string(s)), nil
}

// Scan implements sql.Scanner, always uppercase.
func (s *UCString) Scan(value any) error {
	switch v := value.(type) {
	case string:
		*s = UCString(strings.ToUpper(v))
	case []byte:
		*s = UCString(strings.ToUpper(string(v)))
	case nil:
		*s = ""
	default:
		return fmt.Errorf("cannot scan %T into UCString", value)
	}
	return nil
}

// String implements fmt.Stringer, always uppercase.
func (s UCString) String() string { return strings.ToUpper(string(s)) }

// Equal reports whether s and other are equal (case-insensitively, since
// both normalize to uppercase).
func (s UCString) Equal(other UCString) bool {
	return s.String() == other.String()
}

// EqualString reports whether s equals other, ignoring case.
func (s UCString) EqualString(other string) bool {
	return s.String() == strings.ToUpper(other)
}

// MarshalJSON implements json.Marshaler, always uppercase. Needed because
// encoding/json marshals a bare string-kind type as-is and does not call
// Value/String, so a value constructed directly (not scanned from the DB)
// would otherwise serialize with its original case.
func (s UCString) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ToUpper(string(s)))
}

// UnmarshalJSON implements json.Unmarshaler, always uppercase.
func (s *UCString) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	*s = UCString(strings.ToUpper(str))
	return nil
}
