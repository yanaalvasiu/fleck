# Contributing

Run `go test -race ./...` and `go vet ./...` before opening a pull request. Changes must preserve the shared safety policy: modules may not call `os.Remove` or `os.RemoveAll`; all moves go through the safety validator and Trash function. Add SPDX GPL-3.0-or-later headers to source files and use temporary HOME fixtures for destructive-flow tests.
