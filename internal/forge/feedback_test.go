package forge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListUnaddressedFeedback_UnresolvedThreads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Response for review threads query (first call)
		if strings.Contains(bodyStr, "reviewThreads") {
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": [
            {
              "id": "thread-1",
              "isResolved": false,
              "path": "main.go",
              "line": 42,
              "comments": {
                "nodes": [
                  {
                    "id": "comment-1",
                    "body": "This needs fixing",
                    "author": {
                      "login": "reviewer"
                    }
                  }
                ]
              }
            },
            {
              "id": "thread-2",
              "isResolved": true,
              "path": "main.go",
              "line": 50,
              "comments": {
                "nodes": [
                  {
                    "id": "comment-2",
                    "body": "Already fixed",
                    "author": {
                      "login": "reviewer"
                    }
                  }
                ]
              }
            }
          ]
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		} else if strings.Contains(bodyStr, "comments") {
			// Response for global comments query (second call) - return empty
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	if len(items) != 1 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 1", len(items))
	}

	if items[0].Kind != "inline" {
		t.Errorf("items[0].Kind = %q, want %q", items[0].Kind, "inline")
	}

	if items[0].ID != "thread-1" {
		t.Errorf("items[0].ID = %q, want %q", items[0].ID, "thread-1")
	}

	if items[0].Path != "main.go" {
		t.Errorf("items[0].Path = %q, want %q", items[0].Path, "main.go")
	}

	if items[0].Line != 42 {
		t.Errorf("items[0].Line = %d, want 42", items[0].Line)
	}

	if items[0].Author != "reviewer" {
		t.Errorf("items[0].Author = %q, want %q", items[0].Author, "reviewer")
	}

	if items[0].Body != "This needs fixing" {
		t.Errorf("items[0].Body = %q, want %q", items[0].Body, "This needs fixing")
	}
}

func TestListUnaddressedFeedback_GlobalCommentsBotExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Response for review threads query (first call)
		if strings.Contains(bodyStr, "reviewThreads") {
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		} else if strings.Contains(bodyStr, "comments") {
			// Response for global comments query
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": [
            {
              "id": "comment-1",
              "body": "Human feedback",
              "createdAt": "2024-01-01T10:00:00Z",
              "author": {
                "login": "human"
              },
              "reactionGroups": []
            },
            {
              "id": "comment-2",
              "body": "Bot response",
              "createdAt": "2024-01-01T10:00:00Z",
              "author": {
                "login": "bot"
              },
              "reactionGroups": []
            }
          ]
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	if len(items) != 1 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 1", len(items))
	}

	if items[0].Author != "human" {
		t.Errorf("items[0].Author = %q, want %q", items[0].Author, "human")
	}
}

func TestListUnaddressedFeedback_AcknowledgedCommentExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Response for review threads query (first call)
		if strings.Contains(bodyStr, "reviewThreads") {
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		} else if strings.Contains(bodyStr, "comments") {
			// Response for global comments query
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": [
            {
              "id": "comment-1",
              "body": "Feedback needing ack",
              "createdAt": "2024-01-01T10:00:00Z",
              "author": {
                "login": "human"
              },
              "reactionGroups": []
            },
            {
              "id": "comment-2",
              "body": "Feedback with bot reaction",
              "createdAt": "2024-01-01T10:00:00Z",
              "author": {
                "login": "human"
              },
              "reactionGroups": [
                {
                  "content": "THUMBS_UP",
                  "users": {
                    "nodes": [
                      {
                        "login": "bot"
                      }
                    ]
                  }
                }
              ]
            }
          ]
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	if len(items) != 1 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 1", len(items))
	}

	if items[0].ID != "comment-1" {
		t.Errorf("items[0].ID = %q, want %q", items[0].ID, "comment-1")
	}
}

func TestListUnaddressedFeedback_EmptyCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        },
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	if len(items) != 0 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 0", len(items))
	}
}

