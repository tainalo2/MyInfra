import type { Gauge } from "./api";
import { text } from "./i18n";
import { cpuPair, sizePair } from "./measure";
import { Skeleton } from "./ui";

export function UsageMeters({ sample }: { sample: { measured: boolean; cpu: Gauge; memory: Gauge; disk: Gauge } | null }) {
  if (!sample) {
    return (
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-3" role="status" aria-label={text("common.measures")}>
        <MeterSkeleton />
        <MeterSkeleton />
        <MeterSkeleton />
      </div>
    );
  }
  if (!sample.measured) return <p className="text-muted">{text("common.noMeasure")}</p>;
  return (
    <div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
      <Meter label={text("common.cpu")} amount={cpuPair(sample.cpu.used, sample.cpu.total)} ratio={share(sample.cpu)} />
      <Meter label={text("common.ram")} amount={sizePair(sample.memory.used, sample.memory.total)} ratio={share(sample.memory)} />
      <Meter label={text("common.storage")} amount={sizePair(sample.disk.used, sample.disk.total)} ratio={share(sample.disk)} />
    </div>
  );
}

function MeterSkeleton() {
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between gap-3">
        <Skeleton className="h-4 w-12" />
        <Skeleton className="h-4 w-24" />
      </div>
      <Skeleton className="h-2 w-full rounded-full" />
    </div>
  );
}

function share(gauge: Gauge) {
  if (!gauge || gauge.total <= 0) return 0;
  return Math.max(0, Math.min(1, gauge.used / gauge.total));
}

function Meter({ label, amount, ratio }: { label: string; amount: string; ratio: number }) {
  const percent = Math.round(ratio * 100);
  return (
    <div className="min-w-0">
      <div className="mb-1.5 flex items-baseline justify-between gap-3">
        <span>{label}</span>
        <span className="whitespace-nowrap text-muted">{percent} %</span>
      </div>
      <div
        className="h-2 overflow-hidden rounded-full bg-line"
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        aria-valuetext={amount}
      >
        <div className={`h-full rounded-full ${ratio >= 0.9 ? "bg-bad" : ratio >= 0.75 ? "bg-warn" : "bg-ink"}`} style={{ width: `${ratio * 100}%` }} />
      </div>
      <p className="mt-1.5 truncate text-[13px] text-muted">{amount}</p>
    </div>
  );
}
