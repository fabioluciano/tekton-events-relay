// Package tekton implements shared helpers for decoder code deduplication.
package tekton

import (
	"fmt"
	"strconv"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// baseEventFromRun builds the common domain.Event fields from a run object.
// The resource and rawType determine the Resource and State fields.
func baseEventFromRun(obj *runObject, resource domain.Resource, rawType string) (domain.Event, error) {
	provider := obj.Metadata.Annotations[event.AnnoProvider]
	if provider == "" {
		return domain.Event{}, fmt.Errorf("missing annotation %s on %s/%s",
			event.AnnoProvider, obj.Metadata.Namespace, obj.Metadata.Name)
	}
	return domain.Event{
		Provider:   provider,
		Resource:   resource,
		APIBaseURL: obj.Metadata.Annotations[event.AnnoAPIBaseURL],
		CommitSHA:  obj.Metadata.Annotations[event.AnnoCommitSHA],
		Repo: domain.Repo{
			Owner:     obj.Metadata.Annotations[event.AnnoRepoOwner],
			Name:      obj.Metadata.Annotations[event.AnnoRepoName],
			ID:        obj.Metadata.Annotations[event.AnnoRepoID],
			Workspace: obj.Metadata.Annotations[event.AnnoRepoWorkspace],
			Project:   obj.Metadata.Annotations[event.AnnoRepoProject],
			Org:       obj.Metadata.Annotations[event.AnnoRepoOrg],
		},
		State:       MapState(rawType),
		Description: descriptionFor(obj, rawType),
		RunName:     obj.Metadata.Name,
		RunID:       obj.Metadata.UID,
		Namespace:   obj.Metadata.Namespace,
	}, nil
}

// optionalInt parses an integer annotation, returning nil if missing or invalid.
func optionalInt(annotations map[string]string, key string) *int {
	s := annotations[key]
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

// applyTimestamps copies StartTime and CompletionTime from obj.Status to report.
func applyTimestamps(report *domain.Event, obj *runObject) {
	if obj.Status.StartTime != nil {
		report.StartedAt = *obj.Status.StartTime
	}
	if obj.Status.CompletionTime != nil {
		report.FinishedAt = *obj.Status.CompletionTime
	}
}

// applyOptionalNumbers extracts issue/PR/discussion numbers from annotations.
func applyOptionalNumbers(report *domain.Event, annotations map[string]string) {
	report.IssueNumber = optionalInt(annotations, event.AnnoIssueNumber)
	report.PRNumber = optionalInt(annotations, event.AnnoPRNumber)
	report.DiscussionNumber = optionalInt(annotations, event.AnnoDiscussionNumber)
	report.JiraIssueKey = annotations[event.AnnoJiraIssueKey]
}
