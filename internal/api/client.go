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
		Name  string `json:"name"`
		Type  string `json:"type"`
		Color string `json:"color"`
	} `json:"state"`
	Priority float64 `json:"priority"`
	Team     struct {
		ID string `json:"id"`
	} `json:"team"`
	Assignee struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"assignee"`
	Project struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"project"`
	Labels struct {
		Nodes []Label `json:"nodes"`
	} `json:"labels"`
	DueDate  string  `json:"dueDate"`
	Estimate float64 `json:"estimate"`
	Comments struct {
		Nodes []Comment `json:"nodes"`
	} `json:"comments"`
}

// Label represents an issue label
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// CustomView represents a saved Linear issue view
type CustomView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	ModelName string `json:"modelName"`
	Team      *struct {
		ID string `json:"id"`
	} `json:"team"`
}

// issueFields is the GraphQL selection shared by issue queries
const issueFields = `
	id
	identifier
	title
	description
	url
	branchName
	state {
		name
		type
		color
	}
	priority
	team {
		id
	}
	assignee {
		id
		name
	}
	project {
		id
		name
		color
	}
	labels {
		nodes {
			name
			color
		}
	}
	dueDate
	estimate
	comments {
		nodes {
			body
			createdAt
			user {
				name
			}
		}
	}
`

// maxIssuePages caps how many pages of issues are fetched per request
const maxIssuePages = 20

// pageInfo holds GraphQL connection pagination state
type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
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
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Key     string          `json:"key"`
	States  []WorkflowState `json:"states"`
	Members []TeamMember    `json:"members"`
}

// TeamMember represents a team member
type TeamMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WorkflowState represents a workflow state
type WorkflowState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Position float64 `json:"position"`
	Type     string  `json:"type"`
	Color    string  `json:"color"`
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
					states {
						nodes {
							id
							name
							position
							type
							color
						}
					}
					members {
						nodes {
							id
							name
						}
					}
				}
			}
		}
	`)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		Teams struct {
			Nodes []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Key    string `json:"key"`
				States struct {
					Nodes []WorkflowState `json:"nodes"`
				} `json:"states"`
				Members struct {
					Nodes []TeamMember `json:"nodes"`
				} `json:"members"`
			} `json:"nodes"`
		} `json:"teams"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return nil, err
	}

	teams := make([]Team, len(resp.Teams.Nodes))
	for i, node := range resp.Teams.Nodes {
		states := node.States.Nodes

		typeOrder := map[string]int{
			"triage":    0,
			"backlog":   1,
			"unstarted": 2,
			"started":   3,
			"completed": 4,
			"canceled":  5,
		}

		sort.SliceStable(states, func(i, j int) bool {
			typeI := typeOrder[states[i].Type]
			typeJ := typeOrder[states[j].Type]

			if typeI != typeJ {
				return typeI < typeJ
			}

			return states[i].Position < states[j].Position
		})

		teams[i] = Team{
			ID:      node.ID,
			Name:    node.Name,
			Key:     node.Key,
			States:  states,
			Members: node.Members.Nodes,
		}
	}

	return teams, nil
}

