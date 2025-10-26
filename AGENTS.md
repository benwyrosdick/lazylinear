# Agent Guidelines for lazylinear

## Build/Test Commands
- **Build**: `go build -o lazylinear`
- **Run**: `./lazylinear` or `go run main.go`
- **Test**: `go test ./...`
- **Test single package**: `go test ./internal/api` (or ./internal/config, ./internal/ui)
- **Lint**: `go vet ./...` and `gofmt -s -w .`

## Code Style
- **Imports**: Group stdlib, then third-party, then local packages (separated by blank lines)
- **Formatting**: Use `gofmt` - tabs for indentation
- **Types**: Explicit struct types with json tags; use pointers for receivers and large structs
- **Naming**: PascalCase for exported, camelCase for unexported; descriptive names (e.g. `GetIssues`, `NewClient`)
- **Error handling**: Return errors, don't panic; use `fmt.Errorf` for context; log.Fatal only in main
- **Comments**: Only add godoc comments for exported types/functions
- **Context**: Use `context.Context` for API calls and cancellation
- **GraphQL**: Use inline query strings with `graphql.NewRequest`
- **UI**: Use gocui library conventions; modals render last in layout function
