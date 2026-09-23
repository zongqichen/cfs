# Render from the repository root with VHS 0.11.0.
Output "docs/assets/cfs-demo.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 16
Set Width 1000
Set Height 620
Set Padding 24
Set Framerate 20
Set TypingSpeed 25ms

Hide
Type "source docs/demo/prepare.sh" Enter
Type "clear" Enter
Show

Type "cd ~/work/orders" Enter
Sleep 500ms
Type "cf login --sso -a https://api.eu.example -o commerce -s development" Enter
Sleep 1500ms

Type "cd ~/work/payments" Enter
Sleep 500ms
Type "cf login --sso -a https://api.us.example -o finance -s production" Enter
Sleep 1500ms

Type "cd ~/work/orders" Enter
Type "cf target" Enter
Sleep 1500ms

Type "cd ~/work/payments" Enter
Type "cf target" Enter
Sleep 1500ms

Type "cfs status --redact" Enter
Sleep 2500ms
