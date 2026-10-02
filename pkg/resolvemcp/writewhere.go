package resolvemcp

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// previewRows is how many matched primary keys a preview lists.
const previewRows = 10

var filterColumnRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// writeOperators are the filter operators a filter write accepts.
var writeOperators = map[string]bool{
	"eq": true, "=": true, "neq": true, "!=": true, "<>": true, "gt": true, ">": true, "gte": true, ">=": true,
	"lt": true, "<": true, "lte": true, "<=": true, "like": true, "ilike": true, "in": true,
	"is_null": true, "is_not_null": true,
}

// validateWriteFilters requires at least one filter and that every filter is usable. Reads
// silently drop filters they cannot apply; a write must not, because a dropped filter widens
// the set of rows the write touches.
func validateWriteFilters(model interface{}, filters []common.FilterOption) error {
	if len(filters) == 0 {
		return invalidArg("provide an id or at least one filter")
	}
	v := common.NewColumnValidator(model)
	for _, f := range filters {
		if !filterColumnRe.MatchString(f.Column) || !v.IsValidColumn(f.Column) {
			return invalidArg("unknown filter column %q", truncate(f.Column))
		}
		if !writeOperators[strings.ToLower(f.Operator)] {
			return invalidArg("unsupported filter operator %q", truncate(f.Operator))
		}
		op := strings.ToLower(f.Operator)
		if op != "is_null" && op != "is_not_null" && f.Value == nil {
			return invalidArg("filter on %q needs a value", f.Column)
		}
	}
	return nil
}

// whereRequest describes a filter-based update or delete.
type whereRequest struct {
	schema, entity string
	op             string // "update" or "delete"
	filters        []common.FilterOption
	data           map[string]interface{} // update only
	dryRun         bool
	confirmToken   string
}

// whereResult is what a filter write returns to the client.
type whereResult struct {
	DryRun          bool          `json:"dry_run,omitempty"`
	RequiresConfirm bool          `json:"requires_confirmation,omitempty"`
	Matched         int           `json:"matched"`
	Preview         []interface{} `json:"preview,omitempty"`
	ConfirmToken    string        `json:"confirm_token,omitempty"`
	ExpiresInSec    int           `json:"expires_in_seconds,omitempty"`
	Affected        int           `json:"affected,omitempty"`
	IDs             []interface{} `json:"ids,omitempty"`
}

