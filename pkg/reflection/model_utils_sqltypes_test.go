package reflection_test

import (
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

func TestMapToStruct_SqlJSONB_PreservesDriverValuer(t *testing.T) {
	// Test that SqlJSONB type preserves driver.Valuer interface
	type TestModel struct {
		ID   int64           `bun:"id,pk" json:"id"`
		Meta spectypes.SqlJSONB `bun:"meta" json:"meta"`
	}

	dataMap := map[string]interface{}{
		"id": int64(123),
		"meta": map[string]interface{}{
			"key": "value",
			"num": 42,
		},
	}

	var result TestModel
	err := reflection.MapToStruct(dataMap, &result)
	if err != nil {
		t.Fatalf("MapToStruct() error = %v", err)
	}

	// Verify the field was set
	if result.ID != 123 {
		t.Errorf("ID = %v, want 123", result.ID)
	}

	// Verify SqlJSONB was populated
	if len(result.Meta) == 0 {
		t.Error("Meta is empty, want non-empty")
	}

	// Most importantly: verify driver.Valuer interface works
	value, err := result.Meta.Value()
	if err != nil {
		t.Errorf("Meta.Value() error = %v, want nil", err)
	}

	// Value should return a string representation of the JSON
	if value == nil {
		t.Error("Meta.Value() returned nil, want non-nil")
	}

	// Check it's a valid JSON string
	if str, ok := value.(string); ok {
		if len(str) == 0 {
			t.Error("Meta.Value() returned empty string, want valid JSON")
		}
		t.Logf("SqlJSONB.Value() returned: %s", str)
	} else {
		t.Errorf("Meta.Value() returned type %T, want string", value)
	}
}

func TestMapToStruct_SqlJSONB_FromBytes(t *testing.T) {
	// Test that SqlJSONB can be set from []byte directly
	type TestModel struct {
		ID   int64           `bun:"id,pk" json:"id"`
		Meta spectypes.SqlJSONB `bun:"meta" json:"meta"`
	}

	jsonBytes := []byte(`{"direct":"bytes"}`)
	dataMap := map[string]interface{}{
		"id":   int64(456),
		"meta": jsonBytes,
	}

	var result TestModel
	err := reflection.MapToStruct(dataMap, &result)
	if err != nil {
		t.Fatalf("MapToStruct() error = %v", err)
	}

	if result.ID != 456 {
		t.Errorf("ID = %v, want 456", result.ID)
	}

	if string(result.Meta) != string(jsonBytes) {
		t.Errorf("Meta = %s, want %s", string(result.Meta), string(jsonBytes))
	}

	// Verify driver.Valuer works
	value, err := result.Meta.Value()
	if err != nil {
		t.Errorf("Meta.Value() error = %v", err)
	}
	if value == nil {
		t.Error("Meta.Value() returned nil")
	}
}

func TestMapToStruct_AllSqlTypes(t *testing.T) {
	// Test model with all SQL custom types
	type TestModel struct {
		ID        int64               `bun:"id,pk" json:"id"`
		Name      string              `bun:"name" json:"name"`
		CreatedAt spectypes.SqlTimeStamp `bun:"created_at" json:"created_at"`
		BirthDate spectypes.SqlDate      `bun:"birth_date" json:"birth_date"`
		LoginTime spectypes.SqlTime      `bun:"login_time" json:"login_time"`
		Meta      spectypes.SqlJSONB     `bun:"meta" json:"meta"`
		Tags      spectypes.SqlJSONB     `bun:"tags" json:"tags"`
	}

	now := time.Now()
	birthDate := time.Date(1990, 1, 15, 0, 0, 0, 0, time.UTC)
	loginTime := time.Date(0, 1, 1, 14, 30, 0, 0, time.UTC)

	dataMap := map[string]interface{}{
		"id":         int64(100),
		"name":       "Test User",
		"created_at": now,
		"birth_date": birthDate,
		"login_time": loginTime,
		"meta": map[string]interface{}{
			"role":   "admin",
			"active": true,
		},
		"tags": []interface{}{"golang", "testing", "sql"},
	}

	var result TestModel
	err := reflection.MapToStruct(dataMap, &result)
	if err != nil {
		t.Fatalf("MapToStruct() error = %v", err)
	}

	// Verify basic fields
	if result.ID != 100 {
		t.Errorf("ID = %v, want 100", result.ID)
	}
	if result.Name != "Test User" {
		t.Errorf("Name = %v, want 'Test User'", result.Name)
	}

	// Verify SqlTimeStamp
	if !result.CreatedAt.Valid {
		t.Error("CreatedAt.Valid = false, want true")
	}
	if !result.CreatedAt.Val.Equal(now) {
		t.Errorf("CreatedAt.Val = %v, want %v", result.CreatedAt.Val, now)
	}

	// Verify driver.Valuer for SqlTimeStamp
	tsValue, err := result.CreatedAt.Value()
	if err != nil {
		t.Errorf("CreatedAt.Value() error = %v", err)
	}
	if tsValue == nil {
		t.Error("CreatedAt.Value() returned nil")
	}

	// Verify SqlDate
	if !result.BirthDate.Valid {
		t.Error("BirthDate.Valid = false, want true")
	}
	if !result.BirthDate.Val.Equal(birthDate) {
		t.Errorf("BirthDate.Val = %v, want %v", result.BirthDate.Val, birthDate)
	}

	// Verify driver.Valuer for SqlDate
	dateValue, err := result.BirthDate.Value()
	if err != nil {
		t.Errorf("BirthDate.Value() error = %v", err)
	}
	if dateValue == nil {
		t.Error("BirthDate.Value() returned nil")
	}

	// Verify SqlTime
	if !result.LoginTime.Valid {
		t.Error("LoginTime.Valid = false, want true")
	}

	// Verify driver.Valuer for SqlTime
	timeValue, err := result.LoginTime.Value()
	if err != nil {
		t.Errorf("LoginTime.Value() error = %v", err)
	}
	if timeValue == nil {
		t.Error("LoginTime.Value() returned nil")
	}

	// Verify SqlJSONB for Meta
	if len(result.Meta) == 0 {
		t.Error("Meta is empty")
	}
	metaValue, err := result.Meta.Value()
	if err != nil {
		t.Errorf("Meta.Value() error = %v", err)
	}
	if metaValue == nil {
		t.Error("Meta.Value() returned nil")
	}

	// Verify SqlJSONB for Tags
	if len(result.Tags) == 0 {
		t.Error("Tags is empty")
	}
	tagsValue, err := result.Tags.Value()
	if err != nil {
		t.Errorf("Tags.Value() error = %v", err)
	}
	if tagsValue == nil {
		t.Error("Tags.Value() returned nil")
	}

	t.Logf("All SQL types successfully preserved driver.Valuer interface:")
	t.Logf("  - SqlTimeStamp: %v", tsValue)
	t.Logf("  - SqlDate: %v", dateValue)
	t.Logf("  - SqlTime: %v", timeValue)
	t.Logf("  - SqlJSONB (Meta): %v", metaValue)
	t.Logf("  - SqlJSONB (Tags): %v", tagsValue)
}

func TestMapToStruct_SqlNull_NilValues(t *testing.T) {
	// Test that SqlNull types handle nil values correctly
	type TestModel struct {
		ID        int64               `bun:"id,pk" json:"id"`
		UpdatedAt spectypes.SqlTimeStamp `bun:"updated_at" json:"updated_at"`
		DeletedAt spectypes.SqlTimeStamp `bun:"deleted_at" json:"deleted_at"`
	}

	now := time.Now()
	dataMap := map[string]interface{}{
		"id":         int64(200),
		"updated_at": now,
		"deleted_at": nil, // Explicitly nil
	}

	var result TestModel
	err := reflection.MapToStruct(dataMap, &result)
	if err != nil {
		t.Fatalf("MapToStruct() error = %v", err)
	}

	// UpdatedAt should be valid
	if !result.UpdatedAt.Valid {
		t.Error("UpdatedAt.Valid = false, want true")
	}
	if !result.UpdatedAt.Val.Equal(now) {
		t.Errorf("UpdatedAt.Val = %v, want %v", result.UpdatedAt.Val, now)
	}

	// DeletedAt should be invalid (null)
	if result.DeletedAt.Valid {
		t.Error("DeletedAt.Valid = true, want false (null)")
	}

	// Verify driver.Valuer for null SqlTimeStamp
	deletedValue, err := result.DeletedAt.Value()
	if err != nil {
		t.Errorf("DeletedAt.Value() error = %v", err)
	}
	if deletedValue != nil {
		t.Errorf("DeletedAt.Value() = %v, want nil", deletedValue)
	}
}
