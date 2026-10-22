import { Link } from "react-router-dom";
import { ArrowRightLeft, Cloud, Container, Globe, HardDrive, KeyRound, ListTree, Settings, Shield, Users, Waypoints, type LucideIcon } from "lucide-react";
import { text, type MsgKey } from "./i18n";
import { useI18n } from "./lang";
import { useSession } from "./session";
import { Icon, PageTitle, focus } from "./ui";
import { VersionNote } from "./updates";

type Entry = { to: string; label: MsgKey; hint: MsgKey; icon: LucideIcon; adminOnly?: boolean; deployOnly?: boolean };

const groups: { title: MsgKey; entries: Entry[] }[] = [
  {
    title: "settings.connect",
    entries: [
      { to: "/providers", label: "nav.providers", hint: "settings.providersHint", icon: Cloud, deployOnly: true },
      { to: "/dns", label: "nav.dns", hint: "settings.dnsHint", icon: Globe, deployOnly: true },
      { to: "/dns/enregistrements", label: "nav.dnsRecords", hint: "settings.dnsRecordsHint", icon: ListTree, deployOnly: true },
      { to: "/registres", label: "nav.registries", hint: "settings.registriesHint", icon: Container, deployOnly: true },
      { to: "/images", label: "nav.images", hint: "settings.imagesHint", icon: HardDrive, deployOnly: true },
    ],
  },
  {
    title: "settings.security",
    entries: [
      { to: "/cles", label: "nav.ssh", hint: "settings.sshHint", icon: KeyRound, deployOnly: true },
      { to: "/secrets", label: "nav.secrets", hint: "settings.secretsHint", icon: Shield, deployOnly: true },
      { to: "/utilisateurs", label: "nav.users", hint: "settings.usersHint", icon: Users, adminOnly: true },
    ],
  },
  {
    title: "settings.myinfra",
    entries: [
      { to: "/transfert", label: "nav.transfer", hint: "settings.transferHint", icon: ArrowRightLeft, adminOnly: true },
      { to: "/copies", label: "nav.copies", hint: "settings.copiesHint", icon: Waypoints, adminOnly: true },
    ],
  },
];

function visible(entry: Entry, canDeploy: boolean, canAdmin: boolean) {
  if (entry.adminOnly && !canAdmin) return false;
  if (entry.deployOnly && !canDeploy) return false;
  return true;
}

export function SettingsPage() {
  useI18n();
  const { canDeploy, canAdmin } = useSession();
  return (
    <>
      <PageTitle title={text("nav.settings")} icon={Settings} />
      <div className="flex max-w-[960px] flex-col gap-10">
        {groups.map((group) => {
          const entries = group.entries.filter((entry) => visible(entry, canDeploy, canAdmin));
          if (entries.length === 0) return null;
          return (
            <section key={group.title}>
              <h2 className="mb-3 text-muted">{text(group.title)}</h2>
              <ul className="grid gap-3 sm:grid-cols-2">
                {entries.map((entry) => (
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
          );
        })}
        <VersionNote />
      </div>
    </>
  );
}
