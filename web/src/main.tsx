// The app's collections, declared before anything opens one.
import "../schema";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { ErrorBoundary } from "./app/ErrorBoundary";
import { UiProvider } from "./app/ui-context";
import { WindowControls, markWindowChrome } from "./app/WindowChrome";
import { WorkspaceProvider } from "./app/workspace-context";
import { isMyGo } from "mygo-runtime";
import { Fragment, type ReactNode } from "react";
import { useLang } from "./i18n";
import { events, Notebooks, type Look } from "./platform/mygo";
import { setSkin } from "./features/skin/skin-store";
import { initClipboardStore } from "./features/editor/clipboard-store";
import "./styles/index.css";

markWindowChrome();

/* Everything is mounted again when the language changes: each text reads it as
   it is drawn, and a page drawn before must not keep the old words. */
function Localized({ children }: { children: ReactNode }) {
  return <Fragment key={useLang()}>{children}</Fragment>;
}

const root = createRoot(document.getElementById("root")!);

/* In the desktop app the sidebar's material comes from the settings, and the
   page must know before its first paint: the class is set first. */
function applyLook(value: Look) {
  document.documentElement.classList.toggle("translucent", value.translucent);
  document.documentElement.classList.toggle("no-outline", !value.outline);
  setSkin(value.skin);
}

const look: Promise<void> = isMyGo()
  ? Notebooks.look().then(applyLook, () => undefined)
  : Promise.resolve();
/* The wallpaper changes while its sliders move: the new look arrives whole. */
if (isMyGo()) events.lookChanged.on(applyLook);
initClipboardStore();
void look.then(() => {
  /* Two boundaries, so a render error never leaves a blank window. One inside
     the workspace, around what draws it: the workspace stays open behind the
     message and goes on saving what was written. One outside everything, for
     an error in the workspace itself: Try again opens it afresh. */
  root.render(
    <StrictMode>
      <Localized>
        {/* Outside the boundaries: the window's own buttons work even when the page fails. */}
        <WindowControls />
        <ErrorBoundary>
          <WorkspaceProvider>
            <ErrorBoundary>
              <UiProvider>
                <App />
              </UiProvider>
            </ErrorBoundary>
          </WorkspaceProvider>
        </ErrorBoundary>
      </Localized>
    </StrictMode>,
  );
});
