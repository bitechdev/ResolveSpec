// Package resolvemcp exposes registered database models as Model Context Protocol (MCP)
// tools over HTTP (SSE and streamable HTTP), so an AI agent can discover, read and
// change data without a tool per table.
//
// # How an agent uses it
//
// The tool set is fixed and does not grow with the models:
//
//	list_tables      tables the caller may use, their description and allowed operations
//	describe_table   columns, types, keys, relations, writable fields, limits
//	select_table     read rows: filters, sort, columns, preloads, paging, cursors
//	insert_into_table / update_table / delete_from_table
//	                 writes; filter-based writes are previewed (dry_run) and need the
//	                 confirm_token from the preview
//	list_functions / call_function   registered stored functions
//	resolvespec_annotate             optional free-text notes (Config.EnableAnnotations)
//
// The same guide is sent to MCP clients as the server instructions.
//
// The server is read-only by default (Config.ReadOnly nil means on); set
// ReadOnly: resolvemcp.Bool(false) to enable the write tools.
//
// # Setting it up
//
//	handler := resolvemcp.NewHandlerWithGORM(db, resolvemcp.Config{BaseURL: "http://localhost:8080"})
//	handler.RegisterModel("public", "users", &User{})
//
//	r := mux.NewRouter()
//	resolvemcp.SetupMuxRoutes(r, handler, securityList) // requires an authenticated caller
//
// # Describing the API for agents
//
// Models are documented through the model registry (see package modelregistry):
// SetModelDescription / LoadModelDescriptions on the handler, a ModelDescription()
// method on the model, or comment tags on its fields (gorm/bun "comment:" or
// comment/note/desc tags). The descriptions show up in list_tables and
// describe_table.
//
// ExportCatalog writes the whole picture (guide, tools, limits, tables with columns,
// relations, operations and descriptions) to a JSON or Markdown file on disk, so
// agents and developers can learn the API without connecting:
//
//	handler.ExportCatalog("docs/mcp-catalog.md")
//
// # Security
//
// Routes must be mounted behind Guard(securityList); the *Unauthenticated setup
// functions exist only for use behind another trusted layer. Per-entity rules come from
// modelregistry.ModelRules, and BeforeHandle/AfterHandle hooks can veto or audit any call.
package resolvemcp
