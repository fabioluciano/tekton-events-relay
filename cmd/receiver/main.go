// Package main provides the tekton-events-relay receiver service that listens for CloudEvents
// and dispatches notifications to configured destinations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/cehttp"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/event/tekton"
	"github.com/fabioluciano/tekton-events-relay/internal/logging"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/datadog"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/discord"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/pagerduty"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/azuredevops"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/bitbucket"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/gitea"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/github"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/gitlab"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/sourcehut"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/slack"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/teams"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/webhook"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

func main() {
	configPath := flag.String("config", "/etc/tekton-events-relay/config.toml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		// Bootstrap logger for config load error
		bootLog, _ := logging.New("info", false)
		bootLog.Error("load config", zap.Error(err))
		os.Exit(1)
	}

	log, err := logging.New(cfg.Logging.Level, cfg.Debug.Enabled)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	reg := buildActionHandlers(cfg, log)
	decoders := buildDecoders()
	chain := buildChain(cfg, reg, log)
	srv := buildServer(cfg, decoders, chain, reg, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("server listening",
			zap.String("addr", cfg.Server.Addr),
			zap.Strings("handlers", reg.Names()),
			zap.Strings("decoders", decoders.Names()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http serve", zap.Error(err))
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", zap.Error(err))
	}
}

func buildActionHandlers(cfg *config.Config, log *zap.Logger) *notifier.Registry {
	reg := notifier.NewRegistry()
	n := cfg.Notifiers

	// wrapWithCEL wraps handler with CEL guard if whenExpr is non-empty
	wrapWithCEL := func(handler notifier.ActionHandler, whenExpr string) notifier.ActionHandler {
		if whenExpr == "" {
			return handler
		}
		prog, err := cel.Compile(whenExpr)
		if err != nil {
			log.Fatal("invalid CEL expression", zap.String("expr", whenExpr), zap.Error(err))
		}
		return notifier.NewConditionalHandler(handler, prog, log)
	}

	// SCM — GitHub uses new action handlers
	if c := n.GitHub; c != nil && c.Enabled {
		// Commit status (always enabled for backward compatibility)
		reg.Register(github.NewStatusReporter(c.Token, c.BaseURL, c.InsecureSkipVerify))

		// Action handlers (optional, config-driven)
		if c.Actions != nil {
			// PR comment handler
			if c.Actions.PRComment != nil && c.Actions.PRComment.Enabled {
				handler := github.NewPRCommentHandler(github.PRCommentConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					Template:           c.Actions.PRComment.Template,
					OnStates:           c.Actions.PRComment.OnStates,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.PRComment.When))
			}

			// Issue comment handler
			if c.Actions.IssueComment != nil && c.Actions.IssueComment.Enabled {
				handler := github.NewIssueCommentHandler(github.IssueCommentConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					Template:           c.Actions.IssueComment.Template,
					OnStates:           c.Actions.IssueComment.OnStates,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.IssueComment.When))
			}

			// Label handler
			if c.Actions.Label != nil && c.Actions.Label.Enabled {
				handler := github.NewLabelHandler(github.LabelConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					SuccessLabel:       c.Actions.Label.SuccessLabel,
					FailureLabel:       c.Actions.Label.FailureLabel,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.Label.When))
			}
		}
	}

	// SCM — legacy providers wrapped with NotifierAdapter
	if c := n.GitLabCloud; c != nil && c.Enabled {
		// Commit status via legacy notifier wrapper
		reg.Register(notifier.WrapNotifier(gitlab.NewCloud(gitlab.Config{
			Token:              c.Token,
			BaseURL:            c.BaseURL,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))

		// Action handlers (optional, config-driven)
		if c.Actions != nil {
			// Label handler
			if c.Actions.Label != nil && c.Actions.Label.Enabled {
				handler := gitlab.NewLabelHandler(gitlab.LabelConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					Name:               "gitlab-cloud",
					SuccessLabel:       c.Actions.Label.SuccessLabel,
					FailureLabel:       c.Actions.Label.FailureLabel,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.Label.When))
			}
		}
	}
	if c := n.GitLabServer; c != nil && c.Enabled {
		// Commit status via legacy notifier wrapper
		reg.Register(notifier.WrapNotifier(gitlab.NewServer(gitlab.Config{
			Token:              c.Token,
			BaseURL:            c.BaseURL,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))

		// Action handlers (optional, config-driven)
		if c.Actions != nil {
			// Label handler
			if c.Actions.Label != nil && c.Actions.Label.Enabled {
				handler := gitlab.NewLabelHandler(gitlab.LabelConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					Name:               "gitlab-server",
					SuccessLabel:       c.Actions.Label.SuccessLabel,
					FailureLabel:       c.Actions.Label.FailureLabel,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.Label.When))
			}
		}
	}
	if c := n.BitbucketCloud; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(bitbucket.NewCloud(bitbucket.CloudConfig{
			Username:           c.Username,
			AppPassword:        c.AppPassword,
			BaseURL:            c.BaseURL,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))
	}
	if c := n.BitbucketServer; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(bitbucket.NewServer(bitbucket.ServerConfig{
			Token:              c.Token,
			BaseURL:            c.BaseURL,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))
	}
	if c := n.AzureDevOps; c != nil && c.Enabled {
		// Commit status via legacy notifier wrapper
		reg.Register(notifier.WrapNotifier(azuredevops.New(azuredevops.Config{
			Token:              c.Token,
			BaseURL:            c.BaseURL,
			Genre:              c.Genre,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))

		// Action handlers (optional, config-driven)
		if c.Actions != nil {
			// Label handler
			if c.Actions.Label != nil && c.Actions.Label.Enabled {
				handler := azuredevops.NewLabelHandler(azuredevops.LabelConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					SuccessLabel:       c.Actions.Label.SuccessLabel,
					FailureLabel:       c.Actions.Label.FailureLabel,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.Label.When))
			}
		}
	}
	if c := n.Gitea; c != nil && c.Enabled {
		// Commit status via legacy notifier wrapper
		reg.Register(notifier.WrapNotifier(gitea.New(gitea.Config{
			Token:              c.Token,
			BaseURL:            c.BaseURL,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))

		// Action handlers (optional, config-driven)
		if c.Actions != nil {
			// PR comment handler
			if c.Actions.PRComment != nil && c.Actions.PRComment.Enabled {
				handler := gitea.NewPRCommentHandler(gitea.PRCommentConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					Template:           c.Actions.PRComment.Template,
					OnStates:           c.Actions.PRComment.OnStates,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.PRComment.When))
			}

			// Issue comment handler
			if c.Actions.IssueComment != nil && c.Actions.IssueComment.Enabled {
				handler := gitea.NewIssueCommentHandler(gitea.IssueCommentConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					Template:           c.Actions.IssueComment.Template,
					OnStates:           c.Actions.IssueComment.OnStates,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.IssueComment.When))
			}

			// Label handler
			if c.Actions.Label != nil && c.Actions.Label.Enabled {
				handler := gitea.NewLabelHandler(gitea.LabelConfig{
					Token:              c.Token,
					BaseURL:            c.BaseURL,
					SuccessLabel:       c.Actions.Label.SuccessLabel,
					FailureLabel:       c.Actions.Label.FailureLabel,
					InsecureSkipVerify: c.InsecureSkipVerify,
				})
				reg.Register(wrapWithCEL(handler, c.Actions.Label.When))
			}
		}
	}
	if c := n.SourceHut; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(sourcehut.New(sourcehut.Config{
			Token:              c.Token,
			BaseURL:            c.BaseURL,
			InsecureSkipVerify: c.InsecureSkipVerify,
		})))
	}

	// Chat — wrapped with NotifierAdapter
	if c := n.Slack; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(slack.New(slack.Config{
			WebhookURL: c.WebhookURL, Channel: c.Channel,
			Username: c.Username, IconEmoji: c.IconEmoji, NotifyOn: c.NotifyOn,
		})))
	}
	if c := n.Teams; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(teams.New(teams.Config{WebhookURL: c.WebhookURL, NotifyOn: c.NotifyOn})))
	}
	if c := n.Discord; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(discord.New(discord.Config{
			WebhookURL: c.WebhookURL, Username: c.Username, NotifyOn: c.NotifyOn,
		})))
	}

	// Alerting / Observability — wrapped with NotifierAdapter
	if c := n.PagerDuty; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(pagerduty.New(pagerduty.Config{IntegrationKey: c.IntegrationKey, Severity: c.Severity})))
	}
	if c := n.Datadog; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(datadog.New(datadog.Config{APIKey: c.APIKey, Site: c.Site, Tags: c.Tags, NotifyOn: c.NotifyOn})))
	}

	// Generic — wrapped with NotifierAdapter
	if c := n.Webhook; c != nil && c.Enabled {
		reg.Register(notifier.WrapNotifier(webhook.New(webhook.Config{URL: c.URL, Headers: c.Headers, NotifyOn: c.NotifyOn})))
	}

	if len(reg.Names()) == 0 {
		log.Warn("no action handlers enabled")
	}
	return reg
}

