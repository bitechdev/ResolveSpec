package funcspec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// Handler handles function-based SQL API requests
type Handler struct {
	db                common.Database
	hooks             *HookRegistry
	variablesCallback func(r *http.Request) map[string]interface{}
}

type SqlQueryOptions struct {
	NoCount     bool
	BlankParams bool
	AllowFilter bool
}

func NewSqlQueryOptions() SqlQueryOptions {
	return SqlQueryOptions{
		NoCount:     false,
		BlankParams: true,
		AllowFilter: true,
	}
}

// NewHandler creates a new function API handler
func NewHandler(db common.Database) *Handler {
	return &Handler{
		db:    db,
		hooks: NewHookRegistry(),
	}
}

// GetDatabase returns the underlying database connection
// Implements common.SpecHandler interface
func (h *Handler) GetDatabase() common.Database {
	return h.db
}

func (h *Handler) SetVariablesCallback(callback func(r *http.Request) map[string]interface{}) {
	h.variablesCallback = callback
}

func (h *Handler) GetVariablesCallback() func(r *http.Request) map[string]interface{} {
	return h.variablesCallback
}

// Hooks returns the hook registry for this handler
// Use this to register custom hooks for operations
func (h *Handler) Hooks() *HookRegistry {
	return h.hooks
}

// HTTPFuncType is a function type for HTTP handlers
type HTTPFuncType func(http.ResponseWriter, *http.Request)

