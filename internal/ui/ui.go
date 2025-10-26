package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jroimartin/gocui"
	"lazylinear/internal/api"
)

// UI manages the terminal user interface
type UI struct {
	gui                 *gocui.Gui
	client              *api.Client
	issues              []api.Issue
	allIssues           []api.Issue
	selectedIssue       int
	showHelp            bool
	showSearch          bool
	searchString        string
	assignedToMe        bool
	viewerID            string
	currentView         int
	views               []string
	teams               []api.Team
	currentTeam         int
	showComment         bool
	commentContent      string
	toastMessage        string
	toastTimer          *time.Timer
	showStatus          bool
	selectedStatus      int
	availableStatuses   []string
	showPriority        bool
	selectedPriority    int
	availablePriorities []struct {
		label string
		value int
	}
	showAssignee       bool
	selectedAssignee   int
	availableAssignees []struct {
		id   string
		name string
	}
}

// commentEditor is a custom editor that handles Esc key
type commentEditor struct {
	ui *UI
}

func (e *commentEditor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
	// Handle Esc key to cancel
	if key == gocui.KeyEsc {
		e.ui.cancelComment(e.ui.gui, v)
		return
	}
	// Handle Ctrl+S to submit
	if key == gocui.KeyCtrlS {
		e.ui.submitComment(e.ui.gui, v)
		return
	}
	// Pass all other keys to default editor
	gocui.DefaultEditor.Edit(v, key, ch, mod)
}