func buildDecoders() *event.Registry {
	r := event.NewRegistry()
	r.Register(tekton.New())
	return r
}

func buildChain(cfg *config.Config, reg *notifier.Registry, log *zap.Logger) pipeline.Handler {
	deduper := pipeline.NewDeduper(cfg.DedupeSize)
	return pipeline.Build(
		pipeline.NewValidator(),
		pipeline.NewEventFilter(cfg.Filter.AllowTaskRun, cfg.Filter.AllowPipelineRun, cfg.Filter.IgnoreUnknown),
		deduper,
		pipeline.NewEnricher(cfg.DashboardURL),
		pipeline.NewDispatcher(reg, log),
	)
}

func buildServer(cfg *config.Config, decoders *event.Registry, chain pipeline.Handler, reg *notifier.Registry, log *zap.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "ok\nhandlers: %v\ndecoders: %v\n", reg.Names(), decoders.Names())
	})
	mux.HandleFunc("/", cloudEventsHandler(decoders, chain, log))
	return &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
	}
}

// truncatePayload truncates data to maxSize to prevent memory issues in debug logs.
func truncatePayload(data []byte, maxSize int) []byte {
	if len(data) <= maxSize {
		return data
	}
	return data[:maxSize]
}

func cloudEventsHandler(decoders *event.Registry, chain pipeline.Handler, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ce, err := cehttp.FromRequest(r)
		if err != nil {
			http.Error(w, "not a cloudevent: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Debug mode: log incoming CloudEvent payload
		log.Debug("cloudevent received",
			zap.String("id", ce.ID),
			zap.String("type", ce.Type),
			zap.String("source", ce.Source),
			zap.ByteString("data", truncatePayload(ce.Data, 4096)))

		decoder, err := decoders.Find(ce.Type)
		if err != nil {
			log.Debug("no decoder", zap.String("type", ce.Type))
			w.WriteHeader(http.StatusOK)
			return
		}
		env, err := decoder.Decode(event.RawEvent{ID: ce.ID, Type: ce.Type, Source: ce.Source, Data: ce.Data})
		if err != nil {
			log.Debug("skip event", zap.String("decoder", decoder.Name()), zap.Error(err))
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := chain.Handle(ctx, env); err != nil {
			log.Error("chain failed", zap.String("ce_id", env.CloudEventID), zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
