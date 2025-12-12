package reflection

import (
	"reflect"
	"testing"
)

// Test models for GORM
type GormModelWithGetIDName struct {
	ID   int    `gorm:"column:rid_test;primaryKey" json:"id"`
	Name string `json:"name"`
}

func (m GormModelWithGetIDName) GetIDName() string {
	return "rid_test"
}

type GormModelWithColumnTag struct {
	ID   int    `gorm:"column:custom_id;primaryKey" json:"id"`
	Name string `json:"name"`
}

type GormModelWithJSONFallback struct {
	ID   int    `gorm:"primaryKey" json:"user_id"`
	Name string `json:"name"`
}

// Test models for Bun
type BunModelWithGetIDName struct {
	ID   int    `bun:"rid_test,pk" json:"id"`
	Name string `json:"name"`
}

func (m BunModelWithGetIDName) GetIDName() string {
	return "rid_test"
}

type BunModelWithColumnTag struct {
	ID   int    `bun:"custom_id,pk" json:"id"`
	Name string `json:"name"`
}

type BunModelWithJSONFallback struct {
	ID   int    `bun:",pk" json:"user_id"`
	Name string `json:"name"`
}

func TestGetPrimaryKeyName(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected string
	}{
		{
			name:     "GORM model with GetIDName method",
			model:    GormModelWithGetIDName{},
			expected: "rid_test",
		},
		{
			name:     "GORM model with column tag",
			model:    GormModelWithColumnTag{},
			expected: "custom_id",
		},
		{
			name:     "GORM model with JSON fallback",
			model:    GormModelWithJSONFallback{},
			expected: "user_id",
		},
		{
			name:     "GORM model pointer with GetIDName",
			model:    &GormModelWithGetIDName{},
			expected: "rid_test",
		},
		{
			name:     "GORM model pointer with column tag",
			model:    &GormModelWithColumnTag{},
			expected: "custom_id",
		},
		{
			name:     "Bun model with GetIDName method",
			model:    BunModelWithGetIDName{},
			expected: "rid_test",
		},
		{
			name:     "Bun model with column tag",
			model:    BunModelWithColumnTag{},
			expected: "custom_id",
		},
		{
			name:     "Bun model with JSON fallback",
			model:    BunModelWithJSONFallback{},
			expected: "user_id",
		},
		{
			name:     "Bun model pointer with GetIDName",
			model:    &BunModelWithGetIDName{},
			expected: "rid_test",
		},
		{
			name:     "Bun model pointer with column tag",
			model:    &BunModelWithColumnTag{},
			expected: "custom_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPrimaryKeyName(tt.model)
			if result != tt.expected {
				t.Errorf("GetPrimaryKeyName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractColumnFromGormTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected string
	}{
		{
			name:     "column tag with primaryKey",
			tag:      "column:rid_test;primaryKey",
			expected: "rid_test",
		},
		{
			name:     "column tag with spaces",
			tag:      "column:user_id ; primaryKey ; autoIncrement",
			expected: "user_id",
		},
		{
			name:     "no column tag",
			tag:      "primaryKey;autoIncrement",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractColumnFromGormTag(tt.tag)
			if result != tt.expected {
				t.Errorf("ExtractColumnFromGormTag() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractColumnFromBunTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected string
	}{
		{
			name:     "column name with pk flag",
			tag:      "rid_test,pk",
			expected: "rid_test",
		},
		{
			name:     "only pk flag",
			tag:      ",pk",
			expected: "",
		},
		{
			name:     "column with multiple flags",
			tag:      "user_id,pk,autoincrement",
			expected: "user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractColumnFromBunTag(tt.tag)
			if result != tt.expected {
				t.Errorf("ExtractColumnFromBunTag() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetModelColumns(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected []string
	}{
		{
			name:     "Bun model with multiple columns",
			model:    BunModelWithColumnTag{},
			expected: []string{"custom_id", "name"},
		},
		{
			name:     "GORM model with multiple columns",
			model:    GormModelWithColumnTag{},
			expected: []string{"custom_id", "name"},
		},
		{
			name:     "Bun model pointer",
			model:    &BunModelWithColumnTag{},
			expected: []string{"custom_id", "name"},
		},
		{
			name:     "GORM model pointer",
			model:    &GormModelWithColumnTag{},
			expected: []string{"custom_id", "name"},
		},
		{
			name:     "Bun model with JSON fallback",
			model:    BunModelWithJSONFallback{},
			expected: []string{"user_id", "name"},
		},
		{
			name:     "GORM model with JSON fallback",
			model:    GormModelWithJSONFallback{},
			expected: []string{"user_id", "name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetModelColumns(tt.model)
			if len(result) != len(tt.expected) {
				t.Errorf("GetModelColumns() returned %d columns, want %d", len(result), len(tt.expected))
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("GetModelColumns()[%d] = %v, want %v", i, col, tt.expected[i])
				}
			}
		})
	}
}

// Test models with embedded structs

type BaseModel struct {
	ID        int    `bun:"rid_base,pk" json:"id"`
	CreatedAt string `bun:"created_at" json:"created_at"`
}

type AdhocBuffer struct {
	CQL1      string `json:"cql1,omitempty" gorm:"->" bun:",scanonly"`
	CQL2      string `json:"cql2,omitempty" gorm:"->" bun:",scanonly"`
	RowNumber int64  `json:"_rownumber,omitempty" gorm:"-" bun:",scanonly"`
}

type ModelWithEmbedded struct {
	BaseModel
	Name        string `bun:"name" json:"name"`
	Description string `bun:"description" json:"description"`
	AdhocBuffer
}

type GormBaseModel struct {
	ID        int    `gorm:"column:rid_base;primaryKey" json:"id"`
	CreatedAt string `gorm:"column:created_at" json:"created_at"`
}

type GormAdhocBuffer struct {
	CQL1      string `json:"cql1,omitempty" gorm:"column:cql1;->" bun:",scanonly"`
	CQL2      string `json:"cql2,omitempty" gorm:"column:cql2;->" bun:",scanonly"`
	RowNumber int64  `json:"_rownumber,omitempty" gorm:"-" bun:",scanonly"`
}

type GormModelWithEmbedded struct {
	GormBaseModel
	Name        string `gorm:"column:name" json:"name"`
	Description string `gorm:"column:description" json:"description"`
	GormAdhocBuffer
}

func TestGetPrimaryKeyNameWithEmbedded(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected string
	}{
		{
			name:     "Bun model with embedded base",
			model:    ModelWithEmbedded{},
			expected: "rid_base",
		},
		{
			name:     "Bun model with embedded base (pointer)",
			model:    &ModelWithEmbedded{},
			expected: "rid_base",
		},
		{
			name:     "GORM model with embedded base",
			model:    GormModelWithEmbedded{},
			expected: "rid_base",
		},
		{
			name:     "GORM model with embedded base (pointer)",
			model:    &GormModelWithEmbedded{},
			expected: "rid_base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPrimaryKeyName(tt.model)
			if result != tt.expected {
				t.Errorf("GetPrimaryKeyName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetPrimaryKeyValueWithEmbedded(t *testing.T) {
	bunModel := ModelWithEmbedded{
		BaseModel: BaseModel{
			ID:        123,
			CreatedAt: "2024-01-01",
		},
		Name:        "Test",
		Description: "Test Description",
	}

	gormModel := GormModelWithEmbedded{
		GormBaseModel: GormBaseModel{
			ID:        456,
			CreatedAt: "2024-01-02",
		},
		Name:        "GORM Test",
		Description: "GORM Test Description",
	}

	tests := []struct {
		name     string
		model    any
		expected any
	}{
		{
			name:     "Bun model with embedded base",
			model:    bunModel,
			expected: 123,
		},
		{
			name:     "Bun model with embedded base (pointer)",
			model:    &bunModel,
			expected: 123,
		},
		{
			name:     "GORM model with embedded base",
			model:    gormModel,
			expected: 456,
		},
		{
			name:     "GORM model with embedded base (pointer)",
			model:    &gormModel,
			expected: 456,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPrimaryKeyValue(tt.model)
			if result != tt.expected {
				t.Errorf("GetPrimaryKeyValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetModelColumnsWithEmbedded(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected []string
	}{
		{
			name:     "Bun model with embedded structs",
			model:    ModelWithEmbedded{},
			expected: []string{"rid_base", "created_at", "name", "description", "cql1", "cql2", "_rownumber"},
		},
		{
			name:     "Bun model with embedded structs (pointer)",
			model:    &ModelWithEmbedded{},
			expected: []string{"rid_base", "created_at", "name", "description", "cql1", "cql2", "_rownumber"},
		},
		{
			name:     "GORM model with embedded structs",
			model:    GormModelWithEmbedded{},
			expected: []string{"rid_base", "created_at", "name", "description", "cql1", "cql2", "_rownumber"},
		},
		{
			name:     "GORM model with embedded structs (pointer)",
			model:    &GormModelWithEmbedded{},
			expected: []string{"rid_base", "created_at", "name", "description", "cql1", "cql2", "_rownumber"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetModelColumns(tt.model)
			if len(result) != len(tt.expected) {
				t.Errorf("GetModelColumns() returned %d columns, want %d. Got: %v", len(result), len(tt.expected), result)
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("GetModelColumns()[%d] = %v, want %v", i, col, tt.expected[i])
				}
			}
		})
	}
}

func TestIsColumnWritableWithEmbedded(t *testing.T) {
	tests := []struct {
		name       string
		model      any
		columnName string
		expected   bool
	}{
		{
			name:       "Bun model - writable column in main struct",
			model:      ModelWithEmbedded{},
			columnName: "name",
			expected:   true,
		},
		{
			name:       "Bun model - writable column in embedded base",
			model:      ModelWithEmbedded{},
			columnName: "rid_base",
			expected:   true,
		},
		{
			name:       "Bun model - scanonly column in embedded adhoc buffer",
			model:      ModelWithEmbedded{},
			columnName: "cql1",
			expected:   false,
		},
		{
			name:       "Bun model - scanonly column _rownumber",
			model:      ModelWithEmbedded{},
			columnName: "_rownumber",
			expected:   false,
		},
		{
			name:       "GORM model - writable column in main struct",
			model:      GormModelWithEmbedded{},
			columnName: "name",
			expected:   true,
		},
		{
			name:       "GORM model - writable column in embedded base",
			model:      GormModelWithEmbedded{},
			columnName: "rid_base",
			expected:   true,
		},
		{
			name:       "GORM model - readonly column in embedded adhoc buffer",
			model:      GormModelWithEmbedded{},
			columnName: "cql1",
			expected:   false,
		},
		{
			name:       "GORM model - readonly column _rownumber",
			model:      GormModelWithEmbedded{},
			columnName: "_rownumber",
			expected:   false, // bun:",scanonly" marks it as read-only, takes precedence over gorm:"-"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsColumnWritable(tt.model, tt.columnName)
			if result != tt.expected {
				t.Errorf("IsColumnWritable(%s) = %v, want %v", tt.columnName, result, tt.expected)
			}
		})
	}
}

// Test models with relations for GetSQLModelColumns
type User struct {
	ID          int       `bun:"id,pk" json:"id"`
	Name        string    `bun:"name" json:"name"`
	Email       string    `bun:"email" json:"email"`
	ProfileData string    `json:"profile_data"` // No bun/gorm tag
	Posts       []Post    `bun:"rel:has-many,join:id=user_id" json:"posts"`
	Profile     *Profile  `bun:"rel:has-one,join:id=user_id" json:"profile"`
	RowNumber   int64     `bun:",scanonly" json:"_rownumber"`
}

type Post struct {
	ID      int    `gorm:"column:id;primaryKey" json:"id"`
	Title   string `gorm:"column:title" json:"title"`
	UserID  int    `gorm:"column:user_id;foreignKey" json:"user_id"`
	User    *User  `gorm:"foreignKey:UserID;references:ID" json:"user"`
	Tags    []Tag  `gorm:"many2many:post_tags" json:"tags"`
	Content string `json:"content"` // No bun/gorm tag
}

type Profile struct {
	ID     int    `bun:"id,pk" json:"id"`
	Bio    string `bun:"bio" json:"bio"`
	UserID int    `bun:"user_id" json:"user_id"`
}

type Tag struct {
	ID   int    `gorm:"column:id;primaryKey" json:"id"`
	Name string `gorm:"column:name" json:"name"`
}

// Model with scan-only embedded struct
type EntityWithScanOnlyEmbedded struct {
	ID          int    `bun:"id,pk" json:"id"`
	Name        string `bun:"name" json:"name"`
	AdhocBuffer `bun:",scanonly"` // Entire embedded struct is scan-only
}

func TestGetSQLModelColumns(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected []string
	}{
		{
			name:  "Bun model with relations - excludes relations and non-SQL fields",
			model: User{},
			// Should include: id, name, email (has bun tags)
			// Should exclude: profile_data (no bun tag), Posts/Profile (relations), RowNumber (scan-only in embedded would be excluded)
			expected: []string{"id", "name", "email"},
		},
		{
			name:  "GORM model with relations - excludes relations and non-SQL fields",
			model: Post{},
			// Should include: id, title, user_id (has gorm tags)
			// Should exclude: content (no gorm tag), User/Tags (relations)
			expected: []string{"id", "title", "user_id"},
		},
		{
			name:  "Model with embedded base and scan-only embedded",
			model: EntityWithScanOnlyEmbedded{},
			// Should include: id, name from main struct
			// Should exclude: all fields from AdhocBuffer (scan-only embedded struct)
			expected: []string{"id", "name"},
		},
		{
			name:  "Model with embedded - includes SQL fields, excludes scan-only",
			model: ModelWithEmbedded{},
			// Should include: rid_base, created_at (from BaseModel), name, description (from main)
			// Should exclude: cql1, cql2, _rownumber (from AdhocBuffer - scan-only fields)
			expected: []string{"rid_base", "created_at", "name", "description"},
		},
		{
			name:  "GORM model with embedded - includes SQL fields, excludes scan-only",
			model: GormModelWithEmbedded{},
			// Should include: rid_base, created_at (from GormBaseModel), name, description (from main)
			// Should exclude: cql1, cql2 (scan-only), _rownumber (no gorm column tag, marked as -)
			expected: []string{"rid_base", "created_at", "name", "description"},
		},
		{
			name:  "Simple Profile model",
			model: Profile{},
			// Should include all fields with bun tags
			expected: []string{"id", "bio", "user_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSQLModelColumns(tt.model)
			if len(result) != len(tt.expected) {
				t.Errorf("GetSQLModelColumns() returned %d columns, want %d.\nGot: %v\nWant: %v",
					len(result), len(tt.expected), result, tt.expected)
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("GetSQLModelColumns()[%d] = %v, want %v.\nFull result: %v",
						i, col, tt.expected[i], result)
				}
			}
		})
	}
}

func TestGetSQLModelColumnsVsGetModelColumns(t *testing.T) {
	// Demonstrate the difference between GetModelColumns and GetSQLModelColumns
	user := User{}

	allColumns := GetModelColumns(user)
	sqlColumns := GetSQLModelColumns(user)

	t.Logf("GetModelColumns(User): %v", allColumns)
	t.Logf("GetSQLModelColumns(User): %v", sqlColumns)

	// GetModelColumns should return more columns (includes fields with only json tags)
	if len(allColumns) <= len(sqlColumns) {
		t.Errorf("Expected GetModelColumns to return more columns than GetSQLModelColumns")
	}

	// GetSQLModelColumns should not include 'profile_data' (no bun tag)
	for _, col := range sqlColumns {
		if col == "profile_data" {
			t.Errorf("GetSQLModelColumns should not include 'profile_data' (no bun/gorm tag)")
		}
	}

	// GetModelColumns should include 'profile_data' (has json tag)
	hasProfileData := false
	for _, col := range allColumns {
		if col == "profile_data" {
			hasProfileData = true
			break
		}
	}
	if !hasProfileData {
		t.Errorf("GetModelColumns should include 'profile_data' (has json tag)")
	}
}

// ============= Tests for helpers.go =============

func TestLen(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{
			name:     "slice of ints",
			input:    []int{1, 2, 3, 4, 5},
			expected: 5,
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: 0,
		},
		{
			name:     "array",
			input:    [3]int{1, 2, 3},
			expected: 3,
		},
		{
			name:     "string",
			input:    "hello",
			expected: 5,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "map",
			input:    map[string]int{"a": 1, "b": 2, "c": 3},
			expected: 3,
		},
		{
			name:     "empty map",
			input:    map[string]int{},
			expected: 0,
		},
		{
			name:     "pointer to slice",
			input:    &[]int{1, 2, 3},
			expected: 3,
		},
		{
			name:     "non-lennable type (int)",
			input:    42,
			expected: 0,
		},
		{
			name:     "non-lennable type (struct)",
			input:    struct{}{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Len(tt.input)
			if result != tt.expected {
				t.Errorf("Len() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractTableNameOnly(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple table name",
			input:    "users",
			expected: "users",
		},
		{
			name:     "schema.table",
			input:    "public.users",
			expected: "users",
		},
		{
			name:     "table with comma",
			input:    "users,",
			expected: "users",
		},
		{
			name:     "table with space",
			input:    "users WHERE",
			expected: "users",
		},
		{
			name:     "schema.table with space",
			input:    "public.users WHERE id = 1",
			expected: "users",
		},
		{
			name:     "schema.table with comma",
			input:    "myschema.mytable, other_table",
			expected: "mytable",
		},
		{
			name:     "table with tab",
			input:    "users\tJOIN",
			expected: "users",
		},
		{
			name:     "table with newline",
			input:    "users\nWHERE",
			expected: "users",
		},
		{
			name:     "multiple dots",
			input:    "db.schema.table WHERE",
			expected: "table",
		},
		{
			name:     "no delimiters",
			input:    "tablename",
			expected: "tablename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractTableNameOnly(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractTableNameOnly(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ============= Tests for utility functions =============

func TestExtractSourceColumn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "column with ->> operator",
			input:    "columna->>'val'",
			expected: "columna",
		},
		{
			name:     "column with -> operator",
			input:    "columna->'key'",
			expected: "columna",
		},
		{
			name:     "simple column",
			input:    "columna",
			expected: "columna",
		},
		{
			name:     "table.column with ->> operator",
			input:    "table.columna->>'val'",
			expected: "table.columna",
		},
		{
			name:     "table.column with -> operator",
			input:    "table.columna->'key'",
			expected: "table.columna",
		},
		{
			name:     "column with spaces before operator",
			input:    "columna  ->>'value'",
			expected: "columna",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSourceColumn(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractSourceColumn(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "CamelCase",
			input:    "CamelCase",
			expected: "camel_case",
		},
		{
			name:     "camelCase",
			input:    "camelCase",
			expected: "camel_case",
		},
		{
			name:     "UserID",
			input:    "UserID",
			expected: "user_i_d",
		},
		{
			name:     "HTTPServer",
			input:    "HTTPServer",
			expected: "h_t_t_p_server",
		},
		{
			name:     "lowercase",
			input:    "lowercase",
			expected: "lowercase",
		},
		{
			name:     "UPPERCASE",
			input:    "UPPERCASE",
			expected: "u_p_p_e_r_c_a_s_e",
		},
		{
			name:     "Single",
			input:    "A",
			expected: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToSnakeCase(tt.input)
			if result != tt.expected {
				t.Errorf("ToSnakeCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsNumericType(t *testing.T) {
	tests := []struct {
		name     string
		kind     reflect.Kind
		expected bool
	}{
		{"int", reflect.Int, true},
		{"int8", reflect.Int8, true},
		{"int16", reflect.Int16, true},
		{"int32", reflect.Int32, true},
		{"int64", reflect.Int64, true},
		{"uint", reflect.Uint, true},
		{"uint8", reflect.Uint8, true},
		{"uint16", reflect.Uint16, true},
		{"uint32", reflect.Uint32, true},
		{"uint64", reflect.Uint64, true},
		{"float32", reflect.Float32, true},
		{"float64", reflect.Float64, true},
		{"string", reflect.String, false},
		{"bool", reflect.Bool, false},
		{"struct", reflect.Struct, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNumericType(tt.kind)
			if result != tt.expected {
				t.Errorf("IsNumericType(%v) = %v, want %v", tt.kind, result, tt.expected)
			}
		})
	}
}

func TestIsStringType(t *testing.T) {
	tests := []struct {
		name     string
		kind     reflect.Kind
		expected bool
	}{
		{"string", reflect.String, true},
		{"int", reflect.Int, false},
		{"bool", reflect.Bool, false},
		{"struct", reflect.Struct, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsStringType(tt.kind)
			if result != tt.expected {
				t.Errorf("IsStringType(%v) = %v, want %v", tt.kind, result, tt.expected)
			}
		})
	}
}

func TestIsNumericValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"integer", "123", true},
		{"negative integer", "-456", true},
		{"float", "123.45", true},
		{"negative float", "-123.45", true},
		{"scientific notation", "1.23e10", true},
		{"with spaces", "  789  ", true},
		{"non-numeric", "abc", false},
		{"mixed", "123abc", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNumericValue(tt.value)
			if result != tt.expected {
				t.Errorf("IsNumericValue(%q) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestConvertToNumericType(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		kind        reflect.Kind
		expected    interface{}
		expectError bool
	}{
		// Integer types
		{"int", "123", reflect.Int, int(123), false},
		{"int8", "100", reflect.Int8, int8(100), false},
		{"int16", "1000", reflect.Int16, int16(1000), false},
		{"int32", "100000", reflect.Int32, int32(100000), false},
		{"int64", "9223372036854775807", reflect.Int64, int64(9223372036854775807), false},
		{"negative int", "-456", reflect.Int, int(-456), false},
		{"invalid int", "abc", reflect.Int, nil, true},

		// Unsigned integer types
		{"uint", "123", reflect.Uint, uint(123), false},
		{"uint8", "255", reflect.Uint8, uint8(255), false},
		{"uint16", "65535", reflect.Uint16, uint16(65535), false},
		{"uint32", "4294967295", reflect.Uint32, uint32(4294967295), false},
		{"uint64", "18446744073709551615", reflect.Uint64, uint64(18446744073709551615), false},
		{"invalid uint", "abc", reflect.Uint, nil, true},
		{"negative uint", "-1", reflect.Uint, nil, true},

		// Float types
		{"float32", "123.45", reflect.Float32, float32(123.45), false},
		{"float64", "123.456789", reflect.Float64, float64(123.456789), false},
		{"negative float", "-123.45", reflect.Float64, float64(-123.45), false},
		{"scientific notation", "1.23e10", reflect.Float64, float64(1.23e10), false},
		{"invalid float", "abc", reflect.Float32, nil, true},

		// Edge cases
		{"with spaces", "  789  ", reflect.Int, int(789), false},

		// Unsupported types
		{"unsupported type", "123", reflect.String, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertToNumericType(tt.value, tt.kind)
			if tt.expectError {
				if err == nil {
					t.Errorf("ConvertToNumericType(%q, %v) expected error, got nil", tt.value, tt.kind)
				}
				return
			}
			if err != nil {
				t.Errorf("ConvertToNumericType(%q, %v) unexpected error: %v", tt.value, tt.kind, err)
				return
			}
			if result != tt.expected {
				t.Errorf("ConvertToNumericType(%q, %v) = %v, want %v", tt.value, tt.kind, result, tt.expected)
			}
		})
	}
}

// Test model for GetColumnTypeFromModel
type TypeTestModel struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Age      int     `json:"age"`
	Balance  float64 `json:"balance"`
	Active   bool    `json:"active"`
	Metadata string  `json:"metadata"`
}

func TestGetColumnTypeFromModel(t *testing.T) {
	model := TypeTestModel{
		ID:       1,
		Name:     "Test",
		Age:      30,
		Balance:  100.50,
		Active:   true,
		Metadata: `{"key": "value"}`,
	}

	tests := []struct {
		name     string
		model    interface{}
		colName  string
		expected reflect.Kind
	}{
		{"int field", model, "id", reflect.Int},
		{"string field", model, "name", reflect.String},
		{"int field by name", model, "age", reflect.Int},
		{"float64 field", model, "balance", reflect.Float64},
		{"bool field", model, "active", reflect.Bool},
		{"string with JSON", model, "metadata", reflect.String},
		{"non-existent field", model, "nonexistent", reflect.Invalid},
		{"nil model", nil, "id", reflect.Invalid},
		{"pointer to model", &model, "name", reflect.String},
		{"column with JSON operator", model, "metadata->>'key'", reflect.String},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetColumnTypeFromModel(tt.model, tt.colName)
			if result != tt.expected {
				t.Errorf("GetColumnTypeFromModel(%v, %q) = %v, want %v", tt.model, tt.colName, result, tt.expected)
			}
		})
	}
}

// ============= Tests for relation functions =============

// Models for relation testing
type Author struct {
	ID    int     `bun:"id,pk" json:"id"`
	Name  string  `bun:"name" json:"name"`
	Books []Book  `bun:"rel:has-many,join:id=author_id" json:"books"`
}

type Book struct {
	ID         int        `bun:"id,pk" json:"id"`
	Title      string     `bun:"title" json:"title"`
	AuthorID   int        `bun:"author_id" json:"author_id"`
	Author     *Author    `bun:"rel:belongs-to,join:author_id=id" json:"author"`
	Publisher  *Publisher `bun:"rel:has-one,join:id=book_id" json:"publisher"`
}

type Publisher struct {
	ID     int    `bun:"id,pk" json:"id"`
	Name   string `bun:"name" json:"name"`
	BookID int    `bun:"book_id" json:"book_id"`
}

type Student struct {
	ID      int       `gorm:"column:id;primaryKey" json:"id"`
	Name    string    `gorm:"column:name" json:"name"`
	Courses []Course  `gorm:"many2many:student_courses" json:"courses"`
}

type Course struct {
	ID       int       `gorm:"column:id;primaryKey" json:"id"`
	Title    string    `gorm:"column:title" json:"title"`
	Students []Student `gorm:"many2many:student_courses" json:"students"`
}

// Recursive relation model
type Category struct {
	ID         int         `bun:"id,pk" json:"id"`
	Name       string      `bun:"name" json:"name"`
	ParentID   *int        `bun:"parent_id" json:"parent_id"`
	Parent     *Category   `bun:"rel:belongs-to,join:parent_id=id" json:"parent"`
	Children   []Category  `bun:"rel:has-many,join:id=parent_id" json:"children"`
}

func TestGetRelationType(t *testing.T) {
	tests := []struct {
		name      string
		model     interface{}
		fieldName string
		expected  RelationType
	}{
		// Bun relations
		{"has-many relation", Author{}, "Books", RelationHasMany},
		{"belongs-to relation", Book{}, "Author", RelationBelongsTo},
		{"has-one relation", Book{}, "Publisher", RelationHasOne},

		// GORM relations
		{"many-to-many relation (GORM)", Student{}, "Courses", RelationManyToMany},
		{"many-to-many reverse (GORM)", Course{}, "Students", RelationManyToMany},

		// Recursive relations
		{"recursive belongs-to", Category{}, "Parent", RelationBelongsTo},
		{"recursive has-many", Category{}, "Children", RelationHasMany},

		// Edge cases
		{"non-existent field", Author{}, "NonExistent", RelationUnknown},
		{"nil model", nil, "Books", RelationUnknown},
		{"empty field name", Author{}, "", RelationUnknown},
		{"pointer model", &Author{}, "Books", RelationHasMany},

		// Case-insensitive field names
		{"case-insensitive has-many", Author{}, "books", RelationHasMany},
		{"case-insensitive belongs-to", Book{}, "author", RelationBelongsTo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRelationType(tt.model, tt.fieldName)
			if result != tt.expected {
				t.Errorf("GetRelationType(%T, %q) = %v, want %v", tt.model, tt.fieldName, result, tt.expected)
			}
		})
	}
}

func TestShouldUseJoin(t *testing.T) {
	tests := []struct {
		name     string
		relType  RelationType
		expected bool
	}{
		{"belongs-to should use JOIN", RelationBelongsTo, true},
		{"has-one should use JOIN", RelationHasOne, true},
		{"has-many should NOT use JOIN", RelationHasMany, false},
		{"many-to-many should NOT use JOIN", RelationManyToMany, false},
		{"unknown should NOT use JOIN", RelationUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.relType.ShouldUseJoin()
			if result != tt.expected {
				t.Errorf("RelationType(%v).ShouldUseJoin() = %v, want %v", tt.relType, result, tt.expected)
			}
		})
	}
}

func TestGetRelationModel(t *testing.T) {
	tests := []struct {
		name      string
		model     interface{}
		fieldName string
		isNil     bool
	}{
		{"has-many relation", Author{}, "Books", false},
		{"belongs-to relation", Book{}, "Author", false},
		{"has-one relation", Book{}, "Publisher", false},
		{"many-to-many relation", Student{}, "Courses", false},

		// Recursive relations
		{"recursive belongs-to", Category{}, "Parent", false},
		{"recursive has-many", Category{}, "Children", false},

		// Nested/recursive field paths
		{"nested recursive", Category{}, "Parent.Parent", false},
		{"nested recursive children", Category{}, "Children", false},

		// Edge cases
		{"non-existent field", Author{}, "NonExistent", true},
		{"nil model", nil, "Books", true},
		{"empty field name", Author{}, "", true},
		{"pointer model", &Author{}, "Books", false},

		// Case-insensitive field names
		{"case-insensitive has-many", Author{}, "books", false},
		{"case-insensitive belongs-to", Book{}, "author", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRelationModel(tt.model, tt.fieldName)
			if tt.isNil {
				if result != nil {
					t.Errorf("GetRelationModel(%T, %q) = %v, want nil", tt.model, tt.fieldName, result)
				}
			} else {
				if result == nil {
					t.Errorf("GetRelationModel(%T, %q) = nil, want non-nil", tt.model, tt.fieldName)
				}
			}
		})
	}
}

// ============= Additional edge case tests for better coverage =============

func TestGetPrimaryKeyName_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected string
	}{
		{
			name:     "nil model",
			model:    nil,
			expected: "",
		},
		{
			name:     "string model name (not implemented yet)",
			model:    "SomeModel",
			expected: "",
		},
		{
			name:     "slice of models",
			model:    []BunModelWithColumnTag{},
			expected: "",
		},
		{
			name:     "array of models",
			model:    [3]BunModelWithColumnTag{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPrimaryKeyName(tt.model)
			if result != tt.expected {
				t.Errorf("GetPrimaryKeyName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetPrimaryKeyValue_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected any
	}{
		{
			name:     "nil model",
			model:    nil,
			expected: nil,
		},
		{
			name:     "non-struct type",
			model:    123,
			expected: nil,
		},
		{
			name:     "slice",
			model:    []int{1, 2, 3},
			expected: nil,
		},
		{
			name:     "model without primary key tags - fallback to ID field",
			model: struct {
				ID   int
				Name string
			}{ID: 99, Name: "Test"},
			expected: 99,
		},
		{
			name:     "model without ID field",
			model: struct {
				Name string
			}{Name: "Test"},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPrimaryKeyValue(tt.model)
			if result != tt.expected {
				t.Errorf("GetPrimaryKeyValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetModelColumns_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected []string
	}{
		{
			name:     "nil type",
			model:    nil,
			expected: []string{},
		},
		{
			name:     "non-struct type",
			model:    123,
			expected: []string{},
		},
		{
			name:     "slice type",
			model:    []BunModelWithColumnTag{},
			expected: []string{"custom_id", "name"},
		},
		{
			name:     "array type",
			model:    [3]BunModelWithColumnTag{},
			expected: []string{"custom_id", "name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetModelColumns(tt.model)
			if len(result) != len(tt.expected) {
				t.Errorf("GetModelColumns() returned %d columns, want %d", len(result), len(tt.expected))
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("GetModelColumns()[%d] = %v, want %v", i, col, tt.expected[i])
				}
			}
		})
	}
}

func TestIsColumnWritable_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		model      any
		columnName string
		expected   bool
	}{
		{
			name:       "nil model",
			model:      nil,
			columnName: "name",
			expected:   false,
		},
		{
			name:       "non-struct type",
			model:      123,
			columnName: "name",
			expected:   false,
		},
		{
			name:       "column not found in model (dynamic column)",
			model:      BunModelWithColumnTag{},
			columnName: "dynamic_column",
			expected:   true, // Not found, allow it (might be dynamic)
		},
		{
			name:       "pointer to model",
			model:      &ModelWithEmbedded{},
			columnName: "name",
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsColumnWritable(tt.model, tt.columnName)
			if result != tt.expected {
				t.Errorf("IsColumnWritable(%s) = %v, want %v", tt.columnName, result, tt.expected)
			}
		})
	}
}

func TestIsGormFieldReadOnly_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected bool
	}{
		{
			name:     "read-only marker",
			tag:      "column:name;->",
			expected: true,
		},
		{
			name:     "write restriction <-:false",
			tag:      "column:name;<-:false",
			expected: true,
		},
		{
			name:     "write allowed <-:create",
			tag:      "<-:create",
			expected: false,
		},
		{
			name:     "write allowed <-:update",
			tag:      "<-:update",
			expected: false,
		},
		{
			name:     "no restrictions",
			tag:      "column:name;type:varchar(255)",
			expected: false,
		},
		{
			name:     "empty tag",
			tag:      "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGormFieldReadOnly(tt.tag)
			if result != tt.expected {
				t.Errorf("isGormFieldReadOnly(%q) = %v, want %v", tt.tag, result, tt.expected)
			}
		})
	}
}

func TestGetSQLModelColumns_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		expected []string
	}{
		{
			name:     "nil model",
			model:    nil,
			expected: []string{},
		},
		{
			name:     "non-struct type",
			model:    123,
			expected: []string{},
		},
		{
			name:     "slice type",
			model:    []Profile{},
			expected: []string{"id", "bio", "user_id"},
		},
		{
			name:     "array type",
			model:    [2]Profile{},
			expected: []string{"id", "bio", "user_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSQLModelColumns(tt.model)
			if len(result) != len(tt.expected) {
				t.Errorf("GetSQLModelColumns() returned %d columns, want %d.\nGot: %v\nWant: %v",
					len(result), len(tt.expected), result, tt.expected)
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("GetSQLModelColumns()[%d] = %v, want %v.\nFull result: %v",
						i, col, tt.expected[i], result)
				}
			}
		})
	}
}

// Test models with table:, rel:, join: tags for ExtractColumnFromBunTag
type BunSpecialTagsModel struct {
	Table     string     `bun:"table:users"`
	Relation  []Post     `bun:"rel:has-many"`
	Join      string     `bun:"join:id=user_id"`
	NormalCol string     `bun:"normal_col"`
}

func TestExtractColumnFromBunTag_SpecialTags(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected string
	}{
		{
			name:     "table tag",
			tag:      "table:users",
			expected: "",
		},
		{
			name:     "rel tag",
			tag:      "rel:has-many",
			expected: "",
		},
		{
			name:     "join tag",
			tag:      "join:id=user_id",
			expected: "",
		},
		{
			name:     "normal column",
			tag:      "normal_col,pk",
			expected: "normal_col",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractColumnFromBunTag(tt.tag)
			if result != tt.expected {
				t.Errorf("ExtractColumnFromBunTag(%q) = %q, want %q", tt.tag, result, tt.expected)
			}
		})
	}
}

// Test GORM fallback scenarios
type GormFallbackModel struct {
	UserID int `gorm:"foreignKey:UserId"`
}

func TestGetRelationType_GORMFallback(t *testing.T) {
	tests := []struct {
		name      string
		model     interface{}
		fieldName string
		expected  RelationType
	}{
		{
			name:      "GORM slice without many2many",
			model:     Post{},
			fieldName: "Tags",
			expected:  RelationManyToMany, // Has many2many tag
		},
		{
			name:      "GORM pointer with foreignKey",
			model:     Post{},
			fieldName: "User",
			expected:  RelationBelongsTo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRelationType(tt.model, tt.fieldName)
			if result != tt.expected {
				t.Errorf("GetRelationType(%T, %q) = %v, want %v", tt.model, tt.fieldName, result, tt.expected)
			}
		})
	}
}

