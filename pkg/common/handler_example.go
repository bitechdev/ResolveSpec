package common

// Example showing how to use the common handler interfaces
// This file demonstrates the handler interface hierarchy and usage patterns

// ProcessWithAnyHandler demonstrates using the base SpecHandler interface
// which works with any handler type (resolvespec, restheadspec, or funcspec)
func ProcessWithAnyHandler(handler SpecHandler) Database {
	// All handlers expose GetDatabase() through the SpecHandler interface
	return handler.GetDatabase()
}

// ProcessCRUDRequest demonstrates using the CRUDHandler interface
// which works with resolvespec.Handler and restheadspec.Handler
func ProcessCRUDRequest(handler CRUDHandler, w ResponseWriter, r Request, params map[string]string) {
	// Both resolvespec and restheadspec handlers implement Handle()
	handler.Handle(w, r, params)
}

// ProcessMetadataRequest demonstrates getting metadata from CRUD handlers
func ProcessMetadataRequest(handler CRUDHandler, w ResponseWriter, r Request, params map[string]string) {
	// Both resolvespec and restheadspec handlers implement HandleGet()
	handler.HandleGet(w, r, params)
}

// Example usage patterns (not executable, just for documentation):
/*
// Example 1: Using with resolvespec.Handler
func ExampleResolveSpec() {
	db := // ... get database
	registry := // ... get registry

	handler := resolvespec.NewHandler(db, registry)

	// Can be used as SpecHandler
	var specHandler SpecHandler = handler
	database := specHandler.GetDatabase()

	// Can be used as CRUDHandler
	var crudHandler CRUDHandler = handler
	crudHandler.Handle(w, r, params)
	crudHandler.HandleGet(w, r, params)
}

// Example 2: Using with restheadspec.Handler
func ExampleRestHeadSpec() {
	db := // ... get database
	registry := // ... get registry

	handler := restheadspec.NewHandler(db, registry)

	// Can be used as SpecHandler
	var specHandler SpecHandler = handler
	database := specHandler.GetDatabase()

	// Can be used as CRUDHandler
	var crudHandler CRUDHandler = handler
	crudHandler.Handle(w, r, params)
	crudHandler.HandleGet(w, r, params)
}

// Example 3: Using with funcspec.Handler
func ExampleFuncSpec() {
	db := // ... get database

	handler := funcspec.NewHandler(db)

	// Can be used as SpecHandler
	var specHandler SpecHandler = handler
	database := specHandler.GetDatabase()

	// Can be used as QueryHandler
	var queryHandler QueryHandler = handler
	// funcspec has different methods: SqlQueryList() and SqlQuery()
	// which return HTTP handler functions
}

// Example 4: Polymorphic handler processing
func ProcessHandlers(handlers []SpecHandler) {
	for _, handler := range handlers {
		// All handlers expose the database
		db := handler.GetDatabase()

		// Type switch for specific handler types
		switch h := handler.(type) {
		case CRUDHandler:
			// This is resolvespec or restheadspec
			// Can call Handle() and HandleGet()
			_ = h
		case QueryHandler:
			// This is funcspec
			// Can call SqlQueryList() and SqlQuery()
			_ = h
		}
	}
}
*/
