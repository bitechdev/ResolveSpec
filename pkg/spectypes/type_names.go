package spectypes

import (
	"reflect"
	"strings"
)

// pkgPath is the import path of this package, used to recognise spectypes
// wrappers by reflection.
const pkgPath = "github.com/bitechdev/ResolveSpec/pkg/spectypes"

// canonicalSQLNames maps a spectypes wrapper type name to the PostgreSQL type
// name it represents. Dimensioned types (vector(1536), geometry(Point,4326))
// still need a gorm/bun `type:` tag for the full declaration — this is the
// fallback used for metadata and OpenAPI when no tag is present.
var canonicalSQLNames = map[string]string{
	"SqlVector":       "vector",
	"SqlHalfVector":   "halfvec",
	"SqlSparseVector": "sparsevec",
	"SqlBitVector":    "bit",
	"SqlGeometry":     "geometry",
	"SqlGeography":    "geography",
	"SqlJSONB":        "jsonb",
	"SqlStringArray":  "text[]",
	"SqlInt16Array":   "smallint[]",
	"SqlInt32Array":   "integer[]",
	"SqlInt64Array":   "bigint[]",
	"SqlFloat32Array": "real[]",
	"SqlFloat64Array": "double precision[]",
	"SqlBoolArray":    "boolean[]",
	"SqlUUIDArray":    "uuid[]",
	"SqlDate":         "date",
	"SqlTime":         "time",
	"SqlTimeStamp":    "timestamp",
}

// sqlNullElemNames maps the element type of a SqlNull[T] alias to a PG type name.
var sqlNullElemNames = map[string]string{
	"int16":     "smallint",
	"int32":     "integer",
	"int64":     "bigint",
	"float64":   "double precision",
	"bool":      "boolean",
	"string":    "text",
	"[]uint8":   "bytea",
	"uuid.UUID": "uuid",
	"Time":      "timestamp",
}

// SQLTypeName returns the canonical PostgreSQL type name for a spectypes wrapper
// type, or ("", false) if t is not a recognised spectypes type.
func SQLTypeName(t reflect.Type) (string, bool) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.PkgPath() != pkgPath {
		return "", false
	}

	name := t.Name()
	if n, ok := canonicalSQLNames[name]; ok {
		return n, true
	}

	// SqlNull[T] aliases, e.g. "SqlNull[int16]", "SqlNull[uuid.UUID]".
	if strings.HasPrefix(name, "SqlNull[") && strings.HasSuffix(name, "]") {
		elem := name[len("SqlNull[") : len(name)-1]
		if idx := strings.LastIndex(elem, "."); idx >= 0 {
			// keep last path segment, e.g. "github.com/google/uuid.UUID" -> "uuid.UUID"
			if slash := strings.LastIndex(elem[:idx], "/"); slash >= 0 {
				elem = elem[slash+1:]
			}
		}
		if n, ok := sqlNullElemNames[elem]; ok {
			return n, true
		}
		return "", false
	}

	return "", false
}

// IsSpatialType reports whether t is a PostGIS geometry/geography wrapper.
func IsSpatialType(t reflect.Type) bool {
	n, ok := SQLTypeName(t)
	return ok && (n == "geometry" || n == "geography")
}

// IsVectorType reports whether t is a pgvector wrapper (vector/halfvec/sparsevec).
func IsVectorType(t reflect.Type) bool {
	n, ok := SQLTypeName(t)
	return ok && (n == "vector" || n == "halfvec" || n == "sparsevec")
}
