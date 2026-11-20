# Offline recording of darkubectl's TUI mock profile.
# Run from the repository root with VHS installed:
#   vhs demo/tui-mock.tape

Output assets/tui-mock.gif

Set Shell bash
Set FontSize 16
Set Width 1440
Set Height 900
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set PlaybackSpeed 1
Set Margin 16
Set MarginFill "#161B22"
Set BorderRadius 10
Env TERM "xterm-256color"
Env COLORTERM "truecolor"

Hide
Type "unset NO_COLOR; export TERM=xterm-256color COLORTERM=truecolor; clear" Enter
Type "/tmp/darkubectl-mock tui --mock-file demo/mock-profile.yaml" Enter
Sleep 1s
Show

# Start in the tenant app list, then inspect usage and logs.
Type "4"
Sleep 800ms
Type "]"
Sleep 1s
Type "]"
Type "m"
Sleep 1s
Type "m"

# Select the Git backed worker and step through its detail views.
Type "3"
Down
Sleep 800ms
Type "4"
Type "]"
Sleep 800ms
Type "]"
Sleep 800ms
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s

# Switch to the second tenant and inspect its notebook app.
Type "1"
Down
Sleep 1s
Type "3"
Type "4"
Type "]"
Sleep 1s
Type "]"
Sleep 1s

# Visit Activity alerts and notifications, Tickets, DB, and Registry.
Type "2"
Down
Enter
Sleep 1s
Enter
Sleep 1s
Escape
Right
Sleep 1s
Type "2"
Down
Enter
Sleep 1s
Enter
Sleep 1s
Escape
Type "2"
Down
Enter
Sleep 1s
Enter
Sleep 1s
Escape
Type "2"
Down
Enter
Sleep 500ms
Down
Enter
Sleep 1s
Escape
Ctrl+C
Sleep 1s
