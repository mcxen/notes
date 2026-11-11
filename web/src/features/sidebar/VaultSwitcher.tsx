import { ChevronsUpDown, Cloud, Folder, FolderOpen, FolderPlus, LayoutGrid, Link2, SquareArrowOutUpRight } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { MenuDivider, MenuList } from "../../components/Menu";
import { Popover } from "../../components/Popover";
import { Tooltip } from "../../components/Tooltip";
import { imeComposing } from "../../core/keyboard";
import { rectOf, type AnchorRect } from "../../hooks/useAnchoredPosition";
import { Connections, type VaultEntry } from "../../platform/mygo";
import { t } from "../../i18n";

/** How many vaults it takes before the list gets a filter field. */
const FILTER_FROM = 6;

/** The vault switcher at the foot of the sidebar (desktop app only): the
 *  vaults the app knows, most recent first. A press on one switches this
 *  window to it; its arrow opens it in a window of its own, so several vaults
 *  can be open side by side. A vault that is open already is brought forward
 *  instead of being opened twice. */
export function VaultSwitcher({ name, compact = false }: { name: string; compact?: boolean }) {
  const buttonRef = useRef<HTMLButtonElement | null>(null);
  const [anchor, setAnchor] = useState<AnchorRect | null>(null);
  const [vaults, setVaults] = useState<VaultEntry[]>([]);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState("");
  const open = anchor !== null;

  const close = useCallback(() => { setAnchor(null); setFilter(""); setError(""); }, []);

  useEffect(() => {
    if (!open) return;
    let alive = true;
    void Connections.vaults().then((list) => { if (alive) setVaults(list ?? []); }, () => undefined);
    return () => { alive = false; };
  }, [open]);

  const openVault = (entry: VaultEntry, newWindow: boolean) => {
    if (entry.current) { close(); return; }
    Connections.openVault(entry.path, newWindow).then(close, (reason: unknown) => {
      setError(reason instanceof Error ? reason.message : String(reason));
    });
  };

  const words = filter.trim().toLowerCase();
  const shown = words
    ? vaults.filter((entry) => `${entry.name} ${entry.detail}`.toLowerCase().includes(words))
    : vaults;

  const act = (run: () => Promise<void>) => { close(); void run(); };

  return (
    <>
      <Tooltip label={t("Switch vault")} placement="top-start">
        <button
          type="button"
          ref={buttonRef}
          className={`vault-switch ${compact ? "vault-switch--compact" : ""} ${open ? "is-on" : ""}`}
          aria-label={t("Vault: {name}. Switch vault", { name })}
          aria-haspopup="menu"
          aria-expanded={open}
          onClick={() => setAnchor(open ? null : rectOf(buttonRef.current!))}
        >
          <span className="vault-switch__mark"><Folder size={14} /></span>
          {compact ? null : <span className="vault-switch__name">{name}</span>}
          {compact ? null : <ChevronsUpDown size={13} className="vault-switch__chevron" />}
        </button>
      </Tooltip>

      <Popover open={open} anchor={anchor} placement="top-start" label={t("Vaults")} className="vault-menu" onDismiss={close}>
        <MenuList label={t("Vaults")} autoFocus={vaults.length < FILTER_FROM}>
          <div className="menu__section-title">{t("Vaults")}</div>
          {vaults.length >= FILTER_FROM ? (
            <input
              autoFocus
              className="vault-menu__filter"
              value={filter}
              placeholder={t("Find a vault…")}
              aria-label={t("Find a vault")}
              onChange={(event) => setFilter(event.target.value)}
              onKeyDown={(event) => {
                if (imeComposing(event)) return;
                if (event.key === "Enter" && shown[0]) { event.preventDefault(); openVault(shown[0], false); }
                if (event.key === "Escape") close();
                event.stopPropagation();
              }}
            />
          ) : null}
          <div className="vault-menu__list">
            {shown.map((entry) => (
              <div key={entry.path} className={`vault-row ${entry.current ? "is-current" : ""}`}>
                <button
                  type="button"
                  data-menu-item
                  role="menuitem"
                  className="menu__item vault-row__main"
                  disabled={entry.missing}
                  title={entry.missing ? t("This folder is no longer there") : entry.path}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => openVault(entry, false)}
                >
                  <span className="menu__icon">{entry.remote ? <Cloud size={16} /> : <Folder size={16} />}</span>
                  <span className="menu__copy">
                    <span className="menu__label">{entry.name}</span>
                    <span className="menu__description">{entry.missing ? t("Folder missing") : entry.detail}</span>
                  </span>
                  {entry.current ? <span className="vault-row__state">{t("Here")}</span> : entry.open ? <span className="vault-row__state">{t("Open")}</span> : null}
                </button>
                {entry.current || entry.missing ? null : (
                  <Tooltip label={t("Open in a new window")} placement="top-start">
                    <button
                      type="button"
                      className="icon-btn vault-row__new"
                      aria-label={t("Open {name} in a new window", { name: entry.name })}
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => openVault(entry, true)}
                    >
                      <SquareArrowOutUpRight size={14} />
                    </button>
                  </Tooltip>
                )}
              </div>
            ))}
            {!shown.length ? <div className="vault-menu__empty">{vaults.length ? t("No vault matches.") : t("No vaults yet.")}</div> : null}
          </div>
          {error ? <div className="vault-menu__error" role="alert">{error}</div> : null}
          <MenuDivider />
          <MenuAction icon={<FolderOpen size={16} />} label={t("Open Folder…")} onSelect={() => act(() => Connections.chooseVault())} />
          <MenuAction icon={<FolderPlus size={16} />} label={t("New Vault…")} onSelect={() => act(() => Connections.newVault())} />
          <MenuAction icon={<Link2 size={16} />} label={t("Connect to a Remote Folder…")} onSelect={() => act(() => Connections.openConnect())} />
          <MenuAction icon={<LayoutGrid size={16} />} label={t("Vault Picker…")} onSelect={() => act(() => Connections.openPicker())} />
        </MenuList>
      </Popover>
    </>
  );
}

function MenuAction({ icon, label, onSelect }: { icon: React.ReactNode; label: string; onSelect(): void }) {
  return (
    <button type="button" data-menu-item role="menuitem" className="menu__item" onMouseDown={(event) => event.preventDefault()} onClick={onSelect}>
      <span className="menu__icon">{icon}</span>
      <span className="menu__copy"><span className="menu__label">{label}</span></span>
    </button>
  );
}