// SqlQueryList creates an HTTP handler that executes a SQL query and returns a list with pagination
func (h *Handler) SqlQueryList(sqlquery string, options SqlQueryOptions) HTTPFuncType {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				logger.Error("Panic in SqlQueryList: %v\nStack trace:\n%s", err, string(stack))
				http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
			}
		}()

		// Create local copy to avoid modifying the captured parameter across requests
		sqlquery := sqlquery

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()

		var dbobjlist []map[string]interface{}
		var total int64
		propQry := make(map[string]string)
		inputvars := make([]string, 0)
		metainfo := make(map[string]interface{})
		variables := make(map[string]interface{})

		complexAPI := false

		// Get user context from security package
		userCtx, ok := security.GetUserContext(ctx)
		if !ok {
			logger.Warn("No user context found in request")
			userCtx = &security.UserContext{UserID: 0, UserName: "anonymous"}
		}

		w.Header().Set("Content-Type", "application/json")

		// Initialize hook context
		hookCtx := &HookContext{
			Context:     ctx,
			Handler:     h,
			Request:     r,
			Writer:      w,
			SQLQuery:    sqlquery,
			Variables:   variables,
			InputVars:   inputvars,
			MetaInfo:    metainfo,
			PropQry:     propQry,
			UserContext: userCtx,
			NoCount:     options.NoCount,
			BlankParams: options.BlankParams,
			AllowFilter: options.AllowFilter,
			ComplexAPI:  complexAPI,
		}

		// Execute BeforeQueryList hook
		if err := h.hooks.Execute(BeforeQueryList, hookCtx); err != nil {
			logger.Error("BeforeQueryList hook failed: %v", err)
			sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
			return
		}

		// Check if hook aborted the operation
		if hookCtx.Abort {
			if hookCtx.AbortCode == 0 {
				hookCtx.AbortCode = http.StatusBadRequest
			}
			sendError(w, hookCtx.AbortCode, "operation_aborted", hookCtx.AbortMessage, nil)
			return
		}

		// Use potentially modified SQL query and variables from hooks
		sqlquery = hookCtx.SQLQuery
		variables = hookCtx.Variables
		// complexAPI = hookCtx.ComplexAPI

		// Extract input variables from SQL query (placeholders like [variable])
		sqlquery = h.extractInputVariables(sqlquery, &inputvars)

		// Merge URL path parameters
		sqlquery = h.mergePathParams(r, sqlquery, variables)

		// Parse comprehensive parameters from headers and query string
		reqParams := h.ParseParameters(r)
		complexAPI = reqParams.ComplexAPI

		// Merge query string parameters
		sqlquery = h.mergeQueryParams(r, sqlquery, variables, options.AllowFilter, propQry)

		// Merge header parameters
		sqlquery = h.mergeHeaderParams(r, sqlquery, variables, propQry, &complexAPI)

		// Apply filters from parsed parameters (if not already applied by pAllowFilter)
		if !options.AllowFilter {
			sqlquery = h.ApplyFilters(sqlquery, reqParams)
		}

		// Apply field selection
		sqlquery = h.ApplyFieldSelection(sqlquery, reqParams)

		// Apply DISTINCT if requested
		sqlquery = h.ApplyDistinct(sqlquery, reqParams)

		// Override pNoCount if skipcount is specified
		if reqParams.SkipCount {
			options.NoCount = true
		}

		// Build metainfo
		metainfo["ipaddress"] = getIPAddress(r)
		metainfo["url"] = r.RequestURI
		metainfo["user"] = userCtx.UserName
		metainfo["rid_user"] = fmt.Sprintf("%d", userCtx.UserID)
		metainfo["method"] = r.Method
		metainfo["variables"] = variables

		// Replace meta variables in SQL
		sqlquery = h.replaceMetaVariables(sqlquery, r, userCtx, metainfo, variables)

		// Remove unused input variables
		if options.BlankParams {
			for _, kw := range inputvars {
				replacement := getReplacementForBlankParam(sqlquery, kw)
				sqlquery = strings.ReplaceAll(sqlquery, kw, replacement)
				logger.Debug("Replaced unused variable %s with: %s", kw, replacement)
			}
		}

		// Update hook context with latest SQL query and variables
		hookCtx.SQLQuery = sqlquery
		hookCtx.Variables = variables
		hookCtx.InputVars = inputvars

		// Execute query within transaction
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			sqlqueryCnt := sqlquery

			// Parse sorting and pagination parameters
			sortcols, limit, offset := h.parsePaginationParams(r)

			// Override with parsed parameters if available
			if reqParams.SortColumns != "" {
				sortcols = reqParams.SortColumns
			}
			if reqParams.Limit > 0 {
				limit = reqParams.Limit
			}
			if reqParams.Offset > 0 {
				offset = reqParams.Offset
			}

			hookCtx.SortColumns = sortcols
			hookCtx.Limit = limit
			hookCtx.Offset = offset
			fromPos := strings.Index(strings.ToLower(sqlquery), "from ")
			orderbyPos := strings.Index(strings.ToLower(sqlquery), "order by")

			if len(sortcols) > 0 && (orderbyPos < 0 || (orderbyPos > 0 && orderbyPos < fromPos)) {
				sqlquery = fmt.Sprintf("%s \nORDER BY %s", sqlquery, ValidSQL(sortcols, "select"))
			}

			if !options.NoCount {
				if limit > 0 && offset > 0 {
					sqlquery = fmt.Sprintf("%s \nLIMIT %d OFFSET %d", sqlquery, limit, offset)
				} else if limit > 0 {
					sqlquery = fmt.Sprintf("%s \nLIMIT %d", sqlquery, limit)
				} else {
					sqlquery = fmt.Sprintf("%s \nLIMIT %d", sqlquery, 20000)
				}

				// Get total count
				countQuery := fmt.Sprintf("SELECT COUNT(1) FROM (%s) cnts", sqlqueryCnt)
				var countResult struct{ Count int64 }
				if err := tx.Query(ctx, &countResult, countQuery); err != nil {
					sendError(w, http.StatusBadRequest, "count_failed", "Failed to retrieve record count", err)
					return err
				}
				total = countResult.Count
			}

			// Execute BeforeSQLExec hook
			hookCtx.SQLQuery = sqlquery
			if err := h.hooks.Execute(BeforeSQLExec, hookCtx); err != nil {
				logger.Error("BeforeSQLExec hook failed: %v", err)
				sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
				return err
			}
			// Use potentially modified SQL query from hook
			sqlquery = hookCtx.SQLQuery

			// Execute main query
			rows := make([]map[string]interface{}, 0)
			if err := tx.Query(ctx, &rows, sqlquery); err != nil {
				sendError(w, http.StatusBadRequest, "query_failed", "Failed to retrieve records", err)
				return err
			}

			// Normalize PostgreSQL types for proper JSON marshaling
			dbobjlist = normalizePostgresTypesList(rows)

			if options.NoCount {
				total = int64(len(dbobjlist))
			}

			// Execute AfterSQLExec hook
			hookCtx.Result = dbobjlist
			hookCtx.Total = total
			if err := h.hooks.Execute(AfterSQLExec, hookCtx); err != nil {
				logger.Error("AfterSQLExec hook failed: %v", err)
				sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
				return err
			}
			// Use potentially modified result from hook
			if modifiedResult, ok := hookCtx.Result.([]map[string]interface{}); ok {
				dbobjlist = modifiedResult
			}
			total = hookCtx.Total

			return nil
		})

		if err != nil {
			logger.Error("Transaction failed: %v", err)
			return
		}

		// Execute AfterQueryList hook
		hookCtx.Result = dbobjlist
		hookCtx.Total = total
		hookCtx.Error = err
		if err := h.hooks.Execute(AfterQueryList, hookCtx); err != nil {
			logger.Error("AfterQueryList hook failed: %v", err)
			sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
			return
		}
		// Use potentially modified result from hook
		if modifiedResult, ok := hookCtx.Result.([]map[string]interface{}); ok {
			dbobjlist = modifiedResult
		}
		total = hookCtx.Total

		// Set response headers
		respOffset := 0
		if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				respOffset = o
			}
		}

		w.Header().Set("Content-Range", fmt.Sprintf("items %d-%d/%d", respOffset, respOffset+len(dbobjlist), total))
		logger.Info("Serving: Records %d of %d", len(dbobjlist), total)

		// Execute BeforeResponse hook
		hookCtx.Result = dbobjlist
		hookCtx.Total = total
		if err := h.hooks.Execute(BeforeResponse, hookCtx); err != nil {
			logger.Error("BeforeResponse hook failed: %v", err)
			sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
			return
		}
		// Use potentially modified result from hook
		if modifiedResult, ok := hookCtx.Result.([]map[string]interface{}); ok {
			dbobjlist = modifiedResult
		}

		if len(dbobjlist) == 0 {
			_, _ = w.Write([]byte("[]"))
			return
		}

		// Format response based on response format
		switch reqParams.ResponseFormat {
		case "syncfusion":
			// Syncfusion format: { result: data, count: total }
			response := map[string]interface{}{
				"result": dbobjlist,
				"count":  total,
			}
			data, err := json.Marshal(response)
			if err != nil {
				sendError(w, http.StatusInternalServerError, "json_error", "Could not marshal response", err)
			} else {
				if int64(len(dbobjlist)) < total {
					w.WriteHeader(http.StatusPartialContent)
				}
				_, _ = w.Write(data)
			}

		case "detail":
			// Detail format: complex API with metadata
			metaobj := map[string]interface{}{
				"items":       dbobjlist,
				"count":       fmt.Sprintf("%d", len(dbobjlist)),
				"total":       fmt.Sprintf("%d", total),
				"tablename":   r.URL.Path,
				"tableprefix": "gsql",
			}
			data, err := json.Marshal(metaobj)
			if err != nil {
				sendError(w, http.StatusInternalServerError, "json_error", "Could not marshal response", err)
			} else {
				if int64(len(dbobjlist)) < total {
					w.WriteHeader(http.StatusPartialContent)
				}
				_, _ = w.Write(data)
			}

		default:
			// Simple format: just return the data array (or complex API if requested)
			if complexAPI {
				metaobj := map[string]interface{}{
					"items":       dbobjlist,
					"count":       fmt.Sprintf("%d", len(dbobjlist)),
					"total":       fmt.Sprintf("%d", total),
					"tablename":   r.URL.Path,
					"tableprefix": "gsql",
				}
				data, err := json.Marshal(metaobj)
				if err != nil {
					sendError(w, http.StatusInternalServerError, "json_error", "Could not marshal response", err)
				} else {
					if int64(len(dbobjlist)) < total {
						w.WriteHeader(http.StatusPartialContent)
					}
					_, _ = w.Write(data)
				}
			} else {
				data, err := json.Marshal(dbobjlist)
				if err != nil {
					sendError(w, http.StatusInternalServerError, "json_error", "Could not marshal response", err)
				} else {
					if int64(len(dbobjlist)) < total {
						w.WriteHeader(http.StatusPartialContent)
					}
					_, _ = w.Write(data)
				}
			}
		}
	}
}

