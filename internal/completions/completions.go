// SPDX-License-Identifier: GPL-3.0-or-later
package completions

const Zsh = `#compdef fleck
_arguments '1:command:(clean uninstall duplicates ports procs history update version doctor completion)' '*:flags:(--json --yes --dry-run --debug --no-color --udp --watch --orphans --clone --similar --threshold= --group)'
`

const Bash = `_fleck_complete() {
  local commands="clean uninstall duplicates ports procs history update version doctor completion"
  COMPREPLY=( $(compgen -W "$commands --json --yes --dry-run --debug --no-color --udp --watch --orphans --clone --similar --group" -- "${COMP_WORDS[COMP_CWORD]}") )
}
complete -F _fleck_complete fleck
`

const Fish = `complete -c fleck -f -n '__fish_use_subcommand' -a 'clean uninstall duplicates ports procs history update version doctor completion'
complete -c fleck -l json -l yes -l dry-run -l debug -l no-color -l udp -l watch -l orphans -l clone -l similar -l group
`

func ForShell(shell string) (string, bool) {
	switch shell {
	case "zsh":
		return Zsh, true
	case "bash":
		return Bash, true
	case "fish":
		return Fish, true
	default:
		return "", false
	}
}
