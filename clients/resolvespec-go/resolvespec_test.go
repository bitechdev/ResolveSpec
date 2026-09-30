package resolvespec

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type seen struct {
	method, path string
	header       http.Header
	body         map[string]any
}

func server(t *testing.T, status int, body string, hdr map[string]string) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method, s.path, s.header = r.Method, r.URL.EscapedPath()+"?"+r.URL.RawQuery, r.Header
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			_ = json.Unmarshal(b, &s.body)
		}
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func TestReadBody(t *testing.T) {
	srv, s := server(t, 200, `{"success":true,"data":[{"id":1}]}`, nil)
	c := NewClient(srv.URL+"/", WithToken("tok"), WithHeader("X-Tenant", "a"))
	resp, err := c.Read(context.Background(), "public", "users", nil, &Options{Limit: Int(5), Filters: []FilterOption{{Column: "a", Operator: "eq", Value: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if s.method != "POST" || s.path != "/public/users?" {
		t.Fatalf("got %s %s", s.method, s.path)
	}
	if s.header.Get("Authorization") != "Bearer tok" || s.header.Get("X-Tenant") != "a" {
		t.Fatalf("headers %v", s.header)
	}
	if s.body["operation"] != "read" || s.body["options"].(map[string]any)["limit"] != float64(5) {
		t.Fatalf("body %v", s.body)
	}
	var rows []map[string]any
	if err := resp.Decode(&rows); err != nil || len(rows) != 1 {
		t.Fatalf("decode %v %v", rows, err)
	}
}

func TestIDPlacement(t *testing.T) {
	srv, s := server(t, 200, `{"success":true,"data":{}}`, nil)
	c := NewClient(srv.URL)
	ctx := context.Background()
	_, _ = c.Read(ctx, "s", "e", 7, nil)
	if s.path != "/s/e/7?" || s.body["id"] != nil {
		t.Fatalf("%s %v", s.path, s.body)
	}
	_, _ = c.Update(ctx, "s", "e", map[string]any{"a": 1}, []string{"1", "2"}, nil)
	if s.path != "/s/e?" || !reflect.DeepEqual(s.body["id"], []any{"1", "2"}) || s.body["operation"] != "update" {
		t.Fatalf("%s %v", s.path, s.body)
	}
	_, _ = c.Delete(ctx, "s", "e", "a/b")
	if s.path != "/s/e/a%2Fb?" || s.body["operation"] != "delete" {
		t.Fatalf("%s %v", s.path, s.body)
	}
	_, _ = c.GetMetadata(ctx, "s", "e")
	if s.method != "GET" {
		t.Fatal(s.method)
	}
}

func TestErrors(t *testing.T) {
	srv, _ := server(t, 400, `{"success":false,"error":{"code":"x","message":"bad","detail":"why"}}`, nil)
	_, err := NewClient(srv.URL).Read(context.Background(), "s", "e", nil, nil)
	e, ok := err.(*Error)
	if !ok || e.StatusCode != 400 || e.Code != "x" || e.Message != "bad" || e.Detail != "why" {
		t.Fatalf("%#v", err)
	}
	srv2, _ := server(t, 502, "bad gateway", nil)
	_, err = NewClient(srv2.URL).Read(context.Background(), "s", "e", nil, nil)
	if e := err.(*Error); e.StatusCode != 502 || e.Message != "bad gateway" {
		t.Fatalf("%#v", e)
	}
	srv3, _ := server(t, 200, `{"success":false,"error":{"code":"c","message":"nope"}}`, nil)
	_, err = NewClient(srv3.URL).Read(context.Background(), "s", "e", nil, nil)
	if e := err.(*Error); e.Message != "nope" {
		t.Fatalf("%#v", e)
	}
}
