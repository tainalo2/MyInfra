import type { Gauge } from "./api";
import { currentLocale, text } from "./i18n";

export function vcpu(milli: number) {
  return (milli / 1000).toLocaleString(currentLocale(), { maximumFractionDigits: 1 });
}

export function size(bytes: number) {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toLocaleString(currentLocale(), { maximumFractionDigits: 1 })} ${text("unit.gb")}`;
  if (bytes >= 1024 ** 2) return `${Math.round(bytes / 1024 ** 2).toLocaleString(currentLocale())} ${text("unit.mb")}`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024).toLocaleString(currentLocale())} ${text("unit.kb")}`;
  return `${bytes.toLocaleString(currentLocale())} ${text("unit.byte")}`;
}

export function usageText(cpu: Gauge, memory: Gauge, disk: Gauge) {
  return `CPU ${vcpu(cpu.used)} / ${vcpu(cpu.total)} · RAM ${size(memory.used)} / ${size(memory.total)} · ${text("usage.disk")} ${size(disk.used)} / ${size(disk.total)}`;
}

export function podCpu(milli: number) {
  if (milli >= 1000) return vcpu(milli);
  return `${milli.toLocaleString(currentLocale())} m`;
}

type Priced = {
  plan?: { hourly?: string; monthly?: string; currency?: string };
  hosting?: { price?: string; period?: string; currency?: string };
};

// hourlyRate is the price of an hour of a machine, the month spread on 730 hours.
// A machine added by hand uses the price its owner gave.
export function hourlyRate(server: Priced): number {
  const price = server.plan ?? (server.hosting?.period === "hour" ? { hourly: server.hosting.price } : { monthly: server.hosting?.price });
  const hourly = Number(price.hourly);
  if (Number.isFinite(hourly) && hourly > 0) return hourly;
  const monthly = Number(price.monthly);
  return Number.isFinite(monthly) && monthly > 0 ? monthly / 730 : 0;
}

export function rateCurrency(server: Priced) {
  return server.plan?.currency ?? server.hosting?.currency;
}

export function money(value: number) {
  const digits = value < 0.01 ? 4 : value < 1 ? 3 : 2;
  return value.toLocaleString(currentLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
}
