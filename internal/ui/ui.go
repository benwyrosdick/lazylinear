package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/benwyrosdick/lazylinear/internal/api"
	"github.com/charmbracelet/glamour"
	"github.com/jroimartin/gocui"
	"github.com/muesli/termenv"
	"github.com/nsf/termbox-go"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// navItem is a row in the navigation pane
type navItem struct {
	kind  string // "header", "workspace", "view", "customView", "team" or "project"
	index int
	id    string
	label string
	color string
}

// UI manages the terminal user interface
type UI struct {
	gui                 *gocui.Gui
	client              *api.Client
	issues              []api.Issue
	allIssues           []api.Issue
	selectedIssue       int
	lastRenderedID      string
	inspectedID         string
	renderedDescription string
	renderedComments    []string
	showHelp            bool
	showSearch          bool
	searchString        string
	assignedToMe        bool
	viewerID            string
	viewerName          string
	currentView         int
	views               []string
	closedLoaded        bool
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
	showEdit          bool
	editTitle         string
	editDescription   string
	editActivePane    string
	showCreate        bool
	createTitle       string
	createDescription string
	createActivePane  string
	mdRenderer        *glamour.TermRenderer
	mdStyle           string
	mdWidth           int
	loading           bool
	spinnerFrame      int
	loadingTimer      *time.Timer
	lastActiveView    string
	navItems          []navItem
	navWidth          int
	detailsWidth      int
	resizing          string
	resizeOffset      int
	navCursor         int
	lastSynced        time.Time
	customViews       []api.CustomView
	currentCustomView string
	projectFilter     string
	teamIssueCounts   map[string]int
	tty               *os.File
	images            map[string]*inlineImage
	imagesMu          sync.Mutex
	nextImageID       uint32
	inspectorImages   []imageRef
	placedImages      []imagePlacement
	viewedImage       *imagePlacement
	redrawImages      bool
	mdHeight          int
	screenWidth       int
	screenHeight      int
	cellWidth         int
	cellHeight        int
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
	// Detect the terminal background before gocui takes over the terminal
	mdStyle := "dark"
	if !termenv.HasDarkBackground() {
		mdStyle = "light"
	}
	tty := openImageTerminal()

	g, err := gocui.NewGui(gocui.Output256)
	if err != nil {
		return nil, err
	}

	// Enable InputEsc mode to handle Esc key properly
	g.InputEsc = true

	// Enable mouse support
	g.Mouse = true

	// Enable highlighting and set border colors like lazygit
	g.Highlight = true
	g.SelFgColor = gocui.ColorGreen // Active pane border color
	g.FgColor = gocui.ColorDefault  // Inactive pane border color

	ui := &UI{
		gui:                 g,
		client:              client,
		issues:              []api.Issue{},
		allIssues:           []api.Issue{},
		selectedIssue:       -1,
		lastRenderedID:      "",
		renderedDescription: "",
		renderedComments:    nil,
		showHelp:            false,
		showSearch:          false,
		searchString:        "",
		assignedToMe:        false,
		viewerID:            "",
		currentView:         0,
		views:               []string{"Open", "All", "Triage", "In Review", "In Progress", "Blocked", "Todo", "Backlog"},
		teams:               []api.Team{},
		currentTeam:         0,
		showComment:         false,
		commentContent:      "",
		toastMessage:        "",
		toastTimer:          nil,
		showStatus:          false,
		selectedStatus:      0,
		availableStatuses:   []string{},
		showPriority:        false,
		selectedPriority:    0,
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
		showEdit:         false,
		editActivePane:   "title",
		showCreate:       false,
		createActivePane: "title",
		mdStyle:          mdStyle,
		tty:              tty,
		images:           map[string]*inlineImage{},
		nextImageID:      firstImageID,
		loading:          true,
		spinnerFrame:     0,
		loadingTimer:     nil,
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
	if err := g.SetKeybinding("issues", 'r', gocui.ModNone, ui.reload); err != nil {
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
	if err := g.SetKeybinding("issues", ',', gocui.ModNone, ui.copyURL); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", '.', gocui.ModNone, ui.copyBranch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'y', gocui.ModNone, ui.copyDescription); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'o', gocui.ModNone, ui.openInBrowser); err != nil {
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
	if err := g.SetKeybinding("issues", 'n', gocui.ModNone, ui.toggleCreate); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'e', gocui.ModNone, ui.toggleEdit); err != nil {
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
	if err := g.SetKeybinding("comment", gocui.MouseRelease, gocui.ModNone, ui.releaseEditPane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("help", '?', gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("help", gocui.KeyEsc, gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("image", gocui.KeyEsc, gocui.ModNone, ui.closeImage); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("image", gocui.MouseLeft, gocui.ModNone, ui.closeImage); err != nil {
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
	if err := g.SetKeybinding("edit_title", gocui.KeyTab, gocui.ModNone, ui.switchEditPane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_title", gocui.KeyCtrlS, gocui.ModNone, ui.submitEdit); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_title", gocui.KeyEsc, gocui.ModNone, ui.cancelEdit); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_description", gocui.KeyTab, gocui.ModNone, ui.switchEditPane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_description", gocui.KeyCtrlS, gocui.ModNone, ui.submitEdit); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_description", gocui.KeyEsc, gocui.ModNone, ui.cancelEdit); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_title", gocui.MouseLeft, gocui.ModNone, ui.clickEditTitle); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_title", gocui.MouseRelease, gocui.ModNone, ui.releaseEditPane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_description", gocui.MouseLeft, gocui.ModNone, ui.clickEditDescription); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("edit_description", gocui.MouseRelease, gocui.ModNone, ui.releaseEditPane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_title", gocui.KeyTab, gocui.ModNone, ui.switchCreatePane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_title", gocui.KeyCtrlS, gocui.ModNone, ui.submitCreate); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_title", gocui.KeyEsc, gocui.ModNone, ui.cancelCreate); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_description", gocui.KeyTab, gocui.ModNone, ui.switchCreatePane); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_description", gocui.KeyCtrlS, gocui.ModNone, ui.submitCreate); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_description", gocui.KeyEsc, gocui.ModNone, ui.cancelCreate); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_title", gocui.MouseLeft, gocui.ModNone, ui.clickCreateTitle); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("create_description", gocui.MouseLeft, gocui.ModNone, ui.clickCreateDescription); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.MouseLeft, gocui.ModNone, ui.clickIssues); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.MouseWheelDown, gocui.ModNone, ui.scrollIssuesDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.MouseWheelUp, gocui.ModNone, ui.scrollIssuesUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'l', gocui.ModNone, ui.focusDetails); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'h', gocui.ModNone, ui.focusIssues); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", gocui.KeyEsc, gocui.ModNone, ui.focusIssues); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", ',', gocui.ModNone, ui.copyURL); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", '.', gocui.ModNone, ui.copyBranch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'y', gocui.ModNone, ui.copyDescription); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'o', gocui.ModNone, ui.openInBrowser); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'a', gocui.ModNone, ui.toggleAssignee); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'c', gocui.ModNone, ui.toggleComment); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'e', gocui.ModNone, ui.toggleEdit); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 's', gocui.ModNone, ui.toggleStatus); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'p', gocui.ModNone, ui.togglePriority); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", gocui.KeyArrowDown, gocui.ModNone, ui.scrollDetailsDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", gocui.KeyArrowUp, gocui.ModNone, ui.scrollDetailsUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'j', gocui.ModNone, ui.scrollDetailsDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", 'k', gocui.ModNone, ui.scrollDetailsUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", gocui.MouseWheelDown, gocui.ModNone, ui.scrollDetailsDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", gocui.MouseWheelUp, gocui.ModNone, ui.scrollDetailsUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("details", gocui.MouseLeft, gocui.ModNone, ui.clickDetails); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", 'h', gocui.ModNone, ui.focusNav); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("issues", gocui.KeyEnter, gocui.ModNone, ui.focusDetails); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", gocui.KeyArrowDown, gocui.ModNone, ui.navDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", gocui.KeyArrowUp, gocui.ModNone, ui.navUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", 'j', gocui.ModNone, ui.navDown); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", 'k', gocui.ModNone, ui.navUp); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", gocui.KeyEnter, gocui.ModNone, ui.navSelect); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", 'l', gocui.ModNone, ui.focusIssues); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", gocui.MouseLeft, gocui.ModNone, ui.clickNav); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", 'r', gocui.ModNone, ui.reload); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", 'm', gocui.ModNone, ui.toggleAssigned); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", '/', gocui.ModNone, ui.toggleSearch); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("nav", '?', gocui.ModNone, ui.toggleHelp); err != nil {
		return nil, err
	}
	for _, pane := range []string{"nav", "issues", "details"} {
		if err := g.SetKeybinding(pane, gocui.KeyTab, gocui.ModNone, ui.nextPane); err != nil {
			return nil, err
		}
		if err := g.SetKeybinding(pane, '1', gocui.ModNone, ui.focusNav); err != nil {
			return nil, err
		}
		if err := g.SetKeybinding(pane, '2', gocui.ModNone, ui.focusIssues); err != nil {
			return nil, err
		}
		if err := g.SetKeybinding(pane, '3', gocui.ModNone, ui.focusDetails); err != nil {
			return nil, err
		}
	}

	// Pane resizing: any press or release ends a drag, a press on a gutter
	// starts one, and motion with the button held moves the gutter
	if err := g.SetKeybinding("", gocui.MouseLeft, gocui.ModNone, ui.endResize); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("", gocui.MouseRelease, gocui.ModNone, ui.endResize); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("gutter_nav", gocui.MouseLeft, gocui.ModNone, ui.startResize); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("gutter_details", gocui.MouseLeft, gocui.ModNone, ui.startResize); err != nil {
		return nil, err
	}
	if err := g.SetKeybinding("", gocui.MouseLeft, gocui.Modifier(termbox.ModMotion), ui.dragResize); err != nil {
		return nil, err
	}

	// Start spinner animation
	ui.startSpinner()

	// Load data in background
	go ui.loadData()

	return ui, nil
}

