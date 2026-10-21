import { Link } from "react-router-dom";
import { ArrowRightLeft, Cloud, Container, Globe, HardDrive, KeyRound, ListTree, Lock, Settings, Shield, Waypoints, type LucideIcon } from "lucide-react";
import { text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { Icon, PageTitle, focus } from "./ui";
import { VersionNote } from "./updates";

type Entry = { to: string; label: MsgKey; hint: MsgKey; icon: LucideIcon };

// The settings are set once and seldom touched again: they share one entry
// of the menu, grouped by what they are for.
const groups: { title: MsgKey; entries: Entry[] }[] = [
  {
    title: "settings.connect",
    entries: [
      { to: "/providers", label: "nav.providers", hint: "settings.providersHint", icon: Cloud },
      { to: "/dns", label: "nav.dns", hint: "settings.dnsHint", icon: Globe },
      { to: "/dns/enregistrements", label: "nav.dnsRecords", hint: "settings.dnsRecordsHint", icon: ListTree },
      { to: "/registres", label: "nav.registries", hint: "settings.registriesHint", icon: Container },
      { to: "/images", label: "nav.images", hint: "settings.imagesHint", icon: HardDrive },
    ],
  },
  {
    title: "settings.security",
    entries: [
      { to: "/cles", label: "nav.ssh", hint: "settings.sshHint", icon: KeyRound },
      { to: "/secrets", label: "nav.secrets", hint: "settings.secretsHint", icon: Shield },
      { to: "/compte", label: "nav.account", hint: "settings.accountHint", icon: Lock },
    ],
  },
  {
    title: "settings.myinfra",
    entries: [
      { to: "/transfert", label: "nav.transfer", hint: "settings.transferHint", icon: ArrowRightLeft },
      { to: "/copies", label: "nav.copies", hint: "settings.copiesHint", icon: Waypoints },
    ],
  },
];

export function SettingsPage() {
  useI18n();
  return (
    <>
      <PageTitle title={text("nav.settings")} icon={Settings} />
      <div className="flex max-w-[960px] flex-col gap-10">
        {groups.map((group) => (
          <section key={group.title}>
            <h2 className="mb-3 text-muted">{text(group.title)}</h2>
            <ul className="grid gap-3 sm:grid-cols-2">
              {group.entries.map((entry) => (
                <li key={entry.to}>
                  <Link to={entry.to} className={`card-link flex h-full items-start gap-3 rounded-md border border-line bg-canvas p-4 ${focus}`}>
                    <Icon icon={entry.icon} className="mt-0.5 shrink-0 text-muted" />
                    <span className="min-w-0">
                      <span className="block font-medium">{text(entry.label)}</span>
                      <span className="block text-[13px] text-muted">{text(entry.hint)}</span>
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        ))}
        <VersionNote />
      </div>
    </>
  );
}
