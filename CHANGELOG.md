# Changelog

## 0.1.1

- A vault switcher at the foot of the sidebar: switch this window to another vault, or open it in a window of its own so several vaults are open side by side. Open Folder, New Vault, Connect and the picker are in the same menu.
- Search (⌘K) takes several words, marks them in the results, cuts the line around the match, and opens the page scrolled to the line that matched.
- Selected paragraphs have a bar with Copy, Copy as image (⌘⇧C) and Delete; Copy as image is also in the paragraph menu. The picture goes to the system clipboard.
- The side rail no longer sits under the macOS window buttons when the sidebar is folded.
- Updates can come through the tvv.tw mirror (Settings › About › Update source): Automatic uses it when the system language is Chinese, and a failed or altered download falls back to GitHub.
- Notes now updates itself: it checks for a signed release once a day, shows the release notes, and installs the update when you say so. Check by hand from the app menu (Check for Updates…) or Settings › About.

## 0.1.0

- First release: a vault is a folder with one JSON file per page, kept on this computer or synced with a WebDAV folder or an SMB share. Windows and macOS windows draw their own title bar; there is a settings window, a vault picker, and a single running instance per user.