// Run starts the UI main loop
func (ui *UI) Run() error {
	defer func() {
		if ui.loadingTimer != nil {
			ui.loadingTimer.Stop()
		}
		if ui.toastTimer != nil {
			ui.toastTimer.Stop()
		}
		ui.closeImages()
		ui.gui.Close()
	}()
	return ui.gui.MainLoop()
}

// Close closes the UI
func (ui *UI) Close() {
	ui.gui.Close()
}

func (ui *UI) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	if maxX != ui.screenWidth || maxY != ui.screenHeight {
		ui.screenWidth, ui.screenHeight = maxX, maxY
		if ui.tty != nil {
			ui.cellWidth, ui.cellHeight = cellPixelSize(ui.tty)
			ui.lastRenderedID = ""
			// termbox clears the screen after a resize, which can wipe images, so draw them again afterwards
			time.AfterFunc(100*time.Millisecond, func() {
				ui.gui.Update(func(g *gocui.Gui) error {
					ui.redrawImages = true
					return nil
				})
			})
		}
	}

	// Show loading spinner if still loading
	if ui.loading {
		spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerWidth := 80
		spinnerHeight := 12
		spinnerX := (maxX - spinnerWidth) / 2
		spinnerY := (maxY - spinnerHeight) / 2

		if sv, err := g.SetView("spinner", spinnerX, spinnerY, spinnerX+spinnerWidth, spinnerY+spinnerHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			sv.Frame = false
		}

		if sv, err := g.View("spinner"); err == nil {
			sv.Clear()
			spinner := spinnerChars[ui.spinnerFrame%len(spinnerChars)]

			// ASCII art for LazyLinear
			fmt.Fprintf(sv, "\n")
			fmt.Fprintf(sv, "     _                     _     _                       \n")
			fmt.Fprintf(sv, "    | |                   | |   (_)                      \n")
			fmt.Fprintf(sv, "    | |     __ _ _____   _| |    _ _ __   ___  __ _ _ __ \n")
			fmt.Fprintf(sv, "    | |    / _` |_  / | | | |   | | '_ \\ / _ \\/ _` | '__|\n")
			fmt.Fprintf(sv, "    | |___| (_| |/ /| |_| | |___| | | | |  __/ (_| | |   \n")
			fmt.Fprintf(sv, "    |______\\__,_/___|\\__, |_____|_|_| |_|\\___|\\__,_|_|   \n")
			fmt.Fprintf(sv, "                      __/ |                              \n")
			fmt.Fprintf(sv, "                     |___/                               \n")
			fmt.Fprintf(sv, "\n")
			fmt.Fprintf(sv, "                      %s Loading data...\n", spinner)
		}

		return nil
	}

	// Delete spinner view once loaded
	g.DeleteView("spinner")

	// Icon Strings
	urgentPriorityIcon := ""
	highPriorityIcon := "󰢾"
	mediumPriorityIcon := "󰢽"
	lowPriorityIcon := "󰢼"
	noPriorityIcon := "󰇘"

	triageStateIcon := ""
	backlogStateIcon := "󱥸"
	unstartedStateIcon := ""
	blockedStateIcon := "󰪟"
	inReviewStateIcon := "󰪣"
	startedStateIcon := "󰪡"
	completedStateIcon := ""
	canceledStateIcon := ""

	searchIcon := ""
	// commentIcon := ""
	// editIcon := ""
	// newIcon := "󱈀"
	// userIcon := ""
	// stateIcon := "󱇯"

	// Pane geometry: navigation | issues | inspector
	navWidth := maxX * 18 / 100
	if navWidth < 24 {
		navWidth = 24
	} else if navWidth > 34 {
		navWidth = 34
	}
	detailsWidth := maxX * 30 / 100

	// Widths dragged by the user take precedence, keeping every pane usable
	minNavWidth, minIssuesWidth, minDetailsWidth := 12, 20, 20
	if ui.navWidth > 0 {
		navWidth = ui.navWidth
	}
	if ui.detailsWidth > 0 {
		detailsWidth = ui.detailsWidth
	}
	if navWidth > maxX-detailsWidth-minIssuesWidth {
		navWidth = maxX - detailsWidth - minIssuesWidth
	}
	if navWidth < minNavWidth {
		navWidth = minNavWidth
	}
	if detailsWidth > maxX-navWidth-minIssuesWidth {
		detailsWidth = maxX - navWidth - minIssuesWidth
	}
	if detailsWidth < minDetailsWidth {
		detailsWidth = minDetailsWidth
	}
	if maxX-navWidth-detailsWidth < 20 {
		navWidth = maxX / 5
		detailsWidth = maxX / 4
	}
	issuesX0 := navWidth
	issuesX1 := maxX - detailsWidth - 1
	detailsX0 := maxX - detailsWidth
	topY := 1
	bottomY := maxY - 2

	// Gutters sit beneath the panes so the pane frames draw over them, but
	// clicks on the border columns (outside every pane's interior) land here
	gutters := []struct {
		name string
		x    int
	}{
		{"gutter_nav", issuesX0 - 1},
		{"gutter_details", issuesX1},
	}
	for _, gutter := range gutters {
		if gv, err := g.SetView(gutter.name, gutter.x-1, topY-1, gutter.x+2, bottomY+1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			gv.Frame = false
			g.SetViewOnBottom(gutter.name)
		}
	}

	// Header bar (top)
	if hv, err := g.SetView("header", 0, -1, maxX-1, 1); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		hv.Frame = false
	}
	if hv, err := g.View("header"); err == nil {
		hv.Clear()
		width, _ := hv.Size()
		left := "\033[1;36mLL\033[0m \033[1mlazylinear\033[0m  \033[38;5;245m│  " + ui.breadcrumb() + "\033[0m"
		right := ""
		if ui.viewerName != "" {
			right = "\033[33m" + ui.viewerName + "\033[0m"
		}
		fmt.Fprint(hv, alignLine(left, right, width))
	}

	// Navigation (left side)
	nv, err := g.SetView("nav", 0, topY, issuesX0-1, bottomY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		nv.SelBgColor = gocui.ColorGreen
		nv.SelFgColor = gocui.ColorBlack
	}
	nv.Title = "1 Navigation"

	// Search bar (bottom of the issues column, if enabled)
	issuesY1 := bottomY
	if ui.showSearch {
		issuesY1 = bottomY - 3
		title := searchIcon + "Search (Enter: apply, Esc: cancel)"
		if v, err := g.SetView("search", issuesX0, bottomY-2, issuesX1, bottomY); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = title
			v.Editable = true
			v.Editor = gocui.DefaultEditor
			fmt.Fprint(v, ui.searchString)
			v.SetCursor(len(ui.searchString), 0)
		} else {
			v.Title = title
		}
		g.SetCurrentView("search")
	} else {
		g.DeleteView("search")
	}

	// Issues list (middle)
	v, err := g.SetView("issues", issuesX0, topY, issuesX1, issuesY1)
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

	viewTitle := "2 " + ui.views[ui.currentView]
	if view := ui.customView(ui.currentCustomView); view != nil {
		viewTitle = "2 " + view.Name
		if ui.views[ui.currentView] != "Open" {
			viewTitle = viewTitle + " / " + ui.views[ui.currentView]
		}
	}
	if ui.projectFilter != "" {
		viewTitle = viewTitle + " / " + ui.projectName(ui.projectFilter)
	}
	if ui.assignedToMe {
		viewTitle = viewTitle + " (My Issues)"
	}
	if ui.searchString != "" {
		viewTitle = viewTitle + " [" + ui.searchString + "]"
	}
	viewTitle = fmt.Sprintf("%s - %d issues", viewTitle, len(ui.issues))

	// Add scroll indicator
	if len(ui.issues) > 0 {
		_, viewHeight := v.Size()
		_, oy := v.Origin()
		totalLines := len(ui.issues)
		if totalLines > viewHeight {
			scrollPercent := int((float64(oy) / float64(totalLines-viewHeight)) * 100)
			if scrollPercent > 100 {
				scrollPercent = 100
			}
			viewTitle = fmt.Sprintf("%s (%d%%)", viewTitle, scrollPercent)
		}
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

		priorityIcon := noPriorityIcon
		priorityColor := ""
		switch int(issue.Priority) {
		case 1:
			priorityIcon = urgentPriorityIcon
			priorityColor = "\033[31m"
		case 2:
			priorityIcon = highPriorityIcon
		case 3:
			priorityIcon = mediumPriorityIcon
		case 4:
			priorityIcon = lowPriorityIcon
		}
		if priorityColor != "" {
			priorityIcon = priorityColor + priorityIcon + "\033[0m"
		}

		stateIcon := unstartedStateIcon
		switch issue.State.Type {
		case "triage":
			stateIcon = triageStateIcon
		case "backlog":
			stateIcon = backlogStateIcon
		case "unstarted":
			stateIcon = unstartedStateIcon
		case "started":
			// Check the actual state name for started states
			switch issue.State.Name {
			case "Blocked":
				stateIcon = blockedStateIcon
			case "In Progress":
				stateIcon = startedStateIcon
			case "In Review":
				stateIcon = inReviewStateIcon
			default:
				stateIcon = startedStateIcon
			}
		case "completed":
			stateIcon = completedStateIcon
		case "canceled":
			stateIcon = canceledStateIcon
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
					colorCode = "38;5;245"
				}
			}
		}

		identifierFmt := fmt.Sprintf("%%-%ds", maxIdentifierLen)
		fmt.Fprintf(v, "%s \033[36m"+identifierFmt+"\033[0m \033[%sm%s\033[0m  \033[33m%s\033[0m  %s\n", priorityIcon, issue.Identifier, colorCode, stateIcon, initials, issue.Title)
	}

	// Set cursor to first item if needed
	if len(ui.issues) > 0 && !ui.showEdit && !ui.showCreate && !ui.showComment && !ui.showStatus && !ui.showPriority && !ui.showAssignee {
		_, cy := v.Cursor()
		_, oy := v.Origin()
		_, viewHeight := v.Size()

		// Adjust selectedIssue bounds if needed
		if ui.selectedIssue >= len(ui.issues) {
			ui.selectedIssue = len(ui.issues) - 1
		} else if ui.selectedIssue < 0 {
			ui.selectedIssue = 0
		}

		// Calculate where cursor should be based on selectedIssue
		targetCursorY := ui.selectedIssue - oy

		// Only show highlight if selected issue is within the visible viewport
		if targetCursorY >= 0 && targetCursorY < viewHeight {
			// Selected issue is visible - position cursor on it and enable highlight
			v.Highlight = true
			if cy != targetCursorY {
				v.SetCursor(0, targetCursorY)
			}
		} else {
			// Selected issue is outside visible area - disable highlight
			v.Highlight = false
		}
	}

	// Set focus and cursor
	if ui.showSearch {
		g.SetCurrentView("search")
		g.Cursor = true
	} else if ui.showComment {
		g.SetCurrentView("comment")
		g.Cursor = true
	} else if ui.showEdit {
		if ui.editActivePane == "title" {
			g.SetCurrentView("edit_title")
		} else {
			g.SetCurrentView("edit_description")
		}
		g.Cursor = true
	} else if ui.showCreate {
		if ui.createActivePane == "title" {
			g.SetCurrentView("create_title")
		} else {
			g.SetCurrentView("create_description")
		}
		g.Cursor = true
	} else if ui.showHelp {
		g.SetCurrentView("help")
		g.Cursor = false
	} else if ui.viewedImage != nil {
		g.SetCurrentView("image")
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
		cv := g.CurrentView()
		if cv == nil || (cv.Name() != "nav" && cv.Name() != "issues" && cv.Name() != "details") {
			g.SetCurrentView("issues")
		}
		g.Cursor = false
	}

	// Update navigation content
	currentView := g.CurrentView()
	ui.navItems = ui.buildNavItems()
	if ui.navCursor >= len(ui.navItems) || ui.navItems[ui.navCursor].kind == "header" {
		ui.navCursor = ui.nextNavItem(-1, 1)
	}
	nv.Clear()
	navWidthInner, navHeight := nv.Size()
	for _, item := range ui.navItems {
		fmt.Fprintln(nv, ui.renderNavItem(item, navWidthInner))
	}
	_, navOy := nv.Origin()
	if ui.navCursor < navOy {
		navOy = ui.navCursor
	} else if ui.navCursor >= navOy+navHeight {
		navOy = ui.navCursor - navHeight + 1
	}
	nv.SetOrigin(0, navOy)
	nv.SetCursor(0, ui.navCursor-navOy)
	nv.Highlight = currentView != nil && currentView.Name() == "nav"

	// Issue details (right side)
	dv, err := g.SetView("details", detailsX0, topY, maxX-1, bottomY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		dv.Wrap = true
		dv.Autoscroll = false
	}

	detailsTitle := "3 Inspector"
	if currentView != nil && currentView.Name() == "details" {
		detailsTitle = "3 Inspector [h: back to issues]"
	}

	// Add scroll indicator for details
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		_, viewHeight := dv.Size()
		_, oy := dv.Origin()
		totalLines := len(dv.ViewBufferLines())
		if totalLines > viewHeight && totalLines > 0 {
			scrollPercent := int((float64(oy) / float64(totalLines-viewHeight)) * 100)
			if scrollPercent > 100 {
				scrollPercent = 100
			}
			detailsTitle = fmt.Sprintf("%s (%d%%)", detailsTitle, scrollPercent)
		}
	}
	dv.Title = detailsTitle

	// Update details content
	dv.Clear()
	var placements []imagePlacement
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		var content strings.Builder

		// Cache rendered markdown - only re-render if issue or pane size changed
		detailsWidthInner, detailsHeightInner := dv.Size()
		ui.setMarkdownWidth(detailsWidthInner)
		if ui.tty != nil && detailsHeightInner != ui.mdHeight {
			ui.mdHeight = detailsHeightInner
			ui.lastRenderedID = ""
		}
		if issue.ID != ui.inspectedID {
			dv.SetOrigin(0, 0)
			ui.inspectedID = issue.ID
		}
		if issue.ID != ui.lastRenderedID {
			ui.inspectorImages = nil
			ui.renderedDescription = ui.renderMarkdownWithImages(issue.Description)
			ui.renderedComments = make([]string, len(issue.Comments.Nodes))
			for i, comment := range issue.Comments.Nodes {
				ui.renderedComments[i] = ui.renderMarkdownWithImages(comment.Body)
			}
			ui.lastRenderedID = issue.ID
		}

		stateColorCode := "37"
		if issue.State.Color != "" {
			if strings.HasPrefix(issue.State.Color, "#") {
				stateColorCode = ui.hexToAnsi(issue.State.Color)
			}
		}

		propertyFmt := "\033[38;5;245m%-10s\033[0m %s\n"
		fmt.Fprintf(&content, "\033[36m%s\033[0m\n", issue.Identifier)
		fmt.Fprintf(&content, "\033[1m%s\033[0m\n\n", issue.Title)
		fmt.Fprintf(&content, propertyFmt, "Status", fmt.Sprintf("\033[%sm%s\033[0m", stateColorCode, issue.State.Name))

		priorityIcon := noPriorityIcon
		priorityLabel := "No Priority"
		priorityColor := ""
		switch int(issue.Priority) {
		case 1:
			priorityIcon = urgentPriorityIcon
			priorityLabel = "Urgent"
			priorityColor = "\033[31m"
		case 2:
			priorityIcon = highPriorityIcon
			priorityLabel = "High"
		case 3:
			priorityIcon = mediumPriorityIcon
			priorityLabel = "Medium"
		case 4:
			priorityIcon = lowPriorityIcon
			priorityLabel = "Low"
		}
		if priorityColor != "" {
			fmt.Fprintf(&content, propertyFmt, "Priority", fmt.Sprintf("%s%s %s\033[0m", priorityColor, priorityIcon, priorityLabel))
		} else {
			fmt.Fprintf(&content, propertyFmt, "Priority", fmt.Sprintf("%s %s", priorityIcon, priorityLabel))
		}

		teamName := "—"
		for _, team := range ui.teams {
			if team.ID == issue.Team.ID {
				teamName = team.Name
				break
			}
		}
		fmt.Fprintf(&content, propertyFmt, "Team", teamName)

		projectName := "—"
		if issue.Project.Name != "" {
			projectName = "\033[" + hexTo256(issue.Project.Color) + "m" + issue.Project.Name + "\033[0m"
		}
		fmt.Fprintf(&content, propertyFmt, "Project", projectName)

		assigneeName := "—"
		if issue.Assignee.Name != "" {
			assigneeName = issue.Assignee.Name
		}
		fmt.Fprintf(&content, propertyFmt, "Assignee", assigneeName)

		dueDate := "—"
		if issue.DueDate != "" {
			dueDate = issue.DueDate
		}
		fmt.Fprintf(&content, propertyFmt, "Due", dueDate)

		estimate := "—"
		if issue.Estimate > 0 {
			estimate = fmt.Sprintf("%g", issue.Estimate)
		}
		fmt.Fprintf(&content, propertyFmt, "Estimate", estimate)
		if issue.URL != "" {
			fmt.Fprintf(&content, propertyFmt, "URL", "\033[34m"+issue.URL+"\033[0m")
		}
		if issue.Description != "" {
			fmt.Fprintf(&content, "\n\033[38;5;245mDESCRIPTION\033[0m\n")
			fmt.Fprintf(&content, "%s\n", ui.renderedDescription)
		}
		if len(issue.Labels.Nodes) > 0 {
			var labels []string
			for _, label := range issue.Labels.Nodes {
				labels = append(labels, "\033["+hexTo256(label.Color)+"m#"+label.Name+"\033[0m")
			}
			fmt.Fprintf(&content, "\n\033[38;5;245mLABELS\033[0m\n")
			fmt.Fprintf(&content, "%s\n", strings.Join(labels, "  "))
		}
		if len(issue.Comments.Nodes) > 0 {
			fmt.Fprintf(&content, "\n\033[38;5;245mCOMMENTS\033[0m\n")
			for i, comment := range issue.Comments.Nodes {
				fmt.Fprintf(&content, "\033[33m%s\033[0m \033[38;5;245m(%s)\033[0m\n", comment.User.Name, comment.CreatedAt)
				if i < len(ui.renderedComments) {
					fmt.Fprintf(&content, "%s\n\n", ui.renderedComments[i])
				}
			}
		}

		// Images are drawn over the blank rows reserved for them, except under modals and the toast
		modalOpen := ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp
		if ui.tty != nil && !modalOpen {
			clipY := bottomY
			if ui.toastMessage != "" {
				clipY = bottomY - 3
			}
			placements = ui.inspectorImagePlacements(content.String(), dv, detailsX0, topY, clipY)
		}
		fmt.Fprint(dv, imageTokenPattern.ReplaceAllStringFunc(content.String(), func(token string) string {
			return strings.Repeat(" ", len(token))
		}))
	} else {
		fmt.Fprintln(dv, "Select an issue to view details")
		fmt.Fprintln(dv, "Press '?' for help")
	}
	if ui.viewedImage != nil {
		// Inline images stay visible around the viewer, but not under it or its frame
		viewer := ui.imageModalPlacement(maxX, maxY)
		placements = append(cutOut(placements, viewer.x-1, viewer.y-1, viewer.cols+2, viewer.rows+2), viewer)
	}
	ui.drawImages(placements)

	// Keybindings bar (bottom)
	if v, err := g.SetView("keybindings", 0, maxY-2, maxX-1, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
	}
	if kv, err := g.View("keybindings"); err == nil {
		kv.Clear()

		var keybindings [][2]string
		if ui.showSearch {
			keybindings = [][2]string{{"enter", "apply"}, {"esc", "cancel"}}
		} else if ui.showComment {
			keybindings = [][2]string{{"ctrl+s", "submit"}, {"esc", "cancel"}}
		} else if ui.showStatus || ui.showPriority || ui.showAssignee {
			keybindings = [][2]string{{"j/k", "move"}, {"enter", "select"}, {"esc", "cancel"}}
		} else if ui.showEdit {
			keybindings = [][2]string{{"tab", "switch pane"}, {"ctrl+s", "save"}, {"esc", "cancel"}}
		} else if ui.showCreate {
			keybindings = [][2]string{{"tab", "switch pane"}, {"ctrl+s", "create"}, {"esc", "cancel"}}
		} else if ui.showHelp {
			keybindings = [][2]string{{"?/esc", "close"}}
		} else if ui.viewedImage != nil {
			keybindings = [][2]string{{"esc", "close"}}
		} else if currentView != nil && currentView.Name() == "nav" {
			keybindings = [][2]string{{"j/k", "move"}, {"enter", "select"}, {"l", "issues"}, {"m", "my issues"}, {"/", "search"}, {"r", "refresh"}, {"tab", "pane"}, {"?", "help"}, {"ctrl+c", "quit"}}
		} else if currentView != nil && currentView.Name() == "details" {
			keybindings = [][2]string{{"j/k", "scroll"}, {"h", "back"}, {"e", "edit"}, {"s", "status"}, {"p", "priority"}, {"a", "assign"}, {"c", "comment"}, {"o", "open"}, {"y", "copy description"}, {",", "copy url"}, {".", "copy branch"}, {"tab", "pane"}, {"?", "help"}, {"ctrl+c", "quit"}}
		} else {
			keybindings = [][2]string{{"j/k", "move"}, {"enter", "inspect"}, {"n", "new"}, {"e", "edit"}, {"s", "status"}, {"p", "priority"}, {"a", "assign"}, {"c", "comment"}, {"/", "search"}, {"m", "my issues"}, {"[/]", "view"}, {"{/}", "team"}, {"o", "open"}, {"y", "copy description"}, {",", "copy url"}, {".", "copy branch"}, {"r", "refresh"}, {"tab", "pane"}, {"?", "help"}, {"ctrl+c", "quit"}}
		}

		right := ""
		if !ui.lastSynced.IsZero() {
			right = "\033[38;5;245msynced " + ui.lastSynced.Format("3:04:05 pm") + "\033[0m"
		}
		width, _ := kv.Size()
		available := width - visibleLen(right) - 2

		var hints []string
		used := 0
		for i, kb := range keybindings {
			hint := kb[0] + " " + kb[1]
			if used+len([]rune(hint)) > available {
				if i > 0 {
					hints = append(hints, "…")
				}
				break
			}
			hints = append(hints, "\033[33m"+kb[0]+"\033[0m "+kb[1])
			used += len([]rune(hint)) + 2
		}

		fmt.Fprint(kv, alignLine(strings.Join(hints, "  "), right, width))
	}

	// Toast message (if present)
	if ui.toastMessage != "" {
		toastWidth := len([]rune(ui.toastMessage)) + 2
		if toastWidth > maxX-2 {
			toastWidth = maxX - 2
		}
		if tv, err := g.SetView("toast", maxX-toastWidth-2, bottomY-3, maxX-2, bottomY-1); err != nil {
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

	// Modals (rendered last so they appear on top)
	// Help modal (if enabled)
	if ui.showHelp {
		helpWidth := maxX - 20
		helpHeight := 35
		if helpHeight > maxY-2 {
			helpHeight = maxY - 2
		}
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
			fmt.Fprintln(hv, "  [ / ]   : Switch status (Open/All/Triage/In Review/.../Backlog)")
			fmt.Fprintln(hv, "  { / }   : Switch team")
			fmt.Fprintln(hv, "  tab     : Cycle panes (navigation / issues / inspector)")
			fmt.Fprintln(hv, "  1 2 3   : Focus navigation / issues / inspector")
			fmt.Fprintln(hv, "  h / l   : Move focus left / right")
			fmt.Fprintln(hv, "  enter   : Select navigation item / inspect issue")
			fmt.Fprintln(hv, "  drag    : Drag the lines between panes to resize them")
			fmt.Fprintln(hv, "  click   : Click an image in the inspector to view it full size")
			fmt.Fprintln(hv, "")
			fmt.Fprintln(hv, "Actions:")
			fmt.Fprintln(hv, "  r       : Refresh issues")
			fmt.Fprintln(hv, "  m       : Toggle filter by assigned to me")
			fmt.Fprintln(hv, "  /       : Search issues")
			fmt.Fprintln(hv, "  n       : Create new issue")
			fmt.Fprintln(hv, "  c       : Add comment to selected issue")
			fmt.Fprintln(hv, "  e       : Edit issue title and description")
			fmt.Fprintln(hv, "  s       : Change status of selected issue")
			fmt.Fprintln(hv, "  p       : Change priority of selected issue")
			fmt.Fprintln(hv, "  a       : Assign user to selected issue")
			fmt.Fprintln(hv, "  ,       : Copy issue URL to clipboard")
			fmt.Fprintln(hv, "  .       : Copy git branch name to clipboard")
			fmt.Fprintln(hv, "  y       : Copy issue description (markdown) to clipboard")
			fmt.Fprintln(hv, "  o       : Open issue in Linear in your browser")
			fmt.Fprintln(hv, "  ?       : Toggle this help")
			fmt.Fprintln(hv, "  Ctrl+C  : Quit")
			fmt.Fprintln(hv, "")
			fmt.Fprintln(hv, "Configuration:")
			fmt.Fprintln(hv, "  Set your Linear API key in ~/.lazylinear/config.json")
		}
	} else {
		g.DeleteView("help")
	}

	// Image viewer (if an inline image was clicked)
	if ui.viewedImage != nil {
		p := ui.imageModalPlacement(maxX, maxY)
		iv, err := g.SetView("image", p.x-1, p.y-1, p.x+p.cols, p.y+p.rows)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}
		iv.Title = ui.imageModalTitle(p)
		g.SetCurrentView("image")
	} else {
		g.DeleteView("image")
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
						stateIcon = triageStateIcon
					case "backlog":
						stateIcon = backlogStateIcon
					case "unstarted":
						stateIcon = unstartedStateIcon
					case "started":
						// Check the actual state name for started states
						switch state.Name {
						case "Blocked":
							stateIcon = blockedStateIcon
						case "In Progress":
							stateIcon = startedStateIcon
						case "In Review":
							stateIcon = inReviewStateIcon
						default:
							stateIcon = startedStateIcon
						}
					case "completed":
						stateIcon = completedStateIcon
					case "canceled":
						stateIcon = canceledStateIcon
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
				priorityIcon := noPriorityIcon
				priorityColor := ""
				switch priority.value {
				case 1:
					priorityIcon = urgentPriorityIcon
					priorityColor = "\033[31m"
				case 2:
					priorityIcon = highPriorityIcon
				case 3:
					priorityIcon = mediumPriorityIcon
				case 4:
					priorityIcon = lowPriorityIcon
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

	// Edit pane (if enabled)
	if ui.showEdit {
		editWidth := maxX - 10
		titleHeight := 3
		descriptionHeight := 15
		totalHeight := titleHeight + descriptionHeight + 3
		editX := (maxX - editWidth) / 2
		editY := (maxY - totalHeight) / 2

		// Title pane
		titleY := editY
		if tv, err := g.SetView("edit_title", editX, titleY, editX+editWidth, titleY+titleHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			tv.Title = "Title (Tab: switch, Ctrl+S: save, Esc: cancel)"
			tv.Editable = true
			tv.Editor = gocui.DefaultEditor
			tv.Frame = true
			fmt.Fprint(tv, ui.editTitle)
			tv.SetCursor(len(ui.editTitle), 0)
			if ui.editActivePane == "title" {
				g.SetCurrentView("edit_title")
			}
		} else {
			if ui.editActivePane == "title" {
				tv.Title = "Title (Tab: switch, Ctrl+S: save, Esc: cancel)"
			} else {
				tv.Title = "Title"
			}
		}

		// Description pane
		descriptionY := titleY + titleHeight + 1
		if dv, err := g.SetView("edit_description", editX, descriptionY, editX+editWidth, descriptionY+descriptionHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			dv.Title = "Description (Tab: switch, Ctrl+S: save, Esc: cancel)"
			dv.Editable = true
			dv.Editor = gocui.DefaultEditor
			dv.Wrap = true
			dv.Frame = true
			fmt.Fprint(dv, ui.editDescription)
			if ui.editActivePane == "description" {
				g.SetCurrentView("edit_description")
			}
		} else {
			if ui.editActivePane == "description" {
				dv.Title = "Description (Tab: switch, Ctrl+S: save, Esc: cancel)"
			} else {
				dv.Title = "Description"
			}
		}
	} else {
		g.DeleteView("edit_title")
		g.DeleteView("edit_description")
	}

	// Create pane (if enabled)
	if ui.showCreate {
		createWidth := maxX - 10
		titleHeight := 3
		descriptionHeight := 15
		totalHeight := titleHeight + descriptionHeight + 3
		createX := (maxX - createWidth) / 2
		createY := (maxY - totalHeight) / 2

		// Title pane
		titleY := createY
		if tv, err := g.SetView("create_title", createX, titleY, createX+createWidth, titleY+titleHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			tv.Title = "Title (Tab: switch, Ctrl+S: create, Esc: cancel)"
			tv.Editable = true
			tv.Editor = gocui.DefaultEditor
			tv.Frame = true
			fmt.Fprint(tv, ui.createTitle)
			tv.SetCursor(len(ui.createTitle), 0)
			if ui.createActivePane == "title" {
				g.SetCurrentView("create_title")
			}
		} else {
			if ui.createActivePane == "title" {
				tv.Title = "Title (Tab: switch, Ctrl+S: create, Esc: cancel)"
			} else {
				tv.Title = "Title"
			}
		}

		// Description pane
		descriptionY := titleY + titleHeight + 1
		if dv, err := g.SetView("create_description", createX, descriptionY, createX+createWidth, descriptionY+descriptionHeight); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			dv.Title = "Description (Tab: switch, Ctrl+S: create, Esc: cancel)"
			dv.Editable = true
			dv.Editor = gocui.DefaultEditor
			dv.Wrap = true
			dv.Frame = true
			fmt.Fprint(dv, ui.createDescription)
			if ui.createActivePane == "description" {
				g.SetCurrentView("create_description")
			}
		} else {
			if ui.createActivePane == "description" {
				dv.Title = "Description (Tab: switch, Ctrl+S: create, Esc: cancel)"
			} else {
				dv.Title = "Description"
			}
		}
	} else {
		g.DeleteView("create_title")
		g.DeleteView("create_description")
	}

	return nil
}

