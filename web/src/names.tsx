import { Plus } from "lucide-react";
import type { DnsZone } from "./api";
import { text } from "./i18n";
import { Group, Icon, TextButton, focus } from "./ui";

export type AppName = { subdomain: string; zone: string };

const joined = `min-w-0 border border-line bg-surface px-3 py-2 outline-none focus:border-focus ${focus}`;

// NameList edits the addresses of an application: a subdomain in one of the
// zones of the DNS accounts. Each row says what it is to a screen reader.
export function NameList({
  names,
  zones,
  onChange,
  keepOne = false,
  hint,
}: {
  names: AppName[];
  zones: DnsZone[];
  onChange: (names: AppName[]) => void;
  // keepOne leaves the last row in place: a new application starts with one.
  keepOne?: boolean;
  hint?: string;
}) {
  const update = (index: number, next: Partial<AppName>) => onChange(names.map((row, i) => (i === index ? { ...row, ...next } : row)));
  return (
    <Group label={text("field.domain")} hint={hint}>
      <div className="flex flex-col gap-3">
        {names.map((item, index) => (
          <div key={index} className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <div className="flex min-w-0 flex-1 basis-64 items-stretch">
              <input
                value={item.subdomain}
                placeholder={text("field.subdomain")}
                aria-label={text("names.subdomain", { index: index + 1 })}
                autoCapitalize="off"
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => update(index, { subdomain: e.target.value })}
                className={`flex-1 rounded-l-md rounded-r-none ${joined}`}
              />
              <span className="flex items-center border-y border-line bg-surface px-2 text-muted" aria-hidden>
                .
              </span>
              <select
                value={item.zone}
                aria-label={text("names.zone", { index: index + 1 })}
                onChange={(e) => update(index, { zone: e.target.value })}
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
            {keepOne && names.length <= 1 ? null : (
              <TextButton type="button" onClick={() => onChange(names.filter((_, i) => i !== index))}>
                {text("common.remove")}
              </TextButton>
            )}
          </div>
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
export function namesOf(domains: { hostname: string; kind: string; zone?: string }[] = []): AppName[] {
  return domains
    .filter((domain) => domain.kind === "custom")
    .map((domain) => {
      const zone = domain.zone ?? "";
      const subdomain = zone && domain.hostname.endsWith(`.${zone}`) ? domain.hostname.slice(0, -zone.length - 1) : domain.hostname === zone ? "" : domain.hostname;
      return { subdomain, zone };
    });
}
