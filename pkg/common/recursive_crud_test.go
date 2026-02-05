package common

import (
	"context"
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// Mock Database for testing
type mockDatabase struct {
	insertCalls []map[string]interface{}
	updateCalls []map[string]interface{}
	deleteCalls []interface{}
	lastID      int64
}

func newMockDatabase() *mockDatabase {
	return &mockDatabase{
		insertCalls: make([]map[string]interface{}, 0),
		updateCalls: make([]map[string]interface{}, 0),
		deleteCalls: make([]interface{}, 0),
		lastID:      1,
	}
}

func (m *mockDatabase) NewSelect() SelectQuery                       { return &mockSelectQuery{} }
func (m *mockDatabase) NewInsert() InsertQuery                       { return &mockInsertQuery{db: m} }
func (m *mockDatabase) NewUpdate() UpdateQuery                       { return &mockUpdateQuery{db: m} }
func (m *mockDatabase) NewDelete() DeleteQuery                       { return &mockDeleteQuery{db: m} }
func (m *mockDatabase) RunInTransaction(ctx context.Context, fn func(Database) error) error {
	return fn(m)
}
func (m *mockDatabase) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	return &mockResult{rowsAffected: 1}, nil
}
func (m *mockDatabase) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return nil
}
func (m *mockDatabase) BeginTx(ctx context.Context) (Database, error) {
	return m, nil
}
func (m *mockDatabase) CommitTx(ctx context.Context) error {
	return nil
}
func (m *mockDatabase) RollbackTx(ctx context.Context) error {
	return nil
}
func (m *mockDatabase) GetUnderlyingDB() interface{} {
	return nil
}
func (m *mockDatabase) DriverName() string {
	return "postgres"
}

// Mock SelectQuery
type mockSelectQuery struct{}

func (m *mockSelectQuery) Model(model interface{}) SelectQuery                { return m }
func (m *mockSelectQuery) Table(name string) SelectQuery                      { return m }
func (m *mockSelectQuery) Column(columns ...string) SelectQuery               { return m }
func (m *mockSelectQuery) ColumnExpr(query string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) Where(condition string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) WhereOr(query string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) Join(query string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) LeftJoin(query string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) Preload(relation string, conditions ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) PreloadRelation(relation string, apply ...func(SelectQuery) SelectQuery) SelectQuery { return m }
func (m *mockSelectQuery) JoinRelation(relation string, apply ...func(SelectQuery) SelectQuery) SelectQuery { return m }
func (m *mockSelectQuery) Order(order string) SelectQuery                     { return m }
func (m *mockSelectQuery) OrderExpr(order string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) Limit(n int) SelectQuery                            { return m }
func (m *mockSelectQuery) Offset(n int) SelectQuery                           { return m }
func (m *mockSelectQuery) Group(group string) SelectQuery                     { return m }
func (m *mockSelectQuery) Having(condition string, args ...interface{}) SelectQuery { return m }
func (m *mockSelectQuery) Scan(ctx context.Context, dest interface{}) error  { return nil }
func (m *mockSelectQuery) ScanModel(ctx context.Context) error               { return nil }
func (m *mockSelectQuery) Count(ctx context.Context) (int, error)            { return 0, nil }
func (m *mockSelectQuery) Exists(ctx context.Context) (bool, error)          { return false, nil }

// Mock InsertQuery
type mockInsertQuery struct {
	db     *mockDatabase
	table  string
	values map[string]interface{}
}

func (m *mockInsertQuery) Model(model interface{}) InsertQuery { return m }
func (m *mockInsertQuery) Table(name string) InsertQuery {
	m.table = name
	return m
}
func (m *mockInsertQuery) Value(column string, value interface{}) InsertQuery {
	if m.values == nil {
		m.values = make(map[string]interface{})
	}
	m.values[column] = value
	return m
}
func (m *mockInsertQuery) OnConflict(action string) InsertQuery { return m }
func (m *mockInsertQuery) Returning(columns ...string) InsertQuery { return m }
func (m *mockInsertQuery) Exec(ctx context.Context) (Result, error) {
	// Record the insert call
	m.db.insertCalls = append(m.db.insertCalls, m.values)
	m.db.lastID++
	return &mockResult{lastID: m.db.lastID, rowsAffected: 1}, nil
}

// Mock UpdateQuery
type mockUpdateQuery struct {
	db        *mockDatabase
	table     string
	setValues map[string]interface{}
}

