# fndr shell integration for zsh. Load it in ~/.zshrc with:
#
#   eval "$(fndr init zsh)"
#
# It defines a function named fndr that runs the real program and then cds
# to the folder picked with Ctrl+E. Esc and Ctrl+C leave you where you were.

fndr() {
  local tmp="$(mktemp)"
  # command: the real program; a bare "fndr" here would call this function
  # again, forever.
  command fndr --cwd-file "$tmp" "$@"
  local code=$?
  # $(<file) reads the file without running cat, which may be aliased.
  local dir="$(<"$tmp")"
  if [[ -n "$dir" ]]; then
    # builtin: zsh's own cd, even if a plugin (zoxide) has wrapped it.
    builtin cd -- "$dir"
  fi
  rm -f -- "$tmp"
  return $code
}
