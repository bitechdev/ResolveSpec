package common

import (
	"fmt"
	"reflect"
)

// ValidateAndUnwrapModelResult contains the result of model validation
type ValidateAndUnwrapModelResult struct {
	ModelType    reflect.Type
	Model        interface{}
	ModelPtr     interface{}
	OriginalType reflect.Type
}

// ValidateAndUnwrapModel validates that a model is a struct type and unwraps
// pointers, slices, and arrays to get to the base struct type.
// Returns an error if the model is not a valid struct type.
func ValidateAndUnwrapModel(model interface{}) (*ValidateAndUnwrapModelResult, error) {
	modelType := reflect.TypeOf(model)
	originalType := modelType

	// Unwrap pointers, slices, and arrays to get to the base struct type
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model must be a struct type, got %v. Ensure you register the struct (e.g., ModelCoreAccount{}) not a slice (e.g., []*ModelCoreAccount)", originalType)
	}

	// If the registered model was a pointer or slice, use the unwrapped struct type
	if originalType != modelType {
		model = reflect.New(modelType).Elem().Interface()
	}

	// Create a pointer to the model type for database operations
	modelPtr := reflect.New(reflect.TypeOf(model)).Interface()

	return &ValidateAndUnwrapModelResult{
		ModelType:    modelType,
		Model:        model,
		ModelPtr:     modelPtr,
		OriginalType: originalType,
	}, nil
}
