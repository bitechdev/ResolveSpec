package modelregistry

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// ModelRules defines the permissions and security settings for a model
type ModelRules struct {
	CanPublicRead    bool // Whether the model can be read (GET operations)
	CanPublicUpdate  bool // Whether the model can be updated (PUT/PATCH operations)
	CanPublicCreate  bool // Whether the model can be created (POST operations)
	CanPublicDelete  bool // Whether the model can be deleted (DELETE operations)
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
		CanPublicCreate:  false,
		CanPublicDelete:  false,
		SecurityDisabled: false,
	}
}

// DefaultModelRegistry implements ModelRegistry interface
type DefaultModelRegistry struct {
	models map[string]interface{}
	rules  map[string]ModelRules
	info   map[string]ModelInfo
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

// Sentinel errors so callers (notably the security layer) can distinguish
// "not registered" from every other failure with errors.Is.
var (
	ErrModelNotFound = errors.New("model not found")
	ErrModelExists   = errors.New("model already registered")
	ErrInvalidModel  = errors.New("invalid model")
)

// maxUnwrapDepth bounds pointer/slice/array unwrapping so a recursive type
// (type T *T) cannot spin forever.
const maxUnwrapDepth = 16

// Lock ordering: registriesMutex is always taken before a registry's mutex,
// never the reverse. No caller-supplied code ever runs while a lock is held.

// NewModelRegistry creates a new model registry
func NewModelRegistry() *DefaultModelRegistry {
	return &DefaultModelRegistry{
		models: make(map[string]interface{}),
		rules:  make(map[string]ModelRules),
	}
}

// GetDefaultRegistry returns the current default registry.
func GetDefaultRegistry() *DefaultModelRegistry {
	registriesMutex.RLock()
	defer registriesMutex.RUnlock()
	return defaultRegistry
}

// SetDefaultRegistry replaces the default registry. A nil registry is ignored.
func SetDefaultRegistry(registry *DefaultModelRegistry) {
	if registry == nil {
		return
	}
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

// registriesSnapshot returns a copy of the registry list so callers can
// iterate without holding registriesMutex.
func registriesSnapshot() []*DefaultModelRegistry {
	registriesMutex.RLock()
	defer registriesMutex.RUnlock()
	return append([]*DefaultModelRegistry(nil), registries...)
}

// validateModel checks the model is a struct (or pointer/slice/array of one)
// and returns the normalised non-pointer struct value. It takes no locks.
func validateModel(model interface{}) (result interface{}, err error) {
	// Reflection on a pathological type must fail the registration, not crash the process.
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("%w: %v", ErrInvalidModel, logger.HandlePanic("modelregistry.validateModel", r))
		}
	}()

	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return nil, fmt.Errorf("%w: model cannot be nil", ErrInvalidModel)
	}

	originalType := modelType

	// Unwrap pointers, slices, and arrays to check the underlying type
	for depth := 0; modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array; depth++ {
		if depth >= maxUnwrapDepth {
			return nil, fmt.Errorf("%w: type %s nests deeper than %d levels", ErrInvalidModel, originalType.String(), maxUnwrapDepth)
		}
		modelType = modelType.Elem()
	}

	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: model must be a struct or pointer to struct, got %s", ErrInvalidModel, originalType.String())
	}

	// If a pointer/slice/array was passed, unwrap to the base struct
	if originalType != modelType {
		model = reflect.New(modelType).Elem().Interface()
	}

	if finalType := reflect.TypeOf(model); finalType.Kind() == reflect.Pointer {
		return nil, fmt.Errorf("%w: model must be a non-pointer struct, got pointer to %s. Use MyModel{} instead of &MyModel{}", ErrInvalidModel, finalType.Elem().Name())
	}
	return model, nil
}

// registerLocked validates the model outside the lock, then writes the model
// and its rules under a single lock acquisition so no reader can observe the
// model without its final rules.
func (r *DefaultModelRegistry) registerLocked(name string, model interface{}, rules ModelRules) error {
	model, err := validateModel(model)
	if err != nil {
		return err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.models[name]; exists {
		return fmt.Errorf("%w: %s", ErrModelExists, name)
	}
	r.models[name] = model
	r.rules[name] = rules
	return nil
}

func (r *DefaultModelRegistry) RegisterModel(name string, model interface{}) error {
	return r.registerLocked(name, model, DefaultModelRules())
}

func (r *DefaultModelRegistry) GetModel(name string) (interface{}, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	model, exists := r.models[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrModelNotFound, name)
	}

	return model, nil
}

