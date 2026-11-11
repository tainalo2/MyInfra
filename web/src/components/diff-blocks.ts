export interface StagedPatch {
  id: string;
  hunkId: string;
  path: string;
  content: string;
}

/**
 * Reads the ```diff fenced blocks of an assistant reply and cuts them into
 * one patch per hunk: file header + one "@@" section, so each hunk can be
 * chosen and applied on its own. Ids sort in reply order (the gateway applies
 * the chosen ones sorted by id).
 */
export function patchesFromReply(text: string, tag = Date.now().toString(36)): StagedPatch[] {
  const out: StagedPatch[] = [];
  const fence = /```(?:diff|patch)[^\n]*\n([\s\S]*?)```/g;
  let m: RegExpExecArray | null;
  let n = 0;
  while ((m = fence.exec(text)) !== null) {
    let header: string[] = [];
    let path = '';
    let current: string[] | null = null;
    let hunkLine = '';
    const flush = () => {
      if (current && path) {
        n += 1;
        out.push({
          id: `p_${tag}_${String(n).padStart(3, '0')}`,
          hunkId: hunkLine,
          path,
          content: [...header, ...current].join('\n') + '\n',
        });
      }
      current = null;
    };
    for (const line of m[1].replace(/\n$/, '').split('\n')) {
      if (line.startsWith('--- ')) {
        flush();
        header = [line];
        path = '';
      } else if (line.startsWith('+++ ') && current === null) {
        header.push(line);
        path = line.slice(4).replace(/^b\//, '').trim();
      } else if (line.startsWith('@@')) {
        flush();
        current = [line];
        hunkLine = line;
      } else if (current) {
        current.push(line);
      } else if (header.length > 0 && !line.startsWith('+++')) {
        header.push(line);
      }
    }
    flush();
  }
  return out;
}