func (ui *UI) quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func (ui *UI) cursorDown(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp || ui.showSearch {
		return nil
	}
	if v != nil && len(ui.issues) > 0 {
		_, cy := v.Cursor()
		ox, oy := v.Origin()

		if cy+oy+1 < len(ui.issues) {
			ui.selectedIssue++
			if err := v.SetCursor(0, cy+1); err != nil {
				if err := v.SetOrigin(ox, oy+1); err != nil {
					return err
				}
			}
			if dv, err := g.View("details"); err == nil {
				dv.SetOrigin(0, 0)
			}
		}
	}
	return nil
}

func (ui *UI) cursorUp(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp || ui.showSearch {
		return nil
	}
	if v != nil && len(ui.issues) > 0 {
		cx, cy := v.Cursor()
		ox, oy := v.Origin()

		if cy > 0 {
			ui.selectedIssue--
			if err := v.SetCursor(cx, cy-1); err != nil {
				return err
			}
			if dv, err := g.View("details"); err == nil {
				dv.SetOrigin(0, 0)
			}
		} else if oy > 0 {
			ui.selectedIssue--
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
			if dv, err := g.View("details"); err == nil {
				dv.SetOrigin(0, 0)
			}
		}
	}
	return nil
}

func (ui *UI) refreshIssues(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
	if ui.client != nil {
		teamID := ""
		if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
			teamID = ui.teams[ui.currentTeam].ID
		}
		var fetchedIssues []api.Issue
		var err error
		if ui.currentCustomView != "" && teamID != "" {
			fetchedIssues, err = ui.client.GetCustomViewIssues(context.Background(), ui.currentCustomView, teamID)
		} else {
			fetchedIssues, err = ui.client.GetIssues(context.Background(), teamID, ui.showClosed())
		}
		if err == nil {
			ui.allIssues = fetchedIssues
			ui.closedLoaded = ui.currentCustomView != "" || ui.showClosed()
			ui.lastSynced = time.Now()
			ui.showToast("Issues reloaded successfully")
		} else {
			ui.allIssues = []api.Issue{{Title: fmt.Sprintf("Error loading issues: %v", err)}}
			ui.showToast(fmt.Sprintf("Failed to reload issues: %v", err))
		}
	}
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	ui.lastRenderedID = "" // Invalidate markdown cache
	return nil
}

