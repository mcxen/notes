import { Check } from "lucide-react";
import { useEffect, useState } from "react";
import { MenuDivider, MenuItem, MenuList } from "../../components/Menu";
import type { AnchorRect } from "../../hooks/useAnchoredPosition";
import { language, t } from "../../i18n";
import { TIME_PARTS, timeText, timeValue, type TimePart } from "./time-text";

/** The time picker, open on the line where a "/" command was typed. What it
 *  inserts replaces the words from `from` to `to`. */
export interface TimeMenuState {
  blockId: string;
  from: number;
  to: number;
  anchor: AnchorRect;
}

const LABELS: Record<TimePart, string> = {
  year: "Year",
  month: "Month",
  day: "Day",
  hour: "Hour",
  minute: "Minute",
  second: "Second",
};

/** The time now, read again every second while the picker is open. */
function useNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}

/** Year, month, day, hour, minute and second, each ticked on or off, with the
 *  value it has now; below, the text that would be inserted. All six start on. */
export function TimePicker({ onInsert }: { onInsert(text: string): void }) {
  const now = useNow();
  const [chosen, setChosen] = useState<ReadonlySet<TimePart>>(() => new Set(TIME_PARTS));
  const text = timeText(now, chosen, language() === "zh");

  const toggle = (part: TimePart) => setChosen((current) => {
    const next = new Set(current);
    if (next.has(part)) next.delete(part);
    else next.add(part);
    return next;
  });

  return (
    <MenuList label={t("Date and time")} className="time-picker" autoFocus>
      {TIME_PARTS.map((part) => {
        const on = chosen.has(part);
        return (
          <button
            key={part}
            type="button"
            data-menu-item
            role="menuitemcheckbox"
            aria-checked={on}
            className="menu__item time-picker__row"
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => toggle(part)}
          >
            <span className={`time-picker__box ${on ? "is-on" : ""}`} aria-hidden="true">
              {on ? <Check size={12} /> : null}
            </span>
            <span className="menu__label">{t(LABELS[part])}</span>
            <span className="time-picker__value">{timeValue(now, part)}</span>
          </button>
        );
      })}
      <MenuDivider />
      <div className="time-picker__preview" aria-live="polite">
        {text || t("Choose at least one part")}
      </div>
      <MenuItem label={t("Insert")} disabled={!text} onSelect={() => onInsert(text)} />
    </MenuList>
  );
}
