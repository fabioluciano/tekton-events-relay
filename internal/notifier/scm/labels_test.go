package scm

import (
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const testLabelName = "test"

func TestLabelValidation(t *testing.T) {
	tests := []struct {
		name        string
		labels      LabelSet
		expectValid bool
		expectColor string
	}{
		{
			name: "valid color",
			labels: LabelSet{
				Add: []Label{{Name: "bug", Color: "d73a4a"}},
			},
			expectValid: true,
			expectColor: "d73a4a",
		},
		{
			name: "valid color uppercase",
			labels: LabelSet{
				Add: []Label{{Name: "feature", Color: "A2EEEF"}},
			},
			expectValid: true,
			expectColor: "A2EEEF",
		},
		{
			name: "empty color",
			labels: LabelSet{
				Add: []Label{{Name: "enhancement", Color: ""}},
			},
			expectValid: true,
			expectColor: "",
		},
		{
			name: "invalid color with hash",
			labels: LabelSet{
				Add: []Label{{Name: testLabelName, Color: "#d73a4a"}},
			},
			expectValid: false,
			expectColor: "", // should be cleared
		},
		{
			name: "invalid color too short",
			labels: LabelSet{
				Add: []Label{{Name: testLabelName, Color: "d73"}},
			},
			expectValid: false,
			expectColor: "",
		},
		{
			name: "invalid color too long",
			labels: LabelSet{
				Add: []Label{{Name: testLabelName, Color: "d73a4a00"}},
			},
			expectValid: false,
			expectColor: "",
		},
		{
			name: "invalid color non-hex",
			labels: LabelSet{
				Add: []Label{{Name: testLabelName, Color: "gggggg"}},
			},
			expectValid: false,
			expectColor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := zaptest.NewLogger(t)
			tt.labels.Validate(log)

			if tt.expectValid {
				if tt.labels.Add[0].Color != tt.expectColor {
					t.Errorf("expected color %q, got %q", tt.expectColor, tt.labels.Add[0].Color)
				}
			} else {
				// Invalid colors should be cleared to empty string
				if tt.labels.Add[0].Color != "" {
					t.Errorf("expected invalid color to be cleared, got %q", tt.labels.Add[0].Color)
				}
			}
		})
	}
}

func TestLabelSetRender(t *testing.T) {
	event := domain.Event{
		State:        "success",
		PipelineName: "ci-pipeline",
	}

	tests := []struct {
		name      string
		labelSet  LabelSet
		expectAdd []Label
		expectRem []Label
		expectErr bool
	}{
		{
			name: "static labels with colors",
			labelSet: LabelSet{
				Add: []Label{
					{Name: "ci:passed", Color: "0e8a16"},
					{Name: "ready", Color: ""},
				},
				Remove: []Label{
					{Name: "ci:failed", Color: ""},
				},
			},
			expectAdd: []Label{
				{Name: "ci:passed", Color: "0e8a16"},
				{Name: "ready", Color: ""},
			},
			expectRem: []Label{
				{Name: "ci:failed", Color: ""},
			},
			expectErr: false,
		},
		{
			name: "templated labels with colors",
			labelSet: LabelSet{
				Add: []Label{
					{Name: "ci:{{.State}}", Color: "0e8a16"},
					{Name: "pipeline:{{.PipelineName}}", Color: "1d76db"},
				},
			},
			expectAdd: []Label{
				{Name: "ci:success", Color: "0e8a16"},
				{Name: "pipeline:ci-pipeline", Color: "1d76db"},
			},
			expectErr: false,
		},
		{
			name: "empty template result dropped",
			labelSet: LabelSet{
				Add: []Label{
					{Name: "{{if eq .State \"failure\"}}failed{{end}}", Color: "d73a4a"},
					{Name: "always", Color: ""},
				},
			},
			expectAdd: []Label{
				{Name: "always", Color: ""},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			add, remove, err := tt.labelSet.Render(event)

			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if len(add) != len(tt.expectAdd) {
				t.Errorf("expected %d add labels, got %d", len(tt.expectAdd), len(add))
			}
			for i, expected := range tt.expectAdd {
				if i >= len(add) {
					break
				}
				if add[i].Name != expected.Name || add[i].Color != expected.Color {
					t.Errorf("add[%d]: expected %+v, got %+v", i, expected, add[i])
				}
			}

			if len(remove) != len(tt.expectRem) {
				t.Errorf("expected %d remove labels, got %d", len(tt.expectRem), len(remove))
			}
			for i, expected := range tt.expectRem {
				if i >= len(remove) {
					break
				}
				if remove[i].Name != expected.Name {
					t.Errorf("remove[%d]: expected %+v, got %+v", i, expected, remove[i])
				}
			}
		})
	}
}

func TestLabelSetEmpty(t *testing.T) {
	tests := []struct {
		name        string
		labelSet    LabelSet
		expectEmpty bool
	}{
		{
			name:        "empty labelset",
			labelSet:    LabelSet{},
			expectEmpty: true,
		},
		{
			name: "only add",
			labelSet: LabelSet{
				Add: []Label{{Name: testLabelName}},
			},
			expectEmpty: false,
		},
		{
			name: "only remove",
			labelSet: LabelSet{
				Remove: []Label{{Name: testLabelName}},
			},
			expectEmpty: false,
		},
		{
			name: "both add and remove",
			labelSet: LabelSet{
				Add:    []Label{{Name: "add"}},
				Remove: []Label{{Name: "remove"}},
			},
			expectEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.labelSet.Empty() != tt.expectEmpty {
				t.Errorf("expected Empty() = %v, got %v", tt.expectEmpty, tt.labelSet.Empty())
			}
		})
	}
}
