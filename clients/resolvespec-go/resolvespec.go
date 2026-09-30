package resolvespec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client speaks the ResolveSpec JSON body protocol: POST {operation, data, options}.
type Client struct{ cfg config }

func NewClient(baseURL string, opts ...Option) *Client {
	return &Client{cfg: newConfig(baseURL, opts)}
}

// RecordID is a single id (int or string, sent in the URL) or a []string (sent in the body).
type RecordID any

func urlID(id RecordID) string {
	switch v := id.(type) {
	case nil:
		return ""
	case []string:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func bodyID(id RecordID) []string {
	ids, _ := id.([]string)
	return ids
}

type request struct {
	Operation string   `json:"operation"`
	ID        []string `json:"id,omitempty"`
	Data      any      `json:"data,omitempty"`
	Options   *Options `json:"options,omitempty"`
}

func (c *Client) send(ctx context.Context, method, schema, entity, id string, body any) (*Response, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	req, err := c.cfg.newRequest(ctx, method, buildURL(c.cfg.baseURL, schema, entity, id), payload)
	if err != nil {
		return nil, err
	}
	resp, b, err := c.cfg.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, errorFrom(resp.StatusCode, b)
	}
	var out Response
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if !out.Success && out.Error != nil {
		return nil, &Error{StatusCode: resp.StatusCode, APIError: *out.Error}
	}
	return &out, nil
}

// GetMetadata returns table metadata (GET /{schema}/{entity}).
func (c *Client) GetMetadata(ctx context.Context, schema, entity string) (*Response, error) {
	return c.send(ctx, http.MethodGet, schema, entity, "", nil)
}

// Read reads records; id may be nil, an int/string (URL) or []string (body).
func (c *Client) Read(ctx context.Context, schema, entity string, id RecordID, opts *Options) (*Response, error) {
	return c.send(ctx, http.MethodPost, schema, entity, urlID(id), request{Operation: "read", ID: bodyID(id), Options: opts})
}

func (c *Client) Create(ctx context.Context, schema, entity string, data any, opts *Options) (*Response, error) {
	return c.send(ctx, http.MethodPost, schema, entity, "", request{Operation: "create", Data: data, Options: opts})
}

func (c *Client) Update(ctx context.Context, schema, entity string, data any, id RecordID, opts *Options) (*Response, error) {
	return c.send(ctx, http.MethodPost, schema, entity, urlID(id), request{Operation: "update", ID: bodyID(id), Data: data, Options: opts})
}

func (c *Client) Delete(ctx context.Context, schema, entity string, id RecordID) (*Response, error) {
	return c.send(ctx, http.MethodPost, schema, entity, urlID(id), request{Operation: "delete"})
}