// SqlQuery creates an HTTP handler that executes a SQL query and returns a single record
func (h *Handler) SqlQuery(sqlquery string, options SqlQueryOptions) HTTPFuncType {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				logger.Error("Panic in SqlQuery: %v\nStack trace:\n%s", err, string(stack))
				http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
			}
		}()

		// Create local copy to avoid modifying the captured parameter across requests
		sqlquery := sqlquery

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()

		propQry := make(map[string]string)
		inputvars := make([]string, 0)
		metainfo := make(map[string]interface{})
		variables := make(map[string]interface{})

		dbobj := make(map[string]interface{})
		complexAPI := false

		// Get user context from security package
		userCtx, ok := security.GetUserContext(ctx)
		if !ok {
			logger.Warn("No user context found in request")
			userCtx = &security.UserContext{UserID: 0, UserName: "anonymous"}
		}

		w.Header().Set("Content-Type", "application/json")

		// Initialize hook context
		hookCtx := &HookContext{
			Context:     ctx,
			Handler:     h,
			Request:     r,
			Writer:      w,
			SQLQuery:    sqlquery,
			Variables:   variables,
			InputVars:   inputvars,
			MetaInfo:    metainfo,
			PropQry:     propQry,
			UserContext: userCtx,
			BlankParams: options.BlankParams,
			ComplexAPI:  complexAPI,
		}

		// Execute BeforeQuery hook
		if err := h.hooks.Execute(BeforeQuery, hookCtx); err != nil {
			logger.Error("BeforeQuery hook failed: %v", err)
			sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
			return
		}

		// Check if hook aborted the operation
		if hookCtx.Abort {
			if hookCtx.AbortCode == 0 {
				hookCtx.AbortCode = http.StatusBadRequest
			}
			sendError(w, hookCtx.AbortCode, "operation_aborted", hookCtx.AbortMessage, nil)
			return
		}

		// Use potentially modified SQL query and variables from hooks
		sqlquery = hookCtx.SQLQuery
		variables = hookCtx.Variables

		// Extract input variables from SQL query
		sqlquery = h.extractInputVariables(sqlquery, &inputvars)

		// Merge URL path parameters
		sqlquery = h.mergePathParams(r, sqlquery, variables)

		// Parse comprehensive parameters from headers and query string
		reqParams := h.ParseParameters(r)
		complexAPI = reqParams.ComplexAPI

		// Merge query string parameters
		sqlquery = h.mergeQueryParams(r, sqlquery, variables, false, propQry)

		// Merge header parameters
		sqlquery = h.mergeHeaderParams(r, sqlquery, variables, propQry, &complexAPI)
		hookCtx.ComplexAPI = complexAPI

		// Apply filters from parsed parameters
		sqlquery = h.ApplyFilters(sqlquery, reqParams)

		// Apply field selection
		sqlquery = h.ApplyFieldSelection(sqlquery, reqParams)

		// Apply DISTINCT if requested
		sqlquery = h.ApplyDistinct(sqlquery, reqParams)

		// Build metainfo
		metainfo["ipaddress"] = getIPAddress(r)
		metainfo["url"] = r.RequestURI
		metainfo["user"] = userCtx.UserName
		metainfo["rid_user"] = fmt.Sprintf("%d", userCtx.UserID)
		metainfo["method"] = r.Method
		metainfo["variables"] = variables

		// Replace meta variables in SQL
		sqlquery = h.replaceMetaVariables(sqlquery, r, userCtx, metainfo, variables)

		// Apply field filters from headers
		for k, val := range propQry {
			kLower := strings.ToLower(k)
			if strings.HasPrefix(kLower, "x-fieldfilter-") {
				colname := strings.ReplaceAll(kLower, "x-fieldfilter-", "")
				if strings.Contains(strings.ToLower(sqlquery), colname) {
					if val == "" || val == "0" {
						sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("COALESCE(%s, 0) = %s", ValidSQL(colname, "colname"), ValidSQL(val, "colvalue")))
					} else {
						sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s = %s", ValidSQL(colname, "colname"), ValidSQL(val, "colvalue")))
					}
				}
			}
		}

		// Remove unused input variables
		if options.BlankParams {
			for _, kw := range inputvars {
				replacement := getReplacementForBlankParam(sqlquery, kw)
				sqlquery = strings.ReplaceAll(sqlquery, kw, replacement)
				logger.Debug("Replaced unused variable %s with: %s", kw, replacement)
			}
		}

		// Update hook context with latest SQL query and variables
		hookCtx.SQLQuery = sqlquery
		hookCtx.Variables = variables
		hookCtx.InputVars = inputvars

		// Execute query within transaction
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			// Execute BeforeSQLExec hook
			if err := h.hooks.Execute(BeforeSQLExec, hookCtx); err != nil {
				logger.Error("BeforeSQLExec hook failed: %v", err)
				sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
				return err
			}
			// Use potentially modified SQL query from hook
			sqlquery = hookCtx.SQLQuery

			// Execute main query
			rows := make([]map[string]interface{}, 0)
			if err := tx.Query(ctx, &rows, sqlquery); err != nil {
				sendError(w, http.StatusBadRequest, "query_failed", "Failed to retrieve records", err)
				return err
			}

			if len(rows) > 0 {
				dbobj = normalizePostgresTypes(rows[0])
			}

			// Execute AfterSQLExec hook
			hookCtx.Result = dbobj
			if err := h.hooks.Execute(AfterSQLExec, hookCtx); err != nil {
				logger.Error("AfterSQLExec hook failed: %v", err)
				sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
				return err
			}
			// Use potentially modified result from hook
			if modifiedResult, ok := hookCtx.Result.(map[string]interface{}); ok {
				dbobj = modifiedResult
			}

			return nil
		})

		if err != nil {
			logger.Error("Transaction failed: %v", err)
			return
		}

		// Execute AfterQuery hook
		hookCtx.Result = dbobj
		hookCtx.Error = err
		if err := h.hooks.Execute(AfterQuery, hookCtx); err != nil {
			logger.Error("AfterQuery hook failed: %v", err)
			sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
			return
		}
		// Use potentially modified result from hook
		if modifiedResult, ok := hookCtx.Result.(map[string]interface{}); ok {
			dbobj = modifiedResult
		}

		// Execute BeforeResponse hook
		hookCtx.Result = dbobj
		if err := h.hooks.Execute(BeforeResponse, hookCtx); err != nil {
			logger.Error("BeforeResponse hook failed: %v", err)
			sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
			return
		}
		// Use potentially modified result from hook
		if modifiedResult, ok := hookCtx.Result.(map[string]interface{}); ok {
			dbobj = modifiedResult
		}

		// Check if response should be root-level data
		if val, ok := dbobj["root_as_data"]; ok {
			data, err := json.Marshal(val)
			if err != nil {
				sendError(w, http.StatusInternalServerError, "json_error", "Could not marshal response", err)
			} else {
				_, _ = w.Write(data)
			}
			return
		}

		// Marshal and send response
		data, err := json.Marshal(dbobj)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json_error", "Could not marshal response", err)
		} else {
			_, _ = w.Write(data)
		}
	}
}

