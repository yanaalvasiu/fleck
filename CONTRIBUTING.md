# Contributing

Run `go test -race ./...`, `go vet ./...`, and `git diff --check` before opening a pull request. Changes must preserve the shared safety policy: modules may not call `os.Remove` or `os.RemoveAll`; all moves go through the safety validator and Trash function. Add SPDX GPL-3.0-or-later headers to source files and use temporary HOME fixtures for destructive-flow tests. Home-screen changes should cover both the rendered TTY layout and redirected/non-TTY behavior; keep narrow-terminal output within the available width.
