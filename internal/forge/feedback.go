package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// FeedbackItem represents an unaddressed piece of PR feedback.
type FeedbackItem struct {
	Kind   string // "inline" or "global"
	ID     string // thread ID for inline, comment ID for global
	Path   string // file path (inline only)
	Line   int    // line number (inline only)
	Author string // login of the comment author
	Body   string // comment text
}

// ListUnaddressedFeedback returns all unaddressed feedback on a PR.
// It includes unresolved inline review threads and global comments not authored
// by the bot and not yet acknowledged (no bot reply and no bot reaction).
func ListUnaddressedFeedback(ctx context.Context, owner, repo string, prNumber int, botLogin, token string) ([]FeedbackItem, error) {
	var items []FeedbackItem

	// Fetch unresolved inline review threads
	inlineItems, err := listUnresolvedThreads(ctx, owner, repo, prNumber, token)
	if err != nil {
		return nil, err
	}
	items = append(items, inlineItems...)

	// Fetch unaddressed global comments
	globalItems, err := listUnacknowledgedGlobalComments(ctx, owner, repo, prNumber, botLogin, token)
	if err != nil {
		return nil, err
	}
	items = append(items, globalItems...)

	return items, nil
}

// listUnresolvedThreads fetches all unresolved inline review threads on a PR.
// It handles pagination and returns only threads with isResolved==false.
func listUnresolvedThreads(ctx context.Context, owner, repo string, prNumber int, token string) ([]FeedbackItem, error) {
	var allItems []FeedbackItem
	after := ""

	for {
		items, hasNext, nextCursor, err := fetchReviewThreadsPage(ctx, owner, repo, prNumber, after, token)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, items...)

		if !hasNext {
			break
		}
		after = nextCursor
	}

	return allItems, nil
}

// fetchReviewThreadsPage fetches a single page of review threads from the GraphQL API.
func fetchReviewThreadsPage(ctx context.Context, owner, repo string, prNumber int, after string, token string) ([]FeedbackItem, bool, string, error) {
	const graphqlQuery = `query {
  repository(owner: "%s", name: "%s") {
    pullRequest(number: %d) {
      reviewThreads(first: 100, after: %s) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          isResolved
          path
          line
          comments(first: 100) {
            nodes {
              id
              body
              author {
                login
              }
            }
          }
        }
      }
    }
  }
}`

	afterStr := "null"
	if after != "" {
		afterStr = fmt.Sprintf("\"%s\"", after)
	}

	queryStr := fmt.Sprintf(graphqlQuery, owner, repo, prNumber, afterStr)
	payload := map[string]string{"query": queryStr}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", GitHubBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, "", fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Path       string `json:"path"`
							Line       int    `json:"line"`
							Comments   struct {
								Nodes []struct {
									ID     string `json:"id"`
									Body   string `json:"body"`
									Author struct {
										Login string `json:"login"`
									} `json:"author"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, false, "", fmt.Errorf("failed to parse response: %w", err)
	}

	var items []FeedbackItem
	for _, thread := range result.Data.Repository.PullRequest.ReviewThreads.Nodes {
		// Only include unresolved threads
		if thread.IsResolved {
			continue
		}

		// Get the first comment (or the last if we want the most recent?)
		// Use the first comment as the feedback author
		if len(thread.Comments.Nodes) > 0 {
			comment := thread.Comments.Nodes[0]
			items = append(items, FeedbackItem{
				Kind:   "inline",
				ID:     thread.ID,
				Path:   thread.Path,
				Line:   thread.Line,
				Author: comment.Author.Login,
				Body:   comment.Body,
			})
		}
	}

	hasNextPage := result.Data.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage
	endCursor := result.Data.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor

	return items, hasNextPage, endCursor, nil
}

// listUnacknowledgedGlobalComments fetches global PR comments that are not authored by the bot
// and not yet acknowledged (no bot reply and no bot reaction).
func listUnacknowledgedGlobalComments(ctx context.Context, owner, repo string, prNumber int, botLogin, token string) ([]FeedbackItem, error) {
	var allItems []FeedbackItem
	after := ""

	for {
		items, hasNext, nextCursor, err := fetchGlobalCommentsPage(ctx, owner, repo, prNumber, after, botLogin, token)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, items...)

		if !hasNext {
			break
		}
		after = nextCursor
	}

	return allItems, nil
}

// fetchGlobalCommentsPage fetches a single page of global PR comments from the GraphQL API.
// It filters out comments authored by the bot and comments already acknowledged (bot reaction).
func fetchGlobalCommentsPage(ctx context.Context, owner, repo string, prNumber int, after string, botLogin, token string) ([]FeedbackItem, bool, string, error) {
	const graphqlQuery = `query {
  repository(owner: "%s", name: "%s") {
    pullRequest(number: %d) {
      comments(first: 100, after: %s) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          body
          author {
            login
          }
          reactionGroups {
            content
            users(first: 1) {
              nodes {
                login
              }
            }
          }
        }
      }
    }
  }
}`

	afterStr := "null"
	if after != "" {
		afterStr = fmt.Sprintf("\"%s\"", after)
	}

	queryStr := fmt.Sprintf(graphqlQuery, owner, repo, prNumber, afterStr)
	payload := map[string]string{"query": queryStr}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", GitHubBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, "", fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Repository struct {
				PullRequest struct {
					Comments struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID     string `json:"id"`
							Body   string `json:"body"`
							Author struct {
								Login string `json:"login"`
							} `json:"author"`
							ReactionGroups []struct {
								Content string `json:"content"`
								Users   struct {
									Nodes []struct {
										Login string `json:"login"`
									} `json:"nodes"`
								} `json:"users"`
							} `json:"reactionGroups"`
						} `json:"nodes"`
					} `json:"comments"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, false, "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for GraphQL errors in the response
	if len(result.Errors) > 0 {
		return nil, false, "", fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	var items []FeedbackItem
	for _, comment := range result.Data.Repository.PullRequest.Comments.Nodes {
		// Skip comments authored by the bot
		if comment.Author.Login == botLogin {
			continue
		}

		// Check if comment has been acknowledged
		// Acknowledged = bot has reacted
		acknowledged := false

		// Check for bot reactions
		for _, reactionGroup := range comment.ReactionGroups {
			for _, user := range reactionGroup.Users.Nodes {
				if user.Login == botLogin {
					acknowledged = true
					break
				}
			}
			if acknowledged {
				break
			}
		}

		// Only include unacknowledged comments
		if !acknowledged {
			items = append(items, FeedbackItem{
				Kind:   "global",
				ID:     comment.ID,
				Author: comment.Author.Login,
				Body:   comment.Body,
			})
		}
	}

	hasNextPage := result.Data.Repository.PullRequest.Comments.PageInfo.HasNextPage
	endCursor := result.Data.Repository.PullRequest.Comments.PageInfo.EndCursor

	return items, hasNextPage, endCursor, nil
}
