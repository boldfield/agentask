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
	Kind       string // "inline" or "global"
	ID         string // thread ID for inline, comment node ID for global
	DatabaseID string // numeric comment ID for global items (for REST API)
	PRID       string // PR node ID (for reply comments on global items)
	Path       string // file path (inline only)
	Line       int    // line number (inline only)
	Author     string // login of the comment author
	Body       string // comment text
}

// comment represents a global PR comment from the GraphQL API.
type comment struct {
	ID         string
	DatabaseID string
	Body       string
	CreatedAt  string
	Author     struct {
		Login string
	}
	ReactionGroups []struct {
		Content string
		Users   struct {
			Nodes []struct {
				Login string
			}
		}
	}
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
	var allComments []comment
	var prNodeID string
	after := ""

	// Fetch all comments (handle pagination)
	for {
		comments, prID, hasNext, nextCursor, err := fetchGlobalCommentsPageRaw(ctx, owner, repo, prNumber, after, botLogin, token)
		if err != nil {
			return nil, err
		}
		allComments = append(allComments, comments...)
		prNodeID = prID

		if !hasNext {
			break
		}
		after = nextCursor
	}

	// Filter comments: exclude bot-authored, exclude acknowledged (bot reaction or reply)
	var items []FeedbackItem
	for _, comment := range allComments {
		// Skip comments authored by the bot
		if comment.Author.Login == botLogin {
			continue
		}

		// Check if comment has been acknowledged
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

		// Check for bot replies (bot comment created after this comment)
		if !acknowledged {
			for _, other := range allComments {
				if other.Author.Login == botLogin && other.CreatedAt > comment.CreatedAt {
					acknowledged = true
					break
				}
			}
		}

		// Only include unacknowledged comments
		if !acknowledged {
			items = append(items, FeedbackItem{
				Kind:       "global",
				ID:         comment.ID,
				DatabaseID: comment.DatabaseID,
				PRID:       prNodeID,
				Author:     comment.Author.Login,
				Body:       comment.Body,
			})
		}
	}

	return items, nil
}

// fetchGlobalCommentsPageRaw fetches a single page of global PR comments from the GraphQL API.
// It returns raw comments without filtering, along with the PR node ID.
func fetchGlobalCommentsPageRaw(ctx context.Context, owner, repo string, prNumber int, after string, botLogin, token string) ([]comment, string, bool, string, error) {
	const graphqlQuery = `query {
  repository(owner: "%s", name: "%s") {
    pullRequest(number: %d) {
      id
      comments(first: 100, after: %s) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          databaseId
          body
          author {
            login
          }
          createdAt
          reactionGroups {
            content
            users(first: 100) {
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
		return nil, "", false, "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", GitHubBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false, "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", false, "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", false, "", fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Repository struct {
				PullRequest struct {
					ID       string `json:"id"`
					Comments struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID         string `json:"id"`
							DatabaseID int    `json:"databaseId"`
							Body       string `json:"body"`
							CreatedAt  string `json:"createdAt"`
							Author     struct {
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
		return nil, "", false, "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for GraphQL errors in the response
	if len(result.Errors) > 0 {
		return nil, "", false, "", fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	prNodeID := result.Data.Repository.PullRequest.ID
	var comments []comment
	for _, node := range result.Data.Repository.PullRequest.Comments.Nodes {
		c := comment{
			ID:         node.ID,
			DatabaseID: fmt.Sprintf("%d", node.DatabaseID),
			Body:       node.Body,
			CreatedAt:  node.CreatedAt,
		}
		c.Author.Login = node.Author.Login
		for _, rg := range node.ReactionGroups {
			var userNodes []struct {
				Login string
			}
			for _, u := range rg.Users.Nodes {
				userNodes = append(userNodes, struct {
					Login string
				}{Login: u.Login})
			}
			reactionGroup := struct {
				Content string
				Users   struct {
					Nodes []struct {
						Login string
					}
				}
			}{
				Content: rg.Content,
				Users: struct {
					Nodes []struct {
						Login string
					}
				}{Nodes: userNodes},
			}
			c.ReactionGroups = append(c.ReactionGroups, reactionGroup)
		}
		comments = append(comments, c)
	}

	hasNextPage := result.Data.Repository.PullRequest.Comments.PageInfo.HasNextPage
	endCursor := result.Data.Repository.PullRequest.Comments.PageInfo.EndCursor

	return comments, prNodeID, hasNextPage, endCursor, nil
}

// AcknowledgeFeedbackItem marks a feedback item as addressed.
// For inline items (review threads): posts a reply comment and resolves the thread via GraphQL.
// For global items (comments): posts a reply comment and adds a thumbsup reaction.
func AcknowledgeFeedbackItem(ctx context.Context, owner, repo string, prNumber int, token string, item FeedbackItem, fixingSha string) error {
	if item.Kind == "inline" {
		// For inline items: post reply and resolve thread
		if err := postReviewThreadReply(ctx, item.ID, fixingSha, token); err != nil {
			return err
		}
		if err := resolveReviewThread(ctx, item.ID, token); err != nil {
			return err
		}
	} else if item.Kind == "global" {
		// For global items: post reply and add reaction
		if err := postCommentReply(ctx, item.PRID, fixingSha, item.ID, token); err != nil {
			return err
		}
		if err := addThumbsupReaction(ctx, owner, repo, item.DatabaseID, token); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unknown feedback item kind: %s", item.Kind)
	}
	return nil
}

// postReviewThreadReply posts a reply comment to a review thread via GraphQL.
func postReviewThreadReply(ctx context.Context, threadID, fixingSha, token string) error {
	const mutationTemplate = `mutation {
  addPullRequestReviewThreadReply(input: {threadId: "%s", body: "addressed in %s"}) {
    comment {
      id
    }
  }
}`

	mutation := fmt.Sprintf(mutationTemplate, threadID, fixingSha)
	payload := map[string]string{"query": mutation}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", GitHubBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return nil
}

// resolveReviewThread resolves a review thread via GraphQL.
func resolveReviewThread(ctx context.Context, threadID, token string) error {
	const mutationTemplate = `mutation {
  resolveReviewThread(input: {threadId: "%s"}) {
    thread {
      id
    }
  }
}`

	mutation := fmt.Sprintf(mutationTemplate, threadID)
	payload := map[string]string{"query": mutation}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", GitHubBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return nil
}

// postCommentReply posts a reply comment to a PR global comment via GraphQL.
func postCommentReply(ctx context.Context, prNodeID, fixingSha, originalCommentID, token string) error {
	const mutationTemplate = `mutation {
  createIssueComment(input: {subjectId: "%s", body: "addressed in %s (see comment %s)"}) {
    commentEdge {
      node {
        id
      }
    }
  }
}`

	mutation := fmt.Sprintf(mutationTemplate, prNodeID, fixingSha, originalCommentID)
	payload := map[string]string{"query": mutation}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", GitHubBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return nil
}

// addThumbsupReaction adds a thumbsup reaction to a comment via REST API.
// commentDatabaseID is the numeric database ID (from databaseId field), not the GraphQL node ID.
func addThumbsupReaction(ctx context.Context, owner, repo, commentDatabaseID, token string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%s/reactions", GitHubBaseURL, owner, repo, commentDatabaseID)

	payload := map[string]string{"content": "+1"}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// 200 or 201 are both acceptable for this endpoint
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("reaction request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
