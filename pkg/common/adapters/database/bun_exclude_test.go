package database

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// adhocBuffer mirrors the real-world DBAdhocBuffer: scanonly fields with both
// bun and gorm read-only tags.
type adhocBuffer struct {
	CQL1        string `json:"cql1,omitempty" gorm:"->" bun:",scanonly"`
	CQL2        string `json:"cql2,omitempty" gorm:"->" bun:",scanonly"`
	RowNumber   int64  `json:"_rownumber,omitempty" gorm:"-" bun:",scanonly"`
	RecordError string `json:"_error,omitempty" gorm:"-" bun:",scanonly"`
}

type excludeModel struct {
	bun.BaseModel `bun:"table:public.crmnote,alias:crmnote"`
	ID            int    `json:"id" bun:"id,pk"`
	Note          string `json:"note" bun:"note,type:citext,"`
	Norm          string `json:"norm" bun:"norm,generated"`

	adhocBuffer `json:",omitempty" bun:",scanonly"`
}

func newExcludeDB() *bun.DB {
	return bun.NewDB(&sql.DB{}, pgdialect.New())
}

// TestBunExcludeColumnWithNonWritableColumns feeds the reflection output
// straight into the adapter, as the handlers do, for insert and update.
func TestBunExcludeColumnWithNonWritableColumns(t *testing.T) {
	db := newExcludeDB()
	m := &excludeModel{}
	cols := reflection.NonWritableColumns(m)
	if len(cols) == 0 {
		t.Fatal("expected non-writable columns")
	}

	ins := &BunInsertQuery{query: db.NewInsert().Model(m)}
	ins.ExcludeColumn(cols...)
	insSQL, err := ins.query.AppendQuery(db.QueryGen(), nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	upd := &BunUpdateQuery{query: db.NewUpdate().Model(m).Where("id = 1")}
	upd.ExcludeColumn(cols...)
	updSQL, err := upd.query.AppendQuery(db.QueryGen(), nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	for name, q := range map[string]string{"insert": string(insSQL), "update": string(updSQL)} {
		for _, bad := range []string{"cql1", "cql2", "_rownumber", "_error", "norm"} {
			if strings.Contains(q, `"`+bad+`"`) {
				t.Errorf("%s writes non-writable column %s: %s", name, bad, q)
			}
		}
		if !strings.Contains(q, `"note"`) {
			t.Errorf("%s dropped writable column note: %s", name, q)
		}
	}
}

func TestBunExcludeColumnIgnoresUnknownAndKeepsWritable(t *testing.T) {
	db := newExcludeDB()
	m := &excludeModel{}

	ins := &BunInsertQuery{query: db.NewInsert().Model(m)}
	ins.ExcludeColumn("does_not_exist", "note")
	q, err := ins.query.AppendQuery(db.QueryGen(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(q), `"note"`) {
		t.Errorf("writable column note should have been excluded: %s", q)
	}
}

func TestBunExcludeColumnOnlyNonWritable(t *testing.T) {
	db := newExcludeDB()
	ins := &BunInsertQuery{query: db.NewInsert().Model(&excludeModel{})}
	ins.ExcludeColumn("cql1") // everything filtered out: must not error or panic
	if _, err := ins.query.AppendQuery(db.QueryGen(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestBunExcludeColumnWithoutModel(t *testing.T) {
	db := newExcludeDB()
	ins := &BunInsertQuery{query: db.NewInsert()}
	ins.ExcludeColumn("cql1") // no model yet: must not panic
}
