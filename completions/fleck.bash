# SPDX-License-Identifier: GPL-3.0-or-later
_fleck_complete() {
  local commands="clean uninstall duplicates ports procs history update version doctor completion"
  COMPREPLY=( $(compgen -W "$commands --json --yes --dry-run --debug --no-color --udp --watch --orphans --clone --similar --group" -- "${COMP_WORDS[COMP_CWORD]}") )
}
complete -F _fleck_complete fleck
