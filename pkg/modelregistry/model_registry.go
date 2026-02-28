package modelregistry

import (
	"fmt"
	"reflect"
	"sync"
)

// ModelRules defines the permissions and security settings for a model
type ModelRules struct {
	CanPublicRead    bool // Whether the model can be read (GET operations)
	CanPublicUpdate  bool // Whether the model can be updated (PUT/PATCH operations)
	CanRead          bool // Whether the model can be read (GET operations)
	CanUpdate        bool // Whether the model can be updated (PUT/PATCH operations)
	CanCreate        bool // Whether the model can be created (POST operations)
	CanDelete        bool // Whether the model can be deleted (DELETE operations)
	SecurityDisabled bool // Whether security checks are disabled for this model
}

// DefaultModelRules returns the default rules for a model (all operations allowed, security enabled)
func DefaultModelRules() ModelRules {
	return ModelRules{
		CanRead:          true,
		CanUpdate:        true,
		CanCreate:        true,
		CanDelete:        true,
		CanPublicRead:    false,
		CanPublicUpdate:  false,
		SecurityDisabled: false,
	}
}

// DefaultModelRegistry implements ModelRegistry interface
type DefaultModelRegistry struct {
	models map[string]interface{}
	rules  map[string]ModelRules
	mutex  sync.RWMutex
}

// Global default registry instance
var defaultRegistry = &DefaultModelRegistry{
	models: make(map[string]interface{}),
	rules:  make(map[string]ModelRules),
}

// Global list of registries (searched in order)
var registries = []*DefaultModelRegistry{defaultRegistry}
var registriesMutex sync.RWMutex

// NewModelRegistry creates a new model registry
func NewModelRegistry() *DefaultModelRegistry {
	return &DefaultModelRegistry{
		models: make(map[string]interface{}),
		rules:  make(map[string]ModelRules),
	}
}

func GetDefaultRegistry() *DefaultModelRegistry {
	return defaultRegistry
}

func SetDefaultRegistry(registry *DefaultModelRegistry) {
	registriesMutex.Lock()
	defer registriesMutex.Unlock()

	foundAt := -1
	for idx, r := range registries {
		if r == defaultRegistry {
			foundAt = idx
			break
		}
	}
	defaultRegistry = registry
	if foundAt >= 0 {
		registries[foundAt] = registry
	} else {
		registries = append([]*DefaultModelRegistry{registry}, registries...)
	}
}

// AddRegistry adds a registry to the global list of registries
// Registries are searched in the order they were added
func AddRegistry(registry *DefaultModelRegistry) {
	registriesMutex.Lock()
	defer registriesMutex.Unlock()
	registries = append(registries, registry)
}

func (r *DefaultModelRegistry) RegisterModel(name string, model interface{}) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.models[name]; exists {
		return fmt.Errorf("model %s already registered", name)
	}

	// Validate that model is a non-pointer struct
	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return fmt.Errorf("model cannot be nil")
	}

	originalType := modelType

	// Unwrap pointers, slices, and arrays to check the underlying type
	for modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array {
		modelType = modelType.Elem()
	}

	// Validate that the underlying type is a struct
	if modelType.Kind() != reflect.Struct {
		return fmt.Errorf("model must be a struct or pointer to struct, got %s", originalType.String())
	}

	// If a pointer/slice/array was passed, unwrap to the base struct
	if originalType != modelType {
		// Create a zero value of the struct type
		model = reflect.New(modelType).Elem().Interface()
	}

	// Additional check: ensure model is not a pointer
	finalType := reflect.TypeOf(model)
	if finalType.Kind() == reflect.Ptr {
		return fmt.Errorf("model must be a non-pointer struct, got pointer to %s. Use MyModel{} instead of &MyModel{}", finalType.Elem().Name())
	}

	r.models[name] = model
	// Initialize with default rules if not already set
	if _, exists := r.rules[name]; !exists {
		r.rules[name] = DefaultModelRules()
	}
	return nil
}

func (r *DefaultModelRegistry) GetModel(name string) (interface{}, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	model, exists := r.models[name]
	if !exists {
		return nil, fmt.Errorf("model %s not found", name)
	}

	return model, nil
}

