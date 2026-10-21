import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowRightLeft, CheckCircle2, PowerOff, XCircle } from "lucide-react";
import { ApiError, api, type Catalog, type Cluster, type DnsZone, type Project, type Secret } from "./api";
import { CancelDeployment } from "./deployments";
import { knownMessage, text, type MsgKey } from "./i18n";
import { useActivity } from "./activity";
import { useI18n } from "./lang";
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

type PodState = { role: "myinfra" | "database"; name: string; state: string; tone: "ok" | "wait" | "bad" };

type Transfer = {
  status: string;
  step?: string;
  url?: string;
  method?: string;
  error?: string;
  // What the waiting step waits for: the new copy, or its HTTPS certificate.
  wait?: "starting" | "certificate" | "dns";
  phase?: string;
  pods?: PodState[];
  startedAt?: string;
  stepAt?: string;
  checkedAt?: string;
  now?: string;
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
  // A transfer cut short left its namespace in the cluster: it can be removed.
  const [leftover, setLeftover] = useState(false);
  const [cleaning, setCleaning] = useState(false);
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
      setLeftover(err instanceof ApiError && err.code === "transfer_already");
      setStarting(false);
    }
  }

  async function clean() {
    setCleaning(true);
    try {
      await api(`/api/clusters/${clusterId}/transfer/clean`, { method: "POST" });
      setLeftover(false);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    } finally {
      setCleaning(false);
    }
  }

  const catalog = preview?.catalog;
  const offer = catalog?.offers.find((item) => item.id === offerId);
  const createdCluster = clusters.find((item) => item.id === clusterId);

  return (
    <>
      <PageTitle title={text("transfer.title")} icon={ArrowRightLeft} />
      {error ? (
        <div className="mb-8 flex flex-col gap-3">
          <ErrorText>{error}</ErrorText>
          {leftover ? (
            <div className="flex flex-wrap items-center gap-4">
              <Button type="button" variant="secondary" onClick={() => void clean()} disabled={cleaning}>
                {cleaning ? <Spinner /> : null}
                {text("transfer.clean")}
              </Button>
              <span className="text-[13px] text-muted">{text("transfer.cleanHint")}</span>
            </div>
          ) : null}
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
  useI18n();
  const { data, error } = usePoll<Transfer>("/api/transfer", 2000, {
    stopWhen: (next) => ["done", "failed", "cancelled", "idle"].includes(next.status),
  });
  const { activity } = useActivity();
  const transfer = data ?? { status: "idle" };
  // The clock of the server measures the steps: this screen's own may differ.
  const skew = transfer.now ? Date.now() - Date.parse(transfer.now) : 0;
  const [, tick] = useState(0);
  useEffect(() => {
    const timer = window.setInterval(() => tick((value) => value + 1), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const since = (at?: string) => (at ? Math.max(0, Date.now() - skew - Date.parse(at)) : 0);

  const steps = transfer.method === "pod"
    ? [text("ui.preparationDuPod"), text("ui.demarrageDuPod"), text("ui.copieDesDonnees"), text("ui.arretDeCetteCopie")]
    : [text("ui.ouvertureDeLaMachine"), text("transfer.booting"), text("ui.copieDesDonnees"), text("ui.arretDeCetteCopie")];
  const order = ["creating", "waiting", "copying", "destroying", "done"];
  // The server names the step it failed at; each name belongs to one row.
  const index = transfer.status === "failed" ? failedStep(transfer.step || "") : order.indexOf(transfer.status);
  const running = ["creating", "waiting", "copying", "destroying"].includes(transfer.status);
  const quiet = since(transfer.checkedAt);

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
        <div className="flex max-w-[720px] flex-col gap-6">
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
            {running ? (
              quiet < 45_000 ? (
                <span className="inline-flex items-center gap-2 text-ok">
                  <span className="relative inline-flex size-2.5">
                    <span className="absolute inline-flex size-full animate-ping rounded-full bg-ok opacity-60" aria-hidden />
                    <span className="relative inline-flex size-2.5 rounded-full bg-ok" aria-hidden />
                  </span>
                  {text("transfer.alive", { seconds: Math.round(quiet / 1000) })}
                </span>
              ) : (
                <span className="inline-flex items-center gap-2 text-warn">
                  <span className="inline-block size-2.5 rounded-full bg-warn" aria-hidden />
                  {text("transfer.quiet", { duration: duration(quiet) })}
                </span>
              )
            ) : transfer.status === "done" ? (
              <span className="text-ok">{text("transfer.finished")}</span>
            ) : null}
            {transfer.startedAt ? <span className="text-muted">{text("transfer.elapsed", { duration: duration(since(transfer.startedAt)) })}</span> : null}
          </div>
          {running && activity?.local ? (
            <p className="flex items-start gap-3 rounded-md border border-bad px-4 py-3">
              <Icon icon={PowerOff} className="mt-0.5 shrink-0 text-bad" />
              <span>{text("activity.keepOnText")}</span>
            </p>
          ) : null}
          <ol className="flex flex-col">
            {steps.map((label, step) => {
              const state = transfer.status === "failed" && step === index ? "failed" : step < index || transfer.status === "done" ? "done" : step === index ? "running" : "pending";
              return (
                <li key={label} className="flex gap-4">
                  <span className="flex flex-col items-center">
                    <StepMark state={state} />
                    {step < steps.length - 1 ? <span className={`min-h-6 w-px flex-1 ${state === "done" ? "bg-ok" : "bg-line"}`} /> : null}
                  </span>
                  <div className="min-w-0 flex-1 pb-6">
                    <p className="flex flex-wrap items-baseline justify-between gap-x-4">
                      <span className={state === "pending" ? "text-faint" : state === "failed" ? "text-bad" : ""}>{label}</span>
                      {state === "running" && transfer.stepAt ? <span className="text-[13px] text-muted">{duration(since(transfer.stepAt))}</span> : null}
                    </p>
                    {state === "running" ? <StepDetail transfer={transfer} /> : null}
                  </div>
                </li>
              );
            })}
          </ol>
          {running ? <p className="text-[13px] text-faint">{text("transfer.expected")}</p> : null}
        </div>
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

function duration(ms: number) {
  const seconds = Math.floor(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  if (minutes === 0) return text("transfer.seconds", { seconds });
  return text("transfer.minutes", { minutes, seconds: String(seconds % 60).padStart(2, "0") });
}

function StepMark({ state }: { state: "done" | "running" | "failed" | "pending" }) {
  if (state === "done") return <Icon icon={CheckCircle2} className="text-ok" />;
  if (state === "failed") return <Icon icon={XCircle} className="text-bad" />;
  if (state === "running") return <Spinner />;
  return <span className="mt-1.5 inline-block size-3 rounded-full border border-line" aria-hidden />;
}

const podTone: Record<PodState["tone"], string> = { ok: "bg-ok", wait: "animate-pulse bg-warn", bad: "bg-bad" };

// StepDetail says what the running step does: its phase, what it waits
// for, and where each pod of the new copy is.
function StepDetail({ transfer }: { transfer: Transfer }) {
  const phase = transfer.phase ? text(`transfer.phase.${transfer.phase}` as MsgKey) : "";
  const wait =
    transfer.status === "waiting" && transfer.wait
      ? transfer.wait === "dns"
        ? text("transfer.waitDns")
        : transfer.wait === "certificate"
          ? text("transfer.waitCertificate")
          : text("transfer.waitStarting")
      : "";
  const pods = transfer.pods ?? [];
  const allReady = pods.length > 0 && pods.every((pod) => pod.tone === "ok");
  return (
    <div className="mt-2 flex flex-col gap-2 text-[13px]">
      {phase && !(transfer.status === "waiting" && transfer.phase === "pods") ? <p className="text-muted">{phase}</p> : null}
      {wait ? <p className="text-muted">{allReady && transfer.wait === "starting" ? text("transfer.podsReadyWaiting") : wait}</p> : null}
      {pods.length > 0 ? (
        <ul className="flex flex-col gap-1">
          {pods.map((pod) => (
            <li key={pod.name} className="flex items-baseline gap-2">
              <span className={`inline-block size-2 shrink-0 rounded-full ${podTone[pod.tone]}`} aria-hidden />
              <span className="min-w-0">
                <span className="text-muted">{text(pod.role === "database" ? "transfer.role.database" : "transfer.role.myinfra")}</span>{" "}
                <span className="font-mono">{pod.name}</span>
                {" · "}
                <span className={pod.tone === "bad" ? "text-bad" : pod.tone === "ok" ? "text-ok" : ""}>{podStateLabel(pod.state)}</span>
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {transfer.note ? <p className="text-warn">{transfer.note}</p> : null}
    </div>
  );
}

function podStateLabel(state: string) {
  if (state === "ready" || state === "starting" || state === "database") return text(`transfer.pod.${state}` as MsgKey);
  if (state.startsWith("unschedulable: ")) return text("transfer.pod.unschedulable", { detail: state.slice("unschedulable: ".length) });
  return state;
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
