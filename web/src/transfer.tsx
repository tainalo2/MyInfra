import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowRightLeft } from "lucide-react";
import { api, type Catalog, type Cluster, type DnsZone, type Project, type Secret } from "./api";
import { CancelDeployment } from "./deployments";
import { knownMessage, text } from "./i18n";
import { OfferFields, OfferStorage, cheapestOffer, chosenStorage, formatPrice } from "./offers";
import { ProviderSelect } from "./provider-select";
import { Spinner } from "./kit";
import { usePoll } from "./poll";
import { Button, EmptyList, ErrorText, Field, Icon, Input, LoadingLines, PageTitle, Select } from "./ui";

type Preview = {
  accountId: string;
  provider: string;
  name: string;
  canHost: boolean;
  reason?: string;
  catalog: Catalog;
};

type Transfer = {
  status: string;
  step?: string;
  url?: string;
  method?: string;
  error?: string;
  // What the waiting step waits for: the new copy, or its HTTPS certificate.
  wait?: "starting" | "certificate" | "dns";
  // Why the waiting lasts: the reason Let's Encrypt gave, or a name put back.
  note?: string;
  keep?: boolean;
};

const running = ["creating", "waiting", "copying", "destroying"];

export function Transfer() {
  const navigate = useNavigate();
  const [accounts, setAccounts] = useState<Secret[]>([]);
  const [accountId, setAccountId] = useState("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsReady, setProjectsReady] = useState(false);
  const [projectId, setProjectId] = useState("");
  const [offersLoading, setOffersLoading] = useState(false);
  const [starting, setStarting] = useState(false);
  const [location, setLocation] = useState("");
  const [offerId, setOfferId] = useState("");
  const [period, setPeriod] = useState("hour");
  const [storageId, setStorageId] = useState("");
  const [storageGb, setStorageGb] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [method, setMethod] = useState<"machine" | "pod">("machine");
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [clusterId, setClusterId] = useState("");
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [zone, setZone] = useState("");
  // The name of the new copy in the zone: another copy may already hold myinfra.
  const [subdomain, setSubdomain] = useState("myinfra");
  // This copy stays, synced with the new one, and stops driving the machines.
  const [keep, setKeep] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancel = false;
    (async () => {
      try {
        const [secrets, current, rows, domains] = await Promise.all([
          api<Secret[]>("/api/secrets"),
          api<Transfer>("/api/transfer"),
          api<Cluster[]>("/api/clusters"),
          api<DnsZone[]>("/api/dns/zones"),
        ]);
        if (cancel) return;
        if (running.includes(current.status)) {
          navigate("/transfert/suivi", { replace: true });
          return;
        }
        const providers = secrets.filter((item) => item.kind === "provider_token");
        const created = rows.filter((item) => item.origin !== "imported");
        setAccounts(providers);
        setAccountId(providers[0]?.id ?? "");
        setClusters(created);
        setClusterId(created[0]?.id ?? "");
        setZones(domains);
        setZone(domains[0]?.name ?? "");
      } catch (err) {
        if (!cancel) setError(err instanceof Error ? err.message : text("common.error"));
      } finally {
        if (!cancel) setLoaded(true);
      }
    })();
    return () => {
      cancel = true;
    };
  }, [navigate]);

  useEffect(() => {
    if (!accountId) {
      setProjects([]);
      setProjectId("");
      setProjectsReady(true);
      return;
    }
    let cancel = false;
    setProjectsReady(false);
    api<Project[]>(`/api/providers/${accountId}/projects`)
      .then((rows) => {
        if (cancel) return;
        setProjects(rows);
        setProjectId(rows[0]?.id ?? "");
      })
      .catch((err) => {
        if (!cancel) {
          setProjects([]);
          setProjectId("");
          setError(err instanceof Error ? err.message : text("common.error"));
        }
      })
      .finally(() => {
        if (!cancel) setProjectsReady(true);
      });
    return () => {
      cancel = true;
    };
  }, [accountId]);

  useEffect(() => {
    if (!accountId) {
      setPreview(null);
      setOffersLoading(false);
      return;
    }
    if (!projectsReady || (projects.length > 0 && !projectId)) {
      setOffersLoading(true);
      return;
    }
    let cancel = false;
    setOffersLoading(true);
    const query = new URLSearchParams({ providerId: accountId });
    if (location) query.set("location", location);
    if (projectId) query.set("project", projectId);
    api<Preview>(`/api/transfer/preview?${query}`)
      .then((next) => {
        if (cancel) return;
        setPreview(next);
        setError("");
        const catalog = next.catalog;
        if (catalog.location && catalog.location !== location) setLocation(catalog.location);
        setOfferId((current) => (catalog.offers.some((offer) => offer.id === current) ? current : cheapestOffer(catalog.offers)?.id ?? ""));
        setPeriod((current) => (catalog.periods.includes(current) ? current : catalog.periods[0] ?? "hour"));
      })
      .catch((err) => {
        if (!cancel) {
          setPreview(null);
          setError(err instanceof Error ? err.message : text("common.error"));
        }
      })
      .finally(() => {
        if (!cancel) setOffersLoading(false);
      });
    return () => {
      cancel = true;
    };
  }, [accountId, location, projectId, projectsReady, projects.length]);

  async function start() {
    if (starting) return;
    setError("");
    setStarting(true);
    try {
      if (method === "pod") {
        await api<Transfer>(`/api/clusters/${clusterId}/transfer`, { method: "POST", body: JSON.stringify({ zone, subdomain: subdomain.trim(), keep }) });
      } else {
        const storage = chosenStorage(offer, storageId, storageGb);
        await api<Transfer>("/api/transfer", {
          method: "POST",
          body: JSON.stringify({
            providerId: accountId,
            offerId,
            location,
            projectId,
            period,
            storageId: storage.id,
            storageGb: storage.gb,
            keep,
          }),
        });
      }
      navigate("/transfert/suivi");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setStarting(false);
    }
  }

  const catalog = preview?.catalog;
  const offer = catalog?.offers.find((item) => item.id === offerId);
  const createdCluster = clusters.find((item) => item.id === clusterId);

  return (
    <>
      <PageTitle title={text("transfer.title")} icon={ArrowRightLeft} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {!loaded ? (
        <LoadingLines lines={4} />
      ) : (
        <div className="flex max-w-[720px] flex-col gap-6">
          <p className="text-muted">
            {text("ui.choisissezUneNouvelleMachineOuUn")}
          </p>
          <div className="flex gap-2">
            <button type="button" aria-pressed={method === "machine"} onClick={() => setMethod("machine")} className={`rounded-md border px-3 py-2 ${method === "machine" ? "border-ink bg-status" : "border-line"}`}>
              {text("ui.nouvelleMachine")}
            </button>
            <button type="button" aria-pressed={method === "pod"} onClick={() => setMethod("pod")} className={`rounded-md border px-3 py-2 ${method === "pod" ? "border-ink bg-status" : "border-line"}`}>
              {text("ui.podDansUnCluster")}
            </button>
          </div>
          {method === "pod" ? (
            clusters.length === 0 ? (
              <EmptyList message={text("transfer.noCluster")} label={text("nav.clusters")} onCreate={() => navigate("/clusters")} />
            ) : zones.length === 0 ? (
              <EmptyList message={text("transfer.noDns")} label="DNS" onCreate={() => navigate("/dns")} />
            ) : (
              <>
                <Field label={text("field.cluster")}>
                  <Select value={clusterId} onChange={(e) => setClusterId(e.target.value)}>
                    {clusters.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </Select>
                </Field>
                {zones.length > 1 ? (
                  <Field label={text("field.domain")}>
                    <Select value={zone} onChange={(e) => setZone(e.target.value)}>
                      {zones.map((item) => (
                        <option key={item.accountId + item.name} value={item.name}>
                          {item.name}
                        </option>
                      ))}
                    </Select>
                  </Field>
                ) : null}
                {zone ? (
                  <Field label={text("transfer.name")} hint={text("transfer.nameHint")}>
                    <span className="flex min-w-0 items-center gap-2">
                      <Input
                        value={subdomain}
                        onChange={(e) => setSubdomain(e.target.value.toLowerCase())}
                        autoCapitalize="off"
                        autoComplete="off"
                        spellCheck={false}
                        pattern="[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?"
                        required
                      />
                      <span className="shrink-0 text-muted">.{zone}</span>
                    </span>
                  </Field>
                ) : null}
                <KeepCopy keep={keep} onChange={setKeep} />
                <div>
                  <Button type="button" onClick={start} disabled={!createdCluster || !zone || !subdomain.trim() || starting}>
                    {starting ? <Spinner /> : <Icon icon={ArrowRightLeft} />}
                    {text("ui.transferer")}
                  </Button>
                </div>
              </>
            )
          ) : accounts.length === 0 ? (
            <EmptyList message={text("transfer.noProvider")} label={text("transfer.newProvider")} onCreate={() => navigate("/providers")} />
          ) : (
            <>
          <ProviderSelect
            label={text("ui.provider")}
            value={accountId}
            options={accounts.map((item) => ({ value: item.id, label: item.name, logo: item.provider }))}
            onChange={(value) => {
              setAccountId(value);
              setLocation("");
              setProjectId("");
              setProjects([]);
              setProjectsReady(false);
              setOfferId("");
              setPreview(null);
              setStorageId("");
              setStorageGb("");
            }}
          />
          {projectsReady && projects.length > 0 ? (
            <Field label={text("provider.project")}>
              <Select
                value={projectId}
                onChange={(e) => {
                  setProjectId(e.target.value);
                  setOfferId("");
                }}
              >
                {projects.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          {catalog && catalog.locations.length > 1 ? (
            <Field label={text("field.place")}>
              <Select value={location || catalog.location} onChange={(e) => setLocation(e.target.value)}>
                {catalog.locations.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.label}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          {!offersLoading && projectsReady && catalog && catalog.offers.length === 0 ? <p className="text-muted">{text("ui.aucuneMachineNEstProposeeA")}</p> : null}
          {accountId && (offersLoading || !projectsReady) ? (
            <LoadingLines lines={3} label={text("transfer.offers")} />
          ) : catalog && offer ? (
            <>
              <OfferFields
                offers={catalog.offers}
                value={offer.id}
                currency={catalog.currency}
                onChange={(id) => {
                  setOfferId(id);
                  setStorageId("");
                  setStorageGb("");
                }}
              />
              <p className="text-muted">
                {offer.hourly ? formatPrice(offer.hourly, true, catalog.currency) : null}
                {offer.hourly && offer.monthly ? " · " : null}
                {offer.monthly ? formatPrice(offer.monthly, false, catalog.currency) : null}
              </p>
              <div className="flex gap-2">
                {offer.hourly ? (
                  <button
                    type="button"
                    aria-pressed={period === "hour"}
                    onClick={() => setPeriod("hour")}
                    className={`rounded-md border px-3 py-2 ${period === "hour" ? "border-ink bg-status" : "border-line"}`}
                  >
                    {text("ui.heure")}
                  </button>
                ) : null}
                {offer.monthly ? (
                  <button
                    type="button"
                    aria-pressed={period === "month"}
                    onClick={() => setPeriod("month")}
                    className={`rounded-md border px-3 py-2 ${period === "month" ? "border-ink bg-status" : "border-line"}`}
                  >
                    {text("ui.mois")}
                  </button>
                ) : null}
              </div>
              <OfferStorage
                offer={offer}
                period={period}
                currency={catalog.currency}
                storageId={storageId}
                storageGb={storageGb}
                onChange={(id, gb) => {
                  setStorageId(id);
                  setStorageGb(gb);
                }}
              />
              {catalog.note ? <p className="text-faint">{catalog.note}</p> : null}
              {preview?.reason ? <p className="text-muted">{preview.reason}</p> : null}
            </>
          ) : null}
          {preview?.canHost && offer ? (
            <>
              <KeepCopy keep={keep} onChange={setKeep} />
              <div>
                <Button type="button" onClick={start} disabled={starting}>
                  {starting ? <Spinner /> : <Icon icon={ArrowRightLeft} />}
                  {text("ui.transferer")}
                </Button>
              </div>
            </>
          ) : null}
            </>
          )}
        </div>
      )}
    </>
  );
}

export function TransferFollow() {
  const { data, error } = usePoll<Transfer>("/api/transfer", 2000, {
    stopWhen: (next) => ["done", "failed", "cancelled", "idle"].includes(next.status),
  });
  const transfer = data ?? { status: "idle" };

  const steps = transfer.method === "pod"
    ? [text("ui.preparationDuPod"), text("ui.demarrageDuPod"), text("ui.copieDesDonnees"), text("ui.arretDeCetteCopie")]
    : [text("ui.ouvertureDeLaMachine"), text("transfer.booting"), text("ui.copieDesDonnees"), text("ui.arretDeCetteCopie")];
  const order = ["creating", "waiting", "copying", "destroying", "done"];
  // The server names the step it failed at; each name belongs to one row.
  const index = transfer.status === "failed" ? failedStep(transfer.step || "") : order.indexOf(transfer.status);

  return (
    <>
      <PageTitle title={text("transfer.follow")} icon={ArrowRightLeft} />
      {error ? (
        <div className="mb-8">
          <ErrorText>{error}</ErrorText>
        </div>
      ) : null}
      {transfer.status === "idle" ? (
        <p className="text-muted">{text("transfer.none")}</p>
      ) : transfer.status === "cancelled" ? (
        <p className="text-muted">{text("deploy.cancelled")}</p>
      ) : (
        <ol className="flex max-w-[720px] flex-col gap-4">
          {steps.map((label, step) => {
            const state = transfer.status === "failed" && step === index ? text("status.failed") : step < index || transfer.status === "done" ? text("status.done") : step === index ? text("ui.enCours") : text("status.pending");
            return (
              <li key={label} className="flex items-baseline justify-between gap-4 border-b border-line py-3">
                <span>
                  {label}
                  {transfer.status === "waiting" && step === index && transfer.wait ? (
                    <span className="mt-1 block text-[13px] text-muted">
                      {transfer.wait === "dns" ? text("transfer.waitDns") : transfer.wait === "certificate" ? text("transfer.waitCertificate") : text("transfer.waitStarting")}
                      {transfer.note ? <span className="mt-1 block text-warn">{transfer.note}</span> : null}
                    </span>
                  ) : null}
                </span>
                <span className="text-muted">{state}</span>
              </li>
            );
          })}
        </ol>
      )}
      <CancelDeployment kind="transfer" id="transfer" active={transfer.status === "creating" || transfer.status === "waiting" || transfer.status === "copying"} />
      {transfer.error ? (
        <div className="mt-6">
          <ErrorText>{knownMessage(transfer.error)}</ErrorText>
        </div>
      ) : null}
      {transfer.status === "done" && transfer.url ? (
        <p className="mt-6 text-muted">
          {text("transfer.onlineAt")}{" "}
          <a className="font-semibold underline" href={transfer.url}>
            {transfer.url}
          </a>
          {transfer.keep ? text("transfer.thisStays") : text("transfer.thisStops")}
        </p>
      ) : null}
    </>
  );
}

function failedStep(step: string) {
  if (step.startsWith("Arrêt")) return 3;
  if (step.startsWith("Copie")) return 2;
  if (step.startsWith("Démarrage") || step.startsWith("La machine démarre")) return 1;
  return 0;
}

// KeepCopy asks whether this copy stays once the new one runs: synced with
// it, it no longer drives the machines. Useful to keep MyInfra on a laptop.
function KeepCopy({ keep, onChange }: { keep: boolean; onChange: (keep: boolean) => void }) {
  return (
    <label className="flex items-start gap-3">
      <input type="checkbox" checked={keep} onChange={(event) => onChange(event.target.checked)} className="mt-1" />
      <span>
        {text("transfer.keep")}
        <span className="block text-[13px] text-muted">{text("transfer.keepHint")}</span>
      </span>
    </label>
  );
}
