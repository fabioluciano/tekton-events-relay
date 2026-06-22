package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/email"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
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
	xoauth2 := false
	var token scm.TokenRefresher
	if inst.Auth != nil {
		username = inst.Auth.Username
		switch {
		case inst.Auth.XOAuth2:
			xoauth2 = true
			refresher, err := resolveFileRefresher(inst.Auth.TokenFile, inst.Auth.TokenKey, "email", inst.Name, log)
			if err != nil {
				return nil, err
			}
			token = refresher
		case inst.Auth.Username != "":
			resolved, err := secrets.ResolveOrInfer(inst.Auth.PasswordFile, "email", inst.Name, "password", inst.Auth.PasswordKey, log)
			if err != nil {
				return nil, err
			}
			password = resolved
		}
	}

	handler, err := email.New(email.Config{
		Name:               inst.Name,
		Host:               inst.Host,
		Port:               inst.Port,
		Encryption:         inst.Encryption,
		Username:           username,
		Password:           password,
		XOAuth2:            xoauth2,
		Token:              token,
		From:               inst.From,
		To:                 inst.To,
		Cc:                 inst.Cc,
		Bcc:                inst.Bcc,
		ReplyTo:            inst.ReplyTo,
		Subject:            inst.Subject,
		Template:           inst.Template,
		HTML:               inst.HTML,
		InsecureSkipVerify: inst.InsecureSkipVerify,
		Store:              msgstore.NewMemoryStore(0, 0),
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
