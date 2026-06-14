package gitlab

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	testOrgName  = "myorg"
	testRepoName = "myrepo"
)

func TestProjectIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		event   domain.Event
		want    string
		wantErr bool
	}{
		{
			name:    "with numeric ID",
			event:   domain.Event{Repo: domain.Repo{ID: "12345"}},
			want:    "12345",
			wantErr: false,
		},
		{
			name:    "with owner and name",
			event:   domain.Event{Repo: domain.Repo{Owner: testOrgName, Name: testRepoName}},
			want:    "myorg%2Fmyrepo",
			wantErr: false,
		},
		{
			name:    "with special characters in owner and name",
			event:   domain.Event{Repo: domain.Repo{Owner: "my org", Name: "my/repo"}},
			want:    "my%20org%2Fmy%2Frepo",
			wantErr: false,
		},
		{
			name:    "ID takes precedence over owner/name",
			event:   domain.Event{Repo: domain.Repo{ID: "999", Owner: testOrgName, Name: testRepoName}},
			want:    "999",
			wantErr: false,
		},
		{
			name:    "missing all identifiers",
			event:   domain.Event{Repo: domain.Repo{}},
			want:    "",
			wantErr: true,
		},
		{
			name:    "only owner no name",
			event:   domain.Event{Repo: domain.Repo{Owner: testOrgName}},
			want:    "",
			wantErr: true,
		},
		{
			name:    "only name no owner",
			event:   domain.Event{Repo: domain.Repo{Name: testRepoName}},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := projectIdentifier(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("projectIdentifier() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("projectIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}