func (ui *UI) selectIssue(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp || ui.showSearch {
		return nil
	}
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
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
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
		cv := g.CurrentView()
		if cv != nil {
			ui.lastActiveView = cv.Name()
		}
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
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) cancelComment(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		v.Clear()
		v.SetCursor(0, 0)
	}
	ui.showComment = false
	ui.commentContent = ""
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) toggleStatus(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		cv := g.CurrentView()
		if cv != nil {
			ui.lastActiveView = cv.Name()
		}
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
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) cancelStatus(g *gocui.Gui, v *gocui.View) error {
	ui.showStatus = false
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) togglePriority(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		cv := g.CurrentView()
		if cv != nil {
			ui.lastActiveView = cv.Name()
		}
		ui.showPriority = true
		issue := ui.issues[ui.selectedIssue]
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
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) cancelPriority(g *gocui.Gui, v *gocui.View) error {
	ui.showPriority = false
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) prevView(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
	view := ui.currentView - 1
	if view < 0 {
		view = len(ui.views) - 1
	}
	return ui.setStatusView(g, v, view)
}

func (ui *UI) nextView(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
	view := ui.currentView + 1
	if view >= len(ui.views) {
		view = 0
	}
	return ui.setStatusView(g, v, view)
}

// setStatusView switches the status filter, fetching closed issues the first time "All" needs them
func (ui *UI) setStatusView(g *gocui.Gui, v *gocui.View, view int) error {
	ui.currentView = view
	if ui.showClosed() && !ui.closedLoaded {
		return ui.refreshIssues(g, v)
	}
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	return nil
}

