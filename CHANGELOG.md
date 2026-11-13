# Changelog

## 0.1.6

- Menus hung from the caret (the slash menu, a mention, the time picker) sit against the whole line and follow it as the page scrolls. A short window scrolls the menu instead of laying it over the line being typed. A block dropped under a line that holds others lands after all of them, where the drop line is drawn.
- MCP server: pictures and files. notes_fetch now lists a page's attachments; notes_read_attachment opens one (a picture comes back as an image the assistant can see, a text file as text, other files as bytes; a very large picture as its smaller preview); notes_add_attachment puts a picture or file (up to 2.5 MB) on a page. Both follow the allowed vaults and the Read only switch, and an added file is kept in the page's history.
- MCP server: an assistant can change a part of a page. notes_edit_page turns old_text into new_text within one paragraph, which stays the paragraph it was (its place, its colour, the pictures, dates and links among its words); more lines add paragraphs after it, an empty one removes it. A page with pictures, tables or links, which a whole new body would lose, can now have a word put right.
- MCP server: links between pages. A page's links are read as `[[the page's title]]` (they were empty brackets) and notes_fetch lists the pages a page links to and is linked from; `[[a title]]` or `[[a page's id]]` written in Markdown becomes a link.
- MCP server: search finds pages that have every word of the query, in any order, across bold and other marks; titles first, then the most recently changed. Its snippet is the paragraph found, cut around the words.
- MCP server: new tools. notes_move_page, notes_restore_page, notes_recent_pages; notes_create_page takes if_exists "append", which adds to the page of that title instead of making another (a daily log can be written again and again).
- MCP server: notes_fetch answers once, in words (a few lines about the page, a line of ---, its body), instead of the page twice over as JSON.
- MCP server, fixed: a line starting with bold or italic words, a decimal ("3.14 …") or a date ("2026.11.13 …") was read as a list item and lost its start; the version kept before an assistant's change dropped a paragraph's colour, alignment, fold and a picture's width; a snippet could cut a Chinese character in half; a file with a Chinese name was shown as "pdf"; pages under a page in the Trash were still found and changed. An update with nothing in it is refused.

## 0.1.5

- Date and time: `/date` (also `/time`, `/now`) opens a picker of year, month, day, hour, minute and second. Each shows its value now and can be ticked off; the box below previews what Insert writes, as plain text in place of the typed command. A Chinese page gets 2026年11月12日 16时08分21秒, an English one 2026-11-12 16:08:21. Escape leaves the command as it was.
- Menu bar tray: an icon (a template on macOS, so it follows the bar's light or dark) that shows or hides the vault windows when clicked, and a menu on a right-click with Show Notes or Hide Notes, New Page, Open Vault…, New Vault…, Open Recent, Sync Now, the MCP server (its status, turn on or off, copy the prompt, read only), Settings…, Check for Updates and Quit. Closing a window keeps Notes running in the tray, so an assistant on the MCP server still answers; "Close a window to the tray" turns that off. Settings › General › Menu bar and tray has the rest: hide the tray icon, hide the Dock icon (macOS), and a shortcut to show or hide the windows from anywhere.
- Model Context Protocol server (Settings › Integrations), off by default. It lets AI assistants search, read, create, update and trash pages in the vaults you allow, over Streamable HTTP on port 8999 (or stdio with `notes --mcp-stdio`, which forwards to the running app). Every request needs the key; "Only this computer" listens on 127.0.0.1, otherwise on every interface, for a proxy such as frp, with an optional public address. Read only turns off the changes. The settings page copies the prompt (address, key, vaults and tools), the Claude Code command and the stdio entry. Changes an assistant makes are kept in the page's history, as "Changed by an assistant".

- Export a page (page menu › Export…) as Markdown or HTML: one file, or a .zip when it has sub-pages (in folders) or pictures and files (in `assets/`, linked from the pages). The choices are remembered.
- The picture viewer zooms: a toolbar at the foot of the picture (− · % · + · fit · 1:1), the wheel or a trackpad's pinch about the pointer, + − 0 1 on the keyboard, a drag pans, and a double-click goes to 1:1.

## 0.1.4

- Typing shortcuts now work with an input method: `-`, `#`, `1.`, `[]`, `>` and the rest turned a line into a list or heading only when the space came as the Space key, which a Chinese input method does not send, so "- " stayed text. The space is now also read from the text typed. Folded lists have one too: `>>` and a space.
- Keys for the kind of a line, as Notion's: ⌘1, ⌘2, ⌘3 make a heading; ⌘⌥0 a paragraph, ⌘⌥1–3 headings, ⌘⌥4 a to-do, ⌘⌥5 a bulleted list, ⌘⌥6 a numbered list, ⌘⌥7 a toggle, ⌘⌥8 code; ⌘⇧7, ⌘⇧8, ⌘⇧9 a numbered list, a bulleted list and a to-do. They act on every line a selection reaches, and pressed again on lines already of that kind they turn them back into paragraphs. (Ctrl for ⌘ on Windows and Linux.)
- Page history thins with age instead of keeping only the newest 20: every version of the last hour, then one an hour for a day, then one a day for 30 days (up to 60 per page, and 10 MB). A long evening of edits can no longer push out yesterday's versions.
- Page history: a page was only checkpointed when you moved to another page, so editing one page and closing the window left its history empty. It is now checkpointed 20 seconds after an edit, when the window is hidden or closed, and when the history panel opens. Edits within five minutes of the last version replace it instead of adding another.

## 0.1.3

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
