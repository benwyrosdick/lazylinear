# LazyLinear

A terminal UI for managing Linear issues, inspired by lazygit.

## Installation

### Prerequisites

- Go 1.21 or higher
- A Linear account and API key
- A clipboard utility (for copying URLs and branch names):
  - Linux: `xclip`, `xsel`, or `wl-copy` (Wayland)
  - macOS: `pbcopy` (pre-installed)

### Building from Source

```bash
git clone <repository-url>
cd lazylinear
go build -o lazylinear
```

Optionally, install directly from GitHub:

```bash
go install github.com/benwyrosdick/lazylinear@latest
```

## Configuration

Create a configuration file at `~/.lazylinear/config.json`:

```json
{
  "api_key": "your_linear_api_key_here"
}
```

To get your Linear API key:
1. Go to [Linear Settings](https://linear.app/settings/api)
2. Create a new Personal API Key
3. Copy the key and paste it into your config file

## Usage

Start the application:

```bash
./lazylinear
```

Or if installed to your PATH:

```bash
lazylinear
```

### Navigation

- `j` / `↓` - Move down
- `k` / `↑` - Move up
- `[` / `]` - Switch view (All/In Review/In Progress/Blocked/Todo/Backlog)
- `{` / `}` - Switch team
- `Enter` - Select issue to view details

### Actions

- `r` - Refresh issues
- `m` - Toggle filter by issues assigned to me
- `/` - Search issues (Enter to apply, Esc to cancel)
- `n` - Create new issue
- `c` - Add comment to selected issue
- `e` - Edit issue title and description
- `s` - Change status of selected issue
- `p` - Change priority of selected issue
- `a` - Assign user to selected issue
- `,` - Copy issue URL to clipboard
- `.` - Copy git branch name to clipboard
- `?` - Toggle help menu
- `Ctrl+C` - Quit

### Editing

When editing or creating issues:

- `Tab` - Switch between title and description fields
- `Ctrl+S` - Save changes
- `Esc` - Cancel

## Features

- **Multi-team support** - Switch between different Linear teams
- **View filtering** - Filter issues by status (All, In Review, In Progress, etc.)
- **Personal filter** - View only issues assigned to you
- **Search** - Search issues by title
- **Full CRUD operations** - Create, read, update issues and add comments
- **Status management** - Change issue status with visual state indicators
- **Priority management** - Set issue priority levels
- **Assignee management** - Assign team members to issues
- **Clipboard integration** - Copy issue URLs and git branch names
- **Visual indicators** - Color-coded states and priority icons
- **Issue details** - View full issue description and comments

## Keyboard Shortcuts Reference

Press `?` at any time to view the in-app help menu with all available keyboard shortcuts.
