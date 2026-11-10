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
import { Notebooks } from "./platform/mygo";
import "./styles/index.css";

markWindowChrome();

const root = createRoot(document.getElementById("root")!);

/* The connect window (File > Connect to a Remote Folder) is a form, not a notebook. */
if (new URLSearchParams(window.location.search).get("connect") === "1") {
  root.render(
    <StrictMode>
      <ErrorBoundary>
        <ConnectRemote />
      </ErrorBoundary>
    </StrictMode>,
  );
} else {
  /* In the desktop app the sidebar's material comes from the settings, and the
     page must know before its first paint: the class is set first. */
  const look: Promise<void> = isMyGo()
    ? Notebooks.look().then(
        value => { document.documentElement.classList.toggle("translucent", value.translucent); },
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
      </StrictMode>,
    );
  });
}
