// The app's collections, declared before anything opens one.
import "../schema";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { ErrorBoundary } from "./app/ErrorBoundary";
import { ConnectRemote } from "./app/ConnectRemote";
import { UiProvider } from "./app/ui-context";
import { WindowControls, markWindowChrome } from "./app/WindowChrome";
import { WorkspaceProvider } from "./app/workspace-context";
import { isMyGo } from "mygo-runtime";
import { Fragment, type ReactNode } from "react";
import { useLang } from "./i18n";
import { Notebooks } from "./platform/mygo";
import "./styles/index.css";

markWindowChrome();

/* Everything is mounted again when the language changes: each text reads it as
   it is drawn, and a page drawn before must not keep the old words. */
function Localized({ children }: { children: ReactNode }) {
  return <Fragment key={useLang()}>{children}</Fragment>;
}

const root = createRoot(document.getElementById("root")!);

/* The connect window (File > Connect to a Remote Folder) is a form, not a notebook. */
if (new URLSearchParams(window.location.search).get("connect") === "1") {
  root.render(
    <StrictMode>
      <Localized>
        <ErrorBoundary>
          <ConnectRemote />
        </ErrorBoundary>
      </Localized>
    </StrictMode>,
  );
} else {
  /* In the desktop app the sidebar's material comes from the settings, and the
     page must know before its first paint: the class is set first. */
  const look: Promise<void> = isMyGo()
    ? Notebooks.look().then(
        value => {
          document.documentElement.classList.toggle("translucent", value.translucent);
          document.documentElement.classList.toggle("no-outline", !value.outline);
        },
        () => undefined,
      )
    : Promise.resolve();
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
}
