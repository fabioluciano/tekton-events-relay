package scm

import "fmt"

const (
	fmtSameRepo  = "#%d"
	fmtCrossRepo = "%s/%s#%d"
	fmtUserAt    = "@%s"
)

// CrossRefSyntax defines provider-specific markdown syntax for issue and PR references.
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
		SameRepoIssue:    fmtSameRepo,
		CrossRepoIssue:   fmtCrossRepo,
		SameRepoPR:       fmtSameRepo,
		CrossRepoPR:      fmtCrossRepo,
		UserMention:      fmtUserAt,
		SupportsAutoLink: true,
	},
	"gitlab": {
		SameRepoIssue:    fmtSameRepo,
		CrossRepoIssue:   fmtCrossRepo,
		SameRepoPR:       "!%d", // GitLab uses ! for merge requests
		CrossRepoPR:      "%s/%s!%d",
		UserMention:      fmtUserAt,
		SupportsAutoLink: true,
	},
	"bitbucket_cloud": {
		SameRepoIssue:    fmtSameRepo,
		CrossRepoIssue:   fmtCrossRepo,
		SameRepoPR:       fmtSameRepo,
		CrossRepoPR:      fmtCrossRepo,
		UserMention:      fmtUserAt,
		SupportsAutoLink: true,
	},
	"bitbucket_server": {
		SameRepoIssue:    fmtSameRepo,
		CrossRepoIssue:   fmtCrossRepo,
		SameRepoPR:       fmtSameRepo,
		CrossRepoPR:      fmtCrossRepo,
		UserMention:      fmtUserAt,
		SupportsAutoLink: true,
	},
	"azure_devops": {
		SameRepoIssue:    fmtSameRepo,
		CrossRepoIssue:   "", // Not supported
		SameRepoPR:       fmtSameRepo,
		CrossRepoPR:      "", // Not supported
		UserMention:      fmtUserAt,
		SupportsAutoLink: true,
	},
	"gitea": {
		SameRepoIssue:    fmtSameRepo,
		CrossRepoIssue:   fmtCrossRepo,
		SameRepoPR:       fmtSameRepo,
		CrossRepoPR:      fmtCrossRepo,
		UserMention:      fmtUserAt,
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

// FormatIssueRef returns the provider-specific markdown syntax for referencing an issue.
func FormatIssueRef(provider string, issueNum int, owner, repo string) string {
	syntax, ok := RefSyntax[provider]
	if !ok {
		return fmt.Sprintf(fmtSameRepo, issueNum) // Fallback to GitHub-style
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

// FormatPRRef returns the provider-specific markdown syntax for referencing a pull request or merge request.
func FormatPRRef(provider string, prNum int, owner, repo string) string {
	syntax, ok := RefSyntax[provider]
	if !ok {
		return fmt.Sprintf(fmtSameRepo, prNum) // Fallback
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

// FormatUserMention returns the provider-specific markdown syntax for mentioning a user.
func FormatUserMention(provider, username string) string {
	syntax, ok := RefSyntax[provider]
	if !ok || syntax.UserMention == "" {
		return username // Fallback to plain username
	}
	return fmt.Sprintf(syntax.UserMention, username)
}
