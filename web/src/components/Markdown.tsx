import { Fragment, type ReactNode } from 'react';

// Markdown reads the assistant's answers: paragraphs, headings, lists, quotes,
// tables, code. It builds React elements only, never HTML from the text, so
// nothing the model writes can run in the page. The design system has two
// weights and no italics: strong text takes the 500 weight, emphasis none.

type Block =
  | { kind: 'p'; lines: string[] }
  | { kind: 'h'; level: number; text: string }
  | { kind: 'code'; lang: string; text: string }
  | { kind: 'list'; ordered: boolean; start: number; items: string[] }
  | { kind: 'quote'; lines: string[] }
  | { kind: 'table'; head: string[]; rows: string[][] }
  | { kind: 'hr' };

const listItem = /^\s*([-*+]|\d+[.)])\s+(.*)$/;
const tableRule = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

function cells(line: string) {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|')) s = s.slice(0, -1);
  return s.split('|').map((c) => c.trim());
}

function parse(source: string): Block[] {
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const fence = line.match(/^\s*(`{3,}|~{3,})\s*([\w+-]*)/);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith(fence[1])) body.push(lines[i++]);
      i++;
      blocks.push({ kind: 'code', lang: fence[2], text: body.join('\n') });
      continue;
    }
    if (!line.trim()) {
      i++;
      continue;
    }
    const heading = line.match(/^\s*(#{1,6})\s+(.*?)\s*#*\s*$/);
    if (heading) {
      blocks.push({ kind: 'h', level: heading[1].length, text: heading[2] });
      i++;
      continue;
    }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      blocks.push({ kind: 'hr' });
      i++;
      continue;
    }
    if (line.includes('|') && i + 1 < lines.length && tableRule.test(lines[i + 1])) {
      const head = cells(line);
      const rows: string[][] = [];
      i += 2;
      while (i < lines.length && lines[i].includes('|') && lines[i].trim()) rows.push(cells(lines[i++]));
      blocks.push({ kind: 'table', head, rows });
      continue;
    }
    if (/^\s*>/.test(line)) {
      const body: string[] = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) body.push(lines[i++].replace(/^\s*>\s?/, ''));
      blocks.push({ kind: 'quote', lines: body });
      continue;
    }
    const item = line.match(listItem);
    if (item) {
      const ordered = /\d/.test(item[1]);
      const items: string[] = [];
      const start = ordered ? parseInt(item[1], 10) : 1;
      while (i < lines.length) {
        const m = lines[i].match(listItem);
        if (m && /\d/.test(m[1]) === ordered) {
          items.push(m[2]);
          i++;
        } else if (lines[i].trim() && /^\s{2,}/.test(lines[i]) && items.length) {
          // A continued line, or a nested item, stays with the item above.
          items[items.length - 1] += `\n${lines[i].trim()}`;
          i++;
        } else break;
      }
      blocks.push({ kind: 'list', ordered, start, items });
      continue;
    }
    const body: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !listItem.test(lines[i]) &&
      !/^\s*(#{1,6}\s|>|`{3,}|~{3,})/.test(lines[i])
    ) {
      body.push(lines[i++]);
    }
    blocks.push({ kind: 'p', lines: body });
  }
  return blocks;
}

