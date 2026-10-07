package modelregistry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// ModelInfo is human/AI-facing documentation for a registered model: what it
// is for and what its columns mean. It is optional and has no effect on
// permissions or queries.
type ModelInfo struct {
	// Description says what the model/table holds.
	Description string `json:"description,omitempty"`
	// Purpose says why it exists / when an agent should use it.
	Purpose string `json:"purpose,omitempty"`
	// Tags are free-form labels (e.g. "billing", "pii").
	Tags []string `json:"tags,omitempty"`
	// Columns maps a JSON column name to its description.
	Columns map[string]string `json:"columns,omitempty"`
}

// IsZero reports whether the info carries no documentation.
func (i ModelInfo) IsZero() bool {
	return i.Description == "" && i.Purpose == "" && len(i.Tags) == 0 && len(i.Columns) == 0
}

// Describer can be implemented by a model to document itself. It is the
// fallback used when no ModelInfo description was registered or loaded.
// (The method is not called Description so models may keep a Description field.)
type Describer interface {
	ModelDescription() string
}

// commentTagKeys are the standalone struct tags read as a column description,
// in priority order.
var commentTagKeys = []string{"comment", "note", "desc", "description"}

// FieldComment returns the description of a struct field from its tags. Order:
// standalone comment/note/desc/description tags, then a "comment:" entry inside
// the gorm tag (semicolon separated), then inside the bun tag (comma separated).
// It returns "" when the field carries none.
func FieldComment(sf reflect.StructField) string {
	for _, key := range commentTagKeys {
		if v := strings.TrimSpace(sf.Tag.Get(key)); v != "" {
			return v
		}
	}
	if v := tagOption(sf.Tag.Get("gorm"), ';', "comment:"); v != "" {
		return v
	}
	return tagOption(sf.Tag.Get("bun"), ',', "comment:")
}

func tagOption(tag string, sep byte, key string) string {
	for _, part := range strings.Split(tag, string(sep)) {
		part = strings.TrimSpace(part)
		if len(part) >= len(key) && strings.EqualFold(part[:len(key)], key) {
			return strings.Trim(strings.TrimSpace(part[len(key):]), `'"`)
		}
	}
	return ""
}

// SetModelInfo stores documentation for a model name ("schema.entity"). The
// model does not have to be registered yet, so descriptions can be loaded
// before or after registration. Any previous info for the name is replaced.
func (r *DefaultModelRegistry) SetModelInfo(name string, info ModelInfo) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.info == nil {
		r.info = make(map[string]ModelInfo)
	}
	r.info[name] = cloneInfo(info)
}

// GetModelInfo returns the documentation stored with SetModelInfo (or loaded
// from a descriptions file), without any fallback.
func (r *DefaultModelRegistry) GetModelInfo(name string) (ModelInfo, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	info, ok := r.info[name]
	return cloneInfo(info), ok
}

// RegisterModelWithInfo registers a model together with its documentation.
func (r *DefaultModelRegistry) RegisterModelWithInfo(name string, model interface{}, info ModelInfo) error {
	if err := r.RegisterModel(name, model); err != nil {
		return err
	}
	r.SetModelInfo(name, info)
	return nil
}

// ResolveModelInfo returns the effective documentation for a registered model.
// Stored/loaded info wins; an empty Description falls back to the model's
// Describer. Column descriptions are not resolved here: use the stored map and
// fall back to FieldComment per field.
func (r *DefaultModelRegistry) ResolveModelInfo(name string) ModelInfo {
	info, _ := r.GetModelInfo(name)
	if info.Description == "" {
		if model, err := r.GetModel(name); err == nil {
			info.Description = describerText(model)
		}
	}
	return info
}

func describerText(model interface{}) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	if d, ok := model.(Describer); ok {
		return strings.TrimSpace(d.ModelDescription())
	}
	if t := reflect.TypeOf(model); t != nil && t.Kind() != reflect.Pointer {
		if d, ok := reflect.New(t).Interface().(Describer); ok {
			return strings.TrimSpace(d.ModelDescription())
		}
	}
	return ""
}

// LoadModelInfo reads an external descriptions map from r and applies it. The
// JSON is an object keyed by model name:
//
//	{"public.users": {"description": "...", "purpose": "...", "tags": ["x"],
//	                  "columns": {"email": "Login address"}}}
//
// Entries replace any existing info for the same name and take precedence over
// the model's Describer and its struct-tag comments. It returns the number of
// models loaded.
func (r *DefaultModelRegistry) LoadModelInfo(src io.Reader) (int, error) {
	var m map[string]ModelInfo
	dec := json.NewDecoder(src)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return 0, fmt.Errorf("modelregistry: decode model info: %w", err)
	}
	for name, info := range m {
		r.SetModelInfo(name, info)
	}
	return len(m), nil
}

// LoadModelInfoFile is LoadModelInfo reading from a JSON file.
func (r *DefaultModelRegistry) LoadModelInfoFile(path string) (int, error) {
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // operator-supplied descriptions file
	if err != nil {
		return 0, fmt.Errorf("modelregistry: %w", err)
	}
	defer f.Close()
	return r.LoadModelInfo(f)
}

func cloneInfo(in ModelInfo) ModelInfo {
	out := in
	out.Tags = append([]string(nil), in.Tags...)
	if in.Columns != nil {
		out.Columns = make(map[string]string, len(in.Columns))
		for k, v := range in.Columns {
			out.Columns[k] = v
		}
	}
	return out
}
