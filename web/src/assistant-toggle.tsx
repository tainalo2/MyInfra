import { Link, useLocation } from "react-router-dom";
import { Maximize2, MessageSquareText, X } from "lucide-react";
import { useAssistant } from "./assistant-context";
import { AssistantPane } from "./components/AssistantPane";
import { text } from "./i18n";
import { useI18n } from "./lang";
import { Icon, IconButton, focus } from "./ui";

// The assistant's own page shows the chat already: no side panel there.
const assistantPage = "/assistant-ia";

/** The menu entry that opens the assistant beside the page. Wide screens only, like the panel. */
export function AssistantToggle() {
  useI18n();
  const { isOpen, setIsOpen } = useAssistant();
  const { pathname } = useLocation();
  if (pathname === assistantPage) return null;
  return (
    <button
      type="button"
      onClick={() => setIsOpen(!isOpen)}
      aria-pressed={isOpen}
      title={text(isOpen ? "assistant.close" : "assistant.open")}
      className={`hidden items-center gap-3 rounded-md px-3 py-2 text-left text-ink md:flex ${focus} ${isOpen ? "bg-status" : ""}`}
    >
      <Icon icon={MessageSquareText} className={isOpen ? "text-ink" : "text-muted"} />
      <span className="flex-1">{text("nav.assistant")}</span>
    </button>
  );
}

/** The assistant beside the page: it pushes the content, and keeps its chat from page to page. */
export function AssistantPanel() {
  useI18n();
  const { isOpen, setIsOpen } = useAssistant();
  const { pathname } = useLocation();
  if (!isOpen || pathname === assistantPage) return null;
  return (
    <aside aria-label={text("nav.assistant")} className="hidden min-w-0 shrink-0 flex-col border-l border-line bg-surface md:flex md:w-[400px]">
      <div className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
        <h2>{text("nav.assistant")}</h2>
        <div className="flex items-center gap-4">
          <Link to={assistantPage} aria-label={text("assistant.expand")} title={text("assistant.expand")} className={`-m-1 rounded-md p-1 text-ink ${focus}`}>
            <Icon icon={Maximize2} />
          </Link>
          <IconButton icon={X} label={text("assistant.close")} onClick={() => setIsOpen(false)} />
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col">
        <AssistantPane />
      </div>
    </aside>
  );
}
