import { useState } from "react";
import type { Offer } from "./api";
import { Field, Input, Select } from "./ui";
import { text, text as say, currentLocale } from "./i18n";

export type OfferSort = "price" | "cpu" | "memory" | "disk" | "name";

export function formatPrice(raw: string | undefined, hour: boolean, currency = "EUR") {
  if (!raw) return "";
  const value = Number(raw);
  if (!Number.isFinite(value)) return raw;
  const digits = value < 0.001 ? 6 : hour && value < 1 ? 4 : 2;
  const text = value.toLocaleString(currentLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const unit = currency === "USD" ? "$" : "€";
  return hour ? `${text} ${unit}/h` : `${text} ${unit}/${say("unit.month")}`;
}

export function memory(mb: number) {
  if (mb >= 1024) return `${(mb / 1024).toLocaleString(currentLocale(), { maximumFractionDigits: 1 })} ${say("unit.gb")}`;
  return `${mb} ${say("unit.mb")}`;
}

export function gpuText(gpus?: number, gpu?: string) {
  if (!gpus) return "";
  return `${gpus} ${gpu || "GPU"}`;
}

export function machineLine(name: string, vcpu: number, memoryMb: number, gpus?: number, gpu?: string, diskGb = 0) {
  const card = gpus ? ` · ${gpuText(gpus, gpu)}` : "";
  const disk = diskGb > 0 ? ` · ${diskGb} ${say("unit.gb")}` : "";
  return `${name} · ${vcpu} vCPU · ${memory(memoryMb)}${card}${disk}`;
}

export function offerOptionLabel(offer: Offer, currency: string) {
  const hourly = offer.hourly ? ` · ${formatPrice(offer.hourly, true, currency)}` : "";
  return `${machineLine(offer.name, offer.vcpu, offer.memoryMb, offer.gpus, offer.gpu, offer.diskGb)}${hourly}`;
}

export function sortOffers(offers: Offer[], sort: OfferSort) {
  const copy = [...offers];
  const byName = (left: Offer, right: Offer) => left.name.localeCompare(right.name, "fr");
  const price = (offer: Offer) => {
    const value = Number(offer.hourly);
    return offer.hourly && Number.isFinite(value) ? value : Number.POSITIVE_INFINITY;
  };
  copy.sort((left, right) => {
    let diff = 0;
    if (sort === "cpu") diff = left.vcpu - right.vcpu;
    else if (sort === "memory") diff = left.memoryMb - right.memoryMb;
    else if (sort === "disk") diff = left.diskGb - right.diskGb;
    else if (sort === "name") diff = byName(left, right);
    else diff = price(left) - price(right);
    return diff === 0 ? byName(left, right) : diff;
  });
  return copy;
}

export function cheapestOffer(offers: Offer[]) {
  return sortOffers(offers, "price")[0];
}

export function OfferFields({
  offers,
  value,
  currency,
  allowEmpty,
  onChange,
}: {
  offers: Offer[];
  value: string;
  currency: string;
  allowEmpty?: boolean;
  onChange: (id: string) => void;
}) {
  const [sort, setSort] = useState<OfferSort>("price");
  const ordered = sortOffers(offers, sort);
  return (
    <>
      <Field label={text("ui.trier")}>
        <Select value={sort} onChange={(event) => setSort(event.target.value as OfferSort)}>
          <option value="price">{text("ui.prix")}</option>
          <option value="cpu">{text("ui.processeur")}</option>
          <option value="memory">{text("ui.memoire")}</option>
          <option value="disk">{text("common.disk")}</option>
          <option value="name">{text("field.name")}</option>
        </Select>
      </Field>
      <Field label={text("field.type")}>
        <Select value={value} onChange={(event) => onChange(event.target.value)}>
          {allowEmpty ? <option value="">{text("common.choose")}</option> : null}
          {ordered.map((item) => (
            <option key={item.id} value={item.id}>
              {offerOptionLabel(item, currency)}
            </option>
          ))}
        </Select>
      </Field>
    </>
  );
}

// OfferStorage asks for the storage an offer takes: its size when the offer
// needs one, or an optional extra volume with its price per GB.
export function OfferStorage({
  offer,
  period,
  currency,
  storageId,
  storageGb,
  onChange,
}: {
  offer: Offer;
  period: string;
  currency: string;
  storageId: string;
  storageGb: string;
  onChange: (storageId: string, storageGb: string) => void;
}) {
  const options = offer.storage ?? [];
  const required = options.find((item) => item.required);
  const optional = options.filter((item) => !item.required);
  const selected = required ?? optional.find((item) => item.id === storageId);
  if (!required && optional.length === 0) return null;
  const perGb = (item: NonNullable<Offer["storage"]>[number]) =>
    period === "month" && item.monthlyPerGb
      ? `${formatPrice(item.monthlyPerGb, false, currency)}/${say("unit.gb")}`
      : period !== "month" && item.hourlyPerGb
        ? `${formatPrice(item.hourlyPerGb, true, currency)}/${say("unit.gb")}`
        : "";
  return (
    <>
      {required ? null : (
        <Field label={text("field.extraStorage")}>
          <Select
            value={storageId}
            onChange={(e) => {
              const next = optional.find((item) => item.id === e.target.value);
              onChange(e.target.value, next ? String(next.minGb) : "");
            }}
          >
            <option value="">{text("common.none")}</option>
            {optional.map((item) => (
              <option key={item.id} value={item.id}>
                {item.label}
                {perGb(item) ? ` · ${perGb(item)}` : ""}
              </option>
            ))}
          </Select>
        </Field>
      )}
      {selected ? (
        <Field label={required ? `${required.label} (${say("unit.gb")})` : text("field.sizeGb")} hint={required && perGb(required) ? perGb(required) : undefined}>
          <Input
            type="number"
            inputMode="numeric"
            min={selected.minGb}
            max={selected.maxGb || undefined}
            value={storageGb || String(selected.minGb)}
            onChange={(e) => onChange(selected.id, e.target.value)}
            required
          />
        </Field>
      ) : null}
    </>
  );
}

// chosenStorage is the storage to ask for: the one the offer needs, or the
// extra volume picked, at its smallest size when none was typed.
export function chosenStorage(offer: Offer | undefined, storageId: string, storageGb: string) {
  const options = offer?.storage ?? [];
  const required = options.find((item) => item.required);
  const selected = required ?? options.find((item) => item.id === storageId);
  if (!selected) return { id: "", gb: 0 };
  const size = Number(storageGb);
  return { id: selected.id, gb: Number.isFinite(size) && size > 0 ? size : selected.minGb };
}