func (r *DefaultModelRegistry) GetAllModels() map[string]interface{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make(map[string]interface{})
	for k, v := range r.models {
		result[k] = v
	}
	return result
}

func (r *DefaultModelRegistry) GetModelByEntity(schema, entity string) (interface{}, error) {
	// Try full name first
	fullName := fmt.Sprintf("%s.%s", schema, entity)
	if model, err := r.GetModel(fullName); err == nil {
		return model, nil
	}

	// Fallback to entity name only
	return r.GetModel(entity)
}

// SetModelRules sets the rules for a specific model
func (r *DefaultModelRegistry) SetModelRules(name string, rules ModelRules) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Check if model exists
	if _, exists := r.models[name]; !exists {
		return fmt.Errorf("model %s not found", name)
	}

	r.rules[name] = rules
	return nil
}

// GetModelRules retrieves the rules for a specific model
// Returns default rules if model exists but rules are not set
func (r *DefaultModelRegistry) GetModelRules(name string) (ModelRules, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Check if model exists
	if _, exists := r.models[name]; !exists {
		return ModelRules{}, fmt.Errorf("model %s not found", name)
	}

	// Return rules if set, otherwise return default rules
	if rules, exists := r.rules[name]; exists {
		return rules, nil
	}

	return DefaultModelRules(), nil
}

// RegisterModelWithRules registers a model with specific rules
func (r *DefaultModelRegistry) RegisterModelWithRules(name string, model interface{}, rules ModelRules) error {
	// First register the model
	if err := r.RegisterModel(name, model); err != nil {
		return err
	}

	// Then set the rules (we need to lock again for rules)
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.rules[name] = rules

	return nil
}

// Global convenience functions using the default registry

// RegisterModel registers a model with the default global registry
func RegisterModel(model interface{}, name string) error {
	return defaultRegistry.RegisterModel(name, model)
}

// GetModelByName retrieves a model by searching through all registries in order
// Returns the first match found
func GetModelByName(name string) (interface{}, error) {
	registriesMutex.RLock()
	defer registriesMutex.RUnlock()

	for _, registry := range registries {
		if model, err := registry.GetModel(name); err == nil {
			return model, nil
		}
	}

	return nil, fmt.Errorf("model %s not found in any registry", name)
}

// IterateModels iterates over all models in the default global registry
func IterateModels(fn func(name string, model interface{})) {
	defaultRegistry.mutex.RLock()
	defer defaultRegistry.mutex.RUnlock()

	for name, model := range defaultRegistry.models {
		fn(name, model)
	}
}

// GetModels returns a list of all models from all registries
// Models are collected in registry order, with duplicates included
func GetModels() []interface{} {
	registriesMutex.RLock()
	defer registriesMutex.RUnlock()

	var models []interface{}
	seen := make(map[string]bool)

	for _, registry := range registries {
		registry.mutex.RLock()
		for name, model := range registry.models {
			// Only add the first occurrence of each model name
			if !seen[name] {
				models = append(models, model)
				seen[name] = true
			}
		}
		registry.mutex.RUnlock()
	}

	return models
}

// SetModelRules sets the rules for a specific model in the default registry
func SetModelRules(name string, rules ModelRules) error {
	return defaultRegistry.SetModelRules(name, rules)
}

// GetModelRules retrieves the rules for a specific model from the default registry
func GetModelRules(name string) (ModelRules, error) {
	return defaultRegistry.GetModelRules(name)
}

// GetModelRulesByName retrieves the rules for a model by searching through all registries in order
// Returns the first match found
func GetModelRulesByName(name string) (ModelRules, error) {
	registriesMutex.RLock()
	defer registriesMutex.RUnlock()

	for _, registry := range registries {
		if _, err := registry.GetModel(name); err == nil {
			// Model found in this registry, get its rules
			return registry.GetModelRules(name)
		}
	}

	return ModelRules{}, fmt.Errorf("model %s not found in any registry", name)
}

// RegisterModelWithRules registers a model with specific rules in the default registry
func RegisterModelWithRules(model interface{}, name string, rules ModelRules) error {
	return defaultRegistry.RegisterModelWithRules(name, model, rules)
}
