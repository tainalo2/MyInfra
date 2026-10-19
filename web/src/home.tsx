import { Link, useNavigate } from "react-router-dom";
import { useEffect, useState } from "react";
import { LayoutGrid } from "lucide-react";
import { api, peek, type Application } from "./api";
import { text } from "./i18n";
import { AppLogo } from "./app-logo";
import { RemovalNotice } from "./destructions";
import { EmptyList, HealthDot, ItemList, ListActions, LoadingCards, PageTitle, TextButton, entryClass, removalOf, useDestructions, useListMode } from "./ui";

export function Home() {
  const navigate = useNavigate();
  const [apps, setApps] = useState<Application[] | null>(() => peek<Application[]>("/api/applications"));
  const [error, setError] = useState("");
  const [mode, toggleMode] = useListMode();
  const jobs = useDestructions();

  useEffect(() => {
    let stop = false;
    const load = () => {
      api<Application[]>("/api/applications")
        .then((next) => {
          if (!stop) setApps(next);
        })
        .catch((err) => {
          if (!stop) setError(err.message);
        });
    };
    load();
    const timer = window.setInterval(load, 8000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, []);

  async function togglePause(app: Application) {
    if (app.guarded) return;
    try {
      const next = await api<Application>(`/api/applications/${app.id}/pause`, { method: "POST", body: JSON.stringify({ paused: !app.paused }) });
      setApps((current) => current?.map((item) => (item.id === app.id ? { ...item, ...next } : item)) ?? current);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : text("common.error"));
    }
  }

  return (
    <>
      <PageTitle
        title={text("nav.apps")}
        icon={LayoutGrid}
        action={
          apps && apps.length > 0 ? (
            <ListActions mode={mode} onToggle={toggleMode} label={text("home.new")} onCreate={() => navigate("/nouveau")} />
          ) : null
        }
      />
      {error ? <p className="text-bad">{error}</p> : null}
      {apps === null && !error ? <LoadingCards /> : null}
      {apps && apps.length === 0 ? (
        <EmptyList message={text("home.empty")} label={text("home.new")} onCreate={() => navigate("/nouveau")} />
      ) : null}
      {apps && apps.length > 0 ? (
        <ItemList mode={mode}>
          {apps.map((app) => (
            <li key={app.id} className={mode === "cards" ? "h-full" : undefined}>
              <div className={`${entryClass(mode)} h-full`}>
                <Link to={`/applications/${app.id}`} className="flex items-center gap-3">
                  <AppLogo slug={app.logo ?? ""} name={app.name} size={36} />
                  <span className="flex items-center gap-2">
                    {app.name}
                    <HealthDot status={app.paused ? "paused" : app.runStatus} />
                  </span>
                </Link>
                <RemovalNotice job={removalOf(jobs, "application", app.id)} />
                <span className="text-muted">{app.hostname}</span>
                {app.guarded ? null : (
                  <div>
                    <TextButton type="button" onClick={() => togglePause(app)}>
                      {app.paused ? text("home.resume") : "Pause"}
                    </TextButton>
                  </div>
                )}
              </div>
            </li>
          ))}
        </ItemList>
      ) : null}
    </>
  );
}
