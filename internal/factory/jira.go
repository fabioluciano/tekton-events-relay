package factory

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/jira"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
)

// JiraFactory builds ActionHandlers from Jira instance configurations.
type JiraFactory struct{}

// Build creates action handlers for a single Jira instance.
func (f *JiraFactory) Build(inst config.JiraInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	var oauth2cfg *config.OAuth2Config
	email, tokenFile, tokenKey := "", "", ""
	if inst.Auth != nil {
		oauth2cfg = inst.Auth.OAuth2
		email = inst.Auth.Email
		tokenFile = inst.Auth.TokenFile
		tokenKey = inst.Auth.TokenKey
	}
	token, err := resolveBearerRefresher(oauth2cfg, tokenFile, tokenKey, "jira", inst.Name, log)
	if err != nil {
		return nil, err
	}

	client := jira.NewClient(jira.ClientConfig{
		BaseURL:            inst.BaseURL,
		APIVersion:         inst.APIVersion,
		Email:              email,
		Token:              token,
		InsecureSkipVerify: inst.InsecureSkipVerify,
	}, log)

	handlers := make([]notifier.ActionHandler, 0, len(inst.Actions))
	for _, action := range inst.Actions {
		if !action.Enabled {
			continue
		}
		var handler notifier.ActionHandler
		switch action.Type {
		case config.JiraActionComment:
			handler, err = jira.NewCommentHandler(client, action.Template, log)
		case config.JiraActionTransition:
			handler, err = jira.NewTransitionHandler(client, action.Transition, log)
		default:
			return nil, fmt.Errorf("jira %s: unsupported action type %q", inst.Name, action.Type)
		}
		if err != nil {
			return nil, err
		}
		wrapped, err := middleware.WrapWithCEL(handler, action.When, log)
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, wrapped)
	}
	return handlers, nil
}
