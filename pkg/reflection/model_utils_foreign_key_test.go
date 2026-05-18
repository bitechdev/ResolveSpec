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

// gormEmployee uses gorm foreignKey: tag (mirrors testmodels.Employee).
type gormEmployee struct {
	DepartmentID string  `json:"department_id"`
	ManagerID    string  `json:"manager_id"`
	Department   *fkDept `gorm:"foreignKey:DepartmentID;references:ID" json:"department"`
	Manager      *fkDept `gorm:"foreignKey:ManagerID;references:ID"    json:"manager"`
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
		want      string
	}{
		// Bun join: tag
		{
			name:      "bun join tag returns local column",
			modelType: reflect.TypeOf(bunEmployee{}),
			parentKey: "department",
			want:      "dept_id",
		},
		{
			name:      "bun join tag matched via json tag (case-insensitive)",
			modelType: reflect.TypeOf(bunEmployee{}),
			parentKey: "Department",
			want:      "dept_id",
		},

		// GORM foreignKey: tag
		{
			name:      "gorm foreignKey resolves to column name",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "department",
			want:      "department_id",
		},
		{
			name:      "gorm foreignKey resolves second relation",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "manager",
			want:      "manager_id",
		},
		{
			name:      "gorm foreignKey matched case-insensitively",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "Department",
			want:      "department_id",
		},

		// Pointer and slice unwrapping
		{
			name:      "pointer to struct is unwrapped",
			modelType: reflect.TypeOf(&gormEmployee{}),
			parentKey: "department",
			want:      "department_id",
		},
		{
			name:      "slice of struct is unwrapped",
			modelType: reflect.TypeOf([]gormEmployee{}),
			parentKey: "department",
			want:      "department_id",
		},

		// No tag — returns "" so caller can fall back to convention
		{
			name:      "relation with no FK tag returns empty string",
			modelType: reflect.TypeOf(conventionEmployee{}),
			parentKey: "department",
			want:      "",
		},

		// Unknown parent key
		{
			name:      "unknown parent key returns empty string",
			modelType: reflect.TypeOf(gormEmployee{}),
			parentKey: "nonexistent",
			want:      "",
		},
		{
			name:      "non-struct type returns empty string",
			modelType: reflect.TypeOf(""),
			parentKey: "department",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetForeignKeyColumn(tt.modelType, tt.parentKey)
			if got != tt.want {
				t.Errorf("GetForeignKeyColumn(%v, %q) = %q, want %q", tt.modelType, tt.parentKey, got, tt.want)
			}
		})
	}
}
