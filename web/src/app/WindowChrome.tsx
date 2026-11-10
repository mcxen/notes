import { useEffect, useState } from "react";
import { currentWindow, isMyGo, runtime } from "mygo-runtime";
import { events } from "../platform/mygo";

/* The window's own controls, drawn in the page on Windows and Linux, where the
   window has no system buttons (MyGo's HideWindowButtons). macOS keeps its
   traffic lights and needs none of this. The sizes, icons and states follow
   GodGo's windowControls, which the notes window shares. */

/** Whether this page draws the window's controls itself. */
export function ownWindowControls(): boolean {
  return isMyGo() && runtime().platform !== "darwin";
}

/** Tells the stylesheet which window chrome the page has, before its first paint. */
export function markWindowChrome(): void {
  if (!isMyGo()) return;
  document.documentElement.dataset.platform = runtime().platform;
  if (ownWindowControls()) document.documentElement.classList.add("own-window-controls");
}

const STROKE = { viewBox: "0 0 16 16", fill: "none", stroke: "currentColor", strokeWidth: 1.35, strokeLinecap: "round", strokeLinejoin: "round" } as const;

function MinimizeIcon() {
  return <svg width={16} height={16} aria-hidden {...STROKE}><path d="M3.5 8h9" /></svg>;
}

function MaximizeIcon() {
  return <svg width={16} height={16} aria-hidden {...STROKE}><rect x="3.5" y="3.5" width="9" height="9" rx="1.5" /></svg>;
}

function RestoreIcon() {
  return (
    <svg width={16} height={16} aria-hidden {...STROKE}>
      <rect x="3.5" y="5.5" width="7" height="7" rx="1.2" />
      <path d="M5.5 3.5h5.5a1.5 1.5 0 0 1 1.5 1.5v5.5" />
    </svg>
  );
}

function CloseIcon() {
  return (
    <svg width={14} height={14} viewBox="0 0 56 56" aria-hidden>
      <path fill="currentColor" d="M10.023 43.023c-.796.797-.82 2.157 0 2.954c.82.796 2.157.796 2.977 0l15-15l15 15c.797.796 2.156.82 2.977 0c.796-.82.796-2.157 0-2.954L30.953 28l15.024-15c.796-.797.82-2.156 0-2.953c-.844-.82-2.18-.82-2.977 0l-15 15l-15-15c-.82-.82-2.18-.844-2.977 0c-.796.82-.796 2.156 0 2.953l15 15Z" />
    </svg>
  );
}

/** The minimize, maximize and close buttons, at the top right of the window.
 *  A window that cannot be maximized leaves that button out. */
export function WindowControls({ maximizable = true }: { maximizable?: boolean }) {
  const [maximized, setMaximized] = useState(false);

  useEffect(() => {
    if (!ownWindowControls()) return;
    let alive = true;
    void currentWindow.isMaximized().then(value => { if (alive) setMaximized(value); }, () => undefined);
    const stop = events.windowMaximized.on(value => setMaximized(value));
    return () => { alive = false; stop(); };
  }, []);

  if (!ownWindowControls()) return null;
  const maximizeLabel = maximized ? "Restore" : "Maximize";

  return (
    <div className="window-controls" role="group" aria-label="Window">
      <button type="button" className="window-control" aria-label="Minimize" title="Minimize" onClick={() => void currentWindow.minimize()}>
        <MinimizeIcon />
      </button>
      {maximizable && (
        <button type="button" className="window-control" aria-label={maximizeLabel} title={maximizeLabel} onClick={() => void currentWindow.toggleMaximize()}>
          {maximized ? <RestoreIcon /> : <MaximizeIcon />}
        </button>
      )}
      <button type="button" className="window-control window-control--close" aria-label="Close" title="Close" onClick={() => void currentWindow.close()}>
        <CloseIcon />
      </button>
    </div>
  );
}
