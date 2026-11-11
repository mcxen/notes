import { useState, type FormEvent } from "react";
import { Connections } from "../platform/mygo";
import { WindowControls } from "./WindowChrome";
import { t } from "../i18n";

/* The window that keeps a vault in a remote folder. The Go side fills the
   working copy from the folder, opens the vault in its own window and closes
   this one. The password goes to the system's credential store, not to the
   settings file. */
export function ConnectRemote() {
  const [kind, setKind] = useState<"webdav" | "smb">("webdav");
  const [address, setAddress] = useState("");
  const [share, setShare] = useState("");
  const [folder, setFolder] = useState("");
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [domain, setDomain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await Connections.connect({ kind, address: address.trim(), share: share.trim(), folder: folder.trim(), user: user.trim(), password, domain: domain.trim() });
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure));
      setBusy(false);
    }
  };

  return (
    <>
      <div className="connect-remote__bar" aria-hidden="true" />
      <WindowControls maximizable={false} />
      <form className="connect-remote" onSubmit={submit}>
        <h1 className="connect-remote__title">{t("Connect to a remote folder")}</h1>
        <p className="connect-remote__hint">{t("The vault is kept on this computer and synced with the folder you choose.")}</p>
        <label className="connect-remote__field">
          <span>{t("Kind")}</span>
          <select value={kind} onChange={event => setKind(event.target.value as "webdav" | "smb")} disabled={busy}>
            <option value="webdav">{t("WebDAV")}</option>
            <option value="smb">SMB (Windows share)</option>
          </select>
        </label>
        <label className="connect-remote__field">
          <span>{kind === "webdav" ? "Folder URL" : "Host"}</span>
          <input value={address} onChange={event => setAddress(event.target.value)} placeholder={kind === "webdav" ? "https://dav.example.com/remote.php/webdav/Notes" : "nas.local"} required disabled={busy} />
        </label>
        {kind === "smb" && (
          <>
            <label className="connect-remote__field">
              <span>{t("Share")}</span>
              <input value={share} onChange={event => setShare(event.target.value)} required disabled={busy} />
            </label>
            <label className="connect-remote__field">
              <span>{t("Folder in the share")}</span>
              <input value={folder} onChange={event => setFolder(event.target.value)} disabled={busy} />
            </label>
            <label className="connect-remote__field">
              <span>{t("Domain")}</span>
              <input value={domain} onChange={event => setDomain(event.target.value)} disabled={busy} />
            </label>
          </>
        )}
        <label className="connect-remote__field">
          <span>{t("User")}</span>
          <input value={user} onChange={event => setUser(event.target.value)} autoComplete="username" disabled={busy} />
        </label>
        <label className="connect-remote__field">
          <span>{t("Password")}</span>
          <input type="password" value={password} onChange={event => setPassword(event.target.value)} autoComplete="current-password" disabled={busy} />
        </label>
        {error && <p className="connect-remote__error" role="alert">{error}</p>}
        <div className="connect-remote__actions">
          <button type="submit" className="btn btn--primary" disabled={busy}>{busy ? "Connecting…" : "Connect"}</button>
        </div>
      </form>
    </>
  );
}
