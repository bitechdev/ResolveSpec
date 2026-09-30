package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

type preloadChild struct {
	ID     int `db:"id"`
	UserID int `db:"user_id"`
}

func (preloadChild) TableName() string { return "children" }

type preloadParent struct {
	ID       int            `db:"id"`
	Children []preloadChild `bun:"rel:has-many,join:ID=user_id"`
}

func (preloadParent) TableName() string { return "parents" }

func TestSubqueryPreloadErrorIsReturned(t *testing.T) {
	for name, dest := range map[string]interface{}{
		"slice":  &[]preloadParent{},
		"single": &preloadParent{},
	} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			mock.ExpectBegin()
			mock.ExpectQuery(`FROM parents`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery(`FROM children`).WillReturnError(errors.New("boom"))
			mock.ExpectRollback()

			err = NewPgSQLAdapter(db).RunInTransaction(context.Background(), func(tx common.Database) error {
				return tx.NewSelect().Model(&preloadParent{}).PreloadRelation("Children").Scan(context.Background(), dest)
			})
			if err == nil || !strings.Contains(err.Error(), "preload Children") {
				t.Fatalf("expected the preload error, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
