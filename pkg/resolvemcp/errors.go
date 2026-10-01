package resolvemcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Stable error codes returned to MCP clients.
const (
	CodeInvalidArgument = "invalid_argument"
	CodeNotFound        = "not_found"
	CodeForbidden       = "forbidden"
	CodeLimitExceeded   = "limit_exceeded"
	CodeInternal        = "internal"
)

// ClientError is an error whose code and message are safe to show to an MCP client.
// Hooks may return one (NewClientError) to give the client a specific reason; every other error
// is reported as an opaque internal error with a reference that matches the server log.
type ClientError struct {
	Code    string
	Message string
}

func (e *ClientError) Error() string { return e.Message }

// NewClientError returns an error that reaches the client as {code, message}.
func NewClientError(code, message string) error {
	return &ClientError{Code: code, Message: message}
}

func invalidArg(format string, a ...any) error {
	return NewClientError(CodeInvalidArgument, fmt.Sprintf(format, a...))
}

// errInternal is what recovered panics return: the details are logged, not sent.
var errInternal = NewClientError(CodeInternal, "internal error")

// clientFacing maps err to the code and message the client sees. Anything that is not a
// ClientError or a not-found is logged in full with a short reference and reported as an
// opaque internal error carrying that reference.
func clientFacing(op string, err error) (code, message string) {
	var ce *ClientError
	switch {
	case errors.As(err, &ce):
		if ce.Code == CodeInternal {
			ref := newRef()
			logger.Error("[resolvemcp] %s: internal error ref=%s: %v", op, ref, err)
			return CodeInternal, "internal error (ref " + ref + ")"
		}
		return ce.Code, ce.Message
	case errors.Is(err, errRecordNotFound):
		return CodeNotFound, "record not found"
	}
	ref := newRef()
	logger.Error("[resolvemcp] %s failed ref=%s: %v", op, ref, err)
	return CodeInternal, "internal error (ref " + ref + ")"
}

func newRef() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// toolError builds the error result for a tool call.
func toolError(op string, err error) *mcp.CallToolResult {
	code, msg := clientFacing(op, err)
	b, _ := json.Marshal(map[string]any{
		"success": false,
		"error":   map[string]string{"code": code, "message": msg},
	})
	return mcp.NewToolResultError(string(b))
}