func (m *mockUpdateQuery) Model(model interface{}) UpdateQuery { return m }
func (m *mockUpdateQuery) Table(name string) UpdateQuery {
	m.table = name
	return m
}
func (m *mockUpdateQuery) Set(column string, value interface{}) UpdateQuery { return m }
func (m *mockUpdateQuery) SetMap(values map[string]interface{}) UpdateQuery {
	m.setValues = values
	return m
}
func (m *mockUpdateQuery) Where(condition string, args ...interface{}) UpdateQuery { return m }
func (m *mockUpdateQuery) Returning(columns ...string) UpdateQuery { return m }
func (m *mockUpdateQuery) Exec(ctx context.Context) (Result, error) {
	// Record the update call
	m.db.updateCalls = append(m.db.updateCalls, m.setValues)
	return &mockResult{rowsAffected: 1}, nil
}

// Mock DeleteQuery
type mockDeleteQuery struct {
	db    *mockDatabase
	table string
}

func (m *mockDeleteQuery) Model(model interface{}) DeleteQuery { return m }
func (m *mockDeleteQuery) Table(name string) DeleteQuery {
	m.table = name
	return m
}
func (m *mockDeleteQuery) Where(condition string, args ...interface{}) DeleteQuery { return m }
func (m *mockDeleteQuery) Exec(ctx context.Context) (Result, error) {
	// Record the delete call
	m.db.deleteCalls = append(m.db.deleteCalls, m.table)
	return &mockResult{rowsAffected: 1}, nil
}

// Mock Result
type mockResult struct {
	lastID       int64
	rowsAffected int64
}

func (m *mockResult) LastInsertId() (int64, error) { return m.lastID, nil }
func (m *mockResult) RowsAffected() int64          { return m.rowsAffected }

// Mock ModelRegistry
type mockModelRegistry struct{}

func (m *mockModelRegistry) GetModel(name string) (interface{}, error) { return nil, nil }
func (m *mockModelRegistry) GetModelByEntity(schema, entity string) (interface{}, error) { return nil, nil }
func (m *mockModelRegistry) RegisterModel(name string, model interface{}) error { return nil }
func (m *mockModelRegistry) GetAllModels() map[string]interface{} { return make(map[string]interface{}) }

// Mock RelationshipInfoProvider
type mockRelationshipProvider struct {
	relationships map[string]*RelationshipInfo
}

func newMockRelationshipProvider() *mockRelationshipProvider {
	return &mockRelationshipProvider{
		relationships: make(map[string]*RelationshipInfo),
	}
}

func (m *mockRelationshipProvider) GetRelationshipInfo(modelType reflect.Type, relationName string) *RelationshipInfo {
	key := modelType.Name() + "." + relationName
	return m.relationships[key]
}

func (m *mockRelationshipProvider) RegisterRelation(modelTypeName, relationName string, info *RelationshipInfo) {
	key := modelTypeName + "." + relationName
	m.relationships[key] = info
}

// Test Models
type Department struct {
	ID        int64        `json:"id" bun:"id,pk"`
	Name      string       `json:"name"`
	Employees []*Employee  `json:"employees,omitempty"`
}

func (d Department) TableName() string { return "departments" }
func (d Department) GetIDName() string { return "ID" }

type Employee struct {
	ID           int64   `json:"id" bun:"id,pk"`
	Name         string  `json:"name"`
	DepartmentID int64   `json:"department_id"`
	Tasks        []*Task `json:"tasks,omitempty"`
}

func (e Employee) TableName() string { return "employees" }
func (e Employee) GetIDName() string { return "ID" }

type Task struct {
	ID         int64      `json:"id" bun:"id,pk"`
	Title      string     `json:"title"`
	EmployeeID int64      `json:"employee_id"`
	Comments   []*Comment `json:"comments,omitempty"`
}

func (t Task) TableName() string { return "tasks" }
func (t Task) GetIDName() string { return "ID" }

type Comment struct {
	ID      int64  `json:"id" bun:"id,pk"`
	Text    string `json:"text"`
	TaskID  int64  `json:"task_id"`
}

func (c Comment) TableName() string { return "comments" }
func (c Comment) GetIDName() string { return "ID" }

// Test Cases

