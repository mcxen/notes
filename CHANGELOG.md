# Changelog

## 0.1.3

- Typing shortcuts now work with an input method: `-`, `#`, `1.`, `[]`, `>` and the rest turned a line into a list or heading only when the space came as the Space key, which a Chinese input method does not send, so "- " stayed text. The space is now also read from the text typed. Folded lists have one too: `>>` and a space.
- Keys for the kind of a line, as Notion's: ⌘1, ⌘2, ⌘3 make a heading; ⌘⌥0 a paragraph, ⌘⌥1–3 headings, ⌘⌥4 a to-do, ⌘⌥5 a bulleted list, ⌘⌥6 a numbered list, ⌘⌥7 a toggle, ⌘⌥8 code; ⌘⇧7, ⌘⇧8, ⌘⇧9 a numbered list, a bulleted list and a to-do. They act on every line a selection reaches, and pressed again on lines already of that kind they turn them back into paragraphs. (Ctrl for ⌘ on Windows and Linux.)
- Page history thins with age instead of keeping only the newest 20: every version of the last hour, then one an hour for a day, then one a day for 30 days (up to 60 per page, and 10 MB). A long evening of edits can no longer push out yesterday's versions.
- Page history: a page was only checkpointed when you moved to another page, so editing one page and closing the window left its history empty. It is now checkpointed 20 seconds after an edit, when the window is hidden or closed, and when the history panel opens. Edits within five minutes of the last version replace it instead of adding another.
- New app icon, the amber concept: three cream lines on amber with one red dot (the macOS icon keeps Apple's template margin).
- Toggle list, as in Notion: "/toggle" (or Turn into › Toggle list) makes a line with a triangle that folds what is under it. Whether it is open is kept with the line, so it is the same for everyone who reads the page; on a page you can only read, the triangle opens it for you alone. Enter in an open toggle starts a line inside it. Folded, its lines are left out of arrow-key movement, and still written out when the page is exported or copied.
- Several selected paragraphs can be styled at once from the bar at the foot of the page: Turn into (headings, lists, toggle…), bold, italic, underline, strikethrough, code and colour, across all their words. The marks show pressed when every selected line has them, and a second press clears them.
- The sidebar is wider by default (264 px) and never narrower than its top row needs after the window buttons; its search and collapse buttons were pushed past its edge on macOS.
- First launch: with no vault yet, the window offers to create one at once. It asks for the folder (starting at Documents), then shows the location and a name ("My Notes"), both editable; New Vault makes the folder and opens it on a Welcome page. File › New Vault… opens the same form.

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
