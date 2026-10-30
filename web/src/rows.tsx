import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { ChevronRight, type LucideIcon } from "lucide-react";
import { focus } from "./ui";

// RowGroup holds list rows in one card, under a small title: settings, the
// phone's lists, what a page links to.
export function RowGroup({ title, id, children, className = "" }: { title?: ReactNode; id?: string; children: ReactNode; className?: string }) {
  return (
    <section id={id} className={className}>
      {title ? <h2 className="mb-2 px-1 text-[13px] text-muted md:px-0 md:text-[16px] md:text-ink">{title}</h2> : null}
      <div className="row-group overflow-hidden rounded-md border border-line bg-surface">{children}</div>
    </section>
  );
}

type Tone = "ok" | "warn" | "bad" | "muted" | "link";

const toneText: Record<Tone, string> = { ok: "text-ok", warn: "text-warn", bad: "text-bad", muted: "text-muted", link: "text-link" };
const toneDot: Partial<Record<Tone, string>> = { ok: "bg-ok", warn: "bg-warn", bad: "bg-bad" };

// RowState is what a row says on its right: a count or a word, with a dot
// when it is a state.
export function RowState({ children, tone = "muted" }: { children: ReactNode; tone?: Tone }) {
  return (
    <span className={`inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[13px] ${toneText[tone]}`}>
      {toneDot[tone] ? <span aria-hidden className={`size-2 rounded-full ${toneDot[tone]}`} /> : null}
      {children}
    </span>
  );
}

// ListRow is one line of a list: a tile with an icon or an initial, a name,
// one line of detail, a state on the right and a chevron when it leads on.
export function ListRow({
  to,
  onClick,
  icon,
  tile,
  title,
  sub,
  subTone,
  end,
  chevron,
  danger,
  label,
  phoneTile,
}: {
  to?: string;
  onClick?: () => void;
  icon?: LucideIcon;
  tile?: ReactNode;
  title: ReactNode;
  sub?: ReactNode;
  subTone?: Tone;
  end?: ReactNode;
  chevron?: boolean;
  danger?: boolean;
  label?: string;
  // phoneTile keeps the tile to the phone: a wide screen reads the words.
  phoneTile?: boolean;
}) {
  const Glyph = icon;
  const leads = chevron ?? Boolean(to && !danger);
  const body = (
    <>
      {Glyph || tile ? (
        <span aria-hidden className={`flex size-10 shrink-0 items-center justify-center rounded-md bg-status ${danger ? "text-bad" : "text-muted"} ${phoneTile ? "md:hidden" : ""}`}>
          {Glyph ? <Glyph size={20} strokeWidth={1.75} /> : tile}
        </span>
      ) : null}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={`truncate ${danger ? "text-bad" : ""}`}>{title}</span>
        {sub ? <span className={`truncate text-[13px] ${subTone ? toneText[subTone] : "text-muted"}`}>{sub}</span> : null}
      </span>
      {end}
      {leads ? <ChevronRight aria-hidden size={18} strokeWidth={1.75} className="shrink-0 text-faint" /> : null}
    </>
  );
  const cls = `row ${Glyph || tile ? (phoneTile ? "row-tiled row-phone-tile " : "row-tiled ") : ""}flex min-h-14 w-full items-center gap-3 px-4 py-2 text-left text-ink md:gap-4 md:py-3.5 ${focus}`;
  if (to) {
    return (
      <Link to={to} className={cls} aria-label={label}>
        {body}
      </Link>
    );
  }
  if (onClick) {
    return (
      <button type="button" onClick={onClick} className={cls} aria-label={label}>
        {body}
      </button>
    );
  }
  return <div className={cls}>{body}</div>;
}
