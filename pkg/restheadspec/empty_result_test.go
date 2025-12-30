package restheadspec

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// Test that normalizeResultArray returns empty array when no records found without ID
func TestNormalizeResultArray_EmptyArrayWhenNoID(t *testing.T) {
	handler := &Handler{}

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "nil should return empty array",
			input:    nil,
			expected: []interface{}{},
		},
		{
			name:     "empty slice should return empty array",
			input:    []*EmptyTestModel{},
			expected: []interface{}{},
		},
		{
			name:     "single element should return the element",
			input:    []*EmptyTestModel{{ID: 1, Name: "test"}},
			expected: &EmptyTestModel{ID: 1, Name: "test"},
		},
		{
			name: "multiple elements should return the slice",
			input: []*EmptyTestModel{
				{ID: 1, Name: "test1"},
				{ID: 2, Name: "test2"},
			},
			expected: []*EmptyTestModel{
				{ID: 1, Name: "test1"},
				{ID: 2, Name: "test2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.normalizeResultArray(tt.input)

			// For nil and empty cases, check it returns an empty array
			if tt.input == nil || (tt.name == "empty slice should return empty array") {
				emptyArr, ok := result.([]interface{})
				if !ok {
					t.Errorf("Expected empty array []interface{}{}, got %T: %v", result, result)
					return
				}
				if len(emptyArr) != 0 {
					t.Errorf("Expected empty array with length 0, got length %d", len(emptyArr))
				}
				
				// Verify it serializes to [] and not null
				jsonBytes, err := json.Marshal(result)
				if err != nil {
					t.Errorf("Failed to marshal result: %v", err)
					return
				}
				if string(jsonBytes) != "[]" {
					t.Errorf("Expected JSON '[]', got '%s'", string(jsonBytes))
				}
			}
		})
	}
}

// Test that sendFormattedResponse adds X-No-Data-Found header
func TestSendFormattedResponse_NoDataFoundHeader(t *testing.T) {
	handler := &Handler{}

	// Mock ResponseWriter
	mockWriter := &MockTestResponseWriter{
		headers: make(map[string]string),
	}

	metadata := &common.Metadata{
		Total:    0,
		Count:    0,
		Filtered: 0,
		Limit:    10,
		Offset:   0,
	}

	options := ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{},
	}

	// Test with empty data
	emptyData := []interface{}{}
	handler.sendFormattedResponse(mockWriter, emptyData, metadata, options)

	// Check if X-No-Data-Found header was set
	if mockWriter.headers["X-No-Data-Found"] != "true" {
		t.Errorf("Expected X-No-Data-Found header to be 'true', got '%s'", mockWriter.headers["X-No-Data-Found"])
	}

	// Verify the body is an empty array
	if mockWriter.body == nil {
		t.Error("Expected body to be set, got nil")
	} else {
		bodyBytes, err := json.Marshal(mockWriter.body)
		if err != nil {
			t.Errorf("Failed to marshal body: %v", err)
		}
		// The body should be wrapped in a Response object with "data" field
		bodyStr := string(bodyBytes)
		if !strings.Contains(bodyStr, `"data":[]`) && !strings.Contains(bodyStr, `"result":[]`) {
			t.Errorf("Expected body to contain empty array, got: %s", bodyStr)
		}
	}
}

// Test that sendResponseWithOptions adds X-No-Data-Found header
func TestSendResponseWithOptions_NoDataFoundHeader(t *testing.T) {
	handler := &Handler{}

	// Mock ResponseWriter
	mockWriter := &MockTestResponseWriter{
		headers: make(map[string]string),
	}

	metadata := &common.Metadata{}
	options := &ExtendedRequestOptions{}

	// Test with nil data
	handler.sendResponseWithOptions(mockWriter, nil, metadata, options)

	// Check if X-No-Data-Found header was set
	if mockWriter.headers["X-No-Data-Found"] != "true" {
		t.Errorf("Expected X-No-Data-Found header to be 'true', got '%s'", mockWriter.headers["X-No-Data-Found"])
	}

	// Check status code is 200
	if mockWriter.statusCode != 200 {
		t.Errorf("Expected status code 200, got %d", mockWriter.statusCode)
	}

	// Verify the body is an empty array
	if mockWriter.body == nil {
		t.Error("Expected body to be set, got nil")
	} else {
		bodyBytes, err := json.Marshal(mockWriter.body)
		if err != nil {
			t.Errorf("Failed to marshal body: %v", err)
		}
		bodyStr := string(bodyBytes)
		if bodyStr != "[]" {
			t.Errorf("Expected body to be '[]', got: %s", bodyStr)
		}
	}
}

// MockTestResponseWriter for testing
type MockTestResponseWriter struct {
	headers    map[string]string
	statusCode int
	body       interface{}
}

func (m *MockTestResponseWriter) SetHeader(key, value string) {
	m.headers[key] = value
}

func (m *MockTestResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

func (m *MockTestResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (m *MockTestResponseWriter) WriteJSON(data interface{}) error {
	m.body = data
	return nil
}

func (m *MockTestResponseWriter) UnderlyingResponseWriter() http.ResponseWriter {
	return nil
}

// EmptyTestModel for testing
type EmptyTestModel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
