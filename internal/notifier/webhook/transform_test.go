package webhook

import (
	"strings"
	"testing"

	"github.com/itchyny/gojq"
)

const (
	testKey   = "key"
	testValue = "testValue"
)

func TestApplyTransform_NoTransform(t *testing.T) {
	input := map[string]any{testKey: testValue}
	result, err := ApplyTransform(nil, input)
	if err != nil {
		t.Fatalf("ApplyTransform(nil) error: %v", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}
	if resultMap["key"] != testValue {
		t.Error("ApplyTransform(nil) should return input unchanged")
	}
}

func TestApplyTransform_Identity(t *testing.T) {
	query, _ := gojq.Parse(".")
	compiled, _ := gojq.Compile(query)

	input := map[string]any{testKey: testValue}
	result, err := ApplyTransform(compiled, input)
	if err != nil {
		t.Fatalf("ApplyTransform(.) error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}
	if resultMap["key"] != testValue {
		t.Errorf("result[key] = %v, want %q", resultMap["key"], testValue)
	}
}

func TestApplyTransform_SimpleTransform(t *testing.T) {
	query, _ := gojq.Parse(`. | {name: .run_id, status: "ok"}`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{"run_id": "test-123"}
	result, err := ApplyTransform(compiled, input)
	if err != nil {
		t.Fatalf("ApplyTransform error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}
	if resultMap["name"] != "test-123" {
		t.Errorf("name = %v, want 'test-123'", resultMap["name"])
	}
	if resultMap["status"] != "ok" {
		t.Errorf("status = %v, want 'ok'", resultMap["status"])
	}
}

func TestApplyTransform_NestedObjects(t *testing.T) {
	query, _ := gojq.Parse(`. | {repo_name: (.repo.owner + "/" + .repo.name)}`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{
		"repo": map[string]any{
			"owner": "org",
			"name":  "project",
		},
	}

	result, err := ApplyTransform(compiled, input)
	if err != nil {
		t.Fatalf("ApplyTransform error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}
	if resultMap["repo_name"] != "org/project" {
		t.Errorf("repo_name = %v, want 'org/project'", resultMap["repo_name"])
	}
}

func TestApplyTransform_ArrayConstruction(t *testing.T) {
	query, _ := gojq.Parse(`. | {items: [{id: .run_id}]}`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{"run_id": "test-123"}
	result, err := ApplyTransform(compiled, input)
	if err != nil {
		t.Fatalf("ApplyTransform error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}

	items, ok := resultMap["items"].([]any)
	if !ok {
		t.Fatal("items should be an array")
	}
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
}

func TestApplyTransform_MultipleResults_Error(t *testing.T) {
	// Transform that produces multiple results
	query, _ := gojq.Parse(`.run_id, .namespace`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{
		"run_id":    testRunName,
		"namespace": "default",
	}

	_, err := ApplyTransform(compiled, input)
	if err == nil {
		t.Fatal("expected error for multiple results, got nil")
	}
	if !strings.Contains(err.Error(), "produced 2 results") {
		t.Errorf("error should mention multiple results, got: %v", err)
	}
}

func TestApplyTransform_NoResults_Error(t *testing.T) {
	// Transform that produces no results (empty)
	query, _ := gojq.Parse(`empty`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{testKey: testValue}

	_, err := ApplyTransform(compiled, input)
	if err == nil {
		t.Fatal("expected error for no results, got nil")
	}
	if !strings.Contains(err.Error(), "produced no results") {
		t.Errorf("error should mention no results, got: %v", err)
	}
}

func TestApplyTransform_NilOutput_Error(t *testing.T) {
	// Transform that produces nil
	query, _ := gojq.Parse(`null`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{testKey: testValue}

	_, err := ApplyTransform(compiled, input)
	if err == nil {
		t.Fatal("expected error for nil output, got nil")
	}
	if !strings.Contains(err.Error(), "nil output") {
		t.Errorf("error should mention nil output, got: %v", err)
	}
}

func TestApplyTransform_ExecutionError(t *testing.T) {
	// Transform that will fail at runtime (divide by zero)
	query, _ := gojq.Parse(`1 / 0`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{testKey: testValue}

	_, err := ApplyTransform(compiled, input)
	if err == nil {
		t.Fatal("expected error for execution error, got nil")
	}
	if !strings.Contains(err.Error(), "execution error") {
		t.Errorf("error should mention execution error, got: %v", err)
	}
}

func TestApplyTransform_ComplexUserExample(t *testing.T) {
	// User's transform from the plan
	query, _ := gojq.Parse(`. | {
		id:           .run_id,
		pipelineId:   .pipeline_name,
		startedDate:  .started_at,
		finishedDate: .finished_at,
		result:       "SUCCESS",
		environment:  "PRODUCTION",
		deploymentCommits: [{
			repoUrl:   .repo.owner + "/" + .repo.name,
			commitSha: .commit_sha
		}]
	}`)
	compiled, _ := gojq.Compile(query)

	input := map[string]any{
		"run_id":        "run-123",
		"pipeline_name": "deploy-pipeline",
		"started_at":    "2024-01-01T10:00:00Z",
		"finished_at":   "2024-01-01T10:05:00Z",
		"commit_sha":    "abc123",
		"repo": map[string]any{
			"owner": "myorg",
			"name":  "myrepo",
		},
	}

	result, err := ApplyTransform(compiled, input)
	if err != nil {
		t.Fatalf("ApplyTransform error: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}

	if resultMap["id"] != "run-123" {
		t.Errorf("id = %v, want 'run-123'", resultMap["id"])
	}
	if resultMap["pipelineId"] != "deploy-pipeline" {
		t.Errorf("pipelineId = %v, want 'deploy-pipeline'", resultMap["pipelineId"])
	}
	if resultMap["result"] != "SUCCESS" {
		t.Errorf("result = %v, want 'SUCCESS'", resultMap["result"])
	}
	if resultMap["environment"] != "PRODUCTION" {
		t.Errorf("environment = %v, want 'PRODUCTION'", resultMap["environment"])
	}

	commits, ok := resultMap["deploymentCommits"].([]any)
	if !ok {
		t.Fatal("deploymentCommits should be an array")
	}
	if len(commits) != 1 {
		t.Fatalf("commits length = %d, want 1", len(commits))
	}

	commit, ok := commits[0].(map[string]any)
	if !ok {
		t.Fatal("commit should be a map")
	}
	if commit["repoUrl"] != "myorg/myrepo" {
		t.Errorf("repoUrl = %v, want 'myorg/myrepo'", commit["repoUrl"])
	}
	if commit["commitSha"] != "abc123" {
		t.Errorf("commitSha = %v, want 'abc123'", commit["commitSha"])
	}
}
