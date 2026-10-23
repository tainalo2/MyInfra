import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { text } from "./i18n";
import { Button, ErrorText, Field, Input, TextButton, podFailed, statusLabel } from "./ui";

const focusable = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// Sheet is a dialog: a sheet rising from the bottom on a phone, a centred box
// on a wider screen. Focus stays inside and goes back where it was on close.
export function Sheet({
  title,
  onClose,
  busy = false,
  children,
  wide = false,
}: {
  title: ReactNode;
  onClose: () => void;
  busy?: boolean;
  children: ReactNode;
  wide?: boolean;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const busyRef = useRef(busy);
  busyRef.current = busy;
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = panel.current?.querySelector<HTMLElement>("[autofocus], [data-autofocus]") ?? panel.current?.querySelector<HTMLElement>(focusable);
    first?.focus();
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape" && !busyRef.current) {
        event.preventDefault();
        closeRef.current();
      }
      if (event.key !== "Tab" || !panel.current) return;
      const items = [...panel.current.querySelectorAll<HTMLElement>(focusable)];
      if (items.length === 0) return;
      const head = items[0];
      const tail = items[items.length - 1];
      if (event.shiftKey && document.activeElement === head) {
        event.preventDefault();
        tail.focus();
      } else if (!event.shiftKey && document.activeElement === tail) {
        event.preventDefault();
        head.focus();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, []);

  return (
    <div
      className="fixed inset-0 z-40 flex items-end justify-center bg-ink/40 sm:items-center sm:px-6"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onClose();
      }}
    >
      <div
        ref={panel}
        className={`flex max-h-[90dvh] w-full flex-col rounded-t-xl border border-line bg-surface p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom))] sm:rounded-md sm:p-6 ${wide ? "sm:max-w-xl" : "sm:max-w-md"}`}
      >
        <span className="mx-auto mb-4 block h-1 w-10 rounded-full bg-line sm:hidden" aria-hidden />
        <h2 id={titleId} className="mb-3 text-lg">
          {title}
        </h2>
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </div>
    </div>
  );
}

