// Package slack implements the Notifier for Slack via Incoming Webhooks.
// Doc: https://api.slack.com/messaging/webhooks
//
// Filters by State: by default notifies on failure, error and success.
// Configurable via NotifyOn.
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	fieldKeyShort      = "short"
	fieldKeyTitle      = "title"
	fieldKeyValue      = "value"
	fieldTitleCommit   = "Commit"
	fieldTitleDuration = "Duration"
	colorFailure       = "#e01e5a"
	colorUnknown       = "#aaaaaa"
	emojiFailure       = ":x:"
	emojiUnknown       = ":grey_question:"
	stateError         = "error"
	stateFailure       = "failure"
	stateSuccess       = "success"
)

// Config holds the Slack notifier configuration.
type Config struct {
	WebhookURL string
	Channel    string   // optional — override of the channel configured in webhook
	NotifyOn   []string // states to notify; default: failure, error, success
	Username   string   // displayed name; default: tekton-events-relay
	IconEmoji  string   // default: :rocket:
}

// Notifier implements the notifier for Slack Incoming Webhooks.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

func New(cfg Config) *Notifier {
	if len(cfg.NotifyOn) == 0 {
		cfg.NotifyOn = []string{stateFailure, stateError, stateSuccess}
	}
	if cfg.Username == "" {
		cfg.Username = "tekton-events-relay"
	}
	if cfg.IconEmoji == "" {
		cfg.IconEmoji = ":rocket:"
	}
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		BuildPayload: n.payload,
		UserAgent:    notifier.UserAgent,
	}
	return n
}

func (n *Notifier) Name() string { return "slack" }

func (n *Notifier) Notify(ctx context.Context, e domain.Event) error {
	if !notifier.ShouldNotify(n.cfg.NotifyOn, e.State) {
		return nil
	}
	return n.base.Send(ctx, e)
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	color := colorFor(e.State)
	emoji := emojiFor(e.State)

	text := fmt.Sprintf("%s *%s* — %s", emoji, e.Context, e.Description)
	if e.TargetURL != "" {
		text += fmt.Sprintf("\n<%s|View run>", e.TargetURL)
	}

	msg := map[string]any{
		"username":   n.cfg.Username,
		"icon_emoji": n.cfg.IconEmoji,
		"attachments": []map[string]any{
			{
				"color":     color,
				"text":      text,
				"footer":    fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
				"mrkdwn_in": []string{"text"},
				"fields":    fields(e),
			},
		},
	}
	if n.cfg.Channel != "" {
		msg["channel"] = n.cfg.Channel
	}
	return msg, nil
}

func fields(e domain.Event) []map[string]any {
	f := []map[string]any{
		{fieldKeyTitle: "State", fieldKeyValue: strings.ToUpper(string(e.State)), fieldKeyShort: true},
		{fieldKeyTitle: "Run", fieldKeyValue: e.RunName, fieldKeyShort: true},
	}
	if e.CommitSHA != "" {
		f = append(f, map[string]any{
			"title": fieldTitleCommit, fieldKeyValue: e.CommitSHA[:min(7, len(e.CommitSHA))], fieldKeyShort: true,
		})
	}
	if !e.StartedAt.IsZero() && !e.FinishedAt.IsZero() {
		d := e.FinishedAt.Sub(e.StartedAt).Round(1e9)
		f = append(f, map[string]any{"title": fieldTitleDuration, fieldKeyValue: d.String(), fieldKeyShort: true})
	}
	return f
}

func colorFor(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return "#36a64f"
	case domain.StateFailure, domain.StateError:
		return colorFailure
	case domain.StateRunning:
		return "#daa038"
	default:
		return colorUnknown
	}
}

func emojiFor(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return ":white_check_mark:"
	case domain.StateFailure, domain.StateError:
		return emojiFailure
	case domain.StateRunning:
		return ":hourglass_flowing_sand:"
	default:
		return emojiUnknown
	}
}