// Additional tests for better coverage of GetRelationType
func TestGetRelationType_AdditionalCases(t *testing.T) {
	// Test model with GORM has-one (pointer without foreignKey or with references)
	type Address struct {
		ID     int  `gorm:"column:id;primaryKey"`
		UserID int  `gorm:"column:user_id"`
	}

	type UserWithAddress struct {
		ID      int      `gorm:"column:id;primaryKey"`
		Address *Address `gorm:"references:UserID"` // has-one relation
	}

	// Test model with field type inference
	type Company struct {
		ID   int
		Name string
	}

	type Employee struct {
		ID        int
		Company   Company  // Single struct (not pointer, not slice) - belongs-to
		Coworkers []Employee // Slice without bun/gorm tags - has-many
	}

	tests := []struct {
		name      string
		model     interface{}
		fieldName string
		expected  RelationType
	}{
		{
			name:      "GORM has-one (pointer with references)",
			model:     UserWithAddress{},
			fieldName: "Address",
			expected:  RelationHasOne,
		},
		{
			name:      "Field type inference - single struct",
			model:     Employee{},
			fieldName: "Company",
			expected:  RelationBelongsTo,
		},
		{
			name:      "Field type inference - slice",
			model:     Employee{},
			fieldName: "Coworkers",
			expected:  RelationHasMany,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRelationType(tt.model, tt.fieldName)
			if result != tt.expected {
				t.Errorf("GetRelationType(%T, %q) = %v, want %v", tt.model, tt.fieldName, result, tt.expected)
			}
		})
	}
}