// ConfirmDialog asks before an action. With phrase, the person types it first:
// for what cannot be undone. It shows the error of a failed action itself, so
// the page behind stays as it was.
export function ConfirmDialog({
  title,
  children,
  confirmLabel,
  phrase,
  phraseLabel,
  danger = false,
  onConfirm,
  onClose,
}: {
  title: ReactNode;
  children?: ReactNode;
  confirmLabel: string;
  phrase?: string;
  phraseLabel?: string;
  danger?: boolean;
  onConfirm: () => Promise<unknown> | unknown;
  onClose: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const ready = !phrase || typed === phrase;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!ready || busy) return;
    setBusy(true);
    setError("");
    try {
      await onConfirm();
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setBusy(false);
    }
  }

  return (
    <Sheet title={title} onClose={onClose} busy={busy}>
      <form onSubmit={submit}>
        {children ? <div className="mb-6 text-muted">{children}</div> : null}
        {phrase ? (
          <div className="mb-6">
            <Field label={phraseLabel ?? text("field.confirm")}>
              <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" autoCapitalize="off" spellCheck={false} data-autofocus />
            </Field>
          </div>
        ) : null}
        {error ? (
          <div className="mb-6">
            <ErrorText>{error}</ErrorText>
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-4">
          <Button type="submit" variant={danger ? "danger" : "primary"} disabled={!ready || busy} data-autofocus={phrase ? undefined : true}>
            {busy ? <Spinner /> : null}
            {confirmLabel}
          </Button>
          <TextButton type="button" onClick={onClose} disabled={busy}>
            {text("common.cancel")}
          </TextButton>
        </div>
      </form>
    </Sheet>
  );
}

export function Spinner() {
  return <span className="size-4 animate-spin rounded-full border border-current border-t-transparent" aria-hidden />;
}

// StatusBadge says a status in words as well as in colour.
export function StatusBadge({ status }: { status?: string }) {
  const label = statusLabel(status || "");
  const dot = label.tone === "text-ok" ? "bg-ok" : label.tone === "text-bad" ? "bg-bad" : label.tone === "text-warn" ? "bg-warn" : "bg-faint";
  return (
    <span className={`inline-flex items-center gap-1.5 ${label.tone}`}>
      <span className={`inline-block size-2 shrink-0 rounded-full ${dot}`} aria-hidden />
      {label.text}
    </span>
  );
}

type ToastTone = "ok" | "bad";
type ToastItem = { id: number; message: string; tone: ToastTone };

const ToastContext = createContext<(message: string, tone?: ToastTone) => void>(() => undefined);

export function useToast() {
  return useContext(ToastContext);
}

// ToastHost shows short confirmations, read out by screen readers.
export function ToastHost({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const next = useRef(0);
  const show = useCallback((message: string, tone: ToastTone = "ok") => {
    const id = ++next.current;
    setItems((current) => [...current.slice(-2), { id, message, tone }]);
    window.setTimeout(() => setItems((current) => current.filter((item) => item.id !== id)), 4500);
  }, []);
  return (
    <ToastContext.Provider value={show}>
      {children}
      <div
        className="pointer-events-none fixed inset-x-0 bottom-[calc(5rem+env(safe-area-inset-bottom))] z-50 flex flex-col items-center gap-2 px-4 md:bottom-6"
        role="status"
        aria-live="polite"
      >
        {items.map((item) => (
          <p
            key={item.id}
            className={`pointer-events-auto max-w-md rounded-md border px-4 py-3 shadow-sm ${item.tone === "bad" ? "border-bad bg-surface text-bad" : "border-line bg-ink text-canvas"}`}
          >
            {item.message}
          </p>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

// appHealth is the one way to tell an application's health, so that a list
// and the application's page never disagree: a failing copy makes it failed.
export function appHealth(app: { paused?: boolean; runStatus?: string }, measured?: string, pods: { phase: string; reason?: string }[] = []) {
  if (app.paused) return "paused";
  if (pods.some((pod) => podFailed({ phase: pod.phase, reason: pod.reason ?? "" }))) return "failed";
  return measured || app.runStatus || "";
}

// useWide tells whether the screen is wider than a phone.
export function useWide(query = "(min-width: 768px)") {
  const [wide, setWide] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const media = window.matchMedia(query);
    const onChange = () => setWide(media.matches);
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [query]);
  return wide;
}

export type Step = { title: string; content: ReactNode; ready?: boolean };

// Steps lays out a long form: all of it on a wide screen, one step at a time
// on a phone, with Next checking the fields of the step before moving on. The
// last step carries the submit button.
export function Steps({ steps, submit, error }: { steps: Step[]; submit: ReactNode; error?: ReactNode }) {
  const wide = useWide();
  const [current, setCurrent] = useState(0);
  const body = useRef<HTMLDivElement>(null);
  const index = Math.min(current, steps.length - 1);
  const step = steps[index];
  const last = index === steps.length - 1;

  function move(next: number) {
    setCurrent(next);
    window.scrollTo({ top: 0 });
  }

  function forward() {
    const fields = body.current?.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("input, select, textarea") ?? [];
    for (const field of fields) {
      if (!field.checkValidity()) {
        field.reportValidity();
        return;
      }
    }
    move(index + 1);
  }

  if (wide) {
    return (
      <>
        {steps.map((item) => (
          <div key={item.title} className="contents">
            {item.content}
          </div>
        ))}
        {error}
        <div>{submit}</div>
      </>
    );
  }

  return (
    <>
      <ol className="flex gap-1" aria-label={text("start.progress", { step: index + 1, count: steps.length })}>
        {steps.map((item, i) => (
          <li key={item.title} className={`h-1 flex-1 rounded-full ${i <= index ? "bg-ink" : "bg-line"}`} aria-current={i === index ? "step" : undefined}>
            <span className="sr-only">{item.title}</span>
          </li>
        ))}
      </ol>
      <h2 className="-mt-2 text-lg">
        <span className="text-muted">
          {index + 1}/{steps.length} ·{" "}
        </span>
        {step.title}
      </h2>
      <div ref={body} className="flex flex-col gap-6">
        {step.content}
      </div>
      {error}
      <div className="sticky bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-10 -mx-4 flex items-center gap-3 border-t border-line bg-canvas/95 px-4 py-3 backdrop-blur">
        {index > 0 ? (
          <Button type="button" variant="secondary" onClick={() => move(index - 1)}>
            {text("ui.retour")}
          </Button>
        ) : null}
        <div className="flex-1 [&>button]:w-full">
          {last ? (
            submit
          ) : (
            <Button type="button" onClick={forward} disabled={step.ready === false} className="w-full">
              {text("steps.next")}
            </Button>
          )}
        </div>
      </div>
    </>
  );
}

// Tabs switch between the parts of one thing. The arrow keys move along
// them, as with the tabs of a window.
export function Tabs<K extends string>({
  tabs,
  active,
  onChange,
  label,
}: {
  tabs: { key: K; label: string; mark?: boolean }[];
  active: K;
  onChange: (key: K) => void;
  label: string;
}) {
  function onKey(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    event.preventDefault();
    const at = tabs.findIndex((tab) => tab.key === active);
    const next = tabs[(at + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length];
    onChange(next.key);
    event.currentTarget.querySelector<HTMLButtonElement>(`[data-tab="${next.key}"]`)?.focus();
  }
  return (
    <div role="tablist" aria-label={label} onKeyDown={onKey} className="-mx-1 flex gap-1 overflow-x-auto border-b border-line">
      {tabs.map((tab) => (
        <button
          key={tab.key}
          type="button"
          role="tab"
          data-tab={tab.key}
          aria-selected={tab.key === active}
          tabIndex={tab.key === active ? 0 : -1}
          onClick={() => onChange(tab.key)}
          className={`-mb-px flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2 ${
            tab.key === active ? "border-ink text-ink" : "border-transparent text-muted hover:text-ink"
          } focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus`}
        >
          {tab.label}
          {tab.mark ? <span className="size-1.5 rounded-full bg-bad" role="img" aria-label={text("package.hasProblem")} /> : null}
        </button>
      ))}
    </div>
  );
}
