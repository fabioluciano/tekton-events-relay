package factory

import (
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/batch"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/teams"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// TeamsFactory builds ActionHandlers from Teams instance configurations.
type TeamsFactory struct{}

// Build creates action handlers for a single Teams instance.
func (f *TeamsFactory) Build(inst config.TeamsInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	webhookURLFile := ""
	webhookURLKey := ""
	if inst.Auth != nil {
		webhookURLFile = inst.Auth.WebhookURLFile
		webhookURLKey = inst.Auth.WebhookURLKey
	}
	webhookURL, err := secrets.ResolveOrInfer(webhookURLFile, "teams", inst.Name, "webhook_url", webhookURLKey, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	mentionUsers := make([]teams.MentionEntry, 0, len(inst.MentionUsers))
	for _, mu := range inst.MentionUsers {
		mentionUsers = append(mentionUsers, teams.MentionEntry{Name: mu.Name, ID: mu.ID})
	}

	handler, err := teams.New(teams.Config{
		WebhookURL:   webhookURL,
		Template:     inst.Template,
		MentionUsers: mentionUsers,
		HTTPClient:   httpClient,
		RetryPolicy:  retryPolicy,
	}, log)
	if err != nil {
		return nil, err
	}

	var h notifier.ActionHandler = handler
	if inst.Batch != nil && inst.Batch.Enabled {
		batchCfg := batch.Config{
			Enabled:       true,
			MaxSize:       inst.Batch.MaxSize,
			FlushInterval: inst.Batch.FlushInterval,
		}
		if batchCfg.FlushInterval == 0 {
			batchCfg.FlushInterval = 5 * time.Second
		}
		h = batch.New(handler, "teams", batchCfg, log, nil)
	}

	wrapped, err := middleware.WrapWithCEL(h, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
