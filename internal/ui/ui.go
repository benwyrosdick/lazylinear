package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jroimartin/gocui"
	"lazylinear/internal/api"
)

// UI manages the terminal user interface
type UI struct {
	gui           *gocui.Gui
	client        *api.Client
	issues        []api.Issue
	allIssues     []api.Issue
	selectedIssue int
	showHelp      bool
	showSearch    bool
	searchString  string
	assignedToMe  bool
	viewerID      string
	currentView   int
	views         []string
	teams         []api.Team
	currentTeam   int
}

// NewUI creates a new UI instance
func NewUI(client *api.Client) (*UI, error) {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return nil, err
	}

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

	ui := &UI{
		gui:           g,
		client:        client,
		issues:        issues,
		allIssues:     issues,
		selectedIssue: -1,
		showHelp:      false,
		showSearch:    false,
		searchString:  "",
		assignedToMe:  false,
		viewerID:      viewerID,
		currentView:   0,
		views:         []string{"All", "In Review", "In Progress", "Blocked", "Todo", "Backlog"},
		teams:         teams,
		currentTeam:   0,
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
	if err := g.SetKeybinding("issues", 'h', gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'a', gocui.ModNone, ui.toggleAssigned); err != nil {
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
	if err := g.SetKeybinding("issues", '{', gocui.ModNone, ui.prevTeam); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '}', gocui.ModNone, ui.nextTeam); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("search", gocui.KeyEnter, gocui.ModNone, ui.closeSearch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("search", gocui.KeyCtrlQ, gocui.ModNone, ui.cancelSearch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("search", gocui.KeyEsc, gocui.ModNone, ui.cancelSearch); err != nil {
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
			v.Title = "Search (Enter to apply, Ctrl+Q to cancel)"
			v.Editable = true
			v.Editor = gocui.DefaultEditor
			fmt.Fprint(v, ui.searchString)
			v.SetCursor(len(ui.searchString), 0)
		} else {
			v.Title = "Search (Enter to apply, Ctrl+Q to cancel)"
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
	for _, issue := range ui.issues {
		initials := "--"
		if issue.Assignee.Name != "" {
			parts := strings.Fields(issue.Assignee.Name)
			if len(parts) >= 2 {
				initials = string(parts[0][0]) + string(parts[1][0])
			} else if len(parts) == 1 {
				if len(parts[0]) >= 2 {
					initials = string(parts[0][0]) + string(parts[0][1])
				} else {
					initials = parts[0]
				}
			}
		}
		fmt.Fprintf(v, "\033[32m%s\033[0m \033[33m%s\033[0m %s\n", issue.Identifier, initials, issue.Title)
	}

	// Set cursor to first item if needed
	if len(ui.issues) > 0 {
		_, cy := v.Cursor()
		if cy >= len(ui.issues) {
			v.SetCursor(0, len(ui.issues)-1)
		} else if cy < 0 {
			v.SetCursor(0, 0)
		}
	}

	// Set focus to issues view (unless search is active)
	if !ui.showSearch {
		g.SetCurrentView("issues")
	}

	// Issue details (right side)
	dv, err := g.SetView("details", issuesX+1, teamBarHeight+1, maxX-1, bottomY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		dv.Title = "Issue Details"
	}

	// Update details content
	dv.Clear()
	if ui.showHelp {
		fmt.Fprintln(dv, "LazyLinear Help")
		fmt.Fprintln(dv, "===============")
		fmt.Fprintln(dv, "")
		fmt.Fprintln(dv, "Navigation:")
		fmt.Fprintln(dv, "  j / ↓   : Move down")
		fmt.Fprintln(dv, "  k / ↑   : Move up")
		fmt.Fprintln(dv, "  [ / ]   : Switch view (All/In Review/In Progress/Blocked/Todo/Backlog)")
		fmt.Fprintln(dv, "  { / }   : Switch team")
		fmt.Fprintln(dv, "")
		fmt.Fprintln(dv, "Actions:")
		fmt.Fprintln(dv, "  Enter   : Select issue to view details")
		fmt.Fprintln(dv, "  r       : Refresh issues")
		fmt.Fprintln(dv, "  a       : Toggle filter by assigned to me")
		fmt.Fprintln(dv, "  /       : Search issues (Enter to apply, Ctrl+Q to cancel)")
		fmt.Fprintln(dv, "  ,       : Copy issue URL to clipboard")
		fmt.Fprintln(dv, "  .       : Copy git branch name to clipboard")
		fmt.Fprintln(dv, "  h       : Toggle this help")
		fmt.Fprintln(dv, "  Ctrl+C  : Quit")
		fmt.Fprintln(dv, "")
		fmt.Fprintln(dv, "Configuration:")
		fmt.Fprintln(dv, "  Set your Linear API key in ~/.lazylinear/config.json")
	} else if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		fmt.Fprintf(dv, "ID: %s\n", issue.ID)
		fmt.Fprintf(dv, "Title: %s\n", issue.Title)
		fmt.Fprintf(dv, "State: %s\n", issue.State.Name)
		if issue.Assignee.Name != "" {
			fmt.Fprintf(dv, "Assignee: %s\n", issue.Assignee.Name)
		}
		fmt.Fprintf(dv, "\nDescription:\n%s\n", issue.Description)
		if len(issue.Comments.Nodes) > 0 {
			fmt.Fprintln(dv, "\nComments:")
			for _, comment := range issue.Comments.Nodes {
				fmt.Fprintf(dv, "- %s (%s): %s\n", comment.User.Name, comment.CreatedAt, comment.Body)
			}
		}
	} else {
		fmt.Fprintln(dv, "Select an issue to view details")
		fmt.Fprintln(dv, "Press 'h' for help")
	}

	// Status bar (bottom)
	statusY := maxY - 2
	if ui.showSearch {
		statusY = maxY - 1
	}
	if v, err := g.SetView("status", 0, statusY, maxX-1, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
	}
	if sv, err := g.View("status"); err == nil {
		sv.Clear()
		status := "j/k/↑/↓: navigate | [/]: switch view | Enter: select | r: refresh | /: search | a: my issues | h: help | Ctrl+C: quit"
		if ui.assignedToMe {
			status = "[My Issues] " + status
		}
		if ui.searchString != "" {
			status = fmt.Sprintf("[Search: %s] %s", ui.searchString, status)
		}
		fmt.Fprintln(sv, status)
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
		} else if oy > 0 {
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
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
		} else {
			ui.allIssues = []api.Issue{{Title: fmt.Sprintf("Error loading issues: %v", err)}}
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
	return ui.refreshIssues(g, v)
}

func (ui *UI) copyURL(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		if issue.URL != "" {
			return ui.copyToClipboard(issue.URL)
		}
	}
	return nil
}

func (ui *UI) copyBranch(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		if issue.BranchName != "" {
			return ui.copyToClipboard(issue.BranchName)
		}
	}
	return nil
}

func (ui *UI) copyToClipboard(text string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	if _, err := exec.LookPath("xclip"); err != nil {
		cmd = exec.Command("xsel", "--clipboard", "--input")
		if _, err := exec.LookPath("xsel"); err != nil {
			cmd = exec.Command("wl-copy")
			if _, err := exec.LookPath("wl-copy"); err != nil {
				cmd = exec.Command("pbcopy")
			}
		}
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

	return cmd.Wait()
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
