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
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	BranchName  string `json:"branchName"`
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

// Team represents a Linear team
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
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

// GetTeams fetches all teams
func (c *Client) GetTeams(ctx context.Context) ([]Team, error) {
	req := graphql.NewRequest(`
		query {
			teams {
				nodes {
					id
					name
					key
				}
			}
		}
	`)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		Teams struct {
			Nodes []Team `json:"nodes"`
		} `json:"teams"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return nil, err
	}

	return resp.Teams.Nodes, nil
}

// GetIssues fetches issues from Linear filtered by specified states
func (c *Client) GetIssues(ctx context.Context, teamID string) ([]Issue, error) {
	var query string
	if teamID != "" {
		query = `
		query($teamID: ID!) {
			issues(filter: {
				team: { id: { eq: $teamID } }
				state: {
					name: {
						in: ["In Review", "In Progress", "Blocked", "Todo", "Backlog"]
					}
				}
			}) {
				nodes {
					id
					identifier
					title
					description
					url
					branchName
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
		`
	} else {
		query = `
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
					identifier
					title
					description
					url
					branchName
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
		`
	}

	req := graphql.NewRequest(query)

	if teamID != "" {
		req.Var("teamID", teamID)
	}

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
