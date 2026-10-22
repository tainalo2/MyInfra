import { Link, useNavigate } from "react-router-dom";
import { useState } from "react";
import { LayoutGrid } from "lucide-react";
import { api, type Application } from "./api";
import { ConfirmDialog, StatusBadge, appHealth, useToast } from "./kit";
import { UpdateBadge, useUpdate } from "./updates";
import { usePoll } from "./poll";
import { text } from "./i18n";
import { AppLogo } from "./app-logo";
import { RemovalNotice } from "./destructions";
import { useSession } from "./session";
import { Button, EmptyList, ErrorText, Icon, ItemList, ListModeButton, LoadingCards, PageTitle, TextButton, entryClass, removalOf, useDestructions, useListMode } from "./ui";
import { Plus } from "lucide-react";

export function Home() {
  const navigate = useNavigate();
  const { canDeploy } = useSession();
  const { data: apps, setData: setApps, error } = usePoll<Application[]>("/api/applications", 8000);
  const [mode, toggleMode] = useListMode();
  const jobs = useDestructions();
  const toast = useToast();
  const [pausing, setPausing] = useState<Application | null>(null);

  async function setPaused(app: Application, paused: boolean) {
    const next = await api<Application>(`/api/applications/${app.id}/pause`, { method: "POST", body: JSON.stringify({ paused }) });
    setApps((current) => current?.map((item) => (item.id === app.id ? { ...item, ...next } : item)) ?? current);
    setPausing(null);
    toast(paused ? text("apps.pausedDone", { name: app.name }) : text("apps.resumedDone", { name: app.name }));
  }

  return (
    <>
      <PageTitle
        title={text("nav.apps")}
        icon={LayoutGrid}
        action={
          apps && apps.length > 0 ? (
            <div className="flex items-center gap-4">
              <span className="hidden md:inline-flex">
                <ListModeButton mode={mode} onToggle={toggleMode} />
              </span>
              {canDeploy ? (
                <Button type="button" onClick={() => navigate("/nouveau")}>
                  <Icon icon={Plus} />
                  {text("home.new")}
                </Button>
              ) : null}
            </div>
          ) : null
        }
      />
      {error && !apps ? <ErrorText>{error}</ErrorText> : null}
      {apps === null && !error ? <LoadingCards /> : null}
      {apps && apps.length === 0 ? (
        canDeploy ? <EmptyList message={text("home.empty")} label={text("home.new")} onCreate={() => navigate("/nouveau")} /> : <p className="text-muted">{text("home.empty")}</p>
      ) : null}
      {apps && apps.length > 0 ? (
        <ItemList mode={mode}>
          {apps.map((app) => (
            <li key={app.id} className={mode === "cards" ? "h-full" : undefined}>
              <div className={`${entryClass(mode)} h-full`}>
                <Link to={`/applications/${app.id}`} className="flex items-center gap-3">
                  <AppLogo slug={app.logo ?? ""} name={app.name} size={36} />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{app.name}</span>
                    <span className="flex flex-wrap items-center gap-2">
                      <StatusBadge status={appHealth(app)} />
                      <AppBadge id={app.id} />
                    </span>
                  </span>
                </Link>
                <RemovalNotice job={removalOf(jobs, "application", app.id)} />
                {app.hostname ? (
                  <a href={`https://${app.hostname}`} target="_blank" rel="noreferrer" className="break-all text-muted underline">
                    {app.hostname}
                  </a>
                ) : null}
                {app.guarded || !canDeploy ? null : (
                  <div>
                    <TextButton type="button" onClick={() => (app.paused ? void setPaused(app, false).catch((err) => toast(err.message, "bad")) : setPausing(app))}>
                      {app.paused ? text("home.resume") : text("apps.pauseAction")}
                    </TextButton>
                  </div>
                )}
              </div>
            </li>
          ))}
        </ItemList>
      ) : null}
      {pausing ? (
        <ConfirmDialog title={text("apps.pauseTitle", { name: pausing.name })} confirmLabel={text("apps.pauseAction")} onConfirm={() => setPaused(pausing, true)} onClose={() => setPausing(null)}>
          {text("apps.pauseHint")}
        </ConfirmDialog>
      ) : null}
    </>
  );
}

// AppBadge says that a new version of an application is available.
function AppBadge({ id }: { id: string }) {
  const item = useUpdate("app", id);
  return item ? <UpdateBadge item={item} /> : null;
}
