// Package scm provides SCM provider integrations for commit status, comments, and labels.
package scm

import (
	"fmt"
	"unicode/utf8"
)

// FieldLimits defines SCM provider API field length constraints.
type FieldLimits struct {
	StatusDescription int // Commit status description max length
	StatusContext     int // Commit status context max length
	CommentBody       int // Issue/PR comment body max length
	LabelName         int // Label name max length
}

// Limits maps SCM provider names to their documented API field length limits.
var Limits = map[string]FieldLimits{
	"github": {
		StatusDescription: 140,
		StatusContext:     255,
		CommentBody:       65000,
		LabelName:         50,
	},
	"gitlab": {
		StatusDescription: 255,
		StatusContext:     0, // N/A
		CommentBody:       1000000,
		LabelName:         255,
	},
	"bitbucket-cloud": {
		StatusDescription: 255,
		StatusContext:     255,
		CommentBody:       65000,
		LabelName:         0, // No label support
	},
	"bitbucket-server": {
		StatusDescription: 255,
		StatusContext:     255,
		CommentBody:       65000,
		LabelName:         0, // No label support
	},
	"azure-devops": {
		StatusDescription: 4000,
		StatusContext:     0, // N/A
		CommentBody:       65000,
		LabelName:         255,
	},
	"gitea": {
		StatusDescription: 255,
		StatusContext:     255,
		CommentBody:       65000,
		LabelName:         50,
	},
	"sourcehut": {
		StatusDescription: 0, // N/A (no commit status API)
		StatusContext:     0,
		CommentBody:       0, // Email-based workflow
		LabelName:         0,
	},
}

// Truncate shortens a string to the specified rune limit, appending "..." if truncated.
func Truncate(s string, limit int) string {
	if limit == 0 {
		return s
	}

	runeCount := utf8.RuneCountInString(s)
	if runeCount <= limit {
		return s
	}

	runes := []rune(s)
	if limit < 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

// Validate checks whether a field value exceeds the provider-specific length limit.
func Validate(provider, field, value string) error {
	limits, ok := Limits[provider]
	if !ok {
		return nil // Unknown provider, no validation
	}

	var limit int
	switch field {
	case "status_description":
		limit = limits.StatusDescription
	case "status_context":
		limit = limits.StatusContext
	case "comment_body":
		limit = limits.CommentBody
	case "label_name":
		limit = limits.LabelName
	default:
		return nil // Unknown field, no validation
	}

	if limit == 0 {
		return nil // Unlimited
	}

	length := utf8.RuneCountInString(value)
	if length > limit {
		return fmt.Errorf("field %q exceeds %s limit (%d chars, got %d)", field, provider, limit, length)
	}
	return nil
}
