import { isMyGo } from "mygo-runtime";
import { useCallback, useEffect, useState } from "react";
import { Notebooks, type ShareStatus } from "../../platform/mygo";

const OFF: ShareStatus = { enabled: false, configured: false, url: "", maxFileMB: 25, publications: {} };

/** Whether publishing is on (Settings › Cloud), and what of this vault is published.
 *  Read when the window opens, when it is brought forward again — the settings are
 *  another window — and after a change. Off outside the desktop app. */
export function useShareStatus(): { status: ShareStatus; refresh(): Promise<void> } {
  const [status, setStatus] = useState<ShareStatus>(OFF);
  const refresh = useCallback(async () => {
    if (!isMyGo()) return;
    try {
      setStatus(await Notebooks.shareStatus());
    } catch {
      setStatus(OFF);
    }
  }, []);
  useEffect(() => {
    void refresh();
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [refresh]);
  return { status, refresh };
}
