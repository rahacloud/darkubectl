# darkubectl demo — regenerates assets/demo.gif
#
#   brew install vhs
#   vhs demo/demo.tape
#
# Runs the real CLI against whatever tenant is configured in ~/.darkube/config.yaml,
# so review the result before committing it: app names, namespaces and domains from
# your account end up in the GIF.

Output assets/demo.gif

Require darkubectl

Set Shell zsh
Set FontSize 20
Set Width 1200
Set Height 640
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms
Set PlaybackSpeed 1
Set Margin 16
Set MarginFill "#161B22"
Set BorderRadius 10

Hide
Type "clear" Enter
Show

Type "darkubectl get apps"
Sleep 500ms
Enter
Sleep 4s

Type "darkubectl describe app api"
Sleep 500ms
Enter
Sleep 4s

Type "clear" Enter
Sleep 300ms

Type "darkubectl get orphans"
Sleep 500ms
Enter
Sleep 4s

Type "darkubectl logs app api --tail 12"
Sleep 500ms
Enter
Sleep 4s

Type "clear" Enter
Sleep 300ms

Type "darkubectl terminal app api"
Sleep 500ms
Enter
Sleep 2500ms
Type "hostname && ls /app"
Sleep 300ms
Enter
Sleep 2s
Type "exit"
Enter
Sleep 2s