// Helper functions

// extractInputVariables extracts placeholders like [variable] from the SQL query
func (h *Handler) extractInputVariables(sqlquery string, inputvars *[]string) string {

	testsqlquery := sqlquery
	for i := 0; i <= strings.Count(sqlquery, "[")*4; i++ {
		iStart := strings.Index(testsqlquery, "[")
		if iStart < 0 {
			break
		}
		iEnd := strings.Index(testsqlquery, "]")
		if iEnd < 0 {
			break
		}
		*inputvars = append(*inputvars, testsqlquery[iStart:iEnd+1])
		testsqlquery = testsqlquery[iEnd+1:]
	}
	return sqlquery
}

// mergePathParams merges URL path parameters into the SQL query
func (h *Handler) mergePathParams(r *http.Request, sqlquery string, variables map[string]interface{}) string {

	if h.GetVariablesCallback() != nil {
		pathVars := h.GetVariablesCallback()(r)
		for k, v := range pathVars {
			kword := fmt.Sprintf("[%s]", k)
			if strings.Contains(sqlquery, kword) {
				// Sanitize the value before replacing
				vStr := fmt.Sprintf("%v", v)
				sanitized := ValidSQL(vStr, "colvalue")
				sqlquery = strings.ReplaceAll(sqlquery, kword, sanitized)
			}
			variables[k] = v

		}
	}
	return sqlquery
}

