# darkubectl agent setup — interactive client configuration wizard
#
#   brew install vhs
#   vhs demo/agent-setup.tape
#
# Uses a throwaway project under /tmp so the recording does not change the
# repository or any real agent configuration.

Output assets/agent-setup.gif

Require darkubectl

Set Shell zsh
Set FontSize 19
Set Width 1200
Set Height 720
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set PlaybackSpeed 1
Set Margin 16
Set MarginFill "#161B22"
Set BorderRadius 10

Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env NO_COLOR "false"
Env CLICOLOR "1"
Env CLICOLOR_FORCE "1"

Hide
Type "DEMO_DIR=$(mktemp -d /tmp/darkubectl-agent-demo.XXXXXX)" Enter
Type "cd $DEMO_DIR && git init -q && clear" Enter
Show

Type "darkubectl agent setup"
Sleep 500ms
Enter
Sleep 800ms

Space
Enter
Sleep 400ms
Enter
Sleep 400ms

Down
Space
Down
Space
Enter
Sleep 500ms
Enter
Sleep 800ms

Enter
Sleep 2s

Hide
Type "rm -rf $DEMO_DIR" Enter
