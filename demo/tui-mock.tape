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
Type "go build -o /tmp/darkubectl-mock . && /tmp/darkubectl-mock tui --mock-file demo/mock-profile.yaml" Enter
Sleep 3s
Show

# Show available app actions, then inspect every detail view for the API app.
Type "a"
Sleep 1s
Escape
Type "4"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "m"
Sleep 1s
Type "m"

# Select the Git backed worker, show its actions and step through each view.
Type "3"
Down
Sleep 1s
Type "a"
Sleep 1s
Escape
Type "4"
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s
Type "]"
Sleep 1s

# Keep the populated hamravesh tenant selected for Activity, DB and Registry.
Type "1"
Up
Sleep 500ms
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
Sleep 1s
Enter
Sleep 1s
Down
Enter
Sleep 1s
Sleep 2s