// showClosed reports whether the status filter includes completed and canceled issues
func (ui *UI) showClosed() bool {
	return ui.views[ui.currentView] == "All"
}

// scopeStatus is the status filter applied to the workspace and project counts
func (ui *UI) scopeStatus() string {
	if ui.showClosed() {
		return "All"
	}
	return "Open"
}

func (ui *UI) prevTeam(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
	if len(ui.teams) == 0 {
		return nil
	}
	team := ui.currentTeam - 1
	if team < 0 {
		team = len(ui.teams) - 1
	}
	return ui.setTeam(g, v, team)
}

func (ui *UI) nextTeam(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
	if len(ui.teams) == 0 {
		return nil
	}
	team := ui.currentTeam + 1
	if team >= len(ui.teams) {
		team = 0
	}
	return ui.setTeam(g, v, team)
}

func (ui *UI) setTeam(g *gocui.Gui, v *gocui.View, team int) error {
	ui.currentTeam = team
	ui.projectFilter = ""
	if view := ui.customView(ui.currentCustomView); view != nil && !ui.customViewVisible(*view) {
		ui.currentCustomView = ""
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

func (ui *UI) openInBrowser(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue < 0 || ui.selectedIssue >= len(ui.issues) {
		return nil
	}
	issue := ui.issues[ui.selectedIssue]
	if issue.URL == "" {
		ui.showToast("Issue has no URL")
		return nil
	}

	var cmd *exec.Cmd
	if _, err := exec.LookPath("xdg-open"); err == nil {
		cmd = exec.Command("xdg-open", issue.URL)
	} else if _, err := exec.LookPath("open"); err == nil {
		cmd = exec.Command("open", issue.URL)
	} else {
		ui.showToast("No browser opener found (xdg-open or open)")
		return nil
	}

	if err := cmd.Start(); err != nil {
		ui.showToast(fmt.Sprintf("Failed to open browser: %v", err))
		return nil
	}
	go cmd.Wait()

	ui.showToast(fmt.Sprintf("Opened %s in browser", issue.Identifier))
	return nil
}

func (ui *UI) copyDescription(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]
		if issue.Description == "" {
			ui.showToast("No description to copy")
			return nil
		}
		return ui.copyToClipboard(issue.Description, "Description")
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
		return "38;5;245"
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

func (ui *UI) renderMarkdown(text string) string {
	if text == "" || ui.mdRenderer == nil {
		return text
	}

	rendered, err := ui.mdRenderer.Render(text)
	if err != nil {
		return text
	}

	return strings.TrimSpace(rendered)
}

func (ui *UI) filterIssues() []api.Issue {
	var filtered []api.Issue
	currentViewName := ui.views[ui.currentView]

	for _, issue := range ui.allIssues {
		if ui.assignedToMe && issue.Assignee.ID != ui.viewerID {
			continue
		}
		if !stateMatches(issue, currentViewName) {
			continue
		}
		if ui.projectFilter != "" && issue.Project.ID != ui.projectFilter {
			continue
		}
		if ui.searchString != "" {
			searchLower := strings.ToLower(ui.searchString)
			titleMatch := strings.Contains(strings.ToLower(issue.Title), searchLower)
			identifierMatch := strings.Contains(strings.ToLower(issue.Identifier), searchLower)
			if !titleMatch && !identifierMatch {
				continue
			}
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

func (ui *UI) toggleAssignee(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		cv := g.CurrentView()
		if cv != nil {
			ui.lastActiveView = cv.Name()
		}
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
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) cancelAssignee(g *gocui.Gui, v *gocui.View) error {
	ui.showAssignee = false
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) toggleEdit(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		cv := g.CurrentView()
		if cv != nil {
			ui.lastActiveView = cv.Name()
		}
		issue := ui.issues[ui.selectedIssue]
		ui.editTitle = issue.Title
		ui.editDescription = issue.Description
		ui.editActivePane = "title"
		ui.showEdit = true
	}
	return nil
}

func (ui *UI) switchEditPane(g *gocui.Gui, v *gocui.View) error {
	if ui.editActivePane == "title" {
		ui.editActivePane = "description"
		g.SetCurrentView("edit_description")
	} else {
		ui.editActivePane = "title"
		g.SetCurrentView("edit_title")
	}
	return nil
}

func (ui *UI) submitEdit(g *gocui.Gui, v *gocui.View) error {
	if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
		issue := ui.issues[ui.selectedIssue]

		titleView, err := g.View("edit_title")
		if err != nil {
			return err
		}
		descriptionView, err := g.View("edit_description")
		if err != nil {
			return err
		}

		newTitle := strings.TrimSpace(titleView.Buffer())
		newDescription := strings.TrimSpace(descriptionView.Buffer())

		if newTitle == "" {
			ui.showToast("Title cannot be empty")
			return nil
		}

		if ui.client != nil {
			if err := ui.client.UpdateIssue(context.Background(), issue.ID, newTitle, newDescription); err != nil {
				ui.showToast(fmt.Sprintf("Failed to update issue: %v", err))
			} else {
				ui.issues[ui.selectedIssue].Title = newTitle
				ui.issues[ui.selectedIssue].Description = newDescription
				ui.lastRenderedID = ""
				ui.showToast(fmt.Sprintf("Updated %s", issue.Identifier))
			}
		}
	}

	ui.showEdit = false
	ui.editActivePane = "title"
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) cancelEdit(g *gocui.Gui, v *gocui.View) error {
	ui.showEdit = false
	ui.editActivePane = "title"
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) clickEditTitle(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ui.editActivePane = "title"
		g.SetCurrentView("edit_title")
	}
	return nil
}

func (ui *UI) releaseEditPane(g *gocui.Gui, v *gocui.View) error {
	x, y := v.Cursor()

	content := strings.TrimRight(v.Buffer(), "\n")
	lines := strings.Split(content, "\n")

	if y > len(lines)-1 {
		lastLine := len(lines) - 1
		lastLineLen := len(lines[lastLine])
		v.SetCursor(lastLineLen, lastLine)
	} else if x > len(lines[y]) {
		v.SetCursor(len(lines[y]), y)
	}

	return nil
}

func (ui *UI) clickEditDescription(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ui.editActivePane = "description"
		g.SetCurrentView("edit_description")

		content := strings.TrimRight(v.Buffer(), "\n")
		lines := strings.Split(content, "\n")
		if len(lines) > 0 {
			lastLine := len(lines) - 1
			lastLineLen := len(lines[lastLine])
			v.SetCursor(lastLineLen, lastLine)
		}
	}
	return nil
}

func (ui *UI) clickIssues(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.viewedImage != nil {
		return nil
	}
	if v != nil {
		g.SetCurrentView("issues")
		_, cy := v.Cursor()
		_, oy := v.Origin()
		selectedIndex := cy + oy
		if selectedIndex >= 0 && selectedIndex < len(ui.issues) {
			ui.selectedIssue = selectedIndex
		}
	}
	return nil
}

func (ui *UI) toggleCreate(g *gocui.Gui, v *gocui.View) error {
	cv := g.CurrentView()
	if cv != nil {
		ui.lastActiveView = cv.Name()
	}
	ui.createTitle = ""
	ui.createDescription = ""
	ui.createActivePane = "title"
	ui.showCreate = true
	return nil
}

func (ui *UI) switchCreatePane(g *gocui.Gui, v *gocui.View) error {
	if ui.createActivePane == "title" {
		ui.createActivePane = "description"
		g.SetCurrentView("create_description")
	} else {
		ui.createActivePane = "title"
		g.SetCurrentView("create_title")
	}
	return nil
}

func (ui *UI) submitCreate(g *gocui.Gui, v *gocui.View) error {
	titleView, err := g.View("create_title")
	if err != nil {
		return err
	}
	descriptionView, err := g.View("create_description")
	if err != nil {
		return err
	}

	newTitle := strings.TrimSpace(titleView.Buffer())
	newDescription := strings.TrimSpace(descriptionView.Buffer())

	if newTitle == "" {
		ui.showToast("Title cannot be empty")
		return nil
	}

	if ui.client != nil && len(ui.teams) > 0 {
		teamID := ""
		if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
			teamID = ui.teams[ui.currentTeam].ID
		}
		if teamID == "" && len(ui.teams) > 0 {
			teamID = ui.teams[0].ID
		}

		newIssue, err := ui.client.CreateIssue(context.Background(), teamID, newTitle, newDescription)
		if err != nil {
			ui.showToast(fmt.Sprintf("Failed to create issue: %v", err))
		} else {
			ui.showToast(fmt.Sprintf("Created %s", newIssue.Identifier))
			ui.refreshIssues(g, v)
		}
	}

	ui.showCreate = false
	ui.createActivePane = "title"
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) cancelCreate(g *gocui.Gui, v *gocui.View) error {
	ui.showCreate = false
	ui.createActivePane = "title"
	if ui.lastActiveView != "" {
		g.SetCurrentView(ui.lastActiveView)
	} else {
		g.SetCurrentView("issues")
	}
	return nil
}

