import { text } from "./i18n";
import { Field, Input } from "./ui";

// Credentials are the keys a provider asks for. Each provider names them its
// own way; the server reads them from these fields.
export type Credentials = {
  token: string;
  keyId: string;
  projectId: string;
  applicationKey: string;
  applicationSecret: string;
  consumerKey: string;
};

export const emptyCredentials: Credentials = { token: "", keyId: "", projectId: "", applicationKey: "", applicationSecret: "", consumerKey: "" };

const apiToken = ["hetzner", "cloudflare", "digitalocean", "vultr", "akamai", "upcloud", "gcore", "ionos"];

const billing: Record<string, Parameters<typeof text>[0]> = {
  hetzner: "ui.facturationALHeurePlafonneeAu2",
  digitalocean: "ui.facturationALaSecondePlafonneeAu",
  upcloud: "ui.facturationALHeureLeMois",
  scaleway: "ui.facturationALHeureLeMois",
  vultr: "ui.facturationALHeurePlafonneeAu",
  akamai: "ui.akamaiConnectedCloudLinodeFacturationA",
  exoscale: "ui.facturationALHeureLeDisque",
  gcore: "ui.facturationALHeureMyinfraUtilise",
  kamatera: "ui.facturationAuMoisTraficInclusPrix",
  ionos: "ui.facturationALaMinuteUneMachine",
  infomaniak: "ui.facturationALHeureEntameeLe",
};

const plain = { autoComplete: "off", spellCheck: false } as const;

// CredentialFields asks for the keys of one provider, with how it bills.
export function CredentialFields({
  kind,
  provider,
  value,
  onChange,
}: {
  kind: string;
  provider: string;
  value: Credentials;
  onChange: (value: Credentials) => void;
}) {
  const set = (key: keyof Credentials) => (event: { target: { value: string } }) => onChange({ ...value, [key]: event.target.value });
  const note = kind === "provider_token" ? billing[provider] : undefined;

  let fields;
  if (provider === "ovh") {
    fields = (
      <>
        <Field label={text("provider.appKey")}>
          <Input value={value.applicationKey} onChange={set("applicationKey")} required {...plain} />
        </Field>
        <Field label={text("provider.appSecret")}>
          <Input type="password" value={value.applicationSecret} onChange={set("applicationSecret")} required />
        </Field>
        <Field label={text("provider.consumerKey")}>
          <Input type="password" value={value.consumerKey} onChange={set("consumerKey")} required />
        </Field>
        <p className="text-faint">{kind === "dns_token" ? text("ui.endpointOvhEuLesZonesDns") : text("ui.endpointOvhEuPublicCloudA")}</p>
      </>
    );
  } else if (provider === "scaleway") {
    fields = (
      <>
        <Field label={text("provider.accessKey")}>
          <Input value={value.keyId} onChange={set("keyId")} required {...plain} />
        </Field>
        <Field label={text("provider.secretKey")}>
          <Input type="password" value={value.token} onChange={set("token")} required />
        </Field>
        <Field label={text("provider.project")}>
          <Input value={value.projectId} onChange={set("projectId")} required {...plain} />
        </Field>
      </>
    );
  } else if (provider === "proxmox") {
    fields = (
      <>
        <Field label={text("ui.adresse")}>
          <Input value={value.applicationKey} onChange={set("applicationKey")} required placeholder="https://pve.exemple:8006" {...plain} />
        </Field>
        <Field label={text("ui.jetonApi")}>
          <Input type="password" value={value.token} onChange={set("token")} required />
        </Field>
      </>
    );
  } else if (provider === "openstack") {
    fields = (
      <>
        <Field label={text("ui.adresseKeystone")}>
          <Input value={value.token} onChange={set("token")} required {...plain} />
        </Field>
        <Field label={text("provider.id")}>
          <Input value={value.applicationKey} onChange={set("applicationKey")} required {...plain} />
        </Field>
        <Field label={text("crumb.secret")}>
          <Input type="password" value={value.applicationSecret} onChange={set("applicationSecret")} required />
        </Field>
        <Field label={text("provider.project")}>
          <Input value={value.projectId} onChange={set("projectId")} {...plain} />
        </Field>
      </>
    );
  } else if (provider === "infomaniak" || provider === "exoscale" || provider === "kamatera") {
    fields = (
      <>
        <Field label={provider === "exoscale" ? text("provider.apiKey") : provider === "kamatera" ? text("provider.clientId") : text("provider.id")}>
          <Input value={value.applicationKey} onChange={set("applicationKey")} required {...plain} />
        </Field>
        <Field label={provider === "infomaniak" ? text("crumb.secret") : text("provider.apiSecret")}>
          <Input type="password" value={value.applicationSecret} onChange={set("applicationSecret")} required />
        </Field>
      </>
    );
  } else {
    fields = (
      <Field label={apiToken.includes(provider) ? text("ui.jetonApi") : text("provider.token")}>
        <Input type="password" value={value.token} onChange={set("token")} required />
      </Field>
    );
  }

  return (
    <>
      {fields}
      {note ? <p className="text-faint">{text(note)}</p> : null}
    </>
  );
}
