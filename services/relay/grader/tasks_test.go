package grader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexsviridov/linuxlab/relay/grader"
)

func writeTasksFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}
	return path
}

func TestLoadTasks_valid(t *testing.T) {
	path := writeTasksFile(t, `[
		{"id": "task1", "type": "term", "template": "echo hi"},
		{"id": "task2", "type": "term", "template": "echo bye"}
	]`)

	tasks, err := grader.LoadTasks(path)
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != "task1" || tasks[0].Type != "term" || tasks[0].Template != "echo hi" {
		t.Errorf("unexpected task[0]: %+v", tasks[0])
	}
}

func TestLoadTasks_missing_file(t *testing.T) {
	_, err := grader.LoadTasks("/nonexistent/tasks.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadTasks_invalid_json(t *testing.T) {
	path := writeTasksFile(t, `not json`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadTasks_missing_id(t *testing.T) {
	path := writeTasksFile(t, `[{"type": "term", "template": "echo hi"}]`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestLoadTasks_missing_template(t *testing.T) {
	path := writeTasksFile(t, `[{"id": "task1", "type": "term"}]`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for missing template, got nil")
	}
}

func TestLoadTasks_invalid_type(t *testing.T) {
	path := writeTasksFile(t, `[{"id": "task1", "type": "gui", "template": "echo hi"}]`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
}
