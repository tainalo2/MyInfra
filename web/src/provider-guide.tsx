import { useEffect, useRef, type ReactNode } from "react";
import { ExternalLink, X } from "lucide-react";
import { text } from "./i18n";
import { Icon } from "./ui";

const focus =
  "focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-2 focus-visible:outline-focus";

function Mark({ children }: { children: ReactNode }) {
  return <strong className="font-semibold">{children}</strong>;
}

type Access = { method: string; path: string };

type Guide = {
  title: string;
  href: string;
  link: string;
  steps: ReactNode[];
  rights: ReactNode;
  access?: Access[];
  accessHeaders?: [string, string];
  accessMono?: boolean;
};

const cloudAccess: Access[] = [
  { method: "GET", path: "/me" },
  { method: "GET", path: "/cloud/project" },
  { method: "GET", path: "/cloud/project/*" },
  { method: "POST", path: "/cloud/project/*" },
  { method: "PUT", path: "/cloud/project/*" },
  { method: "DELETE", path: "/cloud/project/*" },
];

const dnsAccess: Access[] = [
  { method: "GET", path: "/me" },
  { method: "GET", path: "/domain/zone" },
  { method: "GET", path: "/domain/zone/*" },
  { method: "POST", path: "/domain/zone/*" },
  { method: "PUT", path: "/domain/zone/*" },
  { method: "DELETE", path: "/domain/zone/*" },
];