// safeHref keeps the links that only navigate: web addresses and the pages
// of MyInfra. Anything else (javascript:, data:…) is shown as text.
function safeHref(url: string) {
  return /^(https?:\/\/|\/(?!\/)|#)/i.test(url) ? url : null;
}

// Every link opens in a new tab, so the conversation stays where it is.
function Link({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer noopener" className="text-link underline underline-offset-[3px] hover:no-underline">
      {children}
    </a>
  );
}

// Tokens, in this order: code, strong, [text](url), <url>, a bare address,
// emphasis. A bare address drops the punctuation that ends its sentence.
const inlineToken = /(`[^`\n]+`)|(\*\*[^*\n]+\*\*|__[^_\n]+__)|(\[[^\]\n]+\]\([^)\s]+\))|(<https?:\/\/[^>\s]+>)|(https?:\/\/[^\s<>()[\]`"]+)|(\*[^*\s][^*\n]*\*|(?<![\w])_[^_\s][^_\n]*_(?![\w]))/g;

function inline(text: string, key = 'i'): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let n = 0;
  for (const m of text.matchAll(inlineToken)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    const k = `${key}-${n++}`;
    const [whole, code, strong, link, angled, bare, em] = m;
    let used = whole.length;
    if (code) {
      out.push(<code key={k} className="rounded bg-status px-1 py-0.5 font-mono text-[13px]">{code.slice(1, -1)}</code>);
    } else if (strong) {
      out.push(<strong key={k} className="font-medium">{inline(strong.slice(2, -2), k)}</strong>);
    } else if (link) {
      const parts = link.match(/^\[([^\]]+)\]\(([^)\s]+)\)$/);
      const href = parts ? safeHref(parts[2]) : null;
      out.push(parts && href ? <Link key={k} href={href}>{inline(parts[1], k)}</Link> : whole);
    } else if (angled) {
      const url = angled.slice(1, -1);
      out.push(<Link key={k} href={url}>{url}</Link>);
    } else if (bare) {
      const url = bare.replace(/[.,;:!?'*_]+$/, '');
      used = url.length;
      out.push(<Link key={k} href={url}>{url}</Link>);
    } else if (em) {
      out.push(<Fragment key={k}>{inline(em.slice(1, -1), k)}</Fragment>);
    }
    last = at + used;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function withBreaks(lines: string[], key: string) {
  return lines.map((l, i) => (
    <Fragment key={`${key}-${i}`}>
      {i > 0 ? <br /> : null}
      {inline(l, `${key}-${i}`)}
    </Fragment>
  ));
}

export function Markdown({ text }: { text: string }) {
  const blocks = parse(text);
  return (
    <div className="flex min-w-0 flex-col gap-3 break-words">
      {blocks.map((b, i) => {
        const k = `b${i}`;
        switch (b.kind) {
          case 'h':
            return <p key={k} className={`font-medium ${b.level <= 2 ? 'text-[16px]' : ''}`}>{inline(b.text, k)}</p>;
          case 'code':
            return (
              <pre key={k} className="overflow-x-auto rounded-md bg-status px-3 py-2 font-mono text-[13px] leading-6">
                <code>{b.text}</code>
              </pre>
            );
          case 'list': {
            const Tag = b.ordered ? 'ol' : 'ul';
            return (
              <Tag key={k} start={b.ordered && b.start !== 1 ? b.start : undefined} className={`flex flex-col gap-1 pl-5 ${b.ordered ? 'list-decimal' : 'list-disc'}`}>
                {b.items.map((it, j) => <li key={j}>{withBreaks(it.split('\n'), `${k}-${j}`)}</li>)}
              </Tag>
            );
          }
          case 'quote':
            return <blockquote key={k} className="border-l-2 border-line pl-3 text-muted">{withBreaks(b.lines, k)}</blockquote>;
          case 'table':
            return (
              <div key={k} className="overflow-x-auto rounded-md border border-line">
                <table className="w-full text-left text-[13px]">
                  <thead className="text-muted">
                    <tr>{b.head.map((c, j) => <th key={j} className="px-3 py-1.5 font-normal">{inline(c, `${k}-h${j}`)}</th>)}</tr>
                  </thead>
                  <tbody>
                    {b.rows.map((r, j) => (
                      <tr key={j} className="border-t border-line">
                        {r.map((c, n) => <td key={n} className="px-3 py-1.5 align-top">{inline(c, `${k}-${j}-${n}`)}</td>)}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
          case 'hr':
            return <hr key={k} className="border-line" />;
          default:
            return <p key={k}>{withBreaks(b.lines, k)}</p>;
        }
      })}
    </div>
  );
}
