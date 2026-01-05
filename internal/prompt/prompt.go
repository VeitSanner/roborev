package prompt

import (
	"fmt"
	"strings"

	"github.com/wesm/roborev/internal/git"
	"github.com/wesm/roborev/internal/storage"
)

// SystemPromptSingle is the base instruction for single commit reviews
const SystemPromptSingle = `You are a code reviewer. Review the git commit shown below for:

1. **Bugs**: Logic errors, off-by-one errors, null/undefined issues, race conditions
2. **Security**: Injection vulnerabilities, auth issues, data exposure
3. **Testing gaps**: Missing unit tests, edge cases not covered, e2e/integration test gaps
4. **Regressions**: Changes that might break existing functionality
5. **Code quality**: Duplication that should be refactored, overly complex logic, unclear naming

After reviewing against all criteria above:

If you find issues, list them with:
- Severity (high/medium/low)
- File and line reference where possible
- A brief explanation of the problem and suggested fix

If you find no issues, confirm you checked for bugs, security issues, testing gaps,
regressions, and code quality concerns, then briefly summarize what the commit does.`

// SystemPromptRange is the base instruction for commit range reviews
const SystemPromptRange = `You are a code reviewer. Review the git commit range shown below for:

1. **Bugs**: Logic errors, off-by-one errors, null/undefined issues, race conditions
2. **Security**: Injection vulnerabilities, auth issues, data exposure
3. **Testing gaps**: Missing unit tests, edge cases not covered, e2e/integration test gaps
4. **Regressions**: Changes that might break existing functionality
5. **Code quality**: Duplication that should be refactored, overly complex logic, unclear naming

After reviewing against all criteria above:

If you find issues, list them with:
- Severity (high/medium/low)
- File and line reference where possible
- A brief explanation of the problem and suggested fix

If you find no issues, confirm you checked for bugs, security issues, testing gaps,
regressions, and code quality concerns, then briefly summarize what the commits do.`

// PreviousReviewsHeader introduces the previous reviews section
const PreviousReviewsHeader = `
## Previous Reviews

The following are reviews of recent commits in this repository. Use them as context
to understand ongoing work and to check if the current commit addresses previous feedback.
`

// ReviewContext holds a commit SHA and its associated review (if any)
type ReviewContext struct {
	SHA    string
	Review *storage.Review
}

// Builder constructs review prompts
type Builder struct {
	db *storage.DB
}

// NewBuilder creates a new prompt builder
func NewBuilder(db *storage.DB) *Builder {
	return &Builder{db: db}
}

// Build constructs a review prompt for a commit or range with context from previous reviews
func (b *Builder) Build(repoPath, gitRef string, repoID int64, contextCount int) (string, error) {
	if git.IsRange(gitRef) {
		return b.buildRangePrompt(repoPath, gitRef, repoID, contextCount)
	}
	return b.buildSinglePrompt(repoPath, gitRef, repoID, contextCount)
}

// buildSinglePrompt constructs a prompt for a single commit
func (b *Builder) buildSinglePrompt(repoPath, sha string, repoID int64, contextCount int) (string, error) {
	var sb strings.Builder

	// Start with system prompt
	sb.WriteString(SystemPromptSingle)
	sb.WriteString("\n")

	// Get previous reviews if requested
	if contextCount > 0 && b.db != nil {
		contexts, err := b.getPreviousReviewContexts(repoPath, sha, contextCount)
		if err != nil {
			// Log but don't fail - previous reviews are nice-to-have context
			// Just continue without them
		} else if len(contexts) > 0 {
			b.writePreviousReviews(&sb, contexts)
		}
	}

	// Current commit section
	shortSHA := sha
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}
	sb.WriteString("## Current Commit\n\n")
	sb.WriteString(fmt.Sprintf("Review the following commit: %s\n", shortSHA))

	return sb.String(), nil
}

// buildRangePrompt constructs a prompt for a commit range
func (b *Builder) buildRangePrompt(repoPath, rangeRef string, repoID int64, contextCount int) (string, error) {
	var sb strings.Builder

	// Start with system prompt for ranges
	sb.WriteString(SystemPromptRange)
	sb.WriteString("\n")

	// Get previous reviews from before the range start
	if contextCount > 0 && b.db != nil {
		startSHA, err := git.GetRangeStart(repoPath, rangeRef)
		if err == nil {
			contexts, err := b.getPreviousReviewContexts(repoPath, startSHA, contextCount)
			if err == nil && len(contexts) > 0 {
				b.writePreviousReviews(&sb, contexts)
			}
		}
	}

	// Get commits in range
	commits, err := git.GetRangeCommits(repoPath, rangeRef)
	if err != nil {
		return "", fmt.Errorf("get range commits: %w", err)
	}

	// Commit range section
	sb.WriteString("## Commit Range\n\n")
	sb.WriteString(fmt.Sprintf("Reviewing %d commits:\n\n", len(commits)))

	for _, sha := range commits {
		info, err := git.GetCommitInfo(repoPath, sha)
		shortSHA := sha
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		if err == nil {
			sb.WriteString(fmt.Sprintf("- %s %s\n", shortSHA, info.Subject))
		} else {
			sb.WriteString(fmt.Sprintf("- %s\n", shortSHA))
		}
	}
	sb.WriteString("\n")

	return sb.String(), nil
}

// writePreviousReviews writes the previous reviews section to the builder
func (b *Builder) writePreviousReviews(sb *strings.Builder, contexts []ReviewContext) {
	sb.WriteString(PreviousReviewsHeader)
	sb.WriteString("\n")

	// Show in chronological order (oldest first) for narrative flow
	for i := len(contexts) - 1; i >= 0; i-- {
		ctx := contexts[i]
		shortSHA := ctx.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}

		sb.WriteString(fmt.Sprintf("--- Review for commit %s ---\n", shortSHA))
		if ctx.Review != nil {
			sb.WriteString(ctx.Review.Output)
		} else {
			sb.WriteString("No review available.")
		}
		sb.WriteString("\n\n")
	}
}

// getPreviousReviewContexts gets the N commits before the target and looks up their reviews
func (b *Builder) getPreviousReviewContexts(repoPath, sha string, count int) ([]ReviewContext, error) {
	// Get parent commits from git
	parentSHAs, err := git.GetParentCommits(repoPath, sha, count)
	if err != nil {
		return nil, fmt.Errorf("get parent commits: %w", err)
	}

	var contexts []ReviewContext
	for _, parentSHA := range parentSHAs {
		ctx := ReviewContext{SHA: parentSHA}

		// Try to look up review for this commit
		review, err := b.db.GetReviewByCommitSHA(parentSHA)
		if err == nil {
			ctx.Review = review
		}
		// If no review found, ctx.Review stays nil

		contexts = append(contexts, ctx)
	}

	return contexts, nil
}

// BuildSimple constructs a simpler prompt without database context
func BuildSimple(repoPath, sha string) (string, error) {
	b := &Builder{}
	return b.Build(repoPath, sha, 0, 0)
}
