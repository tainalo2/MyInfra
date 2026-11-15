import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "./api";
import { AssistantPane } from "./components/AssistantPane";
import { AssistantPrices, AssistantProviders, type AssistantStatus } from "./components/AssistantSettings";
import { SplitViewDiff } from "./components/SplitViewDiff";
import { text } from "./i18n";
import { Tabs, useToast } from "./kit";
import { useI18n } from "./lang";
import { useSession } from "./session";
import { LoadingLines, PageTitle } from "./ui";

type Part = "chat" | "providers" | "prices";

/** The assistant: the chat and the changes it proposes; for an admin, its providers and prices. */
export function AssistantPage() {
  useI18n();
  const { canAdmin } = useSession();
  const toast = useToast();
  const [params, setParams] = useSearchParams();
  const [status, setStatus] = useState<AssistantStatus | null>(null);
  // Back from OpenAI, the page opens on the providers, where the result shows.
  const [part, setPart] = useState<Part>(params.has("chatgpt") && canAdmin ? "providers" : "chat");

  const load = () =>
    api<AssistantStatus>("/api/assistant/status").catch((): AssistantStatus => ({ enabled: false, providers: [], api_keys: [], models: {} }));
  const reload = () => load().then(setStatus);

  useEffect(() => {
    load().then((s) => {
      setStatus(s);
      // Nothing to chat with yet: an admin lands where a key is saved.
      if (!s.enabled && canAdmin) setPart("providers");
    });
  }, []);

  useEffect(() => {
    const result = params.get("chatgpt");
    if (!result) return;
    if (result === "ok") toast(text("assistant.chatgptOk"));
    else toast(text("assistant.chatgptFailed", { step: params.get("step") || "?" }), "bad");
    setParams({}, { replace: true });
  }, [params, setParams, toast]);

  const tabs: { key: Part; label: string }[] = [
    { key: "chat", label: text("assistant.chat") },
    ...(canAdmin
      ? [
          { key: "providers" as Part, label: text("assistant.providers") },
          { key: "prices" as Part, label: text("assistant.prices") },
        ]
      : []),
  ];

  return (
    <>
      <PageTitle inTree title={text("nav.assistant")} />
      <div className="flex flex-col gap-6">
        {tabs.length > 1 ? <Tabs tabs={tabs} active={part} onChange={setPart} label={text("assistant.parts")} /> : null}
        {status === null ? (
          <LoadingLines lines={3} />
        ) : part === "providers" ? (
          <AssistantProviders status={status} onChange={reload} />
        ) : part === "prices" ? (
          <AssistantPrices />
        ) : status.enabled ? (
          <div className="grid items-start gap-6 lg:grid-cols-[minmax(320px,1fr)_2fr]">
            <div className="h-[min(640px,calc(100vh-220px))] min-h-[420px] overflow-hidden rounded-md border border-line bg-surface">
              <AssistantPane />
            </div>
            <SplitViewDiff />
          </div>
        ) : (
          <p className="text-muted">{text(canAdmin ? "assistant.off" : "assistant.offAsk")}</p>
        )}
      </div>
    </>
  );
}