// Test for GetColumnTypeFromModel with more edge cases
func TestGetColumnTypeFromModel_AdditionalCases(t *testing.T) {
	type ModelWithSnakeCase struct {
		UserID   int    `json:"user_id"`
		UserName string // No tag, will match by snake_case conversion
	}

	model := ModelWithSnakeCase{
		UserID:   123,
		UserName: "John",
	}

	tests := []struct {
		name     string
		model    interface{}
		colName  string
		expected reflect.Kind
	}{
		{"field by snake_case name", model, "user_name", reflect.String},
		{"non-struct model", 123, "field", reflect.Invalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetColumnTypeFromModel(tt.model, tt.colName)
			if result != tt.expected {
				t.Errorf("GetColumnTypeFromModel(%v, %q) = %v, want %v", tt.model, tt.colName, result, tt.expected)
			}
		})
	}
}

// Test for getRelationModelSingleLevel edge cases
func TestGetRelationModel_WithTags(t *testing.T) {
	// Test matching by gorm column tag
	type Department struct {
		ID   int    `gorm:"column:dept_id;primaryKey"`
		Name string `gorm:"column:dept_name"`
	}

	type Manager struct {
		ID         int         `gorm:"column:id;primaryKey"`
		DeptID     int         `gorm:"column:department_id"`
		Department *Department `gorm:"column:dept;foreignKey:DeptID"`
	}

	tests := []struct {
		name      string
		model     interface{}
		fieldName string
		isNil     bool
	}{
		// Test matching by gorm column name
		{"match by gorm column", Manager{}, "dept", false},
		// Test matching by json tag
		{"match by json tag", Book{}, "author", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRelationModel(tt.model, tt.fieldName)
			if tt.isNil {
				if result != nil {
					t.Errorf("GetRelationModel(%T, %q) = %v, want nil", tt.model, tt.fieldName, result)
				}
			} else {
				if result == nil {
					t.Errorf("GetRelationModel(%T, %q) = nil, want non-nil", tt.model, tt.fieldName)
				}
			}
		})
	}
}

