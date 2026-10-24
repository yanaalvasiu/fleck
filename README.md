# Fleck

Fleck is an open-source, no-telemetry macOS utility CLI with four cautious modules: Clean, Uninstall, Duplicates, and Ports & Procs. It uses a shared allow-list safety policy and Finder-compatible Trash moves.

## Install

```sh
brew install yanaalvasiu/tap/fleck
```

Source repository: https://github.com/yanaalvasiu/fleck

## Safety model

Every candidate is previewed before action. `--dry-run` prints the plan and exits; non-interactive use requires `--yes`. Items go to `~/.Trash`, never directly to permanent deletion. Paths are resolved immediately before acting, must be below configured roots, and are refused if protected, whitelisted, symlink-escaped, root, or the home directory itself. Ports and process actions refuse PID 1, Fleck itself, its parent shell, launchd, kernel_task, WindowServer, and other users' processes.

If Clean or Uninstall finds a selected item that is not user-writable, it prints the affected count and asks whether to continue with `sudo` or skip those items. Elevation is requested only for those specific items; dry-run and JSON modes never prompt.

All tests use temporary directories and spawned fixtures. No telemetry is collected. The only intended network request is the GitHub Releases latest endpoint, cached for 24 hours and skipped in CI, JSON mode, non-TTY mode, or when disabled.

## Commands

```text
fleck clean [--dry-run] [--yes] [--json]
fleck uninstall [--orphans] [--dry-run] [--yes] [--json]
fleck duplicates [PATH] [--trash|--clone|--similar] [--threshold=N] [--dry-run] [--yes] [--json]
fleck ports [PORT] [--udp]
fleck ports free PORT [--group] [--yes]
fleck procs [SEARCH] [--watch]
fleck procs kill PID [--yes]
fleck history [--json]
fleck update
fleck version
fleck doctor [--json]
```

The implementation uses the Go standard library plus `x/term` and the pure-Go `x/image/webp` decoder to minimize attack surface and avoid cgo. The image similarity matcher supports JPEG, PNG, GIF, and WebP; HEIC is reported as unsupported rather than converted implicitly. APFS clone mode delegates to macOS `/bin/cp -c`, which requests the native `clonefile` operation. Homebrew cask removal intentionally remains a package-manager operation after explicit confirmation; it is not silently invoked.

`fleck uninstall` opens a raw-terminal multi-select app picker on a TTY. It can also discover installed Homebrew casks, unload matching per-user launch agents with `launchctl bootout`, and move application leftovers to Finder-compatible Trash. `fleck duplicates --similar` reports perceptually similar JPEG/PNG/GIF groups for manual review; similar images are never auto-selected. `--clone` replaces selected duplicate files with APFS clones and never calls a delete operation.

## Development

```sh
go test -race ./...
go vet ./...
```

The CI workflow runs tests, lint, and a dependency-license gate on every pull request. Releases target macOS 12+ on amd64 and arm64.
