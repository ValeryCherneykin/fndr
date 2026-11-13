# fndr shell integration for fish. Load it in ~/.config/fish/config.fish
# with:
#
#   fndr init fish | source
#
# It defines a function named fndr that runs the real program and then cds
# to the folder picked with Ctrl+E. Esc and Ctrl+C leave you where you were.

function fndr --description 'fndr, then cd to the folder picked with Ctrl+E'
    set -l tmp (mktemp)
    # command: the real program; a bare "fndr" here would call this
    # function again, forever.
    command fndr --cwd-file $tmp $argv
    set -l code $status
    # string collect keeps the path whole, spaces and all; fish never
    # splits a variable into words.
    set -l dir (string collect < $tmp)
    if test -n "$dir"
        # builtin: fish's own cd, even if a plugin (zoxide) has wrapped it.
        builtin cd -- $dir
    end
    rm -f -- $tmp
    return $code
end
