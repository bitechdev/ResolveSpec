package resolvespec

import (
	"context"
	"testing"
)

func TestContextOperations(t *testing.T) {
	ctx := context.Background()

	// Test Schema
	t.Run("WithSchema and GetSchema", func(t *testing.T) {
		ctx = WithSchema(ctx, "public")
		schema := GetSchema(ctx)
		if schema != "public" {
			t.Errorf("Expected schema 'public', got '%s'", schema)
		}
	})

	// Test Entity
	t.Run("WithEntity and GetEntity", func(t *testing.T) {
		ctx = WithEntity(ctx, "users")
		entity := GetEntity(ctx)
		if entity != "users" {
			t.Errorf("Expected entity 'users', got '%s'", entity)
		}
	})

	// Test TableName
	t.Run("WithTableName and GetTableName", func(t *testing.T) {
		ctx = WithTableName(ctx, "public.users")
		tableName := GetTableName(ctx)
		if tableName != "public.users" {
			t.Errorf("Expected tableName 'public.users', got '%s'", tableName)
		}
	})

	// Test Model
	t.Run("WithModel and GetModel", func(t *testing.T) {
		type TestModel struct {
			ID   int
			Name string
		}
		model := &TestModel{ID: 1, Name: "test"}
		ctx = WithModel(ctx, model)
		retrieved := GetModel(ctx)
		if retrieved == nil {
			t.Error("Expected model to be retrieved, got nil")
		}
		if retrievedModel, ok := retrieved.(*TestModel); ok {
			if retrievedModel.ID != 1 || retrievedModel.Name != "test" {
				t.Errorf("Expected model with ID=1 and Name='test', got ID=%d, Name='%s'", retrievedModel.ID, retrievedModel.Name)
			}
		} else {
			t.Error("Retrieved model is not of expected type")
		}
	})

	// Test ModelPtr
	t.Run("WithModelPtr and GetModelPtr", func(t *testing.T) {
		type TestModel struct {
			ID int
		}
		models := []*TestModel{}
		ctx = WithModelPtr(ctx, &models)
		retrieved := GetModelPtr(ctx)
		if retrieved == nil {
			t.Error("Expected modelPtr to be retrieved, got nil")
		}
	})

	// Test WithRequestData
	t.Run("WithRequestData", func(t *testing.T) {
		type TestModel struct {
			ID   int
			Name string
		}
		model := &TestModel{ID: 1, Name: "test"}
		modelPtr := &[]*TestModel{}

		ctx = WithRequestData(ctx, "test_schema", "test_entity", "test_schema.test_entity", model, modelPtr)

		if GetSchema(ctx) != "test_schema" {
			t.Errorf("Expected schema 'test_schema', got '%s'", GetSchema(ctx))
		}
		if GetEntity(ctx) != "test_entity" {
			t.Errorf("Expected entity 'test_entity', got '%s'", GetEntity(ctx))
		}
		if GetTableName(ctx) != "test_schema.test_entity" {
			t.Errorf("Expected tableName 'test_schema.test_entity', got '%s'", GetTableName(ctx))
		}
		if GetModel(ctx) == nil {
			t.Error("Expected model to be set")
		}
		if GetModelPtr(ctx) == nil {
			t.Error("Expected modelPtr to be set")
		}
	})
}

func TestEmptyContext(t *testing.T) {
	ctx := context.Background()

	t.Run("GetSchema with empty context", func(t *testing.T) {
		schema := GetSchema(ctx)
		if schema != "" {
			t.Errorf("Expected empty schema, got '%s'", schema)
		}
	})

	t.Run("GetEntity with empty context", func(t *testing.T) {
		entity := GetEntity(ctx)
		if entity != "" {
			t.Errorf("Expected empty entity, got '%s'", entity)
		}
	})

	t.Run("GetTableName with empty context", func(t *testing.T) {
		tableName := GetTableName(ctx)
		if tableName != "" {
			t.Errorf("Expected empty tableName, got '%s'", tableName)
		}
	})

	t.Run("GetModel with empty context", func(t *testing.T) {
		model := GetModel(ctx)
		if model != nil {
			t.Errorf("Expected nil model, got %v", model)
		}
	})

	t.Run("GetModelPtr with empty context", func(t *testing.T) {
		modelPtr := GetModelPtr(ctx)
		if modelPtr != nil {
			t.Errorf("Expected nil modelPtr, got %v", modelPtr)
		}
	})
}
