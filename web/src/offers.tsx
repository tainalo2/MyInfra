import { useState } from "react";
import type { Offer } from "./api";
import { Field, Select } from "./ui";
import { text, currentLocale } from "./i18n";

export type OfferSort = "price" | "cpu" | "memory" | "disk" | "name";

export function formatPrice(raw: string | undefined, hour: boolean, currency = "EUR") {
  if (!raw) return "";
  const value = Number(raw);
  if (!Number.isFinite(value)) return raw;
  const digits = value < 0.001 ? 6 : hour && value < 1 ? 4 : 2;
  const text = value.toLocaleString(currentLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const unit = currency === "USD" ? "$" : "€";
  return hour ? `${text} ${unit}/h` : `${text} ${unit}/mois`;
}

function memory(mb: number) {
  if (mb >= 1024) return `${(mb / 1024).toLocaleString(currentLocale(), { maximumFractionDigits: 1 })} Go`;
  return `${mb} Mo`;
}

export function offerOptionLabel(offer: Offer, currency: string) {
  const disk = offer.diskGb > 0 ? ` · ${offer.diskGb} Go` : "";
  const hourly = offer.hourly ? ` · ${formatPrice(offer.hourly, true, currency)}` : "";
  return `${offer.name} · ${offer.vcpu} vCPU · ${memory(offer.memoryMb)}${disk}${hourly}`;
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