// mergeQueryParams merges query string parameters into the SQL query
func (h *Handler) mergeQueryParams(r *http.Request, sqlquery string, variables map[string]interface{}, allowFilter bool, propQry map[string]string) string {
	for parmk, parmv := range r.URL.Query() {
		if len(parmk) == 0 || len(parmv) == 0 {
			continue
		}

		val := parmv[0]
		dec, err := restheadspec.DecodeParam(val)
		if err == nil {
			val = dec
		}

		kword := fmt.Sprintf("[%s]", parmk)
		variables[parmk] = val

		// Replace in SQL if placeholder exists
		if strings.Contains(sqlquery, kword) && len(val) > 0 {
			if strings.HasPrefix(parmk, "p-") {
				// Sanitize the parameter value before replacing
				sanitized := ValidSQL(val, "colvalue")
				sqlquery = strings.ReplaceAll(sqlquery, kword, sanitized)
			}
		}

		// Add to propQry for x- prefixed params
		if strings.HasPrefix(parmk, "x-") {
			propQry[parmk] = val
		}

		// Apply filters if allowed
		if allowFilter && len(parmk) > 1 && strings.Contains(strings.ToLower(sqlquery), strings.ToLower(parmk)) {
			if len(parmv) > 1 {
				// Sanitize each value in the IN clause
				sanitizedValues := make([]string, len(parmv))
				for i, v := range parmv {
					sanitizedValues[i] = ValidSQL(v, "colvalue")
				}
				sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s IN (%s)", ValidSQL(parmk, "colname"), strings.Join(sanitizedValues, ",")))
			} else {
				if strings.Contains(val, "match=") {
					colval := strings.ReplaceAll(val, "match=", "")
					if colval != "*" {
						sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s ILIKE '%%%s%%'", ValidSQL(parmk, "colname"), ValidSQL(colval, "colvalue")))
					}
				} else if val == "" || val == "0" {
					sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("(%[1]s = %[2]s OR %[1]s IS NULL)", ValidSQL(parmk, "colname"), ValidSQL(val, "colvalue")))
				} else {
					if IsNumeric(val) {
						sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s = %s", ValidSQL(parmk, "colname"), ValidSQL(val, "colvalue")))
					} else {
						sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s = '%s'", ValidSQL(parmk, "colname"), ValidSQL(val, "colvalue")))
					}
				}
			}
		}
	}
	return sqlquery
}

