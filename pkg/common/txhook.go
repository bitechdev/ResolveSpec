package common

import "context"

// TxHookName is the shared value of every spec's OnTxBegin HookType.
const TxHookName = "on_tx_begin"

// TxContext is implemented by a spec's HookContext so RunRequestTx can point
// it at the transaction it opens.
type TxContext interface {
	SetTx(tx Database)
}

// RunRequestTx opens a transaction on db, points tc at it, runs onBegin (the
// spec's OnTxBegin hooks) and then body. An error from onBegin or body rolls
// the transaction back; body is not run when onBegin fails.
func RunRequestTx(ctx context.Context, db Database, tc TxContext, onBegin func() error, body func(tx Database) error) error {
	return db.RunInTransaction(ctx, func(tx Database) error {
		tc.SetTx(tx)
		if onBegin != nil {
			if err := onBegin(); err != nil {
				return err
			}
		}
		return body(tx)
	})
}