// executeWhere runs a filter-based write behind the guardrails: filters are validated, the
// matching rows are counted inside the transaction (aborting above Config.MaxWriteRows), and
// without dry_run the write only happens with a confirm token from a preview of the same
// request that matched the same rows.
func (h *Handler) executeWhere(ctx context.Context, req whereRequest) (_ *whereResult, retErr error) {
	defer recoverPanic(&retErr)
	ctx, cancel := h.callContext(ctx)
	defer cancel()

	model, err := h.registry.GetModelByEntity(req.schema, req.entity)
	if err != nil {
		return nil, invalidArg("model not found: %s", buildModelName(req.schema, req.entity))
	}
	unwrapped, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, errInternal
	}
	model = unwrapped.Model
	if err := validateWriteFilters(model, req.filters); err != nil {
		return nil, err
	}
	pkName := reflection.GetPrimaryKeyName(model)
	if pkName == "" {
		return nil, invalidArg("table has no primary key; filter writes are not available")
	}
	tableName := h.getTableName(req.schema, req.entity, model)
	ctx = withRequestData(h.withModelRules(ctx, req.schema, req.entity), req.schema, req.entity, tableName, model, unwrapped.ModelPtr)

	var setCols map[string]interface{}
	if req.op == "update" {
		if setCols, err = writeColumns(model, req.data); err != nil {
			return nil, err
		}
		for col := range setCols {
			if strings.EqualFold(col, pkName) {
				delete(setCols, col)
			}
		}
		if len(setCols) == 0 {
			return nil, invalidArg("no updatable fields in data")
		}
	}

	hookCtx := &HookContext{
		Context: ctx, Handler: h, Schema: req.schema, Entity: req.entity, Model: model,
		Operation: req.op, Tx: h.db,
	}
	if req.op == "update" {
		hookCtx.Data = req.data
	}
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		return nil, err
	}

	user := callerKey(ctx)
	table := buildModelName(req.schema, req.entity)
	res := &whereResult{}
	err = h.runInTx(ctx, hookCtx, func(tx common.Database) error {
		before, after := BeforeUpdate, AfterUpdate
		if req.op == "delete" {
			before, after = BeforeDelete, AfterDelete
		}
		if err := h.hooks.Execute(before, hookCtx); err != nil {
			return err
		}
		if req.op == "update" {
			// A hook (column security) may have narrowed the payload.
			if m, ok := hookCtx.Data.(map[string]interface{}); ok {
				if setCols, err = writeColumns(model, m); err != nil {
					return err
				}
				for col := range setCols {
					if strings.EqualFold(col, pkName) {
						delete(setCols, col)
					}
				}
				if len(setCols) == 0 {
					return invalidArg("no updatable fields in data")
				}
			}
		}

		ids, err := h.matchRows(ctx, tx, hookCtx, model, unwrapped.ModelType, tableName, pkName, req.filters)
		if err != nil {
			return err
		}
		res.Matched = len(ids)
		for i := 0; i < len(ids) && i < previewRows; i++ {
			res.Preview = append(res.Preview, ids[i])
		}

		if req.dryRun {
			res.DryRun = true
			return nil
		}
		binding, err := bindingHash(req.op, req.filters, req.data, ids)
		if err != nil {
			return err
		}
		if req.confirmToken == "" {
			tok, err := h.confirms.issue(user, table, req.op, binding, h.config.ConfirmTTL)
			if err != nil {
				return err
			}
			res.RequiresConfirm = true
			res.ConfirmToken = tok
			res.ExpiresInSec = int(h.config.ConfirmTTL / time.Second)
			return nil
		}
		if err := h.confirms.consume(req.confirmToken, user, table, req.op, binding); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		inList := make([]string, len(ids))
		for i := range ids {
			inList[i] = "?"
		}
		cond := fmt.Sprintf("%s IN (%s)", common.QuoteIdent(pkName), strings.Join(inList, ", "))
		var affected int64
		if req.op == "update" {
			reflection.RemoveNonWritableColumns(model, setCols)
			r, err := tx.NewUpdate().Table(tableName).SetMap(setCols).Where(cond, ids...).Exec(ctx)
			if err != nil {
				return fmt.Errorf("error updating records: %w", err)
			}
			affected = r.RowsAffected()
		} else {
			r, err := tx.NewDelete().Table(tableName).Where(cond, ids...).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete error: %w", err)
			}
			affected = r.RowsAffected()
		}
		if int(affected) != len(ids) {
			// Rows changed under us: roll back rather than report a partial write.
			return invalidArg("matched rows changed during the write; repeat the preview")
		}
		res.Affected = int(affected)
		res.IDs = ids
		hookCtx.Result = map[string]interface{}{"ids": ids, "count": len(ids)}
		return h.hooks.Execute(after, hookCtx)
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// matchRows returns the primary keys of the rows the filters select, narrowed by the BeforeScan
// hooks (row security). It fails when more than Config.MaxWriteRows match.
func (h *Handler) matchRows(ctx context.Context, tx common.Database, hookCtx *HookContext, model interface{}, modelType reflect.Type, tableName, pkName string, filters []common.FilterOption) ([]interface{}, error) {
	sliceType := reflect.SliceOf(reflect.PointerTo(modelType))
	dest := reflect.New(sliceType)
	q := tx.NewSelect().Model(dest.Interface())
	if provider, ok := reflect.New(modelType).Interface().(common.TableNameProvider); !ok || provider.TableName() == "" {
		q = q.Table(tableName)
	}
	q = h.applyFilters(q.Column(pkName), filters, model)
	hookCtx.Query = q
	if err := h.hooks.Execute(BeforeScan, hookCtx); err != nil {
		return nil, err
	}
	if err := hookCtx.Query.Limit(h.config.MaxWriteRows + 1).ScanModel(ctx); err != nil {
		return nil, fmt.Errorf("error matching records: %w", err)
	}
	rows := dest.Elem()
	if rows.Len() > h.config.MaxWriteRows {
		return nil, NewClientError(CodeLimitExceeded, fmt.Sprintf("the filters match more than %d rows; narrow them", h.config.MaxWriteRows))
	}
	ids := make([]interface{}, 0, rows.Len())
	for i := 0; i < rows.Len(); i++ {
		ids = append(ids, reflection.GetPrimaryKeyValue(rows.Index(i).Interface()))
	}
	sort.Slice(ids, func(i, j int) bool { return fmt.Sprint(ids[i]) < fmt.Sprint(ids[j]) })
	return ids, nil
}

// callerKey identifies the caller for confirm-token binding.
func callerKey(ctx context.Context) string {
	if uc, ok := security.GetUserContext(ctx); ok && uc != nil {
		return fmt.Sprintf("%d/%s", uc.UserID, uc.UserName)
	}
	return "anonymous"
}
