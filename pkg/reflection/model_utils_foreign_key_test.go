package reflection

import (
	"reflect"
	"testing"
)

// --- local test models ---

type fkDept struct{}

// bunEmployee uses bun join: tag to declare the FK column explicitly.
type bunEmployee struct {
	DeptID     string   `bun:"dept_id"                          json:"dept_id"`
	Department *fkDept  `bun:"rel:belongs-to,join:dept_id=id"   json:"department"`
}

// bunCompositeEmployee has a composite bun join: (two join: parts).
type bunCompositeEmployee struct {
	DeptID   string  `bun:"dept_id"   json:"dept_id"`
	TenantID string  `bun:"tenant_id" json:"tenant_id"`
	Department *fkDept `bun:"rel:belongs-to,join:dept_id=id,join:tenant_id=id" json:"department"`
}

// gormEmployee uses gorm foreignKey: tag (mirrors testmodels.Employee).
type gormEmployee struct {
	DepartmentID string  `json:"department_id"`
	ManagerID    string  `json:"manager_id"`
	Department   *fkDept `gorm:"foreignKey:DepartmentID;references:ID" json:"department"`
	Manager      *fkDept `gorm:"foreignKey:ManagerID;references:ID"    json:"manager"`
}

// gormCompositeEmployee has a composite GORM foreignKey.
type gormCompositeEmployee struct {
	DeptID   string  `json:"dept_id"`
	TenantID string  `json:"tenant_id"`
	Department *fkDept `gorm:"foreignKey:DeptID,TenantID" json:"department"`
}

// selfRefItem mimics a self-referential model (like mastertaskitem) where the
// parent PK column appears as the left side of a has-many join tag.
type selfRefItem struct {
	RidItem       int32        `json:"rid_item" bun:"rid_item,type:integer,pk"`
	RidParentItem int32        `json:"rid_parentitem" bun:"rid_parentitem,type:integer"`
	// has-one (single parent pointer)
	Parent   *selfRefItem   `json:"Parent,omitempty" bun:"rel:has-one,join:rid_item=rid_parentitem"`
	// has-many (child collection) — same join, duplicate right-side must be deduped
	Children []*selfRefItem `json:"Children,omitempty" bun:"rel:has-many,join:rid_item=rid_parentitem"`
}

// conventionEmployee has no explicit FK tag — relies on naming convention.
type conventionEmployee struct {
	DepartmentID string  `json:"department_id"`
	Department   *fkDept `json:"department"`
}

// noTagEmployee has a relation field with no FK tag and no convention match.
type noTagEmployee struct {
	Unrelated *fkDept `json:"unrelated"`
}

func TestGetForeignKeyColumn(t *testing.T) {
	tests := []struct {
		name      string
		modelType reflect.Type
		parentKey string
		want      []string
	}{
		// Bun join: tag
		{
			name:      "bun join tag returns local column",
			modelType: reflect.TypeOf(bunEmployee{}),
			parentKey: "department",
			want:      []string{"dept_id"},
		},
		{
			name:      "bun join tag matched via json tag (case-insensitive)",
			modelType: reflect.TypeOf(bunEmployee{}),
			parentKey: "Department",
			want:      []string{"dept_id"},
		},
		{
			name:      "bun composite join returns all local columns",
			modelType: reflect.TypeOf(bunCompositeEmployee{}),
			parentKey: "department",
			want:      []string{"dept_id", "tenant_id"},
		},

		// GORM foreignKey: tag
		{
			name:      "gorm foreignKey resolves to column name",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "department",
			want:      []string{"department_id"},
		},
		{
			name:      "gorm foreignKey resolves second relation",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "manager",
			want:      []string{"manager_id"},
		},
		{
			name:      "gorm foreignKey matched case-insensitively",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "Department",
			want:      []string{"department_id"},
		},
		{
			name:      "gorm composite foreignKey returns all columns",
			modelType: reflect.TypeOf(gormCompositeEmployee{}),
			parentKey: "department",
			want:      []string{"dept_id", "tenant_id"},
		},

		// Join left-side scan (parentKey is a raw column name, not a relation field name)
		{
			name:      "self-referential: parent PK column returns child FK column",
			modelType: reflect.TypeOf(selfRefItem{}),
			parentKey: "rid_item",
			want:      []string{"rid_parentitem"},
		},

		// Pointer and slice unwrapping
		{
			name:      "pointer to struct is unwrapped",
			modelType: reflect.TypeOf(&gormEmployee{}),
			parentKey: "department",
			want:      []string{"department_id"},
		},
		{
			name:      "slice of struct is unwrapped",
			modelType: reflect.TypeOf([]gormEmployee{}),
			parentKey: "department",
			want:      []string{"department_id"},
		},

		// No tag — returns nil so caller can fall back to convention
		{
			name:      "relation with no FK tag returns nil",
			modelType: reflect.TypeOf(conventionEmployee{}),
			parentKey: "department",
			want:      nil,
		},

		// Unknown parent key
		{
			name:      "unknown parent key returns nil",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "nonexistent",
			want:      nil,
		},
		{
			name:      "non-struct type returns nil",
			modelType: reflect.TypeOf(""),
			parentKey: "department",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetForeignKeyColumn(tt.modelType, tt.parentKey)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetForeignKeyColumn(%v, %q) = %v, want %v", tt.modelType, tt.parentKey, got, tt.want)
			}
		})
	}
}