func (ui *UI) clickCreateTitle(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ui.createActivePane = "title"
		g.SetCurrentView("create_title")

		content := strings.TrimRight(v.Buffer(), "\n")
		lineLen := len(content)
		if lineLen > 0 {
			v.SetCursor(lineLen, 0)
		}
	}
	return nil
}

func (ui *UI) clickCreateDescription(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ui.createActivePane = "description"
		g.SetCurrentView("create_description")

		content := strings.TrimRight(v.Buffer(), "\n")
		lines := strings.Split(content, "\n")
		if len(lines) > 0 {
			lastLine := len(lines) - 1
			lastLineLen := len(lines[lastLine])
			v.SetCursor(lastLineLen, lastLine)
		}
	}
	return nil
}

// startSpinner starts the spinner animation
func (ui *UI) startSpinner() {
	ui.loadingTimer = time.AfterFunc(100*time.Millisecond, func() {
		ui.spinnerFrame++
		ui.gui.Update(func(g *gocui.Gui) error {
			return nil
		})
		if ui.loading {
			ui.startSpinner()
		}
	})
}

// loadData loads teams and issues in the background
func (ui *UI) loadData() {
	var issues []api.Issue
	var teams []api.Team
	var viewerID string
	var viewerName string
	var customViews []api.CustomView
	var teamIssueCounts map[string]int
	var apiErr error
	var fetchedIssues []api.Issue

	if ui.client != nil {
		ctx := context.Background()
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			if viewer, err := ui.client.GetViewer(ctx); err == nil {
				viewerID = viewer.ID
				viewerName = viewer.Name
			}
		}()
		go func() {
			defer wg.Done()
			customViews, _ = ui.client.GetCustomViews(ctx)
		}()
		go func() {
			defer wg.Done()
			teamIssueCounts, _ = ui.client.GetTeamIssueCounts(ctx)
		}()

		if fetchedTeams, err := ui.client.GetTeams(ctx); err == nil {
			teams = fetchedTeams
		}
		teamID := ""
		if len(teams) > 0 {
			teamID = teams[0].ID
		}
		fetchedIssues, apiErr = ui.client.GetIssues(ctx, teamID, false)
		wg.Wait()
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

	// Update UI with loaded data
	ui.gui.Update(func(g *gocui.Gui) error {
		ui.issues = issues
		ui.allIssues = issues
		ui.teams = teams
		ui.viewerID = viewerID
		ui.viewerName = viewerName
		ui.customViews = customViews
		ui.teamIssueCounts = teamIssueCounts
		ui.availableStatuses = availableStatuses
		if apiErr == nil {
			ui.lastSynced = time.Now()
		}

		if ui.loadingTimer != nil {
			ui.loadingTimer.Stop()
		}
		ui.loading = false

		return nil
	})

	ui.gui.Update(func(g *gocui.Gui) error {
		return nil
	})
}