func TestListUnaddressedFeedback_GraphQLQueryValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Validate that the query doesn't include 'replies' field (which doesn't exist on IssueComment)
		if strings.Contains(bodyStr, "comments") && strings.Contains(bodyStr, "replies") {
			// If 'replies' is in the query, return a GraphQL error like GitHub would
			graphqlResp := `{
  "errors": [
    {
      "message": "Field replies does not exist on type IssueComment",
      "locations": [{"line": 1, "column": 1}],
      "code": "undefinedField",
      "typeName": "IssueComment",
      "fieldName": "replies"
    }
  ]
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
			return
		}

		// Response for review threads query
		if strings.Contains(bodyStr, "reviewThreads") {
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		} else if strings.Contains(bodyStr, "comments") {
			// Response for global comments query - should not have 'replies' field
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	if len(items) != 0 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 0", len(items))
	}
}

func TestListUnaddressedFeedback_GraphQLErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Response for review threads query
		if strings.Contains(bodyStr, "reviewThreads") {
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		} else if strings.Contains(bodyStr, "comments") {
			// Return a GraphQL error
			graphqlResp := `{
  "errors": [
    {
      "message": "Field replies does not exist on type IssueComment"
    }
  ]
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err == nil {
		t.Fatalf("ListUnaddressedFeedback() error = nil, want error for GraphQL errors")
	}

	if !strings.Contains(err.Error(), "graphql error") {
		t.Errorf("ListUnaddressedFeedback() error = %v, want error containing 'graphql error'", err)
	}
}

func TestListUnaddressedFeedback_Pagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Response for review threads query with pagination
		if strings.Contains(bodyStr, "reviewThreads") {
			if callCount == 1 {
				// First page has more
				graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": true,
            "endCursor": "cursor123"
          },
          "nodes": [
            {
              "id": "thread-1",
              "isResolved": false,
              "path": "file1.go",
              "line": 10,
              "comments": {
                "nodes": [
                  {
                    "id": "comment-1",
                    "body": "Comment 1",
                    "author": {
                      "login": "reviewer"
                    }
                  }
                ]
              }
            }
          ]
        }
      }
    }
  }
}`
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(graphqlResp))
			} else if callCount == 2 {
				// Second page is last
				graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": [
            {
              "id": "thread-2",
              "isResolved": false,
              "path": "file2.go",
              "line": 20,
              "comments": {
                "nodes": [
                  {
                    "id": "comment-2",
                    "body": "Comment 2",
                    "author": {
                      "login": "reviewer"
                    }
                  }
                ]
              }
            }
          ]
        }
      }
    }
  }
}`
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(graphqlResp))
			}
		} else if strings.Contains(bodyStr, "comments") {
			// Return empty comments for global comments
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	if len(items) != 2 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 2", len(items))
	}

	if items[0].Path != "file1.go" {
		t.Errorf("items[0].Path = %q, want %q", items[0].Path, "file1.go")
	}

	if items[1].Path != "file2.go" {
		t.Errorf("items[1].Path = %q, want %q", items[1].Path, "file2.go")
	}
}

func TestListUnaddressedFeedback_AcknowledgedByBotReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Response for review threads query (first call)
		if strings.Contains(bodyStr, "reviewThreads") {
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewThreads": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": []
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		} else if strings.Contains(bodyStr, "comments") {
			// Response for global comments query
			graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "comments": {
          "pageInfo": {
            "hasNextPage": false,
            "endCursor": null
          },
          "nodes": [
            {
              "id": "comment-1",
              "body": "Human feedback without reaction",
              "createdAt": "2024-01-01T10:00:00Z",
              "author": {
                "login": "human"
              },
              "reactionGroups": []
            },
            {
              "id": "comment-2",
              "body": "Bot reply acknowledging the feedback",
              "createdAt": "2024-01-01T10:05:00Z",
              "author": {
                "login": "bot"
              },
              "reactionGroups": []
            },
            {
              "id": "comment-3",
              "body": "Another unacknowledged feedback",
              "createdAt": "2024-01-01T10:10:00Z",
              "author": {
                "login": "human"
              },
              "reactionGroups": []
            }
          ]
        }
      }
    }
  }
}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(graphqlResp))
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	items, err := ListUnaddressedFeedback(ctx, "owner", "repo", 42, "bot", "token")

	if err != nil {
		t.Fatalf("ListUnaddressedFeedback() error = %v, want nil", err)
	}

	// Should return 1 item: comment-3 (comment-1 is acknowledged by bot reply comment-2)
	if len(items) != 1 {
		t.Errorf("ListUnaddressedFeedback() returned %d items, want 1", len(items))
	}

	if items[0].ID != "comment-3" {
		t.Errorf("items[0].ID = %q, want %q", items[0].ID, "comment-3")
	}
}