const guides: Record<string, Guide> = {
  hetzner: {
    title: "Hetzner",
    href: "https://console.hetzner.cloud/",
    link: "Console Hetzner Cloud",
    steps: [
      <>
        Ouvrez le projet, puis <Mark>Security</Mark>, puis <Mark>API tokens</Mark>.
      </>,
      <>Générez un jeton et copiez-le tout de suite. Hetzner ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        Choisissez <Mark>Read & Write</Mark>. <Mark>Read</Mark> permet de consulter le projet, <Mark>Write</Mark> est
        nécessaire pour créer une machine.
      </>
    ),
  },
  scaleway: {
    title: "Scaleway",
    href: "https://console.scaleway.com/iam/api-keys",
    link: "Clés API Scaleway",
    steps: [
      <>
        Ouvrez <Mark>IAM & API keys</Mark>, onglet <Mark>API keys</Mark>, puis <Mark>Generate API key</Mark>.
      </>,
      <>
        Copiez l'<Mark>access key</Mark>, qui commence par <Mark>SCW</Mark>, dans <Mark>Access key</Mark>. Elle s'affiche en entier dans la liste.
      </>,
      <>
        Copiez la <Mark>secret key</Mark> dans <Mark>Clé secrète</Mark>. Elle ne réapparaît pas.
      </>,
      <>
        L'identifiant du projet se trouve dans les réglages du projet. Collez-le dans <Mark>Projet</Mark>.
      </>,
      <>
        Choisissez une expiration <Mark>Never</Mark> pour garder le compte relié.
      </>,
    ],
    rights: (
      <>
        Une clé créée pour le propriétaire de l'organisation suffit. Pour une application, la politique doit donner{" "}
        <Mark>InstancesFullAccess</Mark> sur le projet, et la lecture de ce projet.
      </>
    ),
  },
  ovh: {
    title: "OVH",
    href: "https://eu.api.ovh.com/createToken/?GET=/me&GET=/cloud/project&GET=/cloud/project/*&POST=/cloud/project/*&PUT=/cloud/project/*&DELETE=/cloud/project/*",
    link: "Création des clés OVH",
    steps: [
      <>
        Connectez-vous avec le compte Europe. La page ouvre la section <Mark>Rights</Mark> déjà remplie.
      </>,
      <>
        Donnez un nom dans <Mark>Application name</Mark> et une <Mark>Validity</Mark> illimitée.
      </>,
      <>
        Après création, copiez les trois valeurs. Le secret et la consumer key ne réapparaissent pas.
      </>,
      <>
        <Mark>Application key</Mark> va dans <Mark>Clé d'application</Mark>, <Mark>Application secret</Mark> dans{" "}
        <Mark>Secret d'application</Mark>, <Mark>Consumer key</Mark> dans <Mark>Consumer key</Mark>.
      </>,
    ],
    rights: (
      <>
        Dans <Mark>Rights</Mark>, une ligne par accès.
      </>
    ),
    access: cloudAccess,
  },
  "dns_token:cloudflare": {
    title: "Cloudflare",
    href: "https://dash.cloudflare.com/profile/api-tokens?permissionGroupKeys=%5B%7B%22key%22%3A%22dns%22%2C%22type%22%3A%22edit%22%7D%5D&accountId=%2A&zoneId=all&name=MyInfra%20DNS",
    link: "Jetons API Cloudflare",
    steps: [
      <>
        La page prépare un jeton <Mark>DNS Write</Mark>. Donnez-lui un nom, puis créez-le.
      </>,
      <>
        Dans <Mark>Zone Resources</Mark>, limitez les zones aux domaines que MyInfra doit gérer, ou laissez toutes les
        zones.
      </>,
      <>Copiez le jeton tout de suite. Cloudflare ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        <Mark>DNS Write</Mark> permet de créer, modifier et supprimer les enregistrements. Le modèle s'appelle{" "}
        <Mark>Edit zone DNS</Mark>.
      </>
    ),
  },
  "dns_token:ovh": {
    title: "OVH",
    href: "https://eu.api.ovh.com/createToken/?GET=/me&GET=/domain/zone&GET=/domain/zone/*&POST=/domain/zone/*&PUT=/domain/zone/*&DELETE=/domain/zone/*",
    link: "Création des clés OVH",
    steps: [
      <>
        Connectez-vous avec le compte Europe. La page ouvre la section <Mark>Rights</Mark> déjà remplie.
      </>,
      <>
        Donnez un nom dans <Mark>Application name</Mark> et une <Mark>Validity</Mark> illimitée.
      </>,
      <>
        Après création, copiez les trois valeurs. Le secret et la consumer key ne réapparaissent pas.
      </>,
      <>
        <Mark>Application key</Mark> va dans <Mark>Clé d'application</Mark>, <Mark>Application secret</Mark> dans{" "}
        <Mark>Secret d'application</Mark>, <Mark>Consumer key</Mark> dans <Mark>Consumer key</Mark>.
      </>,
    ],
    rights: (
      <>
        Dans <Mark>Rights</Mark>, une ligne par accès.
      </>
    ),
    access: dnsAccess,
  },
  "git_token:github": {
    title: "GitHub",
    href: "https://github.com/settings/personal-access-tokens/new",
    link: "Nouveau jeton GitHub",
    steps: [
      <>
        Créez un jeton <Mark>fine-grained</Mark> et donnez-lui accès aux dépôts à déployer.
      </>,
      <>
        Dans <Mark>Repository permissions</Mark>, mettez <Mark>Contents</Mark> sur <Mark>Read-only</Mark>.
      </>,
      <>Copiez le jeton tout de suite. GitHub ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        <Mark>Contents</Mark> en <Mark>Read-only</Mark> suffit pour lire le dépôt. Avec un jeton classic, le scope{" "}
        <Mark>repo</Mark> couvre les dépôts privés.
      </>
    ),
  },
  digitalocean: {
    title: "DigitalOcean",
    href: "https://cloud.digitalocean.com/account/api/tokens",
    link: "Jetons API DigitalOcean",
    accessHeaders: ["Droit", "Pour"],
    accessMono: false,
    steps: [
      <>
        Ouvrez <Mark>API</Mark>, puis <Mark>Tokens</Mark>.
      </>,
      <>
        <Mark>Generate New Token</Mark>. Donnez un nom et choisissez <Mark>Custom scopes</Mark>.
      </>,
      <>Cochez les droits du tableau, puis générez le jeton. DigitalOcean ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        Un jeton <Mark>Full Access</Mark> convient aussi. Avec <Mark>Custom scopes</Mark>, les droits du tableau
        suffisent pour lire le catalogue et créer une machine.
      </>
    ),
    access: [
      { method: "account:read", path: "Vérifier le compte" },
      { method: "sizes:read", path: "Lire les types de machine" },
      { method: "regions:read", path: "Lire les lieux" },
      { method: "droplet:create", path: "Créer une machine" },
      { method: "droplet:read", path: "Lire les machines" },
      { method: "droplet:delete", path: "Supprimer une machine" },
      { method: "volume:create", path: "Créer un volume" },
      { method: "volume:read", path: "Lire les volumes" },
      { method: "ssh_key:read", path: "Lire les clés SSH" },
    ],
  },
  infomaniak: {
    title: "Infomaniak",
    href: "https://docs.infomaniak.cloud/identity/applications_credentials/",
    link: "Identifiants d'application Infomaniak",
    accessHeaders: ["Service", "Accès"],
    steps: [
      <>
        Ouvrez Horizon, puis <Mark>Identity</Mark>, puis <Mark>Application Credentials</Mark>.
      </>,
      <>
        <Mark>Create Application Credential</Mark>. Choisissez le rôle <Mark>member</Mark>. Sur la région <Mark>dc4-a</Mark>, ajoutez aussi <Mark>reader</Mark>.
      </>,
      <>
        Copiez l'<Mark>ID</Mark> et le <Mark>Secret</Mark>. Le secret ne réapparaît pas.
      </>,
      <>
        Collez l'ID dans <Mark>Identifiant</Mark> et le secret dans <Mark>Secret</Mark>.
      </>,
    ],
    rights: (
      <>
        <Mark>member</Mark> permet de créer une machine. Sur <Mark>dc4-a</Mark>, <Mark>reader</Mark> est nécessaire pour
        lire le catalogue. Sans règles d'accès, l'identifiant a tous les droits du rôle. Pour les limiter, utilisez le
        tableau.
      </>
    ),
    access: [
      { method: "compute", path: "GET /v2.1/flavors/**" },
      { method: "compute", path: "POST /v2.1/servers" },
      { method: "compute", path: "GET /v2.1/servers/**" },
      { method: "compute", path: "DELETE /v2.1/servers/**" },
      { method: "volumev3", path: "GET /v3/**" },
      { method: "volumev3", path: "POST /v3/**" },
      { method: "volumev3", path: "DELETE /v3/**" },
    ],
  },
  upcloud: {
    title: "UpCloud",
    href: "https://hub.upcloud.com/account/api-tokens",
    link: "Jetons API UpCloud",
    steps: [
      <>
        Dans le hub, ouvrez <Mark>Account</Mark>, puis <Mark>API tokens</Mark>, et créez un jeton.
      </>,
      <>
        Copiez-le tout de suite, il commence par <Mark>ucat_</Mark>. UpCloud ne le réaffiche pas.
      </>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        Le jeton doit pouvoir lire le catalogue et créer des serveurs. Si vous limitez ses adresses IP, autorisez celle de
        la machine qui fait tourner MyInfra.
      </>
    ),
  },
  vultr: {
    title: "Vultr",
    href: "https://my.vultr.com/settings/#settingsapi",
    link: "Clés API Vultr",
    steps: [
      <>
        Ouvrez <Mark>Account</Mark>, puis <Mark>API</Mark>, et activez l'accès API.
      </>,
      <>Créez une clé. Vultr ne la réaffiche pas.</>,
      <>
        Collez-la dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        La clé doit pouvoir lire le compte et créer des instances. Une clé en lecture seule ne suffit pas pour ouvrir une machine.
      </>
    ),
  },
  akamai: {
    title: "Akamai",
    href: "https://cloud.linode.com/profile/tokens",
    link: "Jetons Akamai Connected Cloud",
    steps: [
      <>
        Akamai Connected Cloud reprend Linode. Dans Cloud Manager, ouvrez le profil, puis <Mark>API Tokens</Mark>.
      </>,
      <>
        Créez un jeton personnel avec le droit <Mark>Read/Write</Mark> sur les Linodes. Copiez-le tout de suite.
      </>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        Le jeton sert à lister les régions et les tailles, puis à créer et supprimer une machine Debian. Le CDN Akamai n'est pas utilisé.
      </>
    ),
  },
  exoscale: {
    title: "Exoscale",
    href: "https://portal.exoscale.com/iam/api-keys",
    link: "Clés API Exoscale",
    steps: [
      <>
        Dans le portail, ouvrez <Mark>IAM</Mark>, puis <Mark>API Keys</Mark>, et choisissez <Mark>Add</Mark>.
      </>,
      <>
        Donnez un nom et un rôle qui autorise le service <Mark>Compute</Mark> : le rôle <Mark>Compute</Mark> convient.
      </>,
      <>
        Copiez la clé, qui commence par <Mark>EXO</Mark>, et le secret. Exoscale ne réaffiche pas le secret.
      </>,
      <>
        Collez la clé dans <Mark>Clé API</Mark> et le secret dans <Mark>Secret API</Mark>.
      </>,
    ],
    rights: (
      <>
        La clé lit les zones, les types d'instance et les modèles, crée et supprime une instance, et crée un groupe de
        sécurité nommé <Mark>myinfra</Mark> qui laisse passer le trafic entrant. Une clé en lecture seule ne suffit pas.
      </>
    ),
  },
  gcore: {
    title: "Gcore",
    href: "https://portal.gcore.com/accounts/profile/api-tokens",
    link: "Jetons API Gcore",
    steps: [
      <>
        Dans le portail, ouvrez le profil, puis <Mark>API tokens</Mark>, et choisissez <Mark>Create token</Mark>.
      </>,
      <>
        Donnez un nom, une date d'expiration, et gardez l'accès à tout le compte, ou au moins au service{" "}
        <Mark>Cloud</Mark>.
      </>,
      <>Copiez le jeton tout de suite. Gcore ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        MyInfra travaille dans le projet par défaut du compte. Le jeton doit pouvoir lire le catalogue, créer et supprimer
        une instance, et gérer les groupes de sécurité. Les machines partagées et les serveurs dédiés sont écartés.
      </>
    ),
  },
  kamatera: {
    title: "Kamatera",
    href: "https://console.kamatera.com/service/api",
    link: "Clés API Kamatera",
    steps: [
      <>
        Dans la console, ouvrez <Mark>Settings</Mark>, puis <Mark>API Keys</Mark>, et créez une clé.
      </>,
      <>
        Copiez le <Mark>Client ID</Mark> et le <Mark>Secret</Mark>. Le secret ne réapparaît pas.
      </>,
      <>
        Collez le Client ID dans <Mark>Client ID</Mark> et le secret dans <Mark>Secret API</Mark>.
      </>,
    ],
    rights: (
      <>
        La clé agit avec votre compte : elle lit les centres de données et les images, crée et résilie un serveur. Les prix
        viennent du calculateur public de Kamatera, au mois et en dollars.
      </>
    ),
  },
  ionos: {
    title: "IONOS",
    href: "https://docs.ionos.com/cloud/set-up-ionos-cloud/management/token-manager",
    link: "Jetons IONOS Cloud",
    steps: [
      <>
        Dans le DCD, ouvrez <Mark>Management</Mark>, puis <Mark>Token Manager</Mark>.
      </>,
      <>
        Choisissez <Mark>Generate Token</Mark>, puis une durée de validité.
      </>,
      <>Copiez le jeton tout de suite. Il est long, et IONOS ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton API</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        Le jeton agit avec votre utilisateur, qui doit avoir le droit de créer des centres de données et des serveurs.
        Chaque machine ouvre son propre centre de données, supprimé avec elle. L'API n'indique pas les prix : MyInfra
        affiche les tarifs publics des serveurs vCPU.
      </>
    ),
  },
  "git_token:gitlab": {
    title: "GitLab",
    href: "https://gitlab.com/-/user_settings/personal_access_tokens",
    link: "Jetons GitLab",
    steps: [
      <>
        Créez un <Mark>personal access token</Mark>.
      </>,
      <>
        Cochez <Mark>read_repository</Mark> et <Mark>read_api</Mark>, puis choisissez une date dans <Mark>Expiration date</Mark>.
      </>,
      <>Copiez le jeton tout de suite. GitLab ne le réaffiche pas.</>,
      <>
        Collez-le dans <Mark>Jeton</Mark>. Le nom dans MyInfra sert à reconnaître le compte.
      </>,
    ],
    rights: (
      <>
        <Mark>read_repository</Mark> permet de lire le dépôt. <Mark>read_api</Mark> permet à MyInfra de vérifier le
        compte.
      </>
    ),
  },
};