// NewUI creates a new UI instance
func NewUI(client *api.Client) (*UI, error) {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return nil, err
	}

	// Enable InputEsc mode to handle Esc key properly
	g.InputEsc = true

	// Enable highlighting and set border colors like lazygit
	g.Highlight = true
	g.SelFgColor = gocui.ColorGreen // Active pane border color
	g.FgColor = gocui.ColorDefault  // Inactive pane border color

	// Fetch teams and issues
	var issues []api.Issue
	var teams []api.Team
	var viewerID string
	var apiErr error
	var fetchedIssues []api.Issue
	if client != nil {
		if fetchedTeams, err := client.GetTeams(context.Background()); err == nil {
			teams = fetchedTeams
		}
		teamID := ""
		if len(teams) > 0 {
			teamID = teams[0].ID
		}
		fetchedIssues, apiErr = client.GetIssues(context.Background(), teamID)
		if viewer, err := client.GetViewer(context.Background()); err == nil {
			viewerID = viewer.ID
		}
	} else {
		apiErr = fmt.Errorf("no client")
	}
	if apiErr == nil {
		issues = fetchedIssues
	} else {
		issues = []api.Issue{{Title: fmt.Sprintf("Error loading issues: %v", apiErr)}}
	}

	var availableStatuses []string
	if len(teams) > 0 {
		for _, state := range teams[0].States {
			availableStatuses = append(availableStatuses, state.Name)
		}
	}

	ui := &UI{
		gui:               g,
		client:            client,
		issues:            issues,
		allIssues:         issues,
		selectedIssue:     -1,
		showHelp:          false,
		showSearch:        false,
		searchString:      "",
		assignedToMe:      false,
		viewerID:          viewerID,
		currentView:       0,
		views:             []string{"All", "In Review", "In Progress", "Blocked", "Todo", "Backlog"},
		teams:             teams,
		currentTeam:       0,
		showComment:       false,
		commentContent:    "",
		toastMessage:      "",
		toastTimer:        nil,
		showStatus:        false,
		selectedStatus:    0,
		availableStatuses: availableStatuses,
		showPriority:      false,
		selectedPriority:  0,
		availablePriorities: []struct {
			label string
			value int
		}{
			{"No Priority", 0},
			{"Urgent", 1},
			{"High", 2},
			{"Medium", 3},
			{"Low", 4},
		},
		showAssignee:     false,
		selectedAssignee: 0,
		availableAssignees: []struct {
			id   string
			name string
		}{},
	}

	g.SetManagerFunc(ui.layout)

	// Set keybindings
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, ui.quit); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.KeyArrowDown, gocui.ModNone, ui.cursorDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.KeyArrowUp, gocui.ModNone, ui.cursorUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'j', gocui.ModNone, ui.cursorDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'k', gocui.ModNone, ui.cursorUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'r', gocui.ModNone, ui.refreshIssues); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '?', gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'm', gocui.ModNone, ui.toggleAssigned); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '/', gocui.ModNone, ui.toggleSearch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '[', gocui.ModNone, ui.prevView); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", ']', gocui.ModNone, ui.nextView); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.KeyEnter, gocui.ModNone, ui.selectIssue); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", ',', gocui.ModNone, ui.copyURL); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '.', gocui.ModNone, ui.copyBranch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'a', gocui.ModNone, ui.toggleAssignee); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '{', gocui.ModNone, ui.prevTeam); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '}', gocui.ModNone, ui.nextTeam); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'c', gocui.ModNone, ui.toggleComment); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 's', gocui.ModNone, ui.toggleStatus); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'p', gocui.ModNone, ui.togglePriority); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("search", gocui.KeyEnter, gocui.ModNone, ui.closeSearch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("search", gocui.KeyEsc, gocui.ModNone, ui.cancelSearch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("comment", gocui.KeyCtrlS, gocui.ModNone, ui.submitComment); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("comment", gocui.KeyEsc, gocui.ModNone, ui.cancelComment); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("help", '?', gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("help", gocui.KeyEsc, gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("status", gocui.KeyArrowDown, gocui.ModNone, ui.statusDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("status", gocui.KeyArrowUp, gocui.ModNone, ui.statusUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("status", 'j', gocui.ModNone, ui.statusDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("status", 'k', gocui.ModNone, ui.statusUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("status", gocui.KeyEnter, gocui.ModNone, ui.submitStatus); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("status", gocui.KeyEsc, gocui.ModNone, ui.cancelStatus); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("priority", gocui.KeyArrowDown, gocui.ModNone, ui.priorityDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("priority", gocui.KeyArrowUp, gocui.ModNone, ui.priorityUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("priority", 'j', gocui.ModNone, ui.priorityDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("priority", 'k', gocui.ModNone, ui.priorityUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("priority", gocui.KeyEnter, gocui.ModNone, ui.submitPriority); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("priority", gocui.KeyEsc, gocui.ModNone, ui.cancelPriority); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("assignee", gocui.KeyArrowDown, gocui.ModNone, ui.assigneeDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("assignee", gocui.KeyArrowUp, gocui.ModNone, ui.assigneeUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("assignee", 'j', gocui.ModNone, ui.assigneeDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("assignee", 'k', gocui.ModNone, ui.assigneeUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("assignee", gocui.KeyEnter, gocui.ModNone, ui.submitAssignee); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("assignee", gocui.KeyEsc, gocui.ModNone, ui.cancelAssignee); err != nil {
		return nil, err
	}

	return ui, nil
}

// Run starts the UI main loop
func (ui *UI) Run() error {
	defer ui.gui.Close()
	return ui.gui.MainLoop()
}

// Close closes the UI
func (ui *UI) Close() {
	ui.gui.Close()
}

func (ui *UI) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Teams bar (top)
	teamBarHeight := 2
	if tv, err := g.SetView("teams", 0, 0, maxX-1, teamBarHeight); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		tv.Frame = true
	}
	if tv, err := g.View("teams"); err == nil {
		tv.Clear()
		if len(ui.teams) > 0 {
			for i, team := range ui.teams {
				if i == ui.currentTeam {
					fmt.Fprintf(tv, "\033[32m%s\033[0m ", "[ "+team.Name+" ]")
				} else {
					fmt.Fprintf(tv, "%s ", team.Name)
				}
			}
		} else {
			fmt.Fprint(tv, "All")
		}
		tv.Title = "Teams ({/} to switch)"
	}

	// Search bar (if enabled)
	if ui.showSearch {
		if v, err := g.SetView("search", 0, maxY-4, maxX-1, maxY-2); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = "Search (Enter to apply, Esc to cancel)"
			v.Editable = true
			v.Editor = gocui.DefaultEditor
			fmt.Fprint(v, ui.searchString)
			v.SetCursor(len(ui.searchString), 0)
		} else {
			v.Title = "Search (Enter to apply, Esc to cancel)"
		}
		g.SetCurrentView("search")
	} else {
		g.DeleteView("search")
	}

	// Issues list (left side)
	issuesX := int(0.4 * float32(maxX))
	bottomY := maxY - 3
	if ui.showSearch {
		bottomY = maxY - 5
	}
	v, err := g.SetView("issues", 0, teamBarHeight+1, issuesX, bottomY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Highlight = true
		v.SelBgColor = gocui.ColorGreen
		v.SelFgColor = gocui.ColorBlack
	}
	v.Highlight = true
	v.SelBgColor = gocui.ColorGreen
	v.SelFgColor = gocui.ColorBlack

	viewTitle := ui.views[ui.currentView]
	if ui.assignedToMe {
		viewTitle = viewTitle + " (My Issues)"
	}
	if ui.searchString != "" {
		viewTitle = viewTitle + " [" + ui.searchString + "]"
	}
	v.Title = viewTitle

	// Update issues list
	v.Clear()

	maxIdentifierLen := 0
	for _, issue := range ui.issues {
		if len(issue.Identifier) > maxIdentifierLen {
			maxIdentifierLen = len(issue.Identifier)
		}
	}

	for _, issue := range ui.issues {
		initials := "--"
		if issue.Assignee.Name != "" {
			parts := strings.Fields(issue.Assignee.Name)
			if len(parts) >= 2 {
				initials = strings.ToUpper(string(parts[0][0]) + string(parts[1][0]))
			} else if len(parts) == 1 {
				if len(parts[0]) >= 2 {
					initials = strings.ToUpper(string(parts[0][0]) + string(parts[0][1]))
				} else {
					initials = strings.ToUpper(parts[0])
				}
			}
		}

		priorityIcon := "┄"
		priorityColor := ""
		switch int(issue.Priority) {
		case 1:
			priorityIcon = "🞷"
			priorityColor = "\033[31m"
		case 2:
			priorityIcon = "Ⅲ"
		case 3:
			priorityIcon = "Ⅱ"
		case 4:
			priorityIcon = "Ⅰ"
		}
		if priorityColor != "" {
			priorityIcon = priorityColor + priorityIcon + "\033[0m"
		}

		stateIcon := "○"
		switch issue.State.Type {
		case "triage":
			stateIcon = "↔"
		case "backlog":
			stateIcon = "◌"
		case "unstarted":
			stateIcon = "○"
		case "started":
			stateIcon = "◕"
		case "completed":
			stateIcon = "✓"
		case "canceled":
			stateIcon = "x"
		}

		colorCode := "37"
		if issue.State.Color != "" {
			if strings.HasPrefix(issue.State.Color, "#") {
				colorCode = ui.hexToAnsi(issue.State.Color)
			} else {
				switch issue.State.Color {
				case "red":
					colorCode = "31"
				case "green":
					colorCode = "32"
				case "yellow":
					colorCode = "33"
				case "blue":
					colorCode = "34"
				case "magenta":
					colorCode = "35"
				case "cyan":
					colorCode = "36"
				case "white":
					colorCode = "37"
				case "gray", "grey":
					colorCode = "90"
				}
			}
		}

		identifierFmt := fmt.Sprintf("%%-%ds", maxIdentifierLen+1)
		fmt.Fprintf(v, "%s \033[36m"+identifierFmt+"\033[0m \033[%sm%s\033[0m \033[33m%-3s\033[0m %s\n", priorityIcon, issue.Identifier, colorCode, stateIcon, initials, issue.Title)
	}

	// Set cursor to first item if needed
	if len(ui.issues) > 0 {
		_, cy := v.Cursor()
		if cy >= len(ui.issues) {
			v.SetCursor(0, len(ui.issues)-1)
			ui.selectedIssue = len(ui.issues) - 1
		} else if cy < 0 {
			v.SetCursor(0, 0)
			ui.selectedIssue = 0
		} else {
			ui.selectedIssue = cy
		}
	}

	// Set focus and cursor
	if ui.showSearch {
		g.SetCurrentView("search")
		g.Cursor = true
	} else if ui.showComment {
		g.SetCurrentView("comment")
		g.Cursor = true
	} else if ui.showHelp {
		g.SetCurrentView("help")
		g.Cursor = false
	} else if ui.showStatus {
		g.SetCurrentView("status")
		g.Cursor = false
	} else if ui.showPriority {
		g.SetCurrentView("priority")
		g.Cursor = false
	} else if ui.showAssignee {
		g.SetCurrentView("assignee")
		g.Cursor = false
	} else {
		g.SetCurrentView("issues")
		g.Cursor = false
	}

	// Issue details (right side)
	dv, err := g.SetView("details", issuesX+1, teamBarHeight+1, maxX-1, bottomY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		dv.Title = "Issue Details"
		dv.Wrap = true
	}

	// Update details content
	dv.Clear()
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]

		stateColorCode := "37"
		if issue.State.Color != "" {
			if strings.HasPrefix(issue.State.Color, "#") {
				stateColorCode = ui.hexToAnsi(issue.State.Color)
			}
		}

		fmt.Fprintf(dv, "\033[36m%s\033[0m\n", issue.Identifier)
		fmt.Fprintf(dv, "\033[1m%s\033[0m\n\n", issue.Title)
		fmt.Fprintf(dv, "\033[35mState:\033[0m \033[%sm%s\033[0m\n", stateColorCode, issue.State.Name)
		if issue.Priority > 0 {
			priorityIcon := "┄"
			priorityLabel := "No Priority"
			priorityColor := ""
			switch int(issue.Priority) {
			case 1:
				priorityIcon = "🞷"
				priorityLabel = "Urgent"
				priorityColor = "\033[31m"
			case 2:
				priorityIcon = "Ⅲ"
				priorityLabel = "High"
			case 3:
				priorityIcon = "Ⅱ"
				priorityLabel = "Medium"
			case 4:
				priorityIcon = "Ⅰ"
				priorityLabel = "Low"
			}
			if priorityColor != "" {
				fmt.Fprintf(dv, "\033[35mPriority:\033[0m %s%s %s\033[0m\n", priorityColor, priorityIcon, priorityLabel)
			} else {
				fmt.Fprintf(dv, "\033[35mPriority:\033[0m %s %s\n", priorityIcon, priorityLabel)
			}
		}
		if issue.Assignee.Name != "" {
			fmt.Fprintf(dv, "\033[35mAssignee:\033[0m %s\n", issue.Assignee.Name)
		}
		if issue.URL != "" {
			fmt.Fprintf(dv, "\033[35mURL:\033[0m \033[34m%s\033[0m\n", issue.URL)
		}
		if issue.Description != "" {
			fmt.Fprintf(dv, "\n\033[31mDescription:\033[0m\n%s\n", issue.Description)
		}
		if len(issue.Comments.Nodes) > 0 {
			fmt.Fprintf(dv, "\n\033[31mComments:\033[0m\n")
			for _, comment := range issue.Comments.Nodes {
				fmt.Fprintf(dv, "\033[33m%s\033[0m \033[90m(%s)\033[0m\n%s\n\n", comment.User.Name, comment.CreatedAt, comment.Body)
			}
		}
	} else {
		fmt.Fprintln(dv, "Select an issue to view details")
		fmt.Fprintln(dv, "Press '?' for help")
	}

	// Toast message (if present)
	if ui.toastMessage != "" {
		toastHeight := 3
		toastY := maxY - toastHeight - 2
		if ui.showSearch {
			toastY = maxY - toastHeight - 1
		}
		if tv, err := g.SetView("toast", 0, toastY, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			tv.Frame = true
			tv.Title = "Message"
			tv.FgColor = gocui.ColorBlue
		}
		if tv, err := g.View("toast"); err == nil {
			tv.Clear()
			fmt.Fprintln(tv, ui.toastMessage)
		}
	} else {
		g.DeleteView("toast")
	}

	// Status bar (bottom)
	statusY := maxY - 2
	if ui.showSearch {
		statusY = maxY - 1
	}
	if ui.toastMessage != "" {
		statusY -= 3
	}
	if v, err := g.SetView("status", 0, statusY, maxX-1, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
	}
	if sv, err := g.View("status"); err == nil {
		sv.Clear()
		status := "j/k/↑/↓: navigate | [/]: switch view | Enter: select | r: refresh | /: search | m: my issues | ?: help | Ctrl+C: quit"
		if ui.assignedToMe {
			status = "[My Issues] " + status
		}
		if ui.searchString != "" {
			status = fmt.Sprintf("[Search: %s] %s", ui.searchString, status)
		}
		fmt.Fprintln(sv, status)
	}

	// Modals (rendered last so they appear on top)
	// Help modal (if enabled)
	if ui.showHelp {
		helpWidth := maxX - 20
		helpHeight := 20
		helpX := (maxX - helpWidth) / 2
		helpY := (maxY - helpHeight) / 2

		if hv, err := g.SetView("help", helpX, helpY, helpX+helpWidth, helpY+helpHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			hv.Title = "LazyLinear Help (Press ? or Esc to close)"
			hv.Wrap = true
			g.SetCurrentView("help")
		} else {
			hv.Title = "LazyLinear Help (Press ? or Esc to close)"
			g.SetCurrentView("help")
		}

		if hv, err := g.View("help"); err == nil {
			hv.Clear()
			fmt.Fprintln(hv, "")
			fmt.Fprintln(hv, "Navigation:")
			fmt.Fprintln(hv, "  j / ↓   : Move down")
			fmt.Fprintln(hv, "  k / ↑   : Move up")
			fmt.Fprintln(hv, "  [ / ]   : Switch view (All/In Review/In Progress/Blocked/Todo/Backlog)")
			fmt.Fprintln(hv, "  { / }   : Switch team")
			fmt.Fprintln(hv, "")
			fmt.Fprintln(hv, "Actions:")
			fmt.Fprintln(hv, "  Enter   : Select issue to view details")
			fmt.Fprintln(hv, "  r       : Refresh issues")
			fmt.Fprintln(hv, "  m       : Toggle filter by assigned to me")
			fmt.Fprintln(hv, "  /       : Search issues (Enter to apply, Esc to cancel)")
			fmt.Fprintln(hv, "  c       : Add comment to selected issue")
			fmt.Fprintln(hv, "  s       : Change status of selected issue")
			fmt.Fprintln(hv, "  p       : Change priority of selected issue")
			fmt.Fprintln(hv, "  a       : Assign user to selected issue")
			fmt.Fprintln(hv, "  ,       : Copy issue URL to clipboard")
			fmt.Fprintln(hv, "  .       : Copy git branch name to clipboard")
			fmt.Fprintln(hv, "  ?       : Toggle this help")
			fmt.Fprintln(hv, "  Ctrl+C  : Quit")
			fmt.Fprintln(hv, "")
			fmt.Fprintln(hv, "Configuration:")
			fmt.Fprintln(hv, "  Set your Linear API key in ~/.lazylinear/config.json")
		}
	} else {
		g.DeleteView("help")
	}

	// Status pane (if enabled)
	if ui.showStatus {
		statusWidth := 40
		statusHeight := len(ui.availableStatuses) + 2
		statusX := (maxX - statusWidth) / 2
		statusY := (maxY - statusHeight) / 2

		if sv, err := g.SetView("status", statusX, statusY, statusX+statusWidth, statusY+statusHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			sv.Title = "Status (Enter: submit, Esc:cancel)"
			sv.Frame = true
			sv.Highlight = true
			sv.SelBgColor = gocui.ColorBlue
			sv.SelFgColor = gocui.ColorWhite
		}

		if sv, err := g.View("status"); err == nil {
			sv.Clear()
			sv.Title = "Status (Enter: submit, Esc: cancel)"
			sv.Frame = true
			sv.Highlight = true
			sv.SelBgColor = gocui.ColorBlue
			sv.SelFgColor = gocui.ColorWhite

			if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
				for _, state := range ui.teams[ui.currentTeam].States {
					stateIcon := "○"
					switch state.Type {
					case "triage":
						stateIcon = "↔"
					case "backlog":
						stateIcon = "◌"
					case "unstarted":
						stateIcon = "○"
					case "started":
						stateIcon = "◕"
					case "completed":
						stateIcon = "✓"
					case "canceled":
						stateIcon = "x"
					}

					colorCode := "37"
					if state.Color != "" {
						if strings.HasPrefix(state.Color, "#") {
							colorCode = ui.hexToAnsi(state.Color)
						}
					}

					fmt.Fprintf(sv, "\033[%sm%s\033[0m %s\n", colorCode, stateIcon, state.Name)
				}
			} else {
				for _, status := range ui.availableStatuses {
					fmt.Fprintln(sv, status)
				}
			}

			sv.SetCursor(0, ui.selectedStatus)
			g.SetCurrentView("status")
		}
	} else {
		g.DeleteView("status")
	}

	// Priority pane (if enabled)
	if ui.showPriority {
		priorityWidth := 30
		priorityHeight := len(ui.availablePriorities) + 2
		priorityX := (maxX - priorityWidth) / 2
		priorityY := (maxY - priorityHeight) / 2

		if pv, err := g.SetView("priority", priorityX, priorityY, priorityX+priorityWidth, priorityY+priorityHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			pv.Title = "Priority (Enter: submit, Esc: cancel)"
			pv.Frame = true
			pv.Highlight = true
			pv.SelBgColor = gocui.ColorBlue
			pv.SelFgColor = gocui.ColorWhite
		}

		if pv, err := g.View("priority"); err == nil {
			pv.Clear()
			pv.Title = "Priority (Enter: submit, Esc: cancel)"
			pv.Frame = true
			pv.Highlight = true
			pv.SelBgColor = gocui.ColorBlue
			pv.SelFgColor = gocui.ColorWhite
			for _, priority := range ui.availablePriorities {
				priorityIcon := "┄"
				priorityColor := ""
				switch priority.value {
				case 1:
					priorityIcon = "🞷"
					priorityColor = "\033[31m"
				case 2:
					priorityIcon = "Ⅲ"
				case 3:
					priorityIcon = "Ⅱ"
				case 4:
					priorityIcon = "Ⅰ"
				}
				if priorityColor != "" {
					fmt.Fprintf(pv, "%s%s\033[0m %s\n", priorityColor, priorityIcon, priority.label)
				} else {
					fmt.Fprintf(pv, "%s %s\n", priorityIcon, priority.label)
				}
			}
			pv.SetCursor(0, ui.selectedPriority)
			g.SetCurrentView("priority")
		}
	} else {
		g.DeleteView("priority")
	}

	// Comment pane (if enabled)
	if ui.showComment {
		commentWidth := maxX - 20
		commentHeight := 10
		commentX := (maxX - commentWidth) / 2
		commentY := (maxY - commentHeight) / 2

		if cv, err := g.SetView("comment", commentX, commentY, commentX+commentWidth, commentY+commentHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			cv.Title = "Add Comment (Ctrl+S to submit, Esc to cancel)"
			cv.Editable = true
			cv.Editor = &commentEditor{ui: ui}
			cv.Wrap = true
			g.SetCurrentView("comment")
		} else {
			cv.Title = "Add Comment (Ctrl+S to submit, Esc to cancel)"
			g.SetCurrentView("comment")
		}

	} else {
		g.DeleteView("comment")
	}

	// Assignee pane (if enabled)
	if ui.showAssignee {
		assigneeWidth := 40
		assigneeHeight := len(ui.availableAssignees) + 2
		assigneeX := (maxX - assigneeWidth) / 2
		assigneeY := (maxY - assigneeHeight) / 2

		if av, err := g.SetView("assignee", assigneeX, assigneeY, assigneeX+assigneeWidth, assigneeY+assigneeHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			av.Title = "Assignee (Enter: submit, Esc: cancel)"
			av.Frame = true
			av.Highlight = true
			av.SelBgColor = gocui.ColorBlue
			av.SelFgColor = gocui.ColorWhite
		}

		if av, err := g.View("assignee"); err == nil {
			av.Clear()
			av.Title = "Assignee (Enter: submit, Esc: cancel)"
			av.Frame = true
			av.Highlight = true
			av.SelBgColor = gocui.ColorBlue
			av.SelFgColor = gocui.ColorWhite
			for _, assignee := range ui.availableAssignees {
				initials := "--"
				if assignee.name != "" && assignee.name != "No assignee" {
					parts := strings.Fields(assignee.name)
					if len(parts) >= 2 {
						initials = strings.ToUpper(string(parts[0][0]) + string(parts[1][0]))
					} else if len(parts) == 1 {
						if len(parts[0]) >= 2 {
							initials = strings.ToUpper(string(parts[0][0]) + string(parts[0][1]))
						} else {
							initials = strings.ToUpper(parts[0])
						}
					}
				}
				fmt.Fprintf(av, "\033[33m%-3s\033[0m %s\n", initials, assignee.name)
			}
			av.SetCursor(0, ui.selectedAssignee)
			g.SetCurrentView("assignee")
		}
	} else {
		g.DeleteView("assignee")
	}

	return nil
}

func (ui *UI) quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func (ui *UI) cursorDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil && len(ui.issues) > 0 {
		cx, cy := v.Cursor()
		ox, oy := v.Origin()
		_, maxY := v.Size()

		if cy < len(ui.issues)-1 {
			if err := v.SetCursor(cx, cy+1); err != nil {
				if cy+1 >= maxY-1 {
					if err := v.SetOrigin(ox, oy+1); err != nil {
						return err
					}
				}
			}
			ui.selectedIssue = cy + 1
		}
	}
	return nil
}

func (ui *UI) cursorUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil && len(ui.issues) > 0 {
		cx, cy := v.Cursor()
		ox, oy := v.Origin()

		if cy > 0 {
			if err := v.SetCursor(cx, cy-1); err != nil {
				return err
			}
			ui.selectedIssue = cy - 1
		} else if oy > 0 {
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
			ui.selectedIssue = cy - 1
		}
	}
	return nil
}

func (ui *UI) refreshIssues(g *gocui.Gui, v *gocui.View) error {
	if ui.client != nil {
		teamID := ""
		if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
			teamID = ui.teams[ui.currentTeam].ID
		}
		if fetchedIssues, err := ui.client.GetIssues(context.Background(), teamID); err == nil {
			ui.allIssues = fetchedIssues
			ui.showToast("Issues reloaded successfully")
		} else {
			ui.allIssues = []api.Issue{{Title: fmt.Sprintf("Error loading issues: %v", err)}}
			ui.showToast(fmt.Sprintf("Failed to reload issues: %v", err))
		}
	}
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	return nil
}

func (ui *UI) selectIssue(g *gocui.Gui, v *gocui.View) error {
	_, cy := v.Cursor()
	if cy >= 0 && cy < len(ui.issues) {
		ui.selectedIssue = cy
	}
	return nil
}

func (ui *UI) toggleHelp(g *gocui.Gui, v *gocui.View) error {
	ui.showHelp = !ui.showHelp
	if !ui.showHelp {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) toggleAssigned(g *gocui.Gui, v *gocui.View) error {
	ui.assignedToMe = !ui.assignedToMe
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	return nil
}

func (ui *UI) toggleSearch(g *gocui.Gui, v *gocui.View) error {
	ui.showSearch = !ui.showSearch
	if ui.showSearch {
		g.SetCurrentView("search")
	}
	return nil
}

func (ui *UI) closeSearch(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ui.searchString = strings.TrimSpace(v.Buffer())
		ui.issues = ui.filterIssues()
		ui.selectedIssue = -1
	}
	ui.showSearch = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) cancelSearch(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		v.Clear()
		v.SetCursor(0, 0)
	}
	ui.searchString = ""
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	ui.showSearch = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) toggleComment(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		ui.showComment = true
		ui.commentContent = ""
	}
	return nil
}

func (ui *UI) submitComment(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		comment := strings.TrimSpace(v.Buffer())
		if comment != "" && ui.client != nil {
			issue := ui.issues[ui.selectedIssue]
			if err := ui.client.AddComment(context.Background(), issue.ID, comment); err != nil {
				// TODO: Show error to user
			} else {
				// Refresh to show new comment
				ui.refreshIssues(g, v)
			}
		}
		v.Clear()
		v.SetCursor(0, 0)
	}
	ui.showComment = false
	ui.commentContent = ""
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) cancelComment(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		v.Clear()
		v.SetCursor(0, 0)
	}
	ui.showComment = false
	ui.commentContent = ""
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) toggleStatus(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		ui.showStatus = true
		issue := ui.issues[ui.selectedIssue]
		for i, status := range ui.availableStatuses {
			if status == issue.State.Name {
				ui.selectedStatus = i
				break
			}
		}
	}
	return nil
}

func (ui *UI) statusDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedStatus < len(ui.availableStatuses)-1 {
		ui.selectedStatus++
		cx, cy := v.Cursor()
		v.SetCursor(cx, cy+1)
	}
	return nil
}

