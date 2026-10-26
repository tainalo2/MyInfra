import type { LibraryMark } from "./api";
import { text } from "./i18n";
import { useI18n } from "./lang";

// LibraryTag says that a package or template comes from the library, at
// which version, and whether a newer one waits on the updates page.
export function LibraryTag({ mark }: { mark?: LibraryMark }) {
  useI18n();
  if (!mark) return null;
  return (
    <span className="mt-1 flex flex-wrap items-center gap-2 text-[13px] text-faint">
      <span>{text("library.from", { version: mark.version })}</span>
      {mark.edited ? <span>· {text("library.edited")}</span> : null}
      {mark.newer ? <span className="rounded-full border border-line px-2 py-0.5 text-[12px] text-link">{text("library.newer", { version: mark.newer })}</span> : null}
    </span>
  );
}

// CommunityTag marks a package or template the community shares, beside
// the library's own.
export function CommunityTag() {
  useI18n();
  return (
    <span className="rounded-full border border-line px-2 py-0.5 text-[12px] text-muted" title={text("library.communityHint")}>
      {text("library.community")}
    </span>
  );
}
