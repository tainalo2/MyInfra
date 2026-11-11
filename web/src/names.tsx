import type { ReactNode } from "react";
import { Plus } from "lucide-react";
import type { DnsZone } from "./api";
import { text } from "./i18n";
import { Group, Icon, TextButton, focus } from "./ui";

// service is the public service a name leads to; empty is the main one.
export type AppName = { subdomain: string; zone: string; service?: string };

// publicServices are the services of an application that get names, the
// main one first: the public one without names of its own.
export function publicServices(services: { name: string; published: boolean }[] = [], domains: { service?: string }[] = []) {
  const named = new Set(domains.map((domain) => domain.service ?? "").filter(Boolean));
  const publics = services.filter((service) => service.published).map((service) => service.name);
  const main = publics.find((name) => !named.has(name)) ?? publics[0] ?? "";
  return { main, others: publics.filter((name) => name !== main) };
}

const joined = `min-w-0 border border-line bg-surface px-3 py-2 outline-none focus:border-focus ${focus}`;

// NameRow is one address: a subdomain in one of the zones of the DNS accounts,
// and the service it leads to when there is a choice. It says what it is to a
// screen reader.
export function NameRow({
  item,
  index,
  zones,
  onChange,
  services,
  autoFocus,
  children,
}: {
  item: AppName;
  index: number;
  zones: DnsZone[];
  onChange: (next: Partial<AppName>) => void;
  services?: { main: string; others: string[] };
  autoFocus?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <div className="flex min-w-0 flex-1 basis-64 items-stretch">
        <input
          value={item.subdomain}
          placeholder={text("field.subdomain")}
          aria-label={text("names.subdomain", { index: index + 1 })}
          autoCapitalize="off"
          autoComplete="off"
          spellCheck={false}
          autoFocus={autoFocus}
          onChange={(e) => onChange({ subdomain: e.target.value })}
          className={`flex-1 rounded-l-md rounded-r-none ${joined}`}
        />
        <span className="flex items-center border-y border-line bg-surface px-2 text-muted" aria-hidden>
          .
        </span>
        <select
          value={item.zone}
          aria-label={text("names.zone", { index: index + 1 })}
          onChange={(e) => onChange({ zone: e.target.value })}
          className={`max-w-[55%] rounded-l-none rounded-r-md border-l-0 ${joined}`}
        >
          {zones.some((zone) => zone.name === item.zone) ? null : <option value={item.zone}>{item.zone || text("field.domain")}</option>}
          {zones.map((zone) => (
            <option key={zone.accountId + zone.name} value={zone.name}>
              {zone.name}
            </option>
          ))}
        </select>
      </div>
      {services && services.others.length > 0 ? (
        <select
          value={item.service ?? ""}
          aria-label={text("domain.service")}
          onChange={(e) => onChange({ service: e.target.value || undefined })}
          className={`rounded-md ${joined}`}
        >
          <option value="">{text("domain.mainService", { name: services.main })}</option>
          {services.others.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      ) : null}
      {children}
    </div>
  );
}

// NameList edits the addresses of an application that is not running yet.
export function NameList({
  names,
  zones,
  onChange,
  keepOne = false,
  hint,
  services,
}: {
  names: AppName[];
  zones: DnsZone[];
  onChange: (names: AppName[]) => void;
  // keepOne leaves the last row in place: a new application starts with one.
  keepOne?: boolean;
  hint?: string;
  // services are the public services a name may lead to, the main first;
  // with one or none, the choice is not shown.
  services?: { main: string; others: string[] };
}) {
  const update = (index: number, next: Partial<AppName>) => onChange(names.map((row, i) => (i === index ? { ...row, ...next } : row)));
  return (
    <Group label={text("field.domain")} hint={hint}>
      <div className="flex flex-col gap-3">
        {names.map((item, index) => (
          <NameRow key={index} item={item} index={index} zones={zones} onChange={(next) => update(index, next)} services={services}>
            {keepOne && names.length <= 1 ? null : (
              <TextButton type="button" onClick={() => onChange(names.filter((_, i) => i !== index))}>
                {text("common.remove")}
              </TextButton>
            )}
          </NameRow>
        ))}
        <div>
          <TextButton type="button" onClick={() => onChange([...names, { subdomain: "", zone: zones[0]?.name ?? "" }])}>
            <Icon icon={Plus} />
            {text("field.addName")}
          </TextButton>
        </div>
      </div>
    </Group>
  );
}

// namesOf turns the custom domains of an application back into rows.
export function namesOf(domains: { hostname: string; kind: string; zone?: string; service?: string }[] = []): AppName[] {
  return domains
    .filter((domain) => domain.kind === "custom")
    .map((domain) => {
      const zone = domain.zone ?? "";
      const subdomain = zone && domain.hostname.endsWith(`.${zone}`) ? domain.hostname.slice(0, -zone.length - 1) : domain.hostname === zone ? "" : domain.hostname;
      return domain.service ? { subdomain, zone, service: domain.service } : { subdomain, zone };
    });
}