func (ui *UI) statusUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedStatus > 0 {
		ui.selectedStatus--
		cx, cy := v.Cursor()
		v.SetCursor(cx, cy-1)
	}
	return nil
}

func (ui *UI) submitStatus(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		newStatus := ui.availableStatuses[ui.selectedStatus]
		issue := ui.issues[ui.selectedIssue]
		if ui.client != nil {
			var stateID string
			if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
				for _, state := range ui.teams[ui.currentTeam].States {
					if state.Name == newStatus {
						stateID = state.ID
						break
					}
				}
			}
			if stateID != "" {
				if err := ui.client.UpdateIssueStatus(context.Background(), issue.ID, stateID); err != nil {
					ui.showToast(fmt.Sprintf("Failed to update status: %v", err))
				} else {
					ui.issues[ui.selectedIssue].State.Name = newStatus
					ui.showToast(fmt.Sprintf("Changed status to %s for %s", newStatus, issue.Identifier))
				}
			}
		}
	}
	ui.showStatus = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) cancelStatus(g *gocui.Gui, v *gocui.View) error {
	ui.showStatus = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) togglePriority(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		ui.showPriority = true
		issue := ui.issues[ui.selectedIssue]
		// Find current priority
		for i, p := range ui.availablePriorities {
			if p.value == int(issue.Priority) {
				ui.selectedPriority = i
				break
			}
		}
	}
	return nil
}

