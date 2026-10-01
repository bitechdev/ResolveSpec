package direct

import (
	"context"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

func TestKeysLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	k := NewKeys(newTestBase(t, db, nil))
	_, _ = db.Exec(`INSERT INTO users (username,email,password,is_active) VALUES ('u','u@x.io','x',1)`)

	exp := time.Now().Add(time.Hour)
	created, err := k.Create(ctx, sectypes.CreateKeyRequest{
		UserID: 1, KeyType: sectypes.KeyTypeHeaderAPI, Name: "ci",
		Scopes: []string{"read", "write"}, Meta: map[string]any{"env": "prod"}, ExpiresAt: &exp,
	}, sectypes.HashKey("raw1"))
	if err != nil || created.ID == 0 {
		t.Fatalf("%+v %v", created, err)
	}
	// nil scopes/meta must not store the JSON text "null"
	if _, err := k.Create(ctx, sectypes.CreateKeyRequest{UserID: 1, KeyType: sectypes.KeyTypeJWTSecret, Name: "j"}, sectypes.HashKey("raw2")); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Create(ctx, sectypes.CreateKeyRequest{UserID: 1, KeyType: sectypes.KeyTypeGenericAPI, Name: "old", ExpiresAt: ptr(time.Now().Add(-time.Hour))}, sectypes.HashKey("raw3")); err != nil {
		t.Fatal(err)
	}

	all, err := k.List(ctx, 1, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("list all: %d %v", len(all), err)
	}
	one, _ := k.List(ctx, 1, sectypes.KeyTypeHeaderAPI)
	if len(one) != 1 || one[0].Name != "ci" || len(one[0].Scopes) != 2 || one[0].Meta["env"] != "prod" || one[0].ExpiresAt == nil {
		t.Fatalf("list typed: %+v", one)
	}
	if other, _ := k.List(ctx, 2, ""); len(other) != 0 {
		t.Fatal("other user's keys listed")
	}

	got, err := k.Validate(ctx, sectypes.HashKey("raw1"), sectypes.KeyTypeHeaderAPI)
	if err != nil || got.UserID != 1 || got.KeyHash != sectypes.HashKey("raw1") || got.LastUsedAt == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := k.Validate(ctx, sectypes.HashKey("raw1"), sectypes.KeyTypeGenericAPI); err == nil || err.Error() != "invalid or expired key" {
		t.Fatalf("wrong type: %v", err)
	}
	if _, err := k.Validate(ctx, sectypes.HashKey("raw3"), ""); err == nil {
		t.Fatal("expired key validated")
	}

	if _, err := k.Delete(ctx, 2, created.ID); err == nil || err.Error() != "key not found or already deleted" {
		t.Fatalf("foreign delete: %v", err)
	}
	hash, err := k.Delete(ctx, 1, created.ID)
	if err != nil || hash != sectypes.HashKey("raw1") {
		t.Fatalf("%q %v", hash, err)
	}
	if _, err := k.Delete(ctx, 1, created.ID); err == nil {
		t.Fatal("double delete succeeded")
	}
	if _, err := k.Validate(ctx, sectypes.HashKey("raw1"), ""); err == nil {
		t.Fatal("deleted key validated")
	}
}

func ptr[T any](v T) *T { return &v }
