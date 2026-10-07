// Package modelregistry is the shared catalogue of the Go model structs that the
// ResolveSpec front ends (resolvespec, restheadspec, websocketspec, mqttspec,
// resolvemcp, ...) expose as database entities.
//
// A registry maps a model name ("schema.entity") to a struct type and holds:
//   - ModelRules: which operations (read/create/update/delete, public or not)
//     are allowed, and whether security checks are disabled.
//   - ModelInfo: optional documentation (description, purpose, tags, per-column
//     descriptions) meant for humans and AI agents. It never affects queries or
//     permissions.
//
// Register models on a registry created with NewModelRegistry, or through the
// package-level functions that use the default registry:
//
//	reg := modelregistry.NewModelRegistry()
//	_ = reg.RegisterModelWithRules("public.users", User{}, modelregistry.DefaultModelRules())
//	reg.SetModelInfo("public.users", modelregistry.ModelInfo{
//		Description: "Application accounts",
//		Purpose:     "Look up who a person is; never store credentials here",
//		Columns:     map[string]string{"email": "Login address, unique"},
//	})
//
// Descriptions come from, in priority order:
//  1. ModelInfo set with SetModelInfo or loaded from an external JSON map with
//     LoadModelInfoFile (the map can be maintained outside the Go code).
//  2. The model's Describer (ModelDescription() string) for the description.
//  3. Struct tags, read per column by FieldComment: comment, note, desc or
//     description tags, then "comment:" inside the gorm or bun tag.
//
// Models must be non-pointer structs; pointers, slices and arrays of structs are
// unwrapped on registration. All registry methods are safe for concurrent use.
package modelregistry
