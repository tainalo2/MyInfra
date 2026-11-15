import { useEffect, useState, useCallback, useRef } from 'react';
import { Check, RotateCw } from 'lucide-react';
import { useSession } from '../session';
import { text, type MsgKey } from '../i18n';
import { Spinner } from '../kit';
import { useI18n } from '../lang';
import { RowState } from '../rows';
import { Button, Icon, TextButton, focus } from '../ui';

export type HunkStatus = 'pending' | 'selected' | 'rejected' | 'committed';

export interface DiffLine {
  type: 'context' | 'add' | 'remove';
  text: string;
  oldLineNo?: number;
  newLineNo?: number;
}

export interface Hunk {
  id: string;
  file: string;
  header: string;
  lines: DiffLine[];
  status: HunkStatus;
  committed: boolean;
}

export interface StagingState {
  hunks: Hunk[];
  sessionId: string;
}

const API_BASE = '/api/diff';

const hunkTone: Record<HunkStatus, 'ok' | 'link' | 'muted'> = { committed: 'ok', selected: 'link', pending: 'muted', rejected: 'muted' };

export function SplitViewDiff() {
  useI18n();
  // The server decides (403 on /api/diff/commit); the UI only hides what
  // the account may not do. Without "write templates" (or admin), the
  // apply button is absent, not disabled.
  const { canWriteTemplates, canChat } = useSession();
  const [hunks, setHunks] = useState<Hunk[]>([]);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [applying, setApplying] = useState(false);
  const [message, setMessage] = useState('');
  const esRef = useRef<EventSource | null>(null);

  // Re-fetch staging on mount / reload so validated hunks survive refresh.
  const refetchStaging = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/staging`);
      if (!res.ok) return;
      const data: StagingState = await res.json();
      setSessionId(data.sessionId);
      setHunks(data.hunks.map((h) => ({ ...h, status: h.committed ? 'committed' : h.status ?? 'pending' })));
    } catch (e) {
      setMessage(text('assistant.stagingFailed'));
    }
  }, []);

  useEffect(() => {
    refetchStaging();
    return () => {
      esRef.current?.close();
      esRef.current = null;
    };
  }, [refetchStaging]);

  const startStream = useCallback(() => {
    if (esRef.current) return;
    const es = new EventSource(`${API_BASE}/stream`);
    esRef.current = es;
    es.onmessage = (ev) => {
      try {
        const hunk: Hunk = JSON.parse(ev.data);
        setHunks((prev) => {
          if (prev.some((h) => h.id === hunk.id)) return prev;
          return [...prev, { ...hunk, status: 'pending', committed: false }];
        });
      } catch {
        // ignore malformed
      }
    };
    // A hunk replaced by the assistant: same id, new lines, selection reset.
    es.addEventListener('modified', (ev) => {
      try {
        const hunk: Hunk = JSON.parse((ev as MessageEvent).data);
        setHunks((prev) => prev.map((h) => (h.id === hunk.id && !h.committed ? { ...hunk, status: 'pending', committed: false } : h)));
      } catch {
        // ignore malformed
      }
    });
    // End of a patch: the stream stays open for the next one.
    es.addEventListener('done', () => setMessage(text('assistant.patchReceived')));
    es.onerror = () => {
      // EventSource reconnects by itself.
      setMessage(text('assistant.streamLost'));
    };
  }, []);

  // The diff shows up live while the assistant answers: no button, no refresh.
  useEffect(() => {
    if (canChat) startStream();
  }, [canChat, startStream]);

  const toggle = (id: string) => {
    setHunks((prev) =>
      prev.map((h) => {
        if (h.id !== id || h.committed) return h;
        const next: HunkStatus = h.status === 'selected' ? 'pending' : 'selected';
        return { ...h, status: next };
      })
    );
  };

  const reject = (id: string) => {
    setHunks((prev) =>
      prev.map((h) => (h.id === id && !h.committed ? { ...h, status: 'rejected' } : h))
    );
  };

  const apply = async () => {
    if (!canWriteTemplates) {
      setMessage(text('assistant.forbidden'));
      return;
    }
    const selected = hunks.filter((h) => h.status === 'selected' && !h.committed);
    if (selected.length === 0) {
      setMessage(text('assistant.noneSelected'));
      return;
    }
    setApplying(true);
    try {
      const res = await fetch(`${API_BASE}/commit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, hunkIds: selected.map((h) => h.id) }),
      });
      if (res.status === 403) {
        setMessage(text('assistant.forbidden'));
        return;
      }
      if (!res.ok) throw new Error(await res.text());
      const committedIds: string[] = await res.json();
      setHunks((prev) =>
        prev.map((h) =>
          committedIds.includes(h.id) ? { ...h, committed: true, status: 'committed' } : h
        )
      );
      setMessage(text('assistant.applied', { count: committedIds.length }));
      await refetchStaging();
    } catch (e) {
      setMessage(text('assistant.applyFailed'));
    } finally {
      setApplying(false);
    }
  };

  const grouped = hunks.reduce<Record<string, Hunk[]>>((acc, h) => {
    (acc[h.file] = acc[h.file] || []).push(h);
    return acc;
  }, {});

  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={text('assistant.changes')}>
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
        <h2>
          {text('assistant.changes')}
          {hunks.length ? <span className="text-muted"> · {hunks.length}</span> : null}
        </h2>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
          <TextButton type="button" onClick={() => void refetchStaging()}>
            <Icon icon={RotateCw} />
            {text('assistant.reload')}
          </TextButton>
          {canWriteTemplates ? (
            <Button type="button" onClick={() => void apply()} disabled={applying || !hunks.some((h) => h.status === 'selected')}>
              {applying ? <Spinner /> : <Icon icon={Check} />}
              {text('assistant.apply')}
            </Button>
          ) : null}
        </div>
      </div>
      {message ? <p className="text-[13px] text-muted" role="status">{message}</p> : null}
      {hunks.length === 0 ? <p className="text-muted">{text('assistant.changesEmpty')}</p> : null}
      {Object.entries(grouped).map(([file, hs]) => (
        <section key={file} className="overflow-hidden rounded-md border border-line bg-surface">
          <h3 className="border-b border-line px-4 py-2 font-mono text-[13px]">{file}</h3>
          {hs.map((h) => {
            const status: HunkStatus = h.committed ? 'committed' : h.status;
            const open = !h.committed && h.status !== 'rejected';
            return (
              <div key={h.id} className={`border-t border-line first:border-t-0 ${status === 'rejected' ? 'opacity-40' : ''}`}>
                <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-2">
                  <label className="flex min-w-0 flex-1 items-center gap-3">
                    <input
                      type="checkbox"
                      className={`size-4 shrink-0 accent-[var(--color-ink)] ${focus}`}
                      checked={h.status === 'selected'}
                      disabled={!open}
                      onChange={() => toggle(h.id)}
                    />
                    <span className="truncate font-mono text-[13px] text-muted">{h.header}</span>
                  </label>
                  <RowState tone={hunkTone[status]}>{text(`assistant.hunk.${status}` as MsgKey)}</RowState>
                  {open ? (
                    <TextButton type="button" tone="danger" onClick={() => reject(h.id)}>
                      {text('assistant.reject')}
                    </TextButton>
                  ) : null}
                </div>
                <div className="grid border-t border-line md:grid-cols-2">
                  <DiffSide label={text('assistant.before')} lines={h.lines.filter((l) => l.type !== 'add')} />
                  <DiffSide label={text('assistant.after')} lines={h.lines.filter((l) => l.type !== 'remove')} after />
                </div>
              </div>
            );
          })}
        </section>
      ))}
    </section>
  );
}

// One side of a hunk: a removed line reads in the bad colour with a minus,
// an added one in the ok colour with a plus, so colour is never alone.
function DiffSide({ label, lines, after = false }: { label: string; lines: DiffLine[]; after?: boolean }) {
  return (
    <div className={`min-w-0 py-2 ${after ? 'border-t border-line md:border-l md:border-t-0' : ''}`}>
      <span className="block px-4 pb-1 text-[12px] text-faint">{label}</span>
      <pre className="overflow-x-auto font-mono text-[13px] leading-6">
        {lines.map((l, i) => (
          <div key={i} className={`px-4 ${l.type === 'remove' ? 'bg-status text-bad' : l.type === 'add' ? 'bg-status text-ok' : ''}`}>
            <span aria-hidden className="mr-2 inline-block w-2 select-none text-faint">{l.type === 'remove' ? '−' : l.type === 'add' ? '+' : ' '}</span>
            {l.text}
          </div>
        ))}
      </pre>
    </div>
  );
}

export default SplitViewDiff;
