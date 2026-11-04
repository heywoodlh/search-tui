# Website demo, rendered by VHS (https://github.com/charmbracelet/vhs).
# Run through demo/record.sh, which sets up an isolated config and the
# output directory ($DEMO_OUT).

Output "out/demo.webm"
Output "out/demo.mp4"

Require search

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 15
Set LineHeight 1.25
Set Width 1200
Set Height 720
Set Padding 24
Set BorderRadius 10
Set TypingSpeed 45ms
Set WaitTimeout 30s
Set CursorBlink false
# Matches the website's dark palette.
Set Theme { "name": "search", "background": "#1c1917", "foreground": "#e7e5e4", "cursor": "#fb923c", "selection": "#44403c", "black": "#292524", "brightBlack": "#78716c", "red": "#f87171", "brightRed": "#fca5a5", "green": "#a3e635", "brightGreen": "#bef264", "yellow": "#fbbf24", "brightYellow": "#fde68a", "blue": "#60a5fa", "brightBlue": "#93c5fd", "magenta": "#f472b6", "brightMagenta": "#f9a8d4", "cyan": "#22d3ee", "brightCyan": "#67e8f9", "white": "#d6d3d1", "brightWhite": "#fafaf9" }

Hide
Type "export PS1='\[\e[38;2;234;88;12m\]❯\[\e[0m\] '; clear"
Enter
Sleep 300ms
Show

# 1. Inline search with a !bang and the on-device summary.
Type "search -n 2 --summary '!w nix package manager'"
Sleep 400ms
Enter
Wait+Line /❯\s*$/
Sleep 3s
Hide
Type "clear"
Enter
Show

# 2. The TUI.
Type "search"
Enter
Wait+Screen /type a query/
Sleep 800ms
Type "rust borrow checker !so"
Sleep 400ms
Enter
Wait+Screen /stackoverflow.*1\/[0-9]+/
Sleep 1.5s
Type "j"
Sleep 600ms
Type "j"
Sleep 600ms
Screenshot "out/demo.png"
Enter
Sleep 2s
Escape
Sleep 700ms

# 2b. Split like vim and run the same query on another engine alongside.
Ctrl+W
Type "v"
Sleep 900ms
Tab
Sleep 700ms
Type "h"
Sleep 700ms
Enter
Wait+Screen /hackernews ›/
Sleep 3.5s
Ctrl+W
Type "o"
Sleep 800ms

# 3. Help, then quit.
Type "?"
Sleep 2.5s
Type "?"
Sleep 400ms
Type "q"
Sleep 600ms

# 4. Themes.
Type "search --list-themes"
Sleep 300ms
Enter
Sleep 3.5s