// mergeHeaderParams merges HTTP header parameters into the SQL query
func (h *Handler) mergeHeaderParams(r *http.Request, sqlquery string, variables map[string]interface{}, propQry map[string]string, complexAPI *bool) string {
	for kc, v := range r.Header {
		k := strings.ToLower(kc)
		if !strings.HasPrefix(k, "x-") || len(v) == 0 {
			continue
		}

		val := v[0]
		dec, err := restheadspec.DecodeParam(val)
		if err == nil {
			val = dec
		}

		variables[k] = val
		propQry[k] = val

		kword := fmt.Sprintf("[%s]", k)
		if strings.Contains(sqlquery, kword) {
			// Sanitize the header value before replacing
			sanitized := ValidSQL(val, "colvalue")
			sqlquery = strings.ReplaceAll(sqlquery, kword, sanitized)
		}

		// Handle special headers
		if strings.Contains(k, "x-fieldfilter-") {
			colname := strings.ReplaceAll(k, "x-fieldfilter-", "")
			if val == "" || val == "0" {
				sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("COALESCE(%s, 0) = %s", ValidSQL(colname, "colname"), ValidSQL(val, "colvalue")))
			} else {
				sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s = %s", ValidSQL(colname, "colname"), ValidSQL(val, "colvalue")))
			}
		}

		if strings.Contains(k, "x-searchfilter-") {
			colname := strings.ReplaceAll(k, "x-searchfilter-", "")
			sval := strings.ReplaceAll(val, "'", "")
			if sval != "" {
				sqlquery = sqlQryWhere(sqlquery, fmt.Sprintf("%s ILIKE '%%%s%%'", ValidSQL(colname, "colname"), ValidSQL(sval, "colvalue")))
			}
		}

		if strings.Contains(k, "x-custom-sql-w") {
			colval := ValidSQL(val, "select")
			if len(colval) > 0 {
				sqlquery = sqlQryWhere(sqlquery, colval)
			}
		}

		if strings.Contains(k, "x-simpleapi") {
			*complexAPI = !strings.EqualFold(val, "1") && !strings.EqualFold(val, "true")
		}
	}
	return sqlquery
}

