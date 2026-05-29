package scm

import (
	"fmt"
	"unicode/utf8"
)

// FieldLimits defines provider-specific field length limits.
// Limits researched from official API docs and implementation analysis.
type FieldLimits struct {
	StatusDescription int // Commit status description max length
	StatusContext     int // Commit status context max length
	CommentBody       int // Issue/PR comment body max length
	LabelName         int // Label name max length
}

// Limits maps provider names to their API field limits.
// Sources:
// - GitHub: Conservative (undocumented) - 140 chars status desc, 65k comments
// - GitLab: Documented - 255 chars status, 1M chars comments
// - Bitbucket: Implementation - 255 chars status, 65k comments
// - Azure DevOps: Implementation - 4000 chars status, 65k comments
// - Gitea: Conservative (GitHub-compatible) - 255 chars status, 65k comments
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

// Truncate truncates string to max runes, adds "..." if truncated.
// Returns original string if within limit or max is 0 (unlimited).
// Uses rune-aware counting for proper Unicode support.
func Truncate(s string, max int) string {
	if max == 0 {
		return s
	}

	runeCount := utf8.RuneCountInString(s)
	if runeCount <= max {
		return s
	}

	runes := []rune(s)
	if max < 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

// Validate checks if value exceeds provider-specific field limit.
// Returns error if limit exceeded, nil if within limit or no limit defined.
// Unknown providers or fields return nil (no validation).
func Validate(provider, field, value string) error {
	limits, ok := Limits[provider]
	if !ok {
		return nil // Unknown provider, no validation
	}

	var max int
	switch field {
	case "status_description":
		max = limits.StatusDescription
	case "status_context":
		max = limits.StatusContext
	case "comment_body":
		max = limits.CommentBody
	case "label_name":
		max = limits.LabelName
	default:
		return nil // Unknown field, no validation
	}

	if max == 0 {
		return nil // Unlimited
	}

	length := utf8.RuneCountInString(value)
	if length > max {
		return fmt.Errorf("field %q exceeds %s limit (%d chars, got %d)", field, provider, max, length)
	}
	return nil
}

// ValidateAndTruncate applies provider-specific limits to field.
// Returns truncated string with "..." suffix if exceeds limit.
// Returns original string if provider unknown or field has no limit.
//
// Deprecated: Use Validate() instead. Silent truncation can corrupt data.
// This function remains for backward compatibility during migration.
func ValidateAndTruncate(provider, field, value string) string {
	limits, ok := Limits[provider]
	if !ok {
		return value // Unknown provider, no truncation
	}

	var max int
	switch field {
	case "status_description":
		max = limits.StatusDescription
	case "status_context":
		max = limits.StatusContext
	case "comment_body":
		max = limits.CommentBody
	case "label_name":
		max = limits.LabelName
	default:
		return value // Unknown field
	}

	return Truncate(value, max)
}
