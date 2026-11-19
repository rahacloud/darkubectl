# TUI visual style

Interactive TUI views follow the dark Hamravesh console palette: deep violet
accents, muted plum borders, soft lavender labels, and restrained semantic
colors. The terminal owns the background; the TUI does not paint a full-screen
surface.

| Role | Color | Use |
| --- | --- | --- |
| Accent | `#B9A3FF` | Section titles and primary prompts |
| Branch | `#A78BFA` | Focused pane borders, shortcut keys, active tabs, links |
| Key | `#C4B5FD` | Field labels and section headings |
| Value | `#E6E4EF` | Primary data values, IDs, addresses, and resource amounts |
| Border | `#3B354E` | Unfocused pane and divider borders |
| Muted | `#B3AEC2` | Field labels, hints, and secondary text |
| Success | `#4ADE80` | Healthy state and positive results |
| Danger | `#FB7185` | Errors and destructive actions |
| Number | `#FBBF24` | Warnings and numeric emphasis |
| Selection | `#37216A` / `#F5F3FF` | Selected row background / foreground |
| Search match | `#7758D8` / `#FFFFFF` | Match background / foreground |
| Current match | `#A78BFA` / `#171322` | Current match background / foreground |

The shared tokens live in `internal/tui/theme`; table and non-interactive CLI
output keep their existing palette. Bright value text and lighter labels improve
readability on dark terminal backgrounds while purple remains the accent. `NO_COLOR`
disables color while preserving bold or reverse-video selection cues where the
terminal supports them.

## Detail panes

App detail views use aligned label/value rows for metadata, state, and empty or
loading states. View controls belong in the footer: global actions stay on the
left, the selected view's compact shortcuts align to the right, and the hints
change when the selected view or an edit state changes. Logs keep their
timestamped entries, while pod, container, and log settings use label/value
metadata rows without inline key prompts.
