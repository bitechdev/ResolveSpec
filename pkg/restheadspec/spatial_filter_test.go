package restheadspec

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestBuildFilterCondition_Spatial(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		name      string
		filter    common.FilterOption
		wantCond  string
		wantCount int
	}{
		{
			name: "st_dwithin",
			filter: common.FilterOption{
				Column:   "geom",
				Operator: "st_dwithin",
				Value: map[string]interface{}{
					"geom":     "SRID=4326;POINT(0 0)",
					"distance": 1000.0,
				},
			},
			wantCond:  "ST_DWithin(geom, ST_GeomFromEWKT(?), ?)",
			wantCount: 2,
		},
		{
			name: "st_intersects",
			filter: common.FilterOption{
				Column:   "geom",
				Operator: "st_intersects",
				Value:    "SRID=4326;POINT(0 0)",
			},
			wantCond:  "ST_Intersects(geom, ST_GeomFromEWKT(?))",
			wantCount: 1,
		},
		{
			name: "l2_within",
			filter: common.FilterOption{
				Column:   "embedding",
				Operator: "l2_within",
				Value: map[string]interface{}{
					"vector":   []interface{}{1.0, 2.0},
					"distance": 0.3,
				},
			},
			wantCond:  "embedding <-> ? < ?",
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.filter
			cond, args := h.buildFilterCondition(f.Column, &f, "")
			if cond != tt.wantCond {
				t.Errorf("cond = %q, want %q", cond, tt.wantCond)
			}
			if len(args) != tt.wantCount {
				t.Errorf("args = %d, want %d", len(args), tt.wantCount)
			}
		})
	}
}

func TestParseGeoFilter(t *testing.T) {
	h := &Handler{}
	options := &ExtendedRequestOptions{}

	h.parseGeoFilter(options, "x-spatialfilter-geom", "x-spatialfilter-",
		`{"op":"st_dwithin","geom":"SRID=4326;POINT(0 0)","distance":500}`)

	if len(options.Filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(options.Filters))
	}
	f := options.Filters[0]
	if f.Column != "geom" || f.Operator != "st_dwithin" {
		t.Errorf("filter = %+v", f)
	}
	m, ok := f.Value.(map[string]interface{})
	if !ok || m["distance"] != float64(500) {
		t.Errorf("value = %v", f.Value)
	}
	if _, has := m["op"]; has {
		t.Error("op should be stripped from value map")
	}
}

func TestParseGeoFilter_ExplicitValue(t *testing.T) {
	h := &Handler{}
	options := &ExtendedRequestOptions{}

	h.parseGeoFilter(options, "x-vectorfilter-embedding", "x-vectorfilter-",
		`{"op":"cosine_within","logic":"or","value":{"vector":[1,2,3],"distance":0.2}}`)

	if len(options.Filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(options.Filters))
	}
	f := options.Filters[0]
	if f.Operator != "cosine_within" || f.LogicOperator != "OR" {
		t.Errorf("filter = %+v", f)
	}
	if _, ok := f.Value.(map[string]interface{}); !ok {
		t.Errorf("value type = %T", f.Value)
	}
}

func TestParseFloat32List(t *testing.T) {
	got := parseFloat32List("[1,2.5,3]")
	if len(got) != 3 || got[1] != 2.5 {
		t.Errorf("json array = %v", got)
	}
	got = parseFloat32List("1, 2, 3")
	if len(got) != 3 || got[2] != 3 {
		t.Errorf("csv = %v", got)
	}
	if parseFloat32List("") != nil {
		t.Error("empty should be nil")
	}
}
