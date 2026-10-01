package resolvemcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func ptr[T any](v T) *T { return &v }

func TestConfigDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.DefaultLimit != 50 || c.MaxLimit != 1000 || c.MaxOffset != 100000 || c.MaxBatch != 100 ||
		c.MaxPreloadDepth != 2 || c.MaxWriteRows != 100 || c.QueryTimeout != 30*time.Second || c.ConfirmTTL != 5*time.Minute {
		t.Errorf("defaults: %+v", c)
	}
	if c := (Config{DefaultLimit: 5000, MaxLimit: 200}).withDefaults(); c.DefaultLimit != 200 {
		t.Errorf("default limit must not exceed max: %d", c.DefaultLimit)
	}
}

func TestCheckReadLimits(t *testing.T) {
	h, _, _ := newTxHarness(t)
	cases := []struct {
		name    string
		in      common.RequestOptions
		want    int
		wantErr string
	}{
		{"no limit takes default", common.RequestOptions{}, 50, ""},
		{"zero takes default", common.RequestOptions{Limit: ptr(0)}, 50, ""},
		{"negative takes default", common.RequestOptions{Limit: ptr(-3)}, 50, ""},
		{"explicit kept", common.RequestOptions{Limit: ptr(10)}, 10, ""},
		{"clamped", common.RequestOptions{Limit: ptr(1 << 30)}, 1000, ""},
		{"offset too big", common.RequestOptions{Offset: ptr(100001)}, 0, CodeLimitExceeded},
		{"offset negative", common.RequestOptions{Offset: ptr(-1)}, 0, CodeInvalidArgument},
		{"offset at max ok", common.RequestOptions{Offset: ptr(100000)}, 50, ""},
	}
	for _, c := range cases {
		opts := c.in
		err := h.checkReadLimits(&opts)
		if c.wantErr != "" {
			var ce *ClientError
			if !errors.As(err, &ce) || ce.Code != c.wantErr {
				t.Errorf("%s: got %v, want code %s", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || *opts.Limit != c.want {
			t.Errorf("%s: limit %v err %v, want %d", c.name, opts.Limit, err, c.want)
		}
	}
}

func TestValidatePreloads(t *testing.T) {
	type child struct {
		ID int `json:"id" bun:"id,pk"`
	}
	type parent struct {
		ID       int      `json:"id" bun:"id,pk"`
		Children []*child `json:"children" bun:"rel:has-many"`
	}
	h, _, _ := newTxHarness(t)
	ok := func(rel string) bool {
		return h.validatePreloads(&parent{}, []common.PreloadOption{{Relation: rel}}) == nil
	}
	if !ok("children") || !ok("Children") || !ok("children.sub") {
		t.Error("known relations (and depth 2) must pass")
	}
	for _, bad := range []string{"nope", "children.a.b", "children; DROP TABLE x", "", "a..b", "id"} {
		if ok(bad) {
			t.Errorf("preload %q must be rejected", bad)
		}
	}
}

func TestBatchCap(t *testing.T) {
	h, _, ctx := newTxHarness(t)
	h.config.MaxBatch = 2
	items := []interface{}{map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}}
	_, err := h.executeCreate(ctx, "public", "items", items)
	var ce *ClientError
	if !errors.As(err, &ce) || ce.Code != CodeLimitExceeded {
		t.Fatalf("want limit_exceeded, got %v", err)
	}
}

func TestReadSkipsCountUnlessRequested(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* LIMIT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a"))
	mock.ExpectCommit()
	if _, _, err := h.executeReadCounted(ctx, "public", "items", "", common.RequestOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClientFacingHidesInternals(t *testing.T) {
	code, msg := clientFacing("t", errors.New(`pq: relation "secret_table" does not exist`))
	if code != CodeInternal || strings.Contains(msg, "secret_table") || !strings.Contains(msg, "ref ") {
		t.Errorf("raw error leaked: %s %q", code, msg)
	}
	if code, msg := clientFacing("t", invalidArg("bad %s", "x")); code != CodeInvalidArgument || msg != "bad x" {
		t.Errorf("client error: %s %q", code, msg)
	}
	if code, _ := clientFacing("t", errRecordNotFound); code != CodeNotFound {
		t.Errorf("not found: %s", code)
	}
	wrapped := errors.Join(errors.New("ctx"), NewClientError(CodeForbidden, "update not allowed for x"))
	if code, msg := clientFacing("t", wrapped); code != CodeForbidden || msg != "update not allowed for x" {
		t.Errorf("wrapped: %s %q", code, msg)
	}
}

func TestHookPanicIsRecovered(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	h.Hooks().Register(BeforeDelete, func(*HookContext) error { panic("boom: secret") })
	mock.ExpectBegin()
	mock.ExpectRollback()
	_, err := h.executeDelete(ctx, "public", "items", "7")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, msg := clientFacing("t", err); strings.Contains(msg, "boom") || strings.Contains(msg, "secret") {
		t.Errorf("panic value leaked: %q", msg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestToolTimeoutBoundsCall(t *testing.T) {
	h, _, _ := newTxHarness(t)
	h.config.QueryTimeout = 20 * time.Millisecond
	ctx, cancel := h.callContext(context.Background())
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("call context did not time out")
	}
}
