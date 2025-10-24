package api

import (
	"context"
	"sort"

	"github.com/machinebox/graphql"
)

// Client represents the Linear API client
type Client struct {
	client *graphql.Client
	apiKey string
}

// NewClient creates a new Linear API client
func NewClient(apiKey string) *Client {
	client := graphql.NewClient("https://api.linear.app/graphql")
	client.Log = func(s string) { /* log.Println(s) */ } // Enable for debugging

	return &Client{
		client: client,
		apiKey: apiKey,
	}
}

// Issue represents a Linear issue
type Issue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	Assignee struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"assignee"`
	Comments struct {
		Nodes []Comment `json:"nodes"`
	} `json:"comments"`
}

// Comment represents a comment on an issue
type Comment struct {
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	User      struct {
		Name string `json:"name"`
	} `json:"user"`
}

// Viewer represents the current user
type Viewer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetViewer fetches the current user
func (c *Client) GetViewer(ctx context.Context) (*Viewer, error) {
	req := graphql.NewRequest(`
		query {
			viewer {
				id
				name
			}
		}
	`)

	// Set authorization header
	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		Viewer Viewer `json:"viewer"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return nil, err
	}

	return &resp.Viewer, nil
}

// GetIssues fetches issues from Linear filtered by specified states
func (c *Client) GetIssues(ctx context.Context) ([]Issue, error) {
	req := graphql.NewRequest(`
		query {
			issues(filter: {
				state: {
					name: {
						in: ["In Review", "In Progress", "Blocked", "Todo", "Backlog"]
					}
				}
			}) {
				nodes {
					id
					title
					description
					state {
						name
					}
					assignee {
						id
						name
					}
					comments {
						nodes {
							body
							createdAt
							user {
								name
							}
						}
					}
				}
			}
		}
	`)

	// Set authorization header
	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		Issues struct {
			Nodes []Issue `json:"nodes"`
		} `json:"issues"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return nil, err
	}

	issues := resp.Issues.Nodes

	stateOrder := map[string]int{
		"In Review":   0,
		"In Progress": 1,
		"Blocked":     2,
		"Todo":        3,
		"Backlog":     4,
	}

	sort.SliceStable(issues, func(i, j int) bool {
		orderI, okI := stateOrder[issues[i].State.Name]
		orderJ, okJ := stateOrder[issues[j].State.Name]

		if !okI {
			orderI = 999
		}
		if !okJ {
			orderJ = 999
		}

		return orderI < orderJ
	})

	return issues, nil
}