func (ui *UI) priorityDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedPriority < len(ui.availablePriorities)-1 {
		ui.selectedPriority++
		cx, cy := v.Cursor()
		v.SetCursor(cx, cy+1)
	}
	return nil
}

func (ui *UI) priorityUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedPriority > 0 {
		ui.selectedPriority--
		cx, cy := v.Cursor()
		v.SetCursor(cx, cy-1)
	}
	return nil
}

func (ui *UI) submitPriority(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		newPriority := ui.availablePriorities[ui.selectedPriority]
		issue := ui.issues[ui.selectedIssue]
		if ui.client != nil {
			if err := ui.client.UpdateIssuePriority(context.Background(), issue.ID, newPriority.value); err != nil {
				ui.showToast(fmt.Sprintf("Failed to update priority: %v", err))
			} else {
				ui.issues[ui.selectedIssue].Priority = float64(newPriority.value)
				ui.showToast(fmt.Sprintf("Changed priority to %s for %s", newPriority.label, issue.Identifier))
			}
		}
	}
	ui.showPriority = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) cancelPriority(g *gocui.Gui, v *gocui.View) error {
	ui.showPriority = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) prevView(g *gocui.Gui, v *gocui.View) error {
	ui.currentView--
	if ui.currentView < 0 {
		ui.currentView = len(ui.views) - 1
	}
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	return nil
}

