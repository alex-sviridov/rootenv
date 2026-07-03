package grader

import (
	"encoding/json"
	"fmt"
	"os"
)

// Task is one gradeable item loaded from tasks.json.
type Task struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Template string `json:"template"`
}

// LoadTasks reads and validates the tasks.json file at path.
func LoadTasks(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tasks file: %w", err)
	}

	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse tasks file: %w", err)
	}

	for i, task := range tasks {
		if task.ID == "" {
			return nil, fmt.Errorf("task[%d]: missing id", i)
		}
		if task.Template == "" {
			return nil, fmt.Errorf("task[%d] %q: missing template", i, task.ID)
		}
		if task.Type != "term" {
			return nil, fmt.Errorf("task[%d] %q: unsupported type %q (only \"term\" is supported)", i, task.ID, task.Type)
		}
	}

	return tasks, nil
}
