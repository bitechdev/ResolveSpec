package testmodels

import (
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// Department represents a company department
type Department struct {
	ID          int32     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name"`
	Code        string    `json:"code" gorm:"uniqueIndex"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Relations
	Employees []Employee `json:"employees,omitempty" gorm:"foreignKey:DepartmentID;references:ID"`
	Projects  []Project  `json:"projects,omitempty" gorm:"many2many:department_projects;"`
}

func (Department) TableName() string {
	return "departments"
}

// Employee represents a company employee
type Employee struct {
	ID           int32     `json:"id" gorm:"primaryKey;autoIncrement"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Email        string    `json:"email" gorm:"uniqueIndex"`
	Title        string    `json:"title"`
	DepartmentID int32     `json:"department_id"`
	ManagerID    *int32    `json:"manager_id"`
	HireDate     time.Time `json:"hire_date"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Relations
	Department *Department `json:"department,omitempty" gorm:"foreignKey:DepartmentID;references:ID"`
	Manager    *Employee   `json:"manager,omitempty" gorm:"foreignKey:ManagerID;references:ID"`
	Reports    []Employee  `json:"reports,omitempty" gorm:"foreignKey:ManagerID;references:ID"`
	Projects   []Project   `json:"projects,omitempty" gorm:"many2many:employee_projects;"`
	Documents  []Document  `json:"documents,omitempty" gorm:"foreignKey:OwnerID;references:ID"`
}

func (Employee) TableName() string {
	return "employees"
}

// Project represents a company project
type Project struct {
	ID          int32     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name"`
	Code        string    `json:"code" gorm:"uniqueIndex"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Budget      float64   `json:"budget"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Relations
	Departments []Department  `json:"departments,omitempty" gorm:"many2many:department_projects;"`
	Employees   []Employee    `json:"employees,omitempty" gorm:"many2many:employee_projects;"`
	Tasks       []ProjectTask `json:"tasks,omitempty" gorm:"foreignKey:ProjectID;references:ID"`
	Documents   []Document    `json:"documents,omitempty" gorm:"foreignKey:ProjectID;references:ID"`
}

func (Project) TableName() string {
	return "projects"
}

// ProjectTask represents a task within a project
type ProjectTask struct {
	ID          int32     `json:"id" gorm:"primaryKey;autoIncrement"`
	ProjectID   int32     `json:"project_id"`
	AssigneeID  int32     `json:"assignee_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    int       `json:"priority"`
	DueDate     time.Time `json:"due_date"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Relations
	Project  Project   `json:"project,omitempty" gorm:"foreignKey:ProjectID;references:ID"`
	Assignee Employee  `json:"assignee,omitempty" gorm:"foreignKey:AssigneeID;references:ID"`
	Comments []Comment `json:"comments,omitempty" gorm:"foreignKey:TaskID;references:ID"`
}

func (ProjectTask) TableName() string {
	return "project_tasks"
}

// Document represents any document in the system
type Document struct {
	ID          int32     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Path        string    `json:"path"`
	OwnerID     int32     `json:"owner_id"`
	ProjectID   *int32    `json:"project_id"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Relations
	Owner   Employee `json:"owner,omitempty" gorm:"foreignKey:OwnerID;references:ID"`
	Project *Project `json:"project,omitempty" gorm:"foreignKey:ProjectID;references:ID"`
}

func (Document) TableName() string {
	return "documents"
}

// Comment represents a comment on a task
type Comment struct {
	ID        int32     `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID    int32     `json:"task_id"`
	AuthorID  int32     `json:"author_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Relations
	Task   ProjectTask `json:"task,omitempty" gorm:"foreignKey:TaskID;references:ID"`
	Author Employee    `json:"author,omitempty" gorm:"foreignKey:AuthorID;references:ID"`
}

func (Comment) TableName() string {
	return "comments"
}

// RegisterTestModels registers all test models with the provided registry
func RegisterTestModels(registry *modelregistry.DefaultModelRegistry) {
	registry.RegisterModel("departments", Department{})
	registry.RegisterModel("employees", Employee{})
	registry.RegisterModel("projects", Project{})
	registry.RegisterModel("project_tasks", ProjectTask{})
	registry.RegisterModel("documents", Document{})
	registry.RegisterModel("comments", Comment{})
}

// GetTestModels returns a list of all test model instances
func GetTestModels() []interface{} {
	return []interface{}{
		Department{},
		Employee{},
		Project{},
		ProjectTask{},
		Document{},
		Comment{},
	}
}