// replaceMetaVariables replaces meta variables like [rid_user], [user], etc. in the SQL query
func (h *Handler) replaceMetaVariables(sqlquery string, r *http.Request, userCtx *security.UserContext, metainfo map[string]interface{}, variables map[string]interface{}) string {
	if strings.Contains(sqlquery, "[p_meta_default]") {
		data, _ := json.Marshal(metainfo)
		dataStr := strings.ReplaceAll(string(data), "$META$", "/*META*/")
		sqlquery = strings.ReplaceAll(sqlquery, "[p_meta_default]", fmt.Sprintf("$META$%s$META$::jsonb", dataStr))
	}

	if strings.Contains(sqlquery, "[json_variables]") {
		data, _ := json.Marshal(variables)
		dataStr := strings.ReplaceAll(string(data), "$VAR$", "/*VAR*/")

		sqlquery = strings.ReplaceAll(sqlquery, "[json_variables]", fmt.Sprintf("$VAR$%s$VAR$::jsonb", dataStr))
	}

	if strings.Contains(sqlquery, "[rid_user]") {
		sqlquery = strings.ReplaceAll(sqlquery, "[rid_user]", fmt.Sprintf("%d", userCtx.UserID))
	}

	if strings.Contains(sqlquery, "[user]") {
		sqlquery = strings.ReplaceAll(sqlquery, "[user]", fmt.Sprintf("$USR$%s$USR$", strings.ReplaceAll(userCtx.UserName, "$USR$", "/*USR*/")))
	}

	if strings.Contains(sqlquery, "[rid_session]") {
		sqlquery = strings.ReplaceAll(sqlquery, "[rid_session]", fmt.Sprintf("%d", userCtx.SessionRID))
	}
	if strings.Contains(sqlquery, "[id_session]") {
		sqlquery = strings.ReplaceAll(sqlquery, "[id_session]", userCtx.SessionID)
	}

	if strings.Contains(sqlquery, "[method]") {
		sqlquery = strings.ReplaceAll(sqlquery, "[method]", fmt.Sprintf("$M$%s$M$", strings.ReplaceAll(r.Method, "$M$", "/*M*/")))
	}

	if strings.Contains(sqlquery, "[post_body]") {
		bodystr := ""
		if r.Method == "POST" || r.Method == "PUT" {
			if r.Body != nil {
				contents, err := io.ReadAll(r.Body)
				if err == nil {
					bodystr = string(contents)
				}
			}
		}
		sqlquery = strings.ReplaceAll(sqlquery, "[post_body]", fmt.Sprintf("$PBODY$%s$PBODY$", strings.ReplaceAll(bodystr, "$PBODY$", "/*PBODY*/")))
	}

	return sqlquery
}

