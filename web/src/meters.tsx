import type { Gauge } from "./api";
import { text } from "./i18n";
import { size, vcpu } from "./measure";
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
      <Meter label={text("common.cpu")} used={vcpu(sample.cpu.used)} total={vcpu(sample.cpu.total)} ratio={share(sample.cpu)} />
      <Meter label={text("common.ram")} used={size(sample.memory.used)} total={size(sample.memory.total)} ratio={share(sample.memory)} />
      <Meter label={text("common.storage")} used={size(sample.disk.used)} total={size(sample.disk.total)} ratio={share(sample.disk)} />
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

function Meter({ label, used, total, ratio }: { label: string; used: string; total: string; ratio: number }) {
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between gap-3">
        <span>{label}</span>
        <span className="whitespace-nowrap text-muted">
          {used} / {total}
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-line" role="img" aria-label={`${label} ${used} sur ${total}`}>
        <div className="h-full rounded-full bg-ink" style={{ width: `${ratio * 100}%` }} />
      </div>
    </div>
  );
}
