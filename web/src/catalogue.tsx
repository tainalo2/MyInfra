import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Plus } from "lucide-react";
import { api, type CatalogueCard, type CatalogueGroup, type CatalogueVersions } from "./api";
import { cardMatches, categoryName, localizeCard } from "./catalogue-copy";
import { knownMessage, text } from "./i18n";
import { AppLogo } from "./app-logo";
import { ImageOrigin, imageSource } from "./registries";
import { Button, ErrorText, Fold, Icon, Input, LoadingCards, LoadingLines, PageTitle, useTrail } from "./ui";

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
                  <span className="text-muted">
                    {categoryName(article.category)}
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
  const [versions, setVersions] = useState<CatalogueVersions | null>(null);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");

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
          <Button type="button" onClick={() => navigate(`/nouveau?article=${article.id}`)}>
            <Icon icon={Plus} />
            {text("catalogue.install")}
          </Button>
        }
      />
      <div className="flex max-w-[720px] flex-col gap-4">
        <p>{shownCard.description}</p>
        <p>
          <a className="text-link" href={article.site} target="_blank" rel="noreferrer">
            {siteLabel(article.site)}
          </a>
        </p>
        {chosen ? (
          <p>
            <ImageOrigin image={chosen} />
          </p>
        ) : null}
        {versions?.detail ? <p className="text-bad">{knownMessage(versions.detail)}</p> : null}
        <p className="text-muted">{categoryName(article.category)}</p>
        {article.keepsFiles ? <p>{text("apps.keeps")}</p> : null}
        <Fold title={text("field.services")}>
          <ul className="flex flex-col gap-3">
            {article.services.map((service) => {
              const shown = service.published && chosen ? chosen : service.image;
              return (
                <li key={service.name}>
                  <span>{service.name}</span>
                  <span className="text-muted">{service.published ? ` · ${text("common.published")}` : ` · ${text("common.internal")}`}{service.disk ? ` · ${text("common.disk")}` : ""}</span>
                  <p className="mt-1">
                    <ImageOrigin image={shown} />
                  </p>
                </li>
              );
            })}
          </ul>
        </Fold>
      </div>
    </>
  );
}