function guideFor(kind: string, provider: string) {
  if (kind === "provider_token") return guides[provider];
  return guides[`${kind}:${provider}`];
}

function AccessTable({ rows, headers, mono = true }: { rows: Access[]; headers?: [string, string]; mono?: boolean }) {
  const [left, right] = headers ?? ["Méthode", "Chemin"];
  return (
    <table className="mt-4 w-full border-collapse text-left">
      <thead>
        <tr className="border-b border-line">
          <th className="py-1 pr-6 font-semibold">{left}</th>
          <th className="py-1 font-semibold">{right}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={`${row.method}-${row.path}`} className="border-b border-line">
            <td className="py-1 pr-6">
              <Mark>{row.method}</Mark>
            </td>
            <td className={`py-1 ${mono ? "font-mono text-[13px]" : ""}`}>{row.path}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function ProviderGuide({ kind, provider, onClose }: { kind: string; provider: string; onClose: () => void }) {
  const closeRef = useRef<HTMLButtonElement>(null);
  const guide = guideFor(kind, provider);

  const close = useRef(onClose);
  close.current = onClose;

  useEffect(() => {
    closeRef.current?.focus();
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") close.current();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  if (!guide) return null;

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-ink/40 px-6" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="provider-guide-title"
        className="max-h-[92vh] w-full max-w-[560px] overflow-auto rounded-md border border-line bg-surface p-6"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="mb-6 flex items-start justify-between gap-6">
          <h2 id="provider-guide-title" className="text-2xl font-medium">
            {guide.title}
          </h2>
          <button ref={closeRef} type="button" aria-label={text("common.close")} className={`text-ink ${focus}`} onClick={onClose}>
            <Icon icon={X} />
          </button>
        </div>
        <a href={guide.href} target="_blank" rel="noreferrer" className={`inline-flex items-center gap-2 font-semibold underline ${focus}`}>
          {guide.link}
          <Icon icon={ExternalLink} />
        </a>
        <ol className="mt-4 flex list-decimal flex-col gap-2 pl-5">
          {guide.steps.map((step, index) => (
            <li key={index}>{step}</li>
          ))}
        </ol>
        <p className="mt-4 text-muted">{guide.rights}</p>
        {guide.access ? (
          <AccessTable rows={guide.access} headers={guide.accessHeaders} mono={guide.accessMono !== false} />
        ) : null}
      </div>
    </div>
  );
}
