package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/email"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// EmailFactory builds ActionHandlers from email instance configurations.
type EmailFactory struct{}

// Build creates action handlers for a single email instance.
func (f *EmailFactory) Build(inst config.EmailInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	username, password := "", ""
	if inst.Auth != nil && inst.Auth.Username != "" {
		username = inst.Auth.Username
		resolved, err := secrets.ResolveOrInfer(inst.Auth.PasswordFile, "email", inst.Name, "password", inst.Auth.PasswordKey, log)
		if err != nil {
			return nil, err
		}
		password = resolved
	}

	handler, err := email.New(email.Config{
		Name:               inst.Name,
		Host:               inst.Host,
		Port:               inst.Port,
		Encryption:         inst.Encryption,
		Username:           username,
		Password:           password,
		From:               inst.From,
		To:                 inst.To,
		Subject:            inst.Subject,
		Template:           inst.Template,
		HTML:               inst.HTML,
		InsecureSkipVerify: inst.InsecureSkipVerify,
	}, log)
	if err != nil {
		return nil, err
	}

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