func (ui *UI) nextView(g *gocui.Gui, v *gocui.View) error {
	ui.currentView++
	if ui.currentView >= len(ui.views) {
		ui.currentView = 0
	}
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	return nil
}

func (ui *UI) prevTeam(g *gocui.Gui, v *gocui.View) error {
	if len(ui.teams) == 0 {
		return nil
	}
	ui.currentTeam--
	if ui.currentTeam < 0 {
		ui.currentTeam = len(ui.teams) - 1
	}
	ui.availableStatuses = nil
	if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
		for _, state := range ui.teams[ui.currentTeam].States {
			ui.availableStatuses = append(ui.availableStatuses, state.Name)
		}
	}
	return ui.refreshIssues(g, v)
}

func (ui *UI) nextTeam(g *gocui.Gui, v *gocui.View) error {
	if len(ui.teams) == 0 {
		return nil
	}
	ui.currentTeam++
	if ui.currentTeam >= len(ui.teams) {
		ui.currentTeam = 0
	}
	ui.availableStatuses = nil
	if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
		for _, state := range ui.teams[ui.currentTeam].States {
			ui.availableStatuses = append(ui.availableStatuses, state.Name)
		}
	}
	return ui.refreshIssues(g, v)
}

func (ui *UI) copyURL(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		if issue.URL != "" {
			return ui.copyToClipboard(issue.URL, "Issue URL")
		}
	}
	return nil
}

