package gitlab

import (
	"fmt"
	"net/url"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// projectIdentifier returns the numeric ID or the url-encoded path.
func projectIdentifier(s domain.Event) (string, error) {
	if s.Repo.ID != "" {
		return s.Repo.ID, nil
	}
	if s.Repo.Owner != "" && s.Repo.Name != "" {
		return url.PathEscape(s.Repo.Owner + "/" + s.Repo.Name), nil
	}
	return "", fmt.Errorf("gitlab requires repo.ID or repo.Owner+Name")
}
