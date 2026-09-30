package mqttspec

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func newTxHook(handler *Handler, data interface{}) *HookContext {
	return &HookContext{
		Context:   context.Background(),
		TableName: "users",
		Model:     &TestUser{},
		ModelPtr:  &TestUser{},
		Schema:    "public",
		Entity:    "users",
		ID:        "1",
		Data:      data,
		Options:   &common.RequestOptions{},
		Metadata:  map[string]interface{}{},
		Tx:        handler.db,
	}
}

func TestHandler_OnTxBeginFiresFirstOnTransaction(t *testing.T) {
	handler, db := setupTestHandler(t)
	require.NoError(t, db.Create(&TestUser{ID: 1, Name: "a", Email: "a@example.com", Status: "active"}).Error)

	var order []string
	var txs []common.Database
	handler.hooks.Register(OnTxBegin, func(c *HookContext) error {
		order = append(order, "begin")
		txs = append(txs, c.Tx)
		return nil
	})
	handler.hooks.Register(BeforeDelete, func(c *HookContext) error {
		order = append(order, "before")
		return nil
	})

	handler.handleDelete(&Client{ID: "c1"}, &Message{ID: "m1"}, newTxHook(handler, nil))

	var count int64
	require.NoError(t, db.Model(&TestUser{}).Where("id = 1").Count(&count).Error)
	assert.Zero(t, count)
	require.Len(t, txs, 1)
	assert.NotEqual(t, handler.db, txs[0])
	assert.Equal(t, []string{"begin", "before"}, order)
}

func TestHandler_OnTxBeginErrorAbortsWithoutWriting(t *testing.T) {
	handler, db := setupTestHandler(t)
	require.NoError(t, db.Create(&TestUser{ID: 1, Name: "a", Email: "a@example.com", Status: "active"}).Error)
	handler.hooks.Register(OnTxBegin, func(c *HookContext) error { return errors.New("no user") })

	handler.handleDelete(&Client{ID: "c1"}, &Message{ID: "m1"}, newTxHook(handler, nil))

	var count int64
	require.NoError(t, db.Model(&TestUser{}).Where("id = 1").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestHandler_UpdateRunsAfterHookOnSecondTransaction(t *testing.T) {
	handler, db := setupTestHandler(t)
	require.NoError(t, db.Create(&TestUser{ID: 1, Name: "a", Email: "a@example.com", Status: "active"}).Error)

	var begins []common.Database
	var afterTx common.Database
	handler.hooks.Register(OnTxBegin, func(c *HookContext) error {
		begins = append(begins, c.Tx)
		return nil
	})
	handler.hooks.Register(AfterUpdate, func(c *HookContext) error {
		afterTx = c.Tx
		return nil
	})

	handler.handleUpdate(&Client{ID: "c1"}, &Message{ID: "m1"}, newTxHook(handler, map[string]interface{}{"name": "b"}))

	var got TestUser
	require.NoError(t, db.First(&got, 1).Error)
	assert.Equal(t, "b", got.Name)
	require.Len(t, begins, 2)
	assert.NotEqual(t, begins[0], begins[1])
	assert.Equal(t, begins[1], afterTx)
}
