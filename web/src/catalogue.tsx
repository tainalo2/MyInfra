import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Plus } from "lucide-react";
import { api, type CatalogueCard, type CatalogueGroup, type CatalogueVersions, type PackageRow } from "./api";
import { cardMatches, categoryName, localizeCard } from "./catalogue-copy";
import { knownMessage, text } from "./i18n";
import { AppLogo } from "./app-logo";
import { CommunityTag } from "./library-tag";
import { ImageOrigin, imageSource } from "./registries";
import { Button, ErrorText, Fold, Icon, Input, LoadingCards, LoadingLines, PageTitle, TextButton, useTrail, focus } from "./ui";

export function CatalogueBrowser({ selected, onSelect }: { selected: string; onSelect: (article: CatalogueCard) => void }) {
  const [groups, setGroups] = useState<CatalogueGroup[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("");
  const [community, setCommunity] = useState(false);

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
        if (community && !article.community) return false;
        return cardMatches(article, needle);
      }),
    );
  }, [groups, query, category, community]);
  const shared = (groups ?? []).some((group) => group.articles.some((article) => article.community));

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
              <Filter key={group.name} label={categoryName(group.name, group.translations)} active={category === group.name} onClick={() => setCategory(group.name)} />
            ))}
            {shared ? <Filter label={text("library.community")} active={community} onClick={() => setCommunity(!community)} /> : null}
          </div>
          {articles.length === 0 ? <p className="text-muted">{text("apps.noArticle")}</p> : null}
          <ul className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {articles.map((article) => (
              <li key={article.id} className={`catalogue-card flex h-full flex-col gap-4 rounded-md border p-4 ${selected === article.id ? "border-ink bg-status" : "border-line bg-surface"}`}>
                <button type="button" aria-pressed={selected === article.id} onClick={() => onSelect(article)} className="flex flex-1 flex-col gap-3 text-left">
                  <span className="flex items-center gap-3">
                    <AppLogo slug={article.logo} name={article.name} size={40} />
                    <span className="font-medium">{article.name}</span>
                    {article.community ? <CommunityTag /> : null}
                  </span>
                  <span className="text-muted">
                    {categoryName(article.category, article.categoryWords)}
                    {publishedImage(article) ? ` · ${imageSource(publishedImage(article)).label}` : ""}
                  </span>
                  <span>{localizeCard(article).summary}</span>
                </button>
                <Link to={`/catalogue/${article.id}`} className="text-ink">
                  {text("catalogue.details")}
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



// publishedImage is the image that answers the visitors, the one an app is known by.
function publishedImage(article: CatalogueCard) {
  return (article.services.find((service) => service.published) ?? article.services[0])?.image ?? "";
}

function Filter({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`shrink-0 rounded-md border px-3 py-1 ${active ? "border-ink bg-ink text-canvas" : "border-line bg-surface text-muted"} ${focus}`}
    >
      {label}
    </button>
  );
}

export function CatalogueArticle() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [article, setArticle] = useState<CatalogueCard | null>(null);
  const [versions, setVersions] = useState<CatalogueVersions | null>(null);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const [copying, setCopying] = useState(false);

  // A copy becomes the person's own package, to change at will.
  async function copy() {
    setCopying(true);
    try {
      const row = await api<PackageRow>(`/api/catalogue/${id}/copy`, { method: "POST" });
      navigate(`/applications/paquets/${row.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
      setCopying(false);
    }
  }

  useEffect(() => {
    api<CatalogueCard>(`/api/catalogue/${id}`)
      .then(setArticle)
      .catch((err) => setError(err.message))
      .finally(() => setReady(true));
    api<CatalogueVersions>(`/api/catalogue/${id}/versions`)
      .then(setVersions)
      .catch(() => setVersions({ name: "", image: "", tags: [], detail: text("err.versions") }));
  }, [id]);

  useTrail([
    { label: text("nav.apps"), to: "/applications" },
    { label: text("crumb.newApp"), to: "/nouveau" },
    { label: article?.name || text("crumb.application") },
  ]);

  if (!ready) return <LoadingLines lines={4} />;
  if (!article) return <ErrorText>{error || text("err.article_missing")}</ErrorText>;

  const published = article.services.find((service) => service.published) ?? article.services[0];
  const tag = versions?.tags?.includes("latest") ? "latest" : versions?.tags?.[0] || "latest";
  const chosen = published ? withTag(published.image, tag) : "";
  const shownCard = localizeCard(article);

  return (
    <>
      <PageTitle
        title={article.name}
        mark={<AppLogo slug={article.logo} name={article.name} size={48} />}
        action={
          <span className="flex items-center gap-4">
            <TextButton type="button" onClick={() => void copy()} disabled={copying}>
              {text("package.copy")}
            </TextButton>
            <Button type="button" onClick={() => navigate(`/nouveau?article=${article.id}`)}>
              <Icon icon={Plus} />
              {text("catalogue.install")}
            </Button>
          </span>
        }
      />
      <p className="mb-6 text-muted">{shownCard.summary}</p>
      <div className="flex flex-col gap-8 lg:flex-row lg:items-start">
        <div className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-8">
          {error ? <ErrorText>{error}</ErrorText> : null}
          <p>{shownCard.description}</p>
          <section className="flex flex-col gap-2">
            <h2>{text("catalogue.installs")}</h2>
            <ul className="overflow-hidden rounded-md border border-line bg-surface">
              {article.services.map((service) => (
                <li key={service.name} className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-t border-line px-4 py-3 first:border-t-0">
                  <span>{service.name}</span>
                  <span className="text-[13px] text-muted">
                    {service.published ? text("plan.link.proxy") : text("common.internal")}
                    {service.disk ? ` · ${text("common.disk")}` : ""}
                  </span>
                </li>
              ))}
            </ul>
            {article.keepsFiles ? <p className="text-[13px] text-muted">{text("apps.keeps")}</p> : null}
          </section>
          {versions?.detail ? <p className="text-bad">{knownMessage(versions.detail)}</p> : null}
          {article.community ? (
            <p className="flex items-center gap-2 text-muted">
              <CommunityTag />
              {text("library.communityHint")}
            </p>
          ) : null}
          {article.changes ? <p className="text-[13px] text-faint">{article.changes}</p> : null}
          <Fold title={text("catalogue.technical")}>
            <ul className="flex flex-col gap-3">
              {article.services.map((service) => {
                const shown = service.published && chosen ? chosen : service.image;
                return (
                  <li key={service.name}>
                    <span className="font-mono text-[13px]">{service.name}</span>
                    <span className="text-[13px] text-muted">{service.published ? ` · ${text("common.published")}` : ` · ${text("common.internal")}`}{service.disk ? ` · ${text("common.disk")}` : ""}</span>
                    <p className="mt-1">
                      <ImageOrigin image={shown} />
                    </p>
                  </li>
                );
              })}
            </ul>
          </Fold>
        </div>
        <aside className="flex flex-col gap-6 lg:sticky lg:top-4 lg:w-[280px] lg:shrink-0">
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 rounded-md border border-line bg-surface p-4 text-[13px]">
            <dt className="text-muted">{text("catalogue.category")}</dt>
            <dd>{categoryName(article.category, article.categoryWords)}</dd>
            <dt className="text-muted">{text("field.version")}</dt>
            <dd className="break-all font-mono">{tag}</dd>
            <dt className="text-muted">{text("catalogue.sheet")}</dt>
            <dd>{text("library.from", { version: article.version })}</dd>
            {article.site ? (
              <>
                <dt className="text-muted">{text("catalogue.site")}</dt>
                <dd className="min-w-0 break-all">
                  <a className={`text-link ${focus}`} href={article.site} target="_blank" rel="noreferrer">
                    {siteLabel(article.site)}
                  </a>
                </dd>
              </>
            ) : null}
          </dl>
          <SameCategory article={article} />
        </aside>
      </div>
    </>
  );
}

// SameCategory lists a few other applications of the article's category.
function SameCategory({ article }: { article: CatalogueCard }) {
  const [groups, setGroups] = useState<CatalogueGroup[] | null>(null);
  useEffect(() => {
    api<CatalogueGroup[]>("/api/catalogue").then(setGroups).catch(() => setGroups([]));
  }, []);
  const near = (groups ?? []).flatMap((group) => group.articles).filter((item) => item.category === article.category && item.id !== article.id).slice(0, 4);
  if (near.length === 0) return null;
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-[13px] text-muted">{text("catalogue.same")}</h2>
      <ul className="flex flex-col gap-1">
        {near.map((item) => (
          <li key={item.id}>
            <Link to={`/catalogue/${item.id}`} className={`-mx-2 flex items-center gap-2 rounded-md px-2 py-1 text-ink ${focus}`}>
              <AppLogo slug={item.logo} name={item.name} size={24} />
              {item.name}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
