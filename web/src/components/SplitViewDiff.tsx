import { useEffect, useState, useCallback, useRef } from 'react';
import { useSession } from '../session';

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

export function SplitViewDiff() {
  // The server decides (403 on /api/diff/commit); the UI only hides what
  // the account may not do. Without "write templates" (or admin), the
  // "Appliquer" button is absent, not disabled.
  const { canWriteTemplates, canChat } = useSession();
  const [hunks, setHunks] = useState<Hunk[]>([]);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [streaming, setStreaming] = useState(false);
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
      setMessage('Re-fetch staging failed');
    }
  }, []);

  useEffect(() => {
    refetchStaging();
    return () => {
      esRef.current?.close();
    };
  }, [refetchStaging]);

  const startStream = useCallback(() => {
    if (streaming) return;
    setStreaming(true);
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
    es.addEventListener('done', () => {
      setStreaming(false);
      es.close();
    });
    es.onerror = () => {
      setStreaming(false);
      es.close();
      setMessage('Stream ended');
    };
  }, [streaming]);

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
      setMessage('Droits insuffisants.');
      return;
    }
    const selected = hunks.filter((h) => h.status === 'selected' && !h.committed);
    if (selected.length === 0) {
      setMessage('No hunks selected');
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
        setMessage('Droits insuffisants.');
        return;
      }
      if (!res.ok) throw new Error(await res.text());
      const committedIds: string[] = await res.json();
      setHunks((prev) =>
        prev.map((h) =>
          committedIds.includes(h.id) ? { ...h, committed: true, status: 'committed' } : h
        )
      );
      setMessage(`Applied ${committedIds.length} hunk(s)`);
      await refetchStaging();
    } catch (e) {
      setMessage('Apply failed');
    } finally {
      setApplying(false);
    }
  };

  const grouped = hunks.reduce<Record<string, Hunk[]>>((acc, h) => {
    (acc[h.file] = acc[h.file] || []).push(h);
    return acc;
  }, {});

  return (
    <div className="split-view-diff">
      <div className="toolbar">
        {canChat ? <button onClick={startStream} disabled={streaming}>Start diff stream</button> : null}
        {canWriteTemplates ? (
          <button onClick={apply} disabled={applying || !hunks.some((h) => h.status === 'selected')}>
            Appliquer / Commit
          </button>
        ) : null}
        <button onClick={refetchStaging}>Recharger staging</button>
        <span className="msg">{message}</span>
      </div>
      {Object.entries(grouped).map(([file, hs]) => (
        <section key={file} className="file-block">
          <h3>{file}</h3>
          {hs.map((h) => (
            <div key={h.id} className={`hunk ${h.committed ? 'committed' : h.status}`}>
              <div className="hunk-header">
                <label>
                  <input
                    type="checkbox"
                    checked={h.status === 'selected'}
                    disabled={h.committed || h.status === 'rejected'}
                    onChange={() => toggle(h.id)}
                  />
                  {h.header}
                </label>
                <span className="badge">
                  {h.committed ? 'Committed' : h.status === 'rejected' ? 'Rejected' : h.status === 'selected' ? 'Selected' : 'Pending'}
                </span>
                {!h.committed && h.status !== 'rejected' && (
                  <button className="reject" onClick={() => reject(h.id)}>Reject</button>
                )}
              </div>
              <div className="split">
                <pre className="before">
                  {h.lines
                    .filter((l) => l.type !== 'add')
                    .map((l, i) => (
                      <div key={i} className={l.type}>{l.text}</div>
                    ))}
                </pre>
                <pre className="after">
                  {h.lines
                    .filter((l) => l.type !== 'remove')
                    .map((l, i) => (
                      <div key={i} className={l.type}>{l.text}</div>
                    ))}
                </pre>
              </div>
            </div>
          ))}
        </section>
      ))}
    </div>
  );
}

export default SplitViewDiff;