func (ui *UI) copyBranch(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		if issue.BranchName != "" {
			return ui.copyToClipboard(issue.BranchName, "Git branch name")
		}
	}
	return nil
}

func (ui *UI) copyToClipboard(text string, desc string) error {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	} else if _, err := exec.LookPath("xsel"); err == nil {
		cmd = exec.Command("xsel", "--clipboard", "--input")
	} else if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd = exec.Command("wl-copy")
	} else if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd = exec.Command("pbcopy")
	} else {
		ui.showToast("No clipboard manager found")
		return nil
	}

	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	if _, err := in.Write([]byte(text)); err != nil {
		return err
	}

	if err := in.Close(); err != nil {
		return err
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	ui.showToast(desc + " copied to clipboard")
	return nil
}

// showToast displays a temporary toast message
func (ui *UI) showToast(message string) {
	ui.toastMessage = message
	if ui.toastTimer != nil {
		ui.toastTimer.Stop()
	}
	ui.toastTimer = time.AfterFunc(3*time.Second, func() {
		ui.toastMessage = ""
		ui.gui.Update(func(g *gocui.Gui) error {
			return nil
		})
	})
}

func (ui *UI) hexToAnsi(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "37"
	}

	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)

	brightness := (r + g + b) / 3

	if r > 150 && g > 150 && b < 120 {
		return "33"
	}

	if r > 150 && g < 120 && b < 120 {
		return "31"
	}

	if brightness < 64 {
		return "90"
	} else if brightness > 200 {
		return "37"
	}

	if r > g && r > b {
		return "31"
	} else if g > r && g > b {
		return "32"
	} else if b > r && b > g {
		return "34"
	} else if r > 150 && b > 150 {
		return "35"
	} else if g > 150 && b > 150 {
		return "36"
	}

	return "37"
}