// GetIssues fetches issues from Linear filtered by specified states
func (c *Client) GetIssues(ctx context.Context, teamID string) ([]Issue, error) {
	var query string
	if teamID != "" {
		query = `
		query($teamID: ID!, $after: String) {
			issues(first: 50, after: $after, filter: {
				team: { id: { eq: $teamID } }
				state: {
					name: {
						in: ["Triage", "In Review", "In Progress", "Blocked", "Todo", "Backlog"]
					}
				}
			}) {
				nodes {` + issueFields + `}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
		`
	} else {
		query = `
		query($after: String) {
			issues(first: 50, after: $after, filter: {
				state: {
					name: {
						in: ["In Review", "In Progress", "Blocked", "Todo", "Backlog"]
					}
				}
			}) {
				nodes {` + issueFields + `}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
		`
	}

	var issues []Issue
	var after *string
	for page := 0; page < maxIssuePages; page++ {
		req := graphql.NewRequest(query)

		if teamID != "" {
			req.Var("teamID", teamID)
		}
		req.Var("after", after)

		// Set authorization header
		if c.apiKey != "" {
			req.Header.Set("Authorization", c.apiKey)
		}

		var resp struct {
			Issues struct {
				Nodes    []Issue  `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"issues"`
		}

		if err := c.client.Run(ctx, req, &resp); err != nil {
			return nil, err
		}

		issues = append(issues, resp.Issues.Nodes...)
		if !resp.Issues.PageInfo.HasNextPage {
			break
		}
		cursor := resp.Issues.PageInfo.EndCursor
		after = &cursor
	}

	sortIssuesByState(issues)
	return issues, nil
}

// GetCustomViewIssues fetches the issues matched by a saved view, limited to a team
func (c *Client) GetCustomViewIssues(ctx context.Context, viewID string, teamID string) ([]Issue, error) {
	query := `
		query($viewID: String!, $teamID: ID!, $after: String) {
			customView(id: $viewID) {
				issues(first: 50, after: $after, filter: { team: { id: { eq: $teamID } } }) {
					nodes {` + issueFields + `}
					pageInfo {
						hasNextPage
						endCursor
					}
				}
			}
		}
	`

	var issues []Issue
	var after *string
	for page := 0; page < maxIssuePages; page++ {
		req := graphql.NewRequest(query)
		req.Var("viewID", viewID)
		req.Var("teamID", teamID)
		req.Var("after", after)

		if c.apiKey != "" {
			req.Header.Set("Authorization", c.apiKey)
		}

		var resp struct {
			CustomView struct {
				Issues struct {
					Nodes    []Issue  `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"issues"`
			} `json:"customView"`
		}

		if err := c.client.Run(ctx, req, &resp); err != nil {
			return nil, err
		}

		issues = append(issues, resp.CustomView.Issues.Nodes...)
		if !resp.CustomView.Issues.PageInfo.HasNextPage {
			break
		}
		cursor := resp.CustomView.Issues.PageInfo.EndCursor
		after = &cursor
	}

	sortIssuesByState(issues)
	return issues, nil
}

// GetCustomViews fetches the saved issue views visible to the current user
func (c *Client) GetCustomViews(ctx context.Context) ([]CustomView, error) {
	req := graphql.NewRequest(`
		query {
			customViews(first: 100) {
				nodes {
					id
					name
					color
					modelName
					team {
						id
					}
				}
			}
		}
	`)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		CustomViews struct {
			Nodes []CustomView `json:"nodes"`
		} `json:"customViews"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return nil, err
	}

	var views []CustomView
	for _, view := range resp.CustomViews.Nodes {
		if view.ModelName == "Issue" {
			views = append(views, view)
		}
	}
	return views, nil
}

// GetTeamIssueCounts counts open issues per team ID across the workspace
func (c *Client) GetTeamIssueCounts(ctx context.Context) (map[string]int, error) {
	query := `
		query($after: String) {
			issues(first: 250, after: $after, filter: {
				state: {
					name: {
						in: ["Triage", "In Review", "In Progress", "Blocked", "Todo", "Backlog"]
					}
				}
			}) {
				nodes {
					team {
						id
					}
				}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
	`

	counts := map[string]int{}
	var after *string
	for page := 0; page < maxIssuePages; page++ {
		req := graphql.NewRequest(query)
		req.Var("after", after)

		if c.apiKey != "" {
			req.Header.Set("Authorization", c.apiKey)
		}

		var resp struct {
			Issues struct {
				Nodes []struct {
					Team struct {
						ID string `json:"id"`
					} `json:"team"`
				} `json:"nodes"`
				PageInfo pageInfo `json:"pageInfo"`
			} `json:"issues"`
		}

		if err := c.client.Run(ctx, req, &resp); err != nil {
			return nil, err
		}

		for _, node := range resp.Issues.Nodes {
			counts[node.Team.ID]++
		}
		if !resp.Issues.PageInfo.HasNextPage {
			break
		}
		cursor := resp.Issues.PageInfo.EndCursor
		after = &cursor
	}

	return counts, nil
}

func sortIssuesByState(issues []Issue) {
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

}

// AddComment adds a comment to an issue
func (c *Client) AddComment(ctx context.Context, issueID string, body string) error {
	req := graphql.NewRequest(`
		mutation($issueId: String!, $body: String!) {
			commentCreate(input: {
				issueId: $issueId
				body: $body
			}) {
				success
				comment {
					id
				}
			}
		}
	`)

	req.Var("issueId", issueID)
	req.Var("body", body)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return err
	}

	return nil
}

func (c *Client) UpdateIssueStatus(ctx context.Context, issueID string, stateID string) error {
	req := graphql.NewRequest(`
		mutation($issueId: String!, $stateId: String!) {
			issueUpdate(id: $issueId, input: {
				stateId: $stateId
			}) {
				success
				issue {
					id
					state {
						name
					}
					priority
				}
			}
		}
	`)

	req.Var("issueId", issueID)
	req.Var("stateId", stateID)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return err
	}

	return nil
}

func (c *Client) UpdateIssuePriority(ctx context.Context, issueID string, priority int) error {
	req := graphql.NewRequest(`
		mutation($issueId: String!, $priority: Int!) {
			issueUpdate(id: $issueId, input: {
				priority: $priority
			}) {
				success
				issue {
					id
					priority
				}
			}
		}
	`)

	req.Var("issueId", issueID)
	req.Var("priority", priority)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return err
	}

	return nil
}

func (c *Client) UpdateIssueAssignee(ctx context.Context, issueID string, assigneeID string) error {
	req := graphql.NewRequest(`
		mutation($issueId: String!, $assigneeId: String) {
			issueUpdate(id: $issueId, input: {
				assigneeId: $assigneeId
			}) {
				success
				issue {
					id
					assignee {
						id
						name
					}
				}
			}
		}
	`)

	req.Var("issueId", issueID)
	if assigneeID != "" {
		req.Var("assigneeId", assigneeID)
	} else {
		req.Var("assigneeId", nil)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return err
	}

	return nil
}

func (c *Client) UpdateIssue(ctx context.Context, issueID string, title string, description string) error {
	req := graphql.NewRequest(`
		mutation($issueId: String!, $title: String, $description: String) {
			issueUpdate(id: $issueId, input: {
				title: $title
				description: $description
			}) {
				success
				issue {
					id
					title
					description
				}
			}
		}
	`)

	req.Var("issueId", issueID)
	req.Var("title", title)
	req.Var("description", description)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return err
	}

	return nil
}

func (c *Client) CreateIssue(ctx context.Context, teamID string, title string, description string) (*Issue, error) {
	req := graphql.NewRequest(`
		mutation($teamId: String!, $title: String!, $description: String) {
			issueCreate(input: {
				teamId: $teamId
				title: $title
				description: $description
			}) {
				success
				issue {
					id
					identifier
					title
					description
					url
					branchName
					priority
					state {
						name
						type
						color
					}
					team {
						id
					}
					assignee {
						id
						name
					}
				}
			}
		}
	`)

	req.Var("teamId", teamID)
	req.Var("title", title)
	req.Var("description", description)

	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}

	var resp struct {
		IssueCreate struct {
			Success bool  `json:"success"`
			Issue   Issue `json:"issue"`
		} `json:"issueCreate"`
	}

	if err := c.client.Run(ctx, req, &resp); err != nil {
		return nil, err
	}

	return &resp.IssueCreate.Issue, nil
}
