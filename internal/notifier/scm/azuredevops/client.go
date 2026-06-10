// Package azuredevops provides Azure DevOps SCM notifier client.
package azuredevops

import (
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"go.uber.org/zap"
)

// Client holds the Azure DevOps SDK connection.
type Client struct {
	conn  *azuredevops.Connection
	genre string
	log   *zap.Logger
}

// NewClient creates a new Azure DevOps API client using the official SDK.
//
//nolint:revive // insecureSkipVerify intentionally unused: Azure DevOps SDK manages HTTP internally
func NewClient(token, baseURL, genre string, _ bool, debug bool, log *zap.Logger) *Client {
	if log == nil {
		log = zap.NewNop()
	}

	if baseURL == "" {
		baseURL = "https://dev.azure.com"
	}
	if genre == "" {
		genre = "tekton-ci"
	}

	// Create the connection with PAT authentication
	conn := azuredevops.NewPatConnection(baseURL, token)

	// Note: The Azure DevOps Go SDK v7 doesn't support custom HTTP clients or user agents
	// in the public API. TLS and debug settings would need to be handled at a lower level
	// if absolutely required, but the SDK manages HTTP internally.

	return &Client{
		conn:  conn,
		genre: genre,
		log:   log,
	}
}