func TestProcessNestedCUD_SingleLevelInsert(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	// Register Department -> Employees relationship
	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"name": "Engineering",
		"employees": []interface{}{
			map[string]interface{}{
				"name": "John Doe",
			},
			map[string]interface{}{
				"name": "Jane Smith",
			},
		},
	}

	result, err := processor.ProcessNestedCUD(
		context.Background(),
		"insert",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	if result.ID == nil {
		t.Error("Expected result.ID to be set")
	}

	// Verify department was inserted
	if len(db.insertCalls) != 3 {
		t.Errorf("Expected 3 insert calls (1 dept + 2 employees), got %d", len(db.insertCalls))
	}

	// Verify first insert is department
	if db.insertCalls[0]["name"] != "Engineering" {
		t.Errorf("Expected department name 'Engineering', got %v", db.insertCalls[0]["name"])
	}

	// Verify employees were inserted with foreign key
	if db.insertCalls[1]["department_id"] == nil {
		t.Error("Expected employee to have department_id set")
	}
	if db.insertCalls[2]["department_id"] == nil {
		t.Error("Expected employee to have department_id set")
	}
}

func TestProcessNestedCUD_MultiLevelInsert(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	// Register relationships
	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	relProvider.RegisterRelation("Employee", "tasks", &RelationshipInfo{
		FieldName:    "Tasks",
		JSONName:     "tasks",
		RelationType: "has_many",
		ForeignKey:   "EmployeeID",
		RelatedModel: Task{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"name": "Engineering",
		"employees": []interface{}{
			map[string]interface{}{
				"name": "John Doe",
				"tasks": []interface{}{
					map[string]interface{}{
						"title": "Task 1",
					},
					map[string]interface{}{
						"title": "Task 2",
					},
				},
			},
		},
	}

	result, err := processor.ProcessNestedCUD(
		context.Background(),
		"insert",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	if result.ID == nil {
		t.Error("Expected result.ID to be set")
	}

	// Verify: 1 dept + 1 employee + 2 tasks = 4 inserts
	if len(db.insertCalls) != 4 {
		t.Errorf("Expected 4 insert calls, got %d", len(db.insertCalls))
	}

	// Verify department
	if db.insertCalls[0]["name"] != "Engineering" {
		t.Errorf("Expected department name 'Engineering', got %v", db.insertCalls[0]["name"])
	}

	// Verify employee has department_id
	if db.insertCalls[1]["department_id"] == nil {
		t.Error("Expected employee to have department_id set")
	}

	// Verify tasks have employee_id
	if db.insertCalls[2]["employee_id"] == nil {
		t.Error("Expected task to have employee_id set")
	}
	if db.insertCalls[3]["employee_id"] == nil {
		t.Error("Expected task to have employee_id set")
	}
}

func TestProcessNestedCUD_RequestFieldOverride(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"name": "Engineering",
		"employees": []interface{}{
			map[string]interface{}{
				"_request": "update",
				"ID":       int64(10), // Use capital ID to match struct field
				"name":     "John Updated",
			},
		},
	}

	_, err := processor.ProcessNestedCUD(
		context.Background(),
		"insert",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	// Verify department was inserted (1 insert)
	// Employee should be updated (1 update)
	if len(db.insertCalls) != 1 {
		t.Errorf("Expected 1 insert call for department, got %d", len(db.insertCalls))
	}

	if len(db.updateCalls) != 1 {
		t.Errorf("Expected 1 update call for employee, got %d", len(db.updateCalls))
	}

	// Verify update data
	if db.updateCalls[0]["name"] != "John Updated" {
		t.Errorf("Expected employee name 'John Updated', got %v", db.updateCalls[0]["name"])
	}
}

func TestProcessNestedCUD_SkipInsertWhenOnlyRequestField(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	// Data with only _request field for nested employee
	data := map[string]interface{}{
		"name": "Engineering",
		"employees": []interface{}{
			map[string]interface{}{
				"_request": "insert",
				// No other fields besides _request
				// Note: Foreign key will be injected, so employee WILL be inserted
			},
		},
	}

	_, err := processor.ProcessNestedCUD(
		context.Background(),
		"insert",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	// Department + Employee (with injected FK) = 2 inserts
	if len(db.insertCalls) != 2 {
		t.Errorf("Expected 2 insert calls (department + employee with FK), got %d", len(db.insertCalls))
	}

	if db.insertCalls[0]["name"] != "Engineering" {
		t.Errorf("Expected department name 'Engineering', got %v", db.insertCalls[0]["name"])
	}

	// Verify employee has foreign key
	if db.insertCalls[1]["department_id"] == nil {
		t.Error("Expected employee to have department_id injected")
	}
}

func TestProcessNestedCUD_Update(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"ID":   int64(1), // Use capital ID to match struct field
		"name": "Engineering Updated",
		"employees": []interface{}{
			map[string]interface{}{
				"_request": "insert",
				"name":     "New Employee",
			},
		},
	}

	result, err := processor.ProcessNestedCUD(
		context.Background(),
		"update",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	if result.ID != int64(1) {
		t.Errorf("Expected result.ID to be 1, got %v", result.ID)
	}

	// Verify department was updated
	if len(db.updateCalls) != 1 {
		t.Errorf("Expected 1 update call, got %d", len(db.updateCalls))
	}

	// Verify new employee was inserted
	if len(db.insertCalls) != 1 {
		t.Errorf("Expected 1 insert call for new employee, got %d", len(db.insertCalls))
	}
}

func TestProcessNestedCUD_Delete(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"ID": int64(1), // Use capital ID to match struct field
		"employees": []interface{}{
			map[string]interface{}{
				"_request": "delete",
				"ID":       int64(10), // Use capital ID
			},
			map[string]interface{}{
				"_request": "delete",
				"ID":       int64(11), // Use capital ID
			},
		},
	}

	_, err := processor.ProcessNestedCUD(
		context.Background(),
		"delete",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	// Verify employees were deleted first, then department
	// 2 employees + 1 department = 3 deletes
	if len(db.deleteCalls) != 3 {
		t.Errorf("Expected 3 delete calls, got %d", len(db.deleteCalls))
	}
}

func TestProcessNestedCUD_ParentIDPropagation(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	// Register 3-level relationships
	relProvider.RegisterRelation("Department", "employees", &RelationshipInfo{
		FieldName:    "Employees",
		JSONName:     "employees",
		RelationType: "has_many",
		ForeignKey:   "DepartmentID",
		RelatedModel: Employee{},
	})

	relProvider.RegisterRelation("Employee", "tasks", &RelationshipInfo{
		FieldName:    "Tasks",
		JSONName:     "tasks",
		RelationType: "has_many",
		ForeignKey:   "EmployeeID",
		RelatedModel: Task{},
	})

	relProvider.RegisterRelation("Task", "comments", &RelationshipInfo{
		FieldName:    "Comments",
		JSONName:     "comments",
		RelationType: "has_many",
		ForeignKey:   "TaskID",
		RelatedModel: Comment{},
	})

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"name": "Engineering",
		"employees": []interface{}{
			map[string]interface{}{
				"name": "John",
				"tasks": []interface{}{
					map[string]interface{}{
						"title": "Task 1",
						"comments": []interface{}{
							map[string]interface{}{
								"text": "Great work!",
							},
						},
					},
				},
			},
		},
	}

	_, err := processor.ProcessNestedCUD(
		context.Background(),
		"insert",
		data,
		Department{},
		nil,
		"departments",
	)

	if err != nil {
		t.Fatalf("ProcessNestedCUD failed: %v", err)
	}

	// Verify: 1 dept + 1 employee + 1 task + 1 comment = 4 inserts
	if len(db.insertCalls) != 4 {
		t.Errorf("Expected 4 insert calls, got %d", len(db.insertCalls))
	}

	// Verify department
	if db.insertCalls[0]["name"] != "Engineering" {
		t.Error("Expected department to be inserted first")
	}

	// Verify employee has department_id
	if db.insertCalls[1]["department_id"] == nil {
		t.Error("Expected employee to have department_id")
	}

	// Verify task has employee_id
	if db.insertCalls[2]["employee_id"] == nil {
		t.Error("Expected task to have employee_id")
	}

	// Verify comment has task_id
	if db.insertCalls[3]["task_id"] == nil {
		t.Error("Expected comment to have task_id")
	}
}

func TestInjectForeignKeys(t *testing.T) {
	db := newMockDatabase()
	registry := &mockModelRegistry{}
	relProvider := newMockRelationshipProvider()

	processor := NewNestedCUDProcessor(db, registry, relProvider)

	data := map[string]interface{}{
		"name": "John",
	}

	parentIDs := map[string]interface{}{
		"department": int64(5),
	}

	modelType := reflect.TypeOf(Employee{})

	processor.injectForeignKeys(data, modelType, parentIDs)

	// Should inject department_id based on the "department" key in parentIDs
	if data["department_id"] == nil {
		t.Error("Expected department_id to be injected")
	}

	if data["department_id"] != int64(5) {
		t.Errorf("Expected department_id to be 5, got %v", data["department_id"])
	}
}

func TestGetPrimaryKeyName(t *testing.T) {
	dept := Department{}
	pkName := reflection.GetPrimaryKeyName(dept)

	if pkName != "ID" {
		t.Errorf("Expected primary key name 'ID', got '%s'", pkName)
	}

	// Test with pointer
	pkName2 := reflection.GetPrimaryKeyName(&dept)
	if pkName2 != "ID" {
		t.Errorf("Expected primary key name 'ID' from pointer, got '%s'", pkName2)
	}
}
