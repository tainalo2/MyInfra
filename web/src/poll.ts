import { useCallback, useEffect, useRef, useState } from "react";
import { api, peek } from "./api";

type PollOptions<T> = {
  // false stops reading; the last answer stays.
  enabled?: boolean;
  // Once true for an answer, reading stops: a finished job no longer moves.
  stopWhen?: (data: T) => boolean;
};

// usePoll reads a path now, then every ms while the tab is visible. It starts
// from the last answer kept for the tab, so a page opened again shows at once.
export function usePoll<T>(path: string | null, ms: number, options: PollOptions<T> = {}) {
  const { enabled = true, stopWhen } = options;
  const [data, setData] = useState<T | null>(() => (path ? peek<T>(path) : null));
  const [error, setError] = useState("");
  const stopRef = useRef(stopWhen);
  stopRef.current = stopWhen;
  const [done, setDone] = useState(false);

  const reload = useCallback(async () => {
    if (!path) return;
    try {
      const next = await api<T>(path);
      setData(next);
      setError("");
      // A job started again (resumed) starts the reading again too.
      setDone(Boolean(stopRef.current?.(next)));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [path]);

  useEffect(() => {
    setDone(false);
    setData(path ? peek<T>(path) : null);
  }, [path]);

  useEffect(() => {
    if (!path || !enabled) return;
    void reload();
    if (ms <= 0 || done) return;
    const tick = () => {
      if (!document.hidden) void reload();
    };
    const timer = window.setInterval(tick, ms);
    document.addEventListener("visibilitychange", tick);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [path, ms, enabled, done, reload]);

  return { data, setData, error, reload };
}
