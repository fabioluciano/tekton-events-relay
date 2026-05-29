package scm

import "fmt"

// CrossRefSyntax defines provider-specific syntax for referencing issues/PRs in comments.
// Research sources:
// - GitHub: https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/autolinked-references-and-urls
// - GitLab: https://docs.gitlab.com/ee/user/markdown.html
// - Bitbucket: https://confluence.atlassian.com/bitbucketserver/markdown-syntax-guide-776639995.html
// - Azure DevOps: https://learn.microsoft.com/en-us/azure/devops/project/wiki/markdown-guidance
// - Gitea: https://docs.gitea.com/usage/markdown
// - SourceHut: https://man.sr.ht/hub.sr.ht/#referencing-tickets-in-commits-and-patches
type CrossRefSyntax struct {
	SameRepoIssue    string // Format for same-repo issue (e.g., "#%d")
	CrossRepoIssue   string // Format for cross-repo issue (e.g., "%s/%s#%d")
	SameRepoPR       string // Format for same-repo PR/MR (e.g., "#%d" or "!%d")
	CrossRepoPR      string // Format for cross-repo PR/MR
	UserMention      string // Format for user mention (e.g., "@%s")
	SupportsAutoLink bool   // Whether provider auto-converts references to links
}

// RefSyntax maps provider names to their cross-reference syntax.
var RefSyntax = map[string]CrossRefSyntax{
	"github": {
		SameRepoIssue:    "#%d",
		CrossRepoIssue:   "%s/%s#%d",
		SameRepoPR:       "#%d",
		CrossRepoPR:      "%s/%s#%d",
		UserMention:      "@%s",
		SupportsAutoLink: true,
	},
	"gitlab": {
		SameRepoIssue:    "#%d",
		CrossRepoIssue:   "%s/%s#%d",
		SameRepoPR:       "!%d", // GitLab uses ! for merge requests
		CrossRepoPR:      "%s/%s!%d",
		UserMention:      "@%s",
		SupportsAutoLink: true,
	},
	"bitbucket_cloud": {
		SameRepoIssue:    "#%d",
		CrossRepoIssue:   "%s/%s#%d",
		SameRepoPR:       "#%d",
		CrossRepoPR:      "%s/%s#%d",
		UserMention:      "@%s",
		SupportsAutoLink: true,
	},
	"bitbucket_server": {
		SameRepoIssue:    "#%d",
		CrossRepoIssue:   "%s/%s#%d",
		SameRepoPR:       "#%d",
		CrossRepoPR:      "%s/%s#%d",
		UserMention:      "@%s",
		SupportsAutoLink: true,
	},
	"azure_devops": {
		SameRepoIssue:    "#%d",
		CrossRepoIssue:   "", // Not supported
		SameRepoPR:       "#%d",
		CrossRepoPR:      "", // Not supported
		UserMention:      "@%s",
		SupportsAutoLink: true,
	},
	"gitea": {
		SameRepoIssue:    "#%d",
		CrossRepoIssue:   "%s/%s#%d",
		SameRepoPR:       "#%d",
		CrossRepoPR:      "%s/%s#%d",
		UserMention:      "@%s",
		SupportsAutoLink: true,
	},
	"sourcehut": {
		SameRepoIssue:    "", // Use commit trailers
		CrossRepoIssue:   "",
		SameRepoPR:       "", // Mailing list patches
		CrossRepoPR:      "",
		UserMention:      "", // Not supported
		SupportsAutoLink: false,
	},
}

// FormatIssueRef returns provider-specific issue reference syntax.
// If owner/repo are empty, returns same-repo format. Otherwise returns cross-repo format.
// Returns generic "#%d" format for unknown providers.
func FormatIssueRef(provider string, issueNum int, owner, repo string) string {
	syntax, ok := RefSyntax[provider]
	if !ok {
		return fmt.Sprintf("#%d", issueNum) // Fallback to GitHub-style
	}

	if owner == "" || repo == "" {
		if syntax.SameRepoIssue == "" {
			return "" // Provider doesn't support inline refs
		}
		return fmt.Sprintf(syntax.SameRepoIssue, issueNum)
	}

	if syntax.CrossRepoIssue == "" {
		return "" // Provider doesn't support cross-repo refs
	}
	return fmt.Sprintf(syntax.CrossRepoIssue, owner, repo, issueNum)
}

// FormatPRRef returns provider-specific PR/MR reference syntax.
// GitLab uses "!" for merge requests, others use "#".
func FormatPRRef(provider string, prNum int, owner, repo string) string {
	syntax, ok := RefSyntax[provider]
	if !ok {
		return fmt.Sprintf("#%d", prNum) // Fallback
	}

	if owner == "" || repo == "" {
		if syntax.SameRepoPR == "" {
			return ""
		}
		return fmt.Sprintf(syntax.SameRepoPR, prNum)
	}

	if syntax.CrossRepoPR == "" {
		return ""
	}
	return fmt.Sprintf(syntax.CrossRepoPR, owner, repo, prNum)
}

// FormatUserMention returns provider-specific user mention syntax.
// Returns plain username for providers that don't support mentions (SourceHut).
func FormatUserMention(provider, username string) string {
	syntax, ok := RefSyntax[provider]
	if !ok || syntax.UserMention == "" {
		return username // Fallback to plain username
	}
	return fmt.Sprintf(syntax.UserMention, username)
}
