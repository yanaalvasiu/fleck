# Fleck

Fleck is an open-source, no-telemetry utility CLI for macOS and Windows with four cautious modules: Clean, Uninstall, Duplicates, and Ports & Procs. It uses a shared allow-list safety policy and recoverable Trash moves where the platform provides them.

Running `fleck` without a command opens the keyboard-driven home screen when standard output is a terminal. Use `↑`/`↓` or `j`/`k` to move, Enter to open a module, and `q` to quit. The home screen keeps the selected state on the arrow, adapts to narrow terminals by hiding secondary descriptions, and falls back to command help when output is redirected or not a TTY.

## Install

```sh
brew install yanaalvasiu/tap/fleck
```

Source repository: https://github.com/yanaalvasiu/fleck

### macOS

Homebrew installs the native Apple Silicon or Intel build on macOS 12 and later. Release archives are also available for `darwin/arm64` and `darwin/amd64`.

### Windows PowerShell

Download the `windows_amd64` or `windows_arm64` ZIP from the [latest release](https://github.com/yanaalvasiu/fleck/releases/latest), extract `fleck.exe`, and add its directory to `PATH`:

```powershell
$env:Path += ";$HOME\\bin"
fleck version
```

For a Go-based install:

```powershell
go install github.com/yanaalvasiu/fleck/cmd/bin@latest
```

### Windows CMD

Extract the matching release ZIP, then add the directory containing `fleck.exe` to `PATH` for the current CMD session:

```bat
set PATH=%PATH%;%USERPROFILE%\\bin
fleck version
```

The same release binaries work from PowerShell and CMD. Fleck does not require administrator access for its approved user-level Windows paths.

## Safety model

Every candidate is previewed before action. `--dry-run` prints the plan and exits; non-interactive use requires `--yes`. On macOS, items go to `~/.Trash`; on Windows, Fleck uses its recoverable `%LOCALAPPDATA%\\Fleck\\Trash` area. Fleck never directly permanently deletes planned items. Paths are resolved immediately before acting, must be below configured roots, and are refused if protected, whitelisted, symlink-escaped, root, or the home directory itself. Ports and process actions refuse PID 1, Fleck itself, its parent shell, launchd, kernel_task, WindowServer, and other users' processes.

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

Platform limitations: Clean and Uninstall currently use Fleck's macOS cache, application, launch-agent, Homebrew, and Finder-Trash integrations; those integrations are not available on Windows. Ports and process inspection on Windows uses `netstat` and `tasklist`, so command lines, CPU, memory, and user details may be less complete than on macOS. `ports free` and process termination use Windows process tools. APFS clone mode, `launchctl` agent unloading, Homebrew cask handling, and Finder-compatible Trash are macOS-only. Windows builds support the shared safety checks, duplicate discovery, process/port inspection, history, version, and help commands.

`fleck uninstall` opens a raw-terminal multi-select app picker on a TTY. It can also discover installed Homebrew casks, unload matching per-user launch agents with `launchctl bootout`, and move application leftovers to Finder-compatible Trash. `fleck duplicates --similar` reports perceptually similar JPEG/PNG/GIF groups for manual review; similar images are never auto-selected. `--clone` replaces selected duplicate files with APFS clones and never calls a delete operation.

## Development

```sh
go test -race ./...
go vet ./...
```

The CI workflow runs tests, lint, dependency-license checks, and Windows amd64/arm64 cross-builds on every push and pull request. GoReleaser publishes `darwin` amd64/arm64 archives as `tar.gz` and Windows amd64/arm64 archives as `zip` files.