func (ui *UI) focusDetails(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp || ui.showSearch {
		return nil
	}
	g.SetCurrentView("details")
	g.Cursor = false
	return nil
}

func (ui *UI) focusIssues(g *gocui.Gui, v *gocui.View) error {
	g.SetCurrentView("issues")
	g.Cursor = false
	return nil
}

func (ui *UI) scrollDetailsDown(g *gocui.Gui, v *gocui.View) error {
	if ui.viewedImage != nil {
		return nil
	}
	if v != nil {
		_, viewHeight := v.Size()
		lines := len(v.ViewBufferLines())
		ox, oy := v.Origin()

		if oy+viewHeight < lines {
			if err := v.SetOrigin(ox, oy+1); err != nil {
				return nil
			}
		}
	}
	return nil
}

func (ui *UI) scrollDetailsUp(g *gocui.Gui, v *gocui.View) error {
	if ui.viewedImage != nil {
		return nil
	}
	if v != nil {
		ox, oy := v.Origin()
		if oy > 0 {
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ui *UI) scrollIssuesDown(g *gocui.Gui, v *gocui.View) error {
	if ui.viewedImage != nil {
		return nil
	}
	if v != nil && len(ui.issues) > 0 {
		ox, oy := v.Origin()
		_, viewHeight := v.Size()

		// Scroll the view down without changing selection
		if oy+viewHeight < len(ui.issues) {
			if err := v.SetOrigin(ox, oy+1); err != nil {
				return nil
			}
			// Adjust cursor position to keep pointing at the same selected issue
			// selectedIssue doesn't change, but cursor needs to move up by 1 to compensate
			// for the origin moving down by 1
			if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
				newCursorY := ui.selectedIssue - (oy + 1)
				if newCursorY >= 0 && newCursorY < viewHeight {
					v.SetCursor(0, newCursorY)
				}
			}
		}
	}
	return nil
}

func (ui *UI) scrollIssuesUp(g *gocui.Gui, v *gocui.View) error {
	if ui.viewedImage != nil {
		return nil
	}
	if v != nil && len(ui.issues) > 0 {
		ox, oy := v.Origin()
		_, viewHeight := v.Size()

		// Scroll the view up without changing selection
		if oy > 0 {
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
			// Adjust cursor position to keep pointing at the same selected issue
			// selectedIssue doesn't change, but cursor needs to move down by 1 to compensate
			// for the origin moving up by 1
			if ui.selectedIssue >= 0 && ui.selectedIssue < len(ui.issues) {
				newCursorY := ui.selectedIssue - (oy - 1)
				if newCursorY >= 0 && newCursorY < viewHeight {
					v.SetCursor(0, newCursorY)
				}
			}
		}
	}
	return nil
}

func (ui *UI) clickDetails(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.viewedImage != nil {
		return nil
	}
	if ui.openImageAt(mouseX(g, v), mouseY(g, v)) {
		return nil
	}
	return ui.focusDetails(g, v)
}

func (ui *UI) focusNav(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp || ui.showSearch {
		return nil
	}
	g.SetCurrentView("nav")
	g.Cursor = false
	return nil
}

func (ui *UI) nextPane(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		return nil
	}
	switch v.Name() {
	case "nav":
		return ui.focusIssues(g, v)
	case "issues":
		return ui.focusDetails(g, v)
	default:
		return ui.focusNav(g, v)
	}
}

// buildNavItems returns the rows shown in the navigation pane
func (ui *UI) buildNavItems() []navItem {
	items := []navItem{
		{kind: "header", label: "◇ WORKSPACE"},
		{kind: "workspace", index: 0, label: "My issues"},
		{kind: "workspace", index: 1, label: "All issues"},
		{kind: "header", label: "◌ STATUS"},
	}
	for i, name := range ui.views {
		items = append(items, navItem{kind: "view", index: i, label: name})
	}

	var customViews []navItem
	for _, view := range ui.customViews {
		if ui.customViewVisible(view) {
			customViews = append(customViews, navItem{kind: "customView", id: view.ID, label: view.Name, color: view.Color})
		}
	}
	if len(customViews) > 0 {
		items = append(items, navItem{kind: "header", label: "◫ VIEWS"})
		items = append(items, customViews...)
	}

	if len(ui.teams) > 0 {
		items = append(items, navItem{kind: "header", label: "▦ TEAMS"})
		for i, team := range ui.teams {
			items = append(items, navItem{kind: "team", index: i, label: team.Name})
		}
	}

	var projects []navItem
	seen := map[string]bool{}
	for _, issue := range ui.allIssues {
		if issue.Project.ID == "" || seen[issue.Project.ID] {
			continue
		}
		seen[issue.Project.ID] = true
		projects = append(projects, navItem{kind: "project", id: issue.Project.ID, label: issue.Project.Name, color: issue.Project.Color})
	}
	if len(projects) > 0 {
		sort.Slice(projects, func(i, j int) bool {
			return strings.ToLower(projects[i].label) < strings.ToLower(projects[j].label)
		})
		items = append(items, navItem{kind: "header", label: "◆ PROJECTS"})
		items = append(items, projects...)
	}
	return items
}

