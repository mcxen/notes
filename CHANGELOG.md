# Changelog

## 0.1.2

- Copy as image: a photo among the words came out as a blank box. The pictures are now painted on the finished image directly, with their rounded corners, instead of going through the page renderer, which could not draw a large photo in a macOS web view.
- Page outline, as in Notion: a column of short lines at a page's right edge, one for each heading (longer for a heading 1), the heading the page is at darker. Point at the lines to see the headings by name, press one to go there. It appears on pages with two or more headings that are wide enough for it, not beside the history panel or the side peek, and can be turned off in Settings › Appearance › Show the page outline.
- Dark mode: choosing Dark (or Light) in a vault now sets the whole app's appearance, so the translucent sidebar, the window buttons and the Settings and picker windows go dark with the page. Before, a vault kept in Dark under a light system showed a dark page beside a light sidebar whose light text could not be read.
- "Toggle Dark Appearance" now toggles what is shown. With the theme left to the system, the first press chose the dark the system already showed, and the second turned the page light under a still-dark sidebar with black text; the sidebar's quiet text and icons are also brighter in dark when the sidebar is translucent.
- Counts of paragraphs, backlinks and images, and the "matches" of search, were not translated.
- Page history is kept within its limits (the newest 20 versions of a page, none older than 30 days, at most 10 MB), as the app always meant it to: until now nothing ever removed a version, and every version is a whole copy of its page, so a vault only grew. Versions past the limits are removed when the vault opens and as new ones are made.
- A window no longer receives every page's history when it opens: a page's versions are read when the page is opened or its history is shown. A vault of 400 pages starts with about a seventh of the data.
- A fault in a background task (syncing, watching the folder, saving) is logged and the app goes on, instead of quitting with changes still in memory.

## 0.1.1

- Chinese and English: Settings › General › Language (Automatic, English, 简体中文). The pages, the menus and the settings change at once; Automatic follows the system's language.
- The macOS icon now has the margin of Apple's icon template (the artwork is 824 of 1024 pixels), so it is the size of other apps' icons in the Dock instead of one step larger. Windows and Linux keep the full-size icon.
- "Copy link" for pages and blocks is gone: the address it copied opened nothing outside the window.
- A vault switcher at the foot of the sidebar: switch this window to another vault, or open it in a window of its own so several vaults are open side by side. Open Folder, New Vault, Connect and the picker are in the same menu.
- Search (⌘K) takes several words, marks them in the results, cuts the line around the match, and opens the page scrolled to the line that matched.
- Selected paragraphs have a bar with Copy, Copy as image (⌘⇧C) and Delete; Copy as image is also in the paragraph menu. The picture goes to the system clipboard.
- The side rail no longer sits under the macOS window buttons when the sidebar is folded.
- Updates can come through the tvv.tw mirror (Settings › About › Update source): Automatic uses it when the system language is Chinese, and a failed or altered download falls back to GitHub.
- Notes now updates itself: it checks for a signed release once a day, shows the release notes, and installs the update when you say so. Check by hand from the app menu (Check for Updates…) or Settings › About.

## 0.1.0

- First release: a vault is a folder with one JSON file per page, kept on this computer or synced with a WebDAV folder or an SMB share. Windows and macOS windows draw their own title bar; there is a settings window, a vault picker, and a single running instance per user.
