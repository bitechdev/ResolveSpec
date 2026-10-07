package aiproxy

import (
	"bytes"
	"io"
	"sync"

	"github.com/tidwall/gjson"
)

const maxCapture = 1 << 20

// watchBody passes the response through while it looks for OpenAI usage, and fires
// AfterProxy once the body ends or is closed.
type watchBody struct {
	rc      io.ReadCloser
	capture bool // read usage
	sse     bool
	status  int
	c       *call

	buf      bytes.Buffer // JSON body (capped)
	overflow bool
	line     []byte // partial SSE line
	usage    Usage
	once     sync.Once
}

func newWatchBody(rc io.ReadCloser, openai, sse bool, status int, c *call) io.ReadCloser {
	return &watchBody{rc: rc, capture: openai, sse: sse, status: status, c: c}
}

func (w *watchBody) Read(p []byte) (int, error) {
	n, err := w.rc.Read(p)
	if w.capture && n > 0 {
		w.feed(p[:n])
	}
	if err != nil {
		w.end(err)
	}
	return n, err
}

func (w *watchBody) Close() error {
	err := w.rc.Close()
	w.end(nil)
	return err
}

func (w *watchBody) end(err error) {
	w.once.Do(func() {
		if w.capture && !w.sse && !w.overflow {
			if u, ok := parseUsage(w.buf.Bytes()); ok {
				w.usage = u
			}
		}
		if err == io.EOF {
			err = nil
		}
		w.c.finish(w.status, w.usage, err)
	})
}

func (w *watchBody) feed(b []byte) {
	if !w.sse {
		if w.overflow || w.buf.Len()+len(b) > maxCapture {
			w.overflow = true
			return
		}
		w.buf.Write(b)
		return
	}
	w.line = append(w.line, b...)
	for {
		i := bytes.IndexByte(w.line, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSpace(w.line[:i])
		w.line = w.line[i+1:]
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok && bytes.Contains(rest, []byte("usage")) {
			if u, ok := parseUsage(bytes.TrimSpace(rest)); ok {
				w.usage = u
			}
		}
	}
	if len(w.line) > maxCapture {
		w.line = nil
	}
}

// parseUsage reads chat/embeddings ("usage") and responses ("response.usage") shapes.
func parseUsage(data []byte) (Usage, bool) {
	u := gjson.GetBytes(data, "usage")
	if !u.IsObject() {
		u = gjson.GetBytes(data, "response.usage")
	}
	if !u.IsObject() {
		return Usage{}, false
	}
	first := func(keys ...string) int64 {
		for _, k := range keys {
			if v := u.Get(k); v.Exists() {
				return v.Int()
			}
		}
		return 0
	}
	out := Usage{
		PromptTokens:     first("prompt_tokens", "input_tokens"),
		CompletionTokens: first("completion_tokens", "output_tokens"),
		TotalTokens:      first("total_tokens"),
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	return out, true
}
