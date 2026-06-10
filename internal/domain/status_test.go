package domain

import (
	"encoding/json"
	"testing"
)

func TestStateConstants(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StatePending, "pending"},
		{StateRunning, "running"},
		{StateSuccess, "success"},
		{StateFailure, "failure"},
		{StateError, "error"},
		{StateCanceled, "canceled"},
		{StateDone, "done"},
	}
	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			if string(tc.state) != tc.expected {
				t.Errorf("State %q: got %q, want %q", tc.state, string(tc.state), tc.expected)
			}
		})
	}
}

func TestStateUnmarshalText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected State
	}{
		{"pending", "pending", StatePending},
		{"running", "running", StateRunning},
		{"success", "success", StateSuccess},
		{"failure", "failure", StateFailure},
		{"error", "error", StateError},
		{"canceled", "canceled", StateCanceled},
		{"done", "done", StateDone},
		{"arbitrary string accepted", "unknown-state", State("unknown-state")},
		{"empty string accepted", "", State("")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s State
			if err := s.UnmarshalText([]byte(tc.input)); err != nil {
				t.Fatalf("UnmarshalText(%q) returned error: %v", tc.input, err)
			}
			if s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
}

func TestStateMarshalText(t *testing.T) {
	tests := []struct {
		state    State
		expected []byte
	}{
		{StatePending, []byte("pending")},
		{StateRunning, []byte("running")},
		{StateSuccess, []byte("success")},
		{StateFailure, []byte("failure")},
		{StateError, []byte("error")},
		{StateCanceled, []byte("canceled")},
		{StateDone, []byte("done")},
		{State("custom"), []byte("custom")},
	}
	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			got, err := tc.state.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() returned error: %v", err)
			}
			if string(got) != string(tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	issueNum := 42
	prNum := 7
	event := Event{
		State:       StateSuccess,
		CommitSHA:   "abc123",
		Repo:        Repo{Owner: "org", Name: "repo"},
		IssueNumber: &issueNum,
		PRNumber:    &prNum,
		Results: []Result{
			{Name: "IMAGE", Value: "gcr.io/proj/img:latest"},
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	for _, key := range []string{"issue_number", "pr_number", "results"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in JSON output", key)
		}
	}

	// Omitempty: DiscussionNumber is nil, should not appear
	if _, ok := m["discussion_number"]; ok {
		t.Error("key discussion_number should be omitted when nil")
	}

	// Unmarshal back
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal back failed: %v", err)
	}
	if got.IssueNumber == nil || *got.IssueNumber != issueNum {
		t.Errorf("IssueNumber: got %v, want %d", got.IssueNumber, issueNum)
	}
	if got.PRNumber == nil || *got.PRNumber != prNum {
		t.Errorf("PRNumber: got %v, want %d", got.PRNumber, prNum)
	}
	if len(got.Results) != 1 || got.Results[0].Name != "IMAGE" || got.Results[0].Value != "gcr.io/proj/img:latest" {
		t.Errorf("Results: got %v", got.Results)
	}
}

func TestResultJSONTags(t *testing.T) {
	r := Result{Name: "foo", Value: "bar"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal(Result) failed: %v", err)
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"name", "foo"},
		{"value", "bar"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			if got, ok := m[tc.key]; !ok {
				t.Errorf("key %q missing from JSON", tc.key)
			} else if got != tc.want {
				t.Errorf("key %q: got %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestResourceConstants(t *testing.T) {
	tests := []struct {
		resource Resource
		expected string
	}{
		{ResourceTaskRun, "taskrun"},
		{ResourcePipelineRun, "pipelinerun"},
		{ResourceCustomRun, "customrun"},
		{ResourceEventListener, "eventlistener"},
	}
	for _, tc := range tests {
		t.Run(string(tc.resource), func(t *testing.T) {
			if string(tc.resource) != tc.expected {
				t.Errorf("Resource %q: got %q, want %q", tc.resource, string(tc.resource), tc.expected)
			}
		})
	}
}
