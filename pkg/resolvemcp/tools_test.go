package resolvemcp

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

func TestBuildModelInfo_UnwrapsSQLTypes(t *testing.T) {
	type related struct {
		ID int64 `json:"id"`
	}
	type model struct {
		Name     spectypes.SqlString `gorm:"column:name" json:"name"`
		Metadata spectypes.SqlJSONB  `gorm:"column:metadata;type:jsonb" json:"metadata"`
		Related  related             `json:"related"`
	}

	info := buildModelInfo("public", "models", model{})
	columns := make(map[string]columnInfo, len(info.columns))
	for _, column := range info.columns {
		columns[column.jsonName] = column
	}

	if column, ok := columns["name"]; !ok || column.goType != "string" || !column.nullable {
		t.Errorf("expected name SQL wrapper column, got %+v", column)
	}
	if column, ok := columns["metadata"]; !ok || !column.nullable {
		t.Errorf("expected metadata SQL wrapper column, got %+v", column)
	}
	if len(info.relationNames) != 1 || info.relationNames[0] != "related" {
		t.Errorf("expected only related to be a relation, got %v", info.relationNames)
	}
}
