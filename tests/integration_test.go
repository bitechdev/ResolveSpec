package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Database-assigned (serial) ids, captured on create; later tests build on earlier ones.
var deptID, emp1ID, emp2ID, mgrID, projID, task1ID int64

// createdIDs returns the ids of the records in a create response (single object or array).
func createdIDs(resp *http.Response) []int64 {
	var result struct {
		Data interface{} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	items, ok := result.Data.([]interface{})
	if !ok {
		items = []interface{}{result.Data}
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			if id, ok := m["id"].(float64); ok {
				ids = append(ids, int64(id))
				continue
			}
		}
		ids = append(ids, 0)
	}
	return ids
}

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	TestSetup(m)
}

func TestDepartmentEmployees(t *testing.T) {
	// Create test department
	deptPayload := map[string]interface{}{
		"operation": "create",
		"data": map[string]interface{}{
			"name":        "Engineering",
			"code":        "ENG",
			"description": "Engineering Department",
		},
	}

	resp := makeRequest(t, "/departments", deptPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	deptID = createdIDs(resp)[0]

	// Create employees in department
	empPayload := map[string]interface{}{
		"operation": "create",
		"data": []map[string]interface{}{
			{
				"first_name":    "John",
				"last_name":     "Doe",
				"email":         "john@example.com",
				"department_id": deptID,
				"title":         "Senior Engineer",
			},
			{
				"first_name":    "Jane",
				"last_name":     "Smith",
				"email":         "jane@example.com",
				"department_id": deptID,
				"title":         "Engineer",
			},
		},
	}

	resp = makeRequest(t, "/employees", empPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	emps := createdIDs(resp)
	emp1ID, emp2ID = emps[0], emps[1]

	// Read department with employees
	readPayload := map[string]interface{}{
		"operation": "read",
		"options": map[string]interface{}{
			"preload": []map[string]interface{}{
				{
					"relation": "employees",
					"columns":  []string{"id", "first_name", "last_name", "title"},
				},
			},
		},
	}

	resp = makeRequest(t, fmt.Sprintf("/departments/%d", deptID), readPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	data := result["data"].(map[string]interface{})
	employees := data["employees"].([]interface{})
	assert.Equal(t, 2, len(employees))
}

func TestEmployeeHierarchy(t *testing.T) {
	// Create manager
	mgrPayload := map[string]interface{}{
		"operation": "create",
		"data": map[string]interface{}{
			"first_name":    "Alice",
			"last_name":     "Manager",
			"email":         "alice@example.com",
			"title":         "Engineering Manager",
			"department_id": deptID,
		},
	}

	resp := makeRequest(t, "/employees", mgrPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	mgrID = createdIDs(resp)[0]

	// Update employees to set manager
	updatePayload := map[string]interface{}{
		"operation": "update",
		"data": map[string]interface{}{
			"manager_id": mgrID,
		},
	}

	resp = makeRequest(t, fmt.Sprintf("/employees/%d", emp1ID), updatePayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp = makeRequest(t, fmt.Sprintf("/employees/%d", emp2ID), updatePayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read manager with reports
	readPayload := map[string]interface{}{
		"operation": "read",
		"options": map[string]interface{}{
			"preload": []map[string]interface{}{
				{
					"relation": "reports",
					"columns":  []string{"id", "first_name", "last_name", "title"},
				},
			},
		},
	}

	resp = makeRequest(t, fmt.Sprintf("/employees/%d", mgrID), readPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	data := result["data"].(map[string]interface{})
	reports := data["reports"].([]interface{})
	assert.Equal(t, 2, len(reports))
}

func TestProjectStructure(t *testing.T) {
	// Create project
	projectPayload := map[string]interface{}{
		"operation": "create",
		"data": map[string]interface{}{
			"name":        "New Website",
			"code":        "WEB",
			"description": "Company website redesign",
			"status":      "active",
			"start_date":  time.Now().Format(time.RFC3339),
			"end_date":    time.Now().AddDate(0, 3, 0).Format(time.RFC3339),
			"budget":      100000,
		},
	}

	resp := makeRequest(t, "/projects", projectPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	projID = createdIDs(resp)[0]

	// Create project tasks
	taskPayload := map[string]interface{}{
		"operation": "create",
		"data": []map[string]interface{}{
			{
				"project_id":  projID,
				"assignee_id": emp1ID,
				"title":       "Design Homepage",
				"description": "Create homepage design",
				"status":      "in_progress",
				"priority":    1,
				"due_date":    time.Now().AddDate(0, 1, 0).Format(time.RFC3339),
			},
			{
				"project_id":  projID,
				"assignee_id": emp2ID,
				"title":       "Implement Backend",
				"description": "Implement backend APIs",
				"status":      "planned",
				"priority":    2,
				"due_date":    time.Now().AddDate(0, 2, 0).Format(time.RFC3339),
			},
		},
	}

	resp = makeRequest(t, "/project_tasks", taskPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	task1ID = createdIDs(resp)[0]

	// Create task comments
	commentPayload := map[string]interface{}{
		"operation": "create",
		"data": map[string]interface{}{
			"task_id":   task1ID,
			"author_id": mgrID,
			"content":   "Looking good! Please add more animations.",
		},
	}

	resp = makeRequest(t, "/comments", commentPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read project with all relations
	readPayload := map[string]interface{}{
		"operation": "read",
		"options": map[string]interface{}{
			"preload": []map[string]interface{}{
				{
					"relation": "tasks",
					"columns":  []string{"id", "title", "status", "assignee_id"},
					"preload": []map[string]interface{}{
						{
							"relation": "comments",
							"columns":  []string{"id", "content", "author_id"},
							"preload": []map[string]interface{}{
								{
									"relation": "author",
									"columns":  []string{"id", "first_name", "last_name"},
								},
							},
						},
						{
							"relation": "assignee",
							"columns":  []string{"id", "first_name", "last_name", "title"},
						},
					},
				},
			},
		},
	}

	resp = makeRequest(t, fmt.Sprintf("/projects/%d", projID), readPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.True(t, result["success"].(bool))
}
