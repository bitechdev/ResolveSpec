package resolvemcp

import "context"

type contextKey string

const (
	contextKeySchema    contextKey = "schema"
	contextKeyEntity    contextKey = "entity"
	contextKeyTableName contextKey = "tableName"
	contextKeyModel     contextKey = "model"
	contextKeyModelPtr  contextKey = "modelPtr"
)

func WithSchema(ctx context.Context, schema string) context.Context {
	return context.WithValue(ctx, contextKeySchema, schema)
}

func GetSchema(ctx context.Context) string {
	if v := ctx.Value(contextKeySchema); v != nil {
		return v.(string)
	}
	return ""
}

func WithEntity(ctx context.Context, entity string) context.Context {
	return context.WithValue(ctx, contextKeyEntity, entity)
}

func GetEntity(ctx context.Context) string {
	if v := ctx.Value(contextKeyEntity); v != nil {
		return v.(string)
	}
	return ""
}

func WithTableName(ctx context.Context, tableName string) context.Context {
	return context.WithValue(ctx, contextKeyTableName, tableName)
}

func GetTableName(ctx context.Context) string {
	if v := ctx.Value(contextKeyTableName); v != nil {
		return v.(string)
	}
	return ""
}

func WithModel(ctx context.Context, model interface{}) context.Context {
	return context.WithValue(ctx, contextKeyModel, model)
}

func GetModel(ctx context.Context) interface{} {
	return ctx.Value(contextKeyModel)
}

func WithModelPtr(ctx context.Context, modelPtr interface{}) context.Context {
	return context.WithValue(ctx, contextKeyModelPtr, modelPtr)
}

func GetModelPtr(ctx context.Context) interface{} {
	return ctx.Value(contextKeyModelPtr)
}

func withRequestData(ctx context.Context, schema, entity, tableName string, model, modelPtr interface{}) context.Context {
	ctx = WithSchema(ctx, schema)
	ctx = WithEntity(ctx, entity)
	ctx = WithTableName(ctx, tableName)
	ctx = WithModel(ctx, model)
	ctx = WithModelPtr(ctx, modelPtr)
	return ctx
}