func (ui *UI) filterIssues() []api.Issue {
	var filtered []api.Issue
	currentViewName := ui.views[ui.currentView]

	for _, issue := range ui.allIssues {
		if ui.assignedToMe && issue.Assignee.ID != ui.viewerID {
			continue
		}
		if currentViewName != "All" && issue.State.Name != currentViewName {
			continue
		}
		if ui.searchString != "" && !strings.Contains(strings.ToLower(issue.Title), strings.ToLower(ui.searchString)) {
			continue
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

func (ui *UI) toggleAssignee(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		ui.showAssignee = true
		issue := ui.issues[ui.selectedIssue]

		ui.availableAssignees = []struct {
			id   string
			name string
		}{
			{"", "No assignee"},
		}

		if ui.viewerID != "" {
			for _, member := range ui.teams[ui.currentTeam].Members {
				if member.ID == ui.viewerID {
					ui.availableAssignees = append(ui.availableAssignees, struct {
						id   string
						name string
					}{member.ID, member.Name})
					break
				}
			}
		}

		for _, member := range ui.teams[ui.currentTeam].Members {
			if member.ID != ui.viewerID {
				ui.availableAssignees = append(ui.availableAssignees, struct {
					id   string
					name string
				}{member.ID, member.Name})
			}
		}

		for i, a := range ui.availableAssignees {
			if a.id == issue.Assignee.ID {
				ui.selectedAssignee = i
				break
			}
		}
	}
	return nil
}

func (ui *UI) assigneeDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedAssignee < len(ui.availableAssignees)-1 {
		ui.selectedAssignee++
		cx, cy := v.Cursor()
		v.SetCursor(cx, cy+1)
	}
	return nil
}

func (ui *UI) assigneeUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil && ui.selectedAssignee > 0 {
		ui.selectedAssignee--
		cx, cy := v.Cursor()
		v.SetCursor(cx, cy-1)
	}
	return nil
}

func (ui *UI) submitAssignee(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		newAssignee := ui.availableAssignees[ui.selectedAssignee]
		issue := ui.issues[ui.selectedIssue]
		if ui.client != nil {
			if err := ui.client.UpdateIssueAssignee(context.Background(), issue.ID, newAssignee.id); err != nil {
				ui.showToast(fmt.Sprintf("Failed to update assignee: %v", err))
			} else {
				ui.issues[ui.selectedIssue].Assignee.ID = newAssignee.id
				ui.issues[ui.selectedIssue].Assignee.Name = newAssignee.name
				if newAssignee.name == "No assignee" {
					ui.showToast(fmt.Sprintf("Removed assignee from %s", issue.Identifier))
				} else {
					ui.showToast(fmt.Sprintf("Assigned %s to %s", issue.Identifier, newAssignee.name))
				}
			}
		}
	}
	ui.showAssignee = false
	g.SetCurrentView("issues")
	return nil
}

func (ui *UI) cancelAssignee(g *gocui.Gui, v *gocui.View) error {
	ui.showAssignee = false
	g.SetCurrentView("issues")
	return nil
}