func TestMapToStruct(t *testing.T) {
	// Test model with various field types
	type TestModel struct {
		ID       int64   `bun:"id,pk" json:"id"`
		Name     string  `bun:"name" json:"name"`
		Age      int     `bun:"age" json:"age"`
		Active   bool    `bun:"active" json:"active"`
		Score    float64 `bun:"score" json:"score"`
		Data     []byte  `bun:"data" json:"data"`
		MetaJSON []byte  `bun:"meta_json" json:"meta_json"`
	}

	tests := []struct {
		name     string
		dataMap  map[string]interface{}
		expected TestModel
		wantErr  bool
	}{
		{
			name: "Basic types conversion",
			dataMap: map[string]interface{}{
				"id":     int64(123),
				"name":   "Test User",
				"age":    30,
				"active": true,
				"score":  95.5,
			},
			expected: TestModel{
				ID:     123,
				Name:   "Test User",
				Age:    30,
				Active: true,
				Score:  95.5,
			},
			wantErr: false,
		},
		{
			name: "Byte slice (SqlJSONB-like) from []byte",
			dataMap: map[string]interface{}{
				"id":   int64(456),
				"name": "JSON Test",
				"data": []byte(`{"key":"value"}`),
			},
			expected: TestModel{
				ID:   456,
				Name: "JSON Test",
				Data: []byte(`{"key":"value"}`),
			},
			wantErr: false,
		},
		{
			name: "Byte slice from string",
			dataMap: map[string]interface{}{
				"id":   int64(789),
				"data": "string data",
			},
			expected: TestModel{
				ID:   789,
				Data: []byte("string data"),
			},
			wantErr: false,
		},
		{
			name: "Byte slice from map (JSON marshal)",
			dataMap: map[string]interface{}{
				"id": int64(999),
				"meta_json": map[string]interface{}{
					"field1": "value1",
					"field2": 42,
				},
			},
			expected: TestModel{
				ID:       999,
				MetaJSON: []byte(`{"field1":"value1","field2":42}`),
			},
			wantErr: false,
		},
		{
			name: "Byte slice from slice (JSON marshal)",
			dataMap: map[string]interface{}{
				"id":        int64(111),
				"meta_json": []interface{}{"item1", "item2", 3},
			},
			expected: TestModel{
				ID:       111,
				MetaJSON: []byte(`["item1","item2",3]`),
			},
			wantErr: false,
		},
		{
			name: "Field matching by bun tag",
			dataMap: map[string]interface{}{
				"id":   int64(222),
				"name": "Tagged Field",
			},
			expected: TestModel{
				ID:   222,
				Name: "Tagged Field",
			},
			wantErr: false,
		},
		{
			name: "Nil values",
			dataMap: map[string]interface{}{
				"id":   int64(333),
				"data": nil,
			},
			expected: TestModel{
				ID:   333,
				Data: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result TestModel
			err := MapToStruct(tt.dataMap, &result)

			if (err != nil) != tt.wantErr {
				t.Errorf("MapToStruct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Compare fields individually for better error messages
			if result.ID != tt.expected.ID {
				t.Errorf("ID = %v, want %v", result.ID, tt.expected.ID)
			}
			if result.Name != tt.expected.Name {
				t.Errorf("Name = %v, want %v", result.Name, tt.expected.Name)
			}
			if result.Age != tt.expected.Age {
				t.Errorf("Age = %v, want %v", result.Age, tt.expected.Age)
			}
			if result.Active != tt.expected.Active {
				t.Errorf("Active = %v, want %v", result.Active, tt.expected.Active)
			}
			if result.Score != tt.expected.Score {
				t.Errorf("Score = %v, want %v", result.Score, tt.expected.Score)
			}

			// For byte slices, compare as strings for JSON data
			if tt.expected.Data != nil {
				if string(result.Data) != string(tt.expected.Data) {
					t.Errorf("Data = %s, want %s", string(result.Data), string(tt.expected.Data))
				}
			}
			if tt.expected.MetaJSON != nil {
				if string(result.MetaJSON) != string(tt.expected.MetaJSON) {
					t.Errorf("MetaJSON = %s, want %s", string(result.MetaJSON), string(tt.expected.MetaJSON))
				}
			}
		})
	}
}

func TestMapToStruct_Errors(t *testing.T) {
	type TestModel struct {
		ID int `bun:"id" json:"id"`
	}

	tests := []struct {
		name    string
		dataMap map[string]interface{}
		target  interface{}
		wantErr bool
	}{
		{
			name:    "Nil dataMap",
			dataMap: nil,
			target:  &TestModel{},
			wantErr: true,
		},
		{
			name:    "Nil target",
			dataMap: map[string]interface{}{"id": 1},
			target:  nil,
			wantErr: true,
		},
		{
			name:    "Non-pointer target",
			dataMap: map[string]interface{}{"id": 1},
			target:  TestModel{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MapToStruct(tt.dataMap, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapToStruct() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
