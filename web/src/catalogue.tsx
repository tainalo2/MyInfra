import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Plus } from "lucide-react";
import { api, type CatalogueCard, type CatalogueGroup, type CatalogueVersions, type Cluster, type DnsZone, type Server } from "./api";
import { cardMatches, categoryName, localizeCard } from "./catalogue-copy";
import { knownMessage, text } from "./i18n";
import { AppLogo } from "./app-logo";
import { Button, ErrorText, Field, Fold, Icon, Input, LoadingCards, LoadingLines, PageTitle, Select, TextButton, useTrail } from "./ui";

export function CatalogueBrowser({ selected, onSelect }: { selected: string; onSelect: (article: CatalogueCard) => void }) {
  const [groups, setGroups] = useState<CatalogueGroup[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("");

  useEffect(() => {
    api<CatalogueGroup[]>("/api/catalogue")
      .then(setGroups)
      .catch((err) => setError(err.message));
  }, []);

  const articles = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (groups ?? []).flatMap((group) =>
      group.articles.filter((article) => {
        if (category && article.category !== category) return false;
        return cardMatches(article, needle);
      }),
    );
  }, [groups, query, category]);

  return (
    <>
      {error ? <p className="text-bad">{error}</p> : null}
      {groups === null && !error ? <LoadingCards /> : null}
      {groups ? (
        <div className="flex flex-col gap-8">
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={text("common.search")} aria-label={text("common.search")} />
          <div className="flex flex-wrap gap-2">
            <Filter label={text("common.all")} active={category === ""} onClick={() => setCategory("")} />
            {groups.map((group) => (
              <Filter key={group.name} label={categoryName(group.name)} active={category === group.name} onClick={() => setCategory(group.name)} />
            ))}
          </div>
          {articles.length === 0 ? <p className="text-muted">{text("apps.noArticle")}</p> : null}
          <ul className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {articles.map((article) => (
              <li key={article.id} className={`catalogue-card flex h-full flex-col gap-4 rounded-md border p-4 ${selected === article.id ? "border-ink bg-status" : "border-line bg-surface"}`}>
                <button type="button" aria-pressed={selected === article.id} onClick={() => onSelect(article)} className="flex flex-1 flex-col gap-3 text-left">
                  <span className="flex items-center gap-3">
                    <AppLogo slug={article.logo} name={article.name} size={40} />
                    <span className="font-medium">{article.name}</span>
                  </span>
                  <span className="text-muted">{categoryName(article.category)}</span>
                  <span>{localizeCard(article).summary}</span>
                </button>
                <Link to={`/catalogue/${article.id}`} className="text-ink">
                  {text("apps.more")}
                </Link>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </>
  );
}

function siteLabel(site: string) {
  try {
    const url = new URL(site);
    const host = url.hostname.replace(/^www\./, "");
    const path = url.pathname.replace(/\/$/, "");
    return path ? `${host}${path}` : host;
  } catch {
    return site.replace(/^https?:\/\//, "").replace(/\/$/, "");
  }
}

export function withTag(image: string, tag: string) {
  const slash = image.lastIndexOf("/");
  const colon = image.lastIndexOf(":");
  const base = colon > slash ? image.slice(0, colon) : image;
  return tag ? `${base}:${tag}` : image;
}

function imageRef(image: string) {
  const slash = image.lastIndexOf("/");
  const colon = image.lastIndexOf(":");
  const tagged = colon > slash;
  const name = tagged ? image.slice(0, colon) : image;
  const tag = tagged ? image.slice(colon + 1) : "";
  const version = tag.replace(/^version-/, "").replace(/^v(?=\d)/, "");
  return { version: version || tag, href: registryHref(name) };
}

function registryHref(name: string) {
  const parts = name.split("/");
  const host = parts[0]?.includes(".") ? parts[0] : "";
  if (host === "ghcr.io") {
    const owner = parts[1];
    const pkg = parts.slice(2).join("/");
    return `https://github.com/orgs/${owner}/packages/container/package/${pkg}`;
  }
  if (host === "quay.io") return `https://quay.io/repository/${parts.slice(1).join("/")}`;
  if (host === "lscr.io") return `https://docs.linuxserver.io/images/docker-${parts[parts.length - 1]}/`;
  if (host === "codeberg.org") return `https://codeberg.org/${parts[1]}/-/packages/container/${parts.slice(2).join("/")}`;
  if (host) return `https://${name}`;
  if (parts.length === 1) return `https://hub.docker.com/_/${name}`;
  return `https://hub.docker.com/r/${name}`;
}

function Filter({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-md border px-3 py-1 ${active ? "border-ink bg-status text-ink" : "border-line bg-surface text-muted"}`}
    >
      {label}
    </button>
  );
}

export function CatalogueArticle() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [article, setArticle] = useState<CatalogueCard | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [zones, setZones] = useState<DnsZone[]>([]);
  const [ready, setReady] = useState(false);
  const [name, setName] = useState("");
  const [names, setNames] = useState<{ subdomain: string; zone: string }[]>([]);
  const [destination, setDestination] = useState("server");
  const [serverId, setServerId] = useState("");
  const [clusterId, setClusterId] = useState("");
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [versions, setVersions] = useState<CatalogueVersions | null>(null);
  const [tag, setTag] = useState("latest");
  const [tagTouched, setTagTouched] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    Promise.all([
      api<CatalogueCard>(`/api/catalogue/${id}`),
      api<Server[]>("/api/servers"),
      api<Cluster[]>("/api/clusters"),
      api<DnsZone[]>("/api/dns/zones"),
    ])
      .then(([card, nextServers, nextClusters, nextZones]) => {
        const lone = nextServers.filter((server) => !server.clusterId);
        const created = nextClusters.filter((cluster) => cluster.origin !== "imported");
        setArticle(card);
        setName(card.name);
        setServers(lone);
        setClusters(created);
        setZones(nextZones);
        if (lone[0]) setServerId(lone[0].id);
        if (created[0]) setClusterId(created[0].id);
        if (lone.length === 0 && created.length > 0) setDestination("cluster");
        if (nextZones[0]) setNames([{ subdomain: "", zone: nextZones[0].name }]);
      })
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
    api<CatalogueVersions>(`/api/catalogue/${id}/versions`)
      .then(setVersions)
      .catch(() => setVersions({ name: "", image: "", tags: [], detail: text("err.versions") }));
  }, [id]);

  useEffect(() => {
    if (!versions || tagTouched) return;
    if (versions.tags.includes("latest")) setTag("latest");
    else if (versions.tags[0]) setTag(versions.tags[0]);
  }, [versions, tagTouched]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const created = await api<{ id: string }>(`/api/catalogue/${id}`, {
        method: "POST",
        body: JSON.stringify({
          name,
          names: names.filter((item) => item.subdomain.trim() !== ""),
          destinationKind: destination,
          serverId: destination === "server" ? serverId : "",
          clusterId: destination === "cluster" ? clusterId : "",
          inputs,
          tag,
        }),
      });
      navigate(`/applications/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  useTrail([
    { label: text("nav.apps"), to: "/" },
    { label: text("crumb.newApp"), to: "/nouveau" },
    { label: article?.name || text("crumb.application") },
  ]);

  if (!ready) return <LoadingLines lines={4} />;
  if (!article) return <p className="text-bad">{error || text("err.article_missing")}</p>;

  const published = article.services.find((service) => service.published) ?? article.services[0];
  const chosen = published ? withTag(published.image, tag) : "";
  const release = chosen ? imageRef(chosen) : null;
  const tags = versions?.tags?.length ? versions.tags : tag ? [tag] : [];

  const shownCard = localizeCard(article);

  return (
    <>
      <PageTitle title={article.name} mark={<AppLogo slug={article.logo} name={article.name} size={48} />} />
      <div className="mb-8 flex max-w-[720px] flex-col gap-4">
        <p>{shownCard.description}</p>
        <p>
          <a className="text-link" href={article.site} target="_blank" rel="noreferrer">
            {siteLabel(article.site)}
          </a>
        </p>
        {release ? (
          <p>
            <a className="text-link" href={release.href} target="_blank" rel="noreferrer">
              {chosen}
            </a>
          </p>
        ) : null}
        {versions?.detail ? <p className="text-bad">{knownMessage(versions.detail)}</p> : null}
        <p className="text-muted">{categoryName(article.category)}</p>
        {article.keepsFiles ? <p>{text("apps.keeps")}</p> : null}
        <Fold title={text("field.services")}>
          <ul className="flex flex-col gap-3">
            {article.services.map((service) => {
              const shown = service.published && chosen ? chosen : service.image;
              const image = imageRef(shown);
              return (
                <li key={service.name}>
                  <span>{service.name}</span>
                  <span className="text-muted">{service.published ? ` · ${text("common.published")}` : ` · ${text("common.internal")}`}{service.disk ? ` · ${text("common.disk")}` : ""}</span>
                  <p className="mt-1">
                    <a className="text-link" href={image.href} target="_blank" rel="noreferrer">
                      {shown}
                    </a>
                  </p>
                </li>
              );
            })}
          </ul>
        </Fold>
      </div>
      <form onSubmit={onSubmit} className="flex max-w-[720px] flex-col gap-6">
        <Field label={text("field.name")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field label={text("field.version")}>
          <Select
            value={tag}
            onChange={(e) => {
              setTagTouched(true);
              setTag(e.target.value);
            }}
          >
            {tags.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={text("field.domain")}>
          {zones.length === 0 ? (
            <p className="text-muted">
              {text("apps.noDomain")} <Link to="/dns">{text("apps.linkDns")}</Link>
            </p>
          ) : (
            <div className="flex flex-col gap-3">
              {names.map((item, index) => (
                <div key={index} className="flex items-stretch">
                  <input
                    value={item.subdomain}
                    placeholder={text("field.subdomain")}
                    onChange={(e) => setNames((current) => current.map((row, i) => (i === index ? { ...row, subdomain: e.target.value } : row)))}
                    className="min-w-0 flex-1 rounded-l-md rounded-r-none border border-line bg-surface px-3 py-2 outline-none focus:border-focus"
                  />
                  <span className="flex items-center border-y border-line bg-surface px-2 text-muted">.</span>
                  <select
                    value={item.zone}
                    onChange={(e) => setNames((current) => current.map((row, i) => (i === index ? { ...row, zone: e.target.value } : row)))}
                    className="max-w-[55%] rounded-l-none rounded-r-md border border-l-0 border-line bg-surface px-3 py-2 outline-none focus:border-focus"
                  >
                    {zones.map((zone) => (
                      <option key={zone.accountId + zone.name} value={zone.name}>
                        {zone.name}
                      </option>
                    ))}
                  </select>
                  {names.length > 1 ? (
                    <span className="ml-3 self-center">
                      <TextButton type="button" onClick={() => setNames((current) => current.filter((_, i) => i !== index))}>
                        {text("common.remove")}
                      </TextButton>
                    </span>
                  ) : null}
                </div>
              ))}
              <div>
                <TextButton type="button" onClick={() => setNames((current) => [...current, { subdomain: "", zone: zones[0].name }])}>
                  <Icon icon={Plus} />
                  {text("field.addName")}
                </TextButton>
              </div>
            </div>
          )}
        </Field>
        <Field label={text("field.where")}>
          <Select value={destination} onChange={(e) => setDestination(e.target.value)}>
            <option value="server">{text("common.oneMachine")}</option>
            <option value="cluster">{text("common.oneCluster")}</option>
          </Select>
        </Field>
        {destination === "server" ? (
          servers.length === 0 ? (
            <p className="text-muted">
              {text("apps.noMachine")} <Link to="/clusters">{text("apps.addMachine")}</Link>
            </p>
          ) : (
            <Field label={text("field.machine")}>
              <Select value={serverId} onChange={(e) => setServerId(e.target.value)}>
                {servers.map((server) => (
                  <option key={server.id} value={server.id}>
                    {server.name}
                  </option>
                ))}
              </Select>
            </Field>
          )
        ) : clusters.length === 0 ? (
          <p className="text-muted">
            {text("apps.noCluster")} <Link to="/clusters">{text("apps.createCluster")}</Link>
          </p>
        ) : (
          <Field label={text("field.cluster")}>
            <Select value={clusterId} onChange={(e) => setClusterId(e.target.value)}>
              {clusters.map((cluster) => (
                <option key={cluster.id} value={cluster.id}>
                  {cluster.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
        {shownCard.inputs.map((input) => (
          <Field key={input.key} label={input.label}>
            <Input value={inputs[input.key] ?? ""} onChange={(e) => setInputs((current) => ({ ...current, [input.key]: e.target.value }))} required />
          </Field>
        ))}
        {error ? <ErrorText>{error}</ErrorText> : null}
        <div>
          <Button type="submit">{text("apps.install")}</Button>
        </div>
      </form>
    </>
  );
}