func (r *DefaultModelRegistry) GetAllModels() map[string]interface{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make(map[string]interface{}, len(r.models))
	for k, v := range r.models {
		result[k] = v
	}
	return result
}

func (r *DefaultModelRegistry) GetModelByEntity(schema, entity string) (interface{}, error) {
	// Try full name first
	fullName := fmt.Sprintf("%s.%s", schema, entity)
	model, err := r.GetModel(fullName)
	if err == nil {
		return model, nil
	}
	if !errors.Is(err, ErrModelNotFound) {
		return nil, err
	}

	// Fallback to entity name only
	return r.GetModel(entity)
}

// SetModelRules sets the rules for a specific model
func (r *DefaultModelRegistry) SetModelRules(name string, rules ModelRules) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.models[name]; !exists {
		return fmt.Errorf("%w: %s", ErrModelNotFound, name)
	}

	r.rules[name] = rules
	return nil
}

// GetModelRules retrieves the rules for a specific model
// Returns default rules if model exists but rules are not set
func (r *DefaultModelRegistry) GetModelRules(name string) (ModelRules, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if _, exists := r.models[name]; !exists {
		return ModelRules{}, fmt.Errorf("%w: %s", ErrModelNotFound, name)
	}

	if rules, exists := r.rules[name]; exists {
		return rules, nil
	}

	return DefaultModelRules(), nil
}

// RegisterModelWithRules registers a model with specific rules atomically
func (r *DefaultModelRegistry) RegisterModelWithRules(name string, model interface{}, rules ModelRules) error {
	return r.registerLocked(name, model, rules)
}

// Global convenience functions using the default registry

// RegisterModel registers a model with the default global registry
func RegisterModel(model interface{}, name string) error {
	return GetDefaultRegistry().RegisterModel(name, model)
}

// GetModelByName retrieves a model by searching through all registries in order
// Returns the first match found
func GetModelByName(name string) (interface{}, error) {
	for _, registry := range registriesSnapshot() {
		if model, err := registry.GetModel(name); err == nil {
			return model, nil
		}
	}

	return nil, fmt.Errorf("%w: %s (in any registry)", ErrModelNotFound, name)
}

// IterateModels iterates over all models in the default global registry.
// It iterates over a snapshot, so fn may safely call back into the registry.
// A panic in fn is recovered and logged with the model name, and iteration
// continues with the remaining models.
func IterateModels(fn func(name string, model interface{})) {
	for name, model := range GetDefaultRegistry().GetAllModels() {
		callIsolated(name, model, fn)
	}
}

func callIsolated(name string, model interface{}, fn func(name string, model interface{})) {
	defer func() {
		if r := recover(); r != nil {
			_ = logger.HandlePanic("modelregistry.IterateModels", r, "model", name)
		}
	}()
	fn(name, model)
}

// GetModels returns a list of all models from all registries.
// Only the first occurrence of each model name is included.
func GetModels() []interface{} {
	var models []interface{}
	seen := make(map[string]bool)

	for _, registry := range registriesSnapshot() {
		for name, model := range registry.GetAllModels() {
			if !seen[name] {
				models = append(models, model)
				seen[name] = true
			}
		}
	}

	return models
}

// SetModelRules sets the rules for a specific model in the default registry
func SetModelRules(name string, rules ModelRules) error {
	return GetDefaultRegistry().SetModelRules(name, rules)
}

// GetModelRules retrieves the rules for a specific model from the default registry
func GetModelRules(name string) (ModelRules, error) {
	return GetDefaultRegistry().GetModelRules(name)
}

// GetModelRulesByName retrieves the rules for a model by searching through all registries in order
// Returns the first match found. The error wraps ErrModelNotFound when no registry has the model.
func GetModelRulesByName(name string) (ModelRules, error) {
	for _, registry := range registriesSnapshot() {
		rules, err := registry.GetModelRules(name)
		if err == nil {
			return rules, nil
		}
		if !errors.Is(err, ErrModelNotFound) {
			return ModelRules{}, err
		}
	}

	return ModelRules{}, fmt.Errorf("%w: %s (in any registry)", ErrModelNotFound, name)
}

// RegisterModelWithRules registers a model with specific rules in the default registry
func RegisterModelWithRules(model interface{}, name string, rules ModelRules) error {
	return GetDefaultRegistry().RegisterModelWithRules(name, model, rules)
}