// renderNavItem formats a navigation row to fit within width columns
func (ui *UI) renderNavItem(item navItem, width int) string {
	if item.kind == "header" {
		return "\033[38;5;245m" + truncate(item.label, width) + "\033[0m"
	}

	active := false
	count := ""
	label := item.label
	prefix := ""
	switch item.kind {
	case "workspace":
		active = ui.assignedToMe == (item.index == 0) && ui.currentCustomView == "" && ui.projectFilter == ""
		count = fmt.Sprint(ui.countIssues(ui.scopeStatus(), item.index == 0, ""))
	case "view":
		active = item.index == ui.currentView
		if ui.views[item.index] != "All" || ui.closedLoaded {
			count = fmt.Sprint(ui.countIssues(ui.views[item.index], ui.assignedToMe, ui.projectFilter))
		}
	case "customView":
		active = item.id == ui.currentCustomView
	case "project":
		active = item.id == ui.projectFilter
		count = fmt.Sprint(ui.countIssues(ui.scopeStatus(), ui.assignedToMe, item.id))
	case "team":
		active = item.index == ui.currentTeam
		maxKeyLen := 0
		for _, team := range ui.teams {
			if len(team.Key) > maxKeyLen {
				maxKeyLen = len(team.Key)
			}
		}
		prefix = fmt.Sprintf("%-*s ", maxKeyLen, ui.teams[item.index].Key)
		if ui.teamIssueCounts != nil {
			count = fmt.Sprint(ui.teamIssueCounts[ui.teams[item.index].ID])
		}
	}

	marker := "  "
	if active {
		marker = "\033[36m›\033[0m "
	}

	labelWidth := width - 2 - len([]rune(prefix)) - len(count) - 1
	if labelWidth < 1 {
		labelWidth = 1
	}
	label = truncate(label, labelWidth)
	padding := strings.Repeat(" ", labelWidth-len([]rune(label)))
	if item.color != "" {
		style := hexTo256(item.color)
		if active {
			style += ";1"
		}
		label = "\033[" + style + "m" + label + "\033[0m"
	} else if active {
		label = "\033[1m" + label + "\033[0m"
	}
	if prefix != "" {
		prefix = "\033[36m" + prefix + "\033[0m"
	}
	return marker + prefix + label + padding + " \033[38;5;245m" + count + "\033[0m"
}

// countIssues counts loaded issues matching a status filter (see stateMatches) and project ("" matches any)
func (ui *UI) countIssues(stateName string, mineOnly bool, projectID string) int {
	count := 0
	for _, issue := range ui.allIssues {
		if mineOnly && issue.Assignee.ID != ui.viewerID {
			continue
		}
		if !stateMatches(issue, stateName) {
			continue
		}
		if projectID != "" && issue.Project.ID != projectID {
			continue
		}
		count++
	}
	return count
}

// stateMatches reports whether an issue passes a status filter: "All" matches every issue,
// "Open" anything not completed or canceled, and any other value the state with that name
func stateMatches(issue api.Issue, status string) bool {
	switch status {
	case "All":
		return true
	case "Open":
		return issue.State.Type != "completed" && issue.State.Type != "canceled"
	default:
		return issue.State.Name == status
	}
}

// customView returns the loaded saved view with the given ID, or nil
func (ui *UI) customView(id string) *api.CustomView {
	if id == "" {
		return nil
	}
	for i := range ui.customViews {
		if ui.customViews[i].ID == id {
			return &ui.customViews[i]
		}
	}
	return nil
}

// customViewVisible reports whether a saved view belongs to the workspace or the current team
func (ui *UI) customViewVisible(view api.CustomView) bool {
	if view.Team == nil {
		return true
	}
	return ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) && view.Team.ID == ui.teams[ui.currentTeam].ID
}

// projectName looks up a project's name from the loaded issues
func (ui *UI) projectName(id string) string {
	for _, issue := range ui.allIssues {
		if issue.Project.ID == id {
			return issue.Project.Name
		}
	}
	return ""
}

// nextNavItem returns the index of the next selectable nav item from start in direction dir
func (ui *UI) nextNavItem(start int, dir int) int {
	for i := start + dir; i >= 0 && i < len(ui.navItems); i += dir {
		if ui.navItems[i].kind != "header" {
			return i
		}
	}
	if start < 0 {
		return 0
	}
	return start
}

func (ui *UI) navDown(g *gocui.Gui, v *gocui.View) error {
	ui.navCursor = ui.nextNavItem(ui.navCursor, 1)
	return nil
}

func (ui *UI) navUp(g *gocui.Gui, v *gocui.View) error {
	ui.navCursor = ui.nextNavItem(ui.navCursor, -1)
	return nil
}

func (ui *UI) navSelect(g *gocui.Gui, v *gocui.View) error {
	if ui.navCursor < 0 || ui.navCursor >= len(ui.navItems) {
		return nil
	}
	item := ui.navItems[ui.navCursor]
	switch item.kind {
	case "workspace":
		ui.assignedToMe = item.index == 0
		ui.projectFilter = ""
		if ui.currentCustomView != "" {
			ui.currentCustomView = ""
			return ui.refreshIssues(g, v)
		}
	case "view":
		return ui.setStatusView(g, v, item.index)
	case "customView":
		if item.id != ui.currentCustomView {
			ui.currentCustomView = item.id
			ui.currentView = 0
			ui.projectFilter = ""
			return ui.refreshIssues(g, v)
		}
		return nil
	case "project":
		ui.projectFilter = item.id
	case "team":
		if item.index != ui.currentTeam {
			return ui.setTeam(g, v, item.index)
		}
		return nil
	default:
		return nil
	}
	ui.issues = ui.filterIssues()
	ui.selectedIssue = -1
	return nil
}

func (ui *UI) clickNav(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.viewedImage != nil {
		return nil
	}
	g.SetCurrentView("nav")
	_, cy := v.Cursor()
	_, oy := v.Origin()
	index := cy + oy
	if index < 0 || index >= len(ui.navItems) || ui.navItems[index].kind == "header" {
		return nil
	}
	ui.navCursor = index
	return ui.navSelect(g, v)
}

// setMarkdownWidth rebuilds the markdown renderer when the inspector width changes
func (ui *UI) setMarkdownWidth(width int) {
	if ui.mdRenderer != nil && ui.mdWidth == width {
		return
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(ui.mdStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return
	}
	ui.mdRenderer = renderer
	ui.mdWidth = width
	ui.lastRenderedID = ""
}

// breadcrumb describes the current team and filters for the header
func (ui *UI) breadcrumb() string {
	parts := []string{"All teams"}
	if ui.currentTeam >= 0 && ui.currentTeam < len(ui.teams) {
		parts[0] = ui.teams[ui.currentTeam].Name
	}
	if view := ui.customView(ui.currentCustomView); view != nil {
		parts = append(parts, view.Name)
		if ui.assignedToMe {
			parts = append(parts, "Mine")
		}
	} else if ui.assignedToMe {
		parts = append(parts, "My issues")
	} else {
		parts = append(parts, "All issues")
	}
	if ui.projectFilter != "" {
		parts = append(parts, ui.projectName(ui.projectFilter))
	}
	if ui.views[ui.currentView] != "Open" {
		parts = append(parts, ui.views[ui.currentView])
	}
	if ui.searchString != "" {
		parts = append(parts, "\""+ui.searchString+"\"")
	}
	return strings.Join(parts, " / ")
}

// alignLine places left and right on one line of the given width
func alignLine(left, right string, width int) string {
	gap := width - visibleLen(left) - visibleLen(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func visibleLen(s string) int {
	return utf8.RuneCountInString(ansiPattern.ReplaceAllString(s, ""))
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func (ui *UI) reload(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee {
		return nil
	}
	if ui.client != nil {
		ctx := context.Background()
		if views, err := ui.client.GetCustomViews(ctx); err == nil {
			ui.customViews = views
		}
		if counts, err := ui.client.GetTeamIssueCounts(ctx); err == nil {
			ui.teamIssueCounts = counts
		}
	}
	return ui.refreshIssues(g, v)
}

// hexTo256 converts a hex color to the nearest 256-color foreground code
func hexTo256(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "39"
	}

	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)

	toCube := func(c int) int {
		if c < 48 {
			return 0
		}
		if c < 115 {
			return 1
		}
		return (c - 35) / 40
	}
	return fmt.Sprintf("38;5;%d", 16+36*toCube(r)+6*toCube(g)+toCube(b))
}

func (ui *UI) startResize(g *gocui.Gui, v *gocui.View) error {
	if ui.showEdit || ui.showCreate || ui.showComment || ui.showStatus || ui.showPriority || ui.showAssignee || ui.showHelp || ui.viewedImage != nil {
		return nil
	}
	// The gutter covers two border columns; remember which one was grabbed
	ui.resizing = v.Name()
	ui.resizeOffset, _ = v.Cursor()
	return nil
}

func (ui *UI) dragResize(g *gocui.Gui, v *gocui.View) error {
	if ui.resizing == "" || v == nil {
		return nil
	}
	maxX, _ := g.Size()
	x := mouseX(g, v) - ui.resizeOffset
	switch ui.resizing {
	case "gutter_nav":
		ui.navWidth = x + 1
	case "gutter_details":
		ui.detailsWidth = maxX - x - 1
	}
	return nil
}

func (ui *UI) endResize(g *gocui.Gui, v *gocui.View) error {
	ui.resizing = ""
	return nil
}

// mouseX returns the screen column of the mouse event delivered to v
func mouseX(g *gocui.Gui, v *gocui.View) int {
	x0, _, _, _, err := g.ViewPosition(v.Name())
	if err != nil {
		return 0
	}
	cx, _ := v.Cursor()
	return x0 + 1 + cx
}

// mouseY returns the screen row of the last mouse event on v
func mouseY(g *gocui.Gui, v *gocui.View) int {
	_, y0, _, _, err := g.ViewPosition(v.Name())
	if err != nil {
		return 0
	}
	_, cy := v.Cursor()
	return y0 + 1 + cy
}
