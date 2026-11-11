import { AssistantPane } from "./components/AssistantPane";
import { AssistantSettings } from "./components/AssistantSettings";
import { SplitViewDiff } from "./components/SplitViewDiff";
import { useSession } from "./session";

/** The assistant: chat on the left, the patches it proposes on the right. */
export function AssistantPage() {
  const { canAdmin } = useSession();
  return (
    <div className="flex flex-col gap-6">
      {canAdmin ? <AssistantSettings /> : null}
      <div className="grid gap-4 lg:grid-cols-[minmax(320px,1fr)_2fr]">
        <AssistantPane />
        <SplitViewDiff />
      </div>
    </div>
  );
}