// parsePaginationParams extracts sort, limit, and offset parameters from request
func (h *Handler) parsePaginationParams(r *http.Request) (sortcols string, limit, offset int) {
	limit = 20
	offset = 0

	if sortStr := r.URL.Query().Get("sort"); sortStr != "" {
		sortcols = sortStr
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	return
}

// ValidSQL validates and sanitizes SQL input to prevent injection
// mode can be: "colname", "colvalue", "select"
func ValidSQL(input, mode string) string {
	// Remove dangerous characters based on mode
	switch mode {
	case "colname":
		// For column names, only allow alphanumeric, underscore, and dot
		reg := regexp.MustCompile(`[^a-zA-Z0-9_\.]`)
		return reg.ReplaceAllString(input, "")
	case "colvalue":
		// For column values, escape single quotes and backslashes
		result := strings.ReplaceAll(input, "\\", "\\\\")
		result = strings.ReplaceAll(result, "'", "''")
		return result
	case "select":
		// For SELECT clauses, be more permissive but still safe
		// Remove semicolons and common SQL injection patterns
		dangerous := []string{
			";", "--", "/*", "*/", "xp_", "sp_",
			"DROP ", "drop ", "Drop ",
			"DELETE ", "delete ", "Delete ",
			"TRUNCATE ", "truncate ", "Truncate ",
			"UPDATE ", "update ", "Update ",
			"INSERT ", "insert ", "Insert ",
			"EXEC ", "exec ", "Exec ",
			"EXECUTE ", "execute ", "Execute ",
			"UNION ", "union ", "Union ",
			"DECLARE ", "declare ", "Declare ",
			"ALTER ", "alter ", "Alter ",
			"CREATE ", "create ", "Create ",
		}
		result := input
		for _, d := range dangerous {
			result = strings.ReplaceAll(result, d, "")
		}
		return result
	default:
		return input
	}
}

// sqlQryWhere adds a WHERE clause to a SQL query or appends to existing WHERE with AND
func sqlQryWhere(sqlquery, condition string) string {
	lowerQuery := strings.ToLower(sqlquery)
	wherePos := strings.Index(lowerQuery, " where ")
	groupPos := strings.Index(lowerQuery, " group by")
	orderPos := strings.Index(lowerQuery, " order by")
	limitPos := strings.Index(lowerQuery, " limit ")

	// Find the insertion point (before GROUP BY, ORDER BY, or LIMIT)
	insertPos := len(sqlquery)
	if groupPos > 0 && groupPos < insertPos {
		insertPos = groupPos
	}
	if orderPos > 0 && orderPos < insertPos {
		insertPos = orderPos
	}
	if limitPos > 0 && limitPos < insertPos {
		insertPos = limitPos
	}

	if wherePos > 0 {
		// WHERE exists, add AND condition before GROUP BY / ORDER BY / LIMIT
		before := sqlquery[:insertPos]
		after := sqlquery[insertPos:]
		return fmt.Sprintf("%s AND %s %s", before, condition, after)
	} else {
		// No WHERE exists, add it before GROUP BY / ORDER BY / LIMIT
		before := sqlquery[:insertPos]
		after := sqlquery[insertPos:]
		return fmt.Sprintf("%s WHERE %s %s", before, condition, after)
	}
}

// IsNumeric checks if a string contains only numeric characters
func IsNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// getReplacementForBlankParam determines the replacement value for an unused parameter
// based on whether it appears within quotes in the SQL query.
// It checks for PostgreSQL quotes: single quotes (”) and dollar quotes ($...$)
func getReplacementForBlankParam(sqlquery, param string) string {
	// Find the parameter in the query
	idx := strings.Index(sqlquery, param)
	if idx < 0 {
		return ""
	}

	// Check characters immediately before and after the parameter
	var charBefore, charAfter byte

	if idx > 0 {
		charBefore = sqlquery[idx-1]
	}

	endIdx := idx + len(param)
	if endIdx < len(sqlquery) {
		charAfter = sqlquery[endIdx]
	}

	// Check if parameter is surrounded by quotes (single quote or dollar sign for PostgreSQL dollar-quoted strings)
	if (charBefore == '\'' || charBefore == '$') && (charAfter == '\'' || charAfter == '$') {
		// Parameter is in quotes, return empty string
		return ""
	}

	// Parameter is not in quotes, return NULL
	return "NULL"
}

// makeResultReceiver creates a slice of interface{} pointers for scanning SQL rows
// func makeResultReceiver(length int) []interface{} {
// 	result := make([]interface{}, length)
// 	for i := 0; i < length; i++ {
// 		var v interface{}
// 		result[i] = &v
// 	}
// 	return result
// }

// getIPAddress extracts the real IP address from the request
func getIPAddress(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	return r.RemoteAddr
}

// sendError sends a JSON error response
func sendError(w http.ResponseWriter, status int, code, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errObj := common.APIError{
		Code:    code,
		Message: message,
	}
	if err != nil {
		errObj.Detail = err.Error()
	}

	data, _ := json.Marshal(map[string]interface{}{
		"success": false,
		"error":   errObj,
	})
	_, _ = w.Write(data)
}

// normalizePostgresTypesList normalizes a list of result maps to handle PostgreSQL types correctly
func normalizePostgresTypesList(rows []map[string]interface{}) []map[string]interface{} {
	if len(rows) == 0 {
		return rows
	}

	normalized := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		normalized[i] = normalizePostgresTypes(row)
	}
	return normalized
}

// normalizePostgresTypes normalizes a result map to handle PostgreSQL types correctly for JSON marshaling
// This is necessary because when scanning into map[string]interface{}, PostgreSQL types like jsonb, bytea, etc.
// are scanned as []byte which would be base64-encoded when marshaled to JSON.
func normalizePostgresTypes(row map[string]interface{}) map[string]interface{} {
	if row == nil {
		return nil
	}

	normalized := make(map[string]interface{}, len(row))
	for key, value := range row {
		normalized[key] = normalizePostgresValue(value)
	}
	return normalized
}

// normalizePostgresValue normalizes a single value to the appropriate Go type for JSON marshaling
func normalizePostgresValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []byte:
		// Check if it's valid JSON (jsonb type)
		// Try to unmarshal as JSON first
		var jsonObj interface{}
		if err := json.Unmarshal(v, &jsonObj); err == nil {
			// It's valid JSON, return as json.RawMessage so it's not double-encoded
			return json.RawMessage(v)
		}
		// Not valid JSON, could be bytea - keep as []byte for base64 encoding
		return v

	case []interface{}:
		// Recursively normalize array elements
		normalized := make([]interface{}, len(v))
		for i, elem := range v {
			normalized[i] = normalizePostgresValue(elem)
		}
		return normalized

	case map[string]interface{}:
		// Recursively normalize nested maps
		return normalizePostgresTypes(v)

	default:
		// For other types (int, float, string, bool, etc.), return as-is
		return v
	}
}
