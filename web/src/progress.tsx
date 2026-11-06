import type { ReactNode } from "react";
import { Check, X } from "lucide-react";
import { text } from "./i18n";
import { StatusBadge } from "./kit";

export type ProgressStep = { key: string; title: ReactNode; status: string; detail?: ReactNode; children?: ReactNode };

function done(status: string) {
  return status === "done" || status === "linked" || status === "ready";
}

// ProgressSteps follows a long operation: a bar that says how far it went,
// then each step with its mark, its state and what it is doing.
export function ProgressSteps({ steps }: { steps: ProgressStep[] }) {
  const finished = steps.filter((step) => done(step.status)).length;
  const current = Math.min(steps.length, finished + (steps.some((step) => step.status === "running") ? 1 : 0)) || finished;
  const ratio = steps.length ? finished / steps.length : 0;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <div
          className="h-1.5 overflow-hidden rounded-full bg-line"
          role="img"
          aria-label={text("progress.label", { step: Math.max(1, current), total: steps.length })}
        >
          <div className="h-full rounded-full bg-ink" style={{ width: `${ratio * 100}%` }} />
        </div>
        <span className="text-[13px] text-muted">{text("progress.label", { step: Math.max(1, current), total: steps.length })}</span>
      </div>
      <ol className="flex flex-col">
        {steps.map((step, index) => {
          const ok = done(step.status);
          const bad = step.status === "failed";
          const live = step.status === "running";
          const last = index === steps.length - 1;
          return (
            <li key={step.key} className="flex gap-4">
              <span className="flex flex-col items-center">
                <span
                  aria-hidden
                  className={`flex size-6 shrink-0 items-center justify-center rounded-full border text-[12px] ${
                    ok ? "border-ok bg-ok text-canvas" : bad ? "border-bad bg-bad text-canvas" : live ? "border-ink text-ink" : "border-line text-faint"
                  }`}
                >
                  {ok ? <Check size={14} strokeWidth={2} /> : bad ? <X size={14} strokeWidth={2} /> : live ? <span className="size-2 animate-pulse rounded-full bg-ink" /> : index + 1}
                </span>
                {last ? null : <span aria-hidden className={`w-px flex-1 ${ok ? "bg-ok" : "bg-line"}`} />}
              </span>
              <div className={`flex min-w-0 flex-1 flex-col gap-1 ${last ? "" : "pb-6"}`}>
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span className={ok || live || bad ? "text-ink" : "text-muted"}>{step.title}</span>
                  {live || bad ? <StatusBadge status={bad ? "failed" : "deploying"} /> : null}
                </span>
                {step.detail ? <span className="text-[13px] text-muted">{step.detail}</span> : null}
                {step.children}
              </div>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
