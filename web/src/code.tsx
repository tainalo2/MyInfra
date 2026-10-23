import { useEffect, useRef } from "react";
import { Compartment, EditorState, RangeSetBuilder, type Extension } from "@codemirror/state";
import {
  Decoration,
  EditorView,
  MatchDecorator,
  ViewPlugin,
  drawSelection,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
  placeholder as hint,
  type DecorationSet,
  type ViewUpdate,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import {
  HighlightStyle,
  StreamLanguage,
  bracketMatching,
  foldGutter,
  foldKeymap,
  indentOnInput,
  syntaxHighlighting,
  syntaxTree,
  type StreamParser,
} from "@codemirror/language";
import { lintGutter, setDiagnostics, type Diagnostic } from "@codemirror/lint";
import { tags as t } from "@lezer/highlight";
import { yaml } from "@codemirror/lang-yaml";
import { json } from "@codemirror/lang-json";
import { xml } from "@codemirror/lang-xml";
import { properties } from "@codemirror/legacy-modes/mode/properties";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { nginx } from "@codemirror/legacy-modes/mode/nginx";
import type { CodeLanguage, CodeProblem } from "./code-lazy";

// hcl colours OpenTofu files: no mode for it ships with CodeMirror.
const hcl: StreamParser<{ string: boolean }> = {
  name: "hcl",
  startState: () => ({ string: false }),
  token(stream, state) {
    if (state.string) {
      if (stream.match(/^(?:[^"\\$]|\\.|\$(?!\{))+/)) return "string";
      if (stream.match("${")) return "meta";
      stream.next();
      state.string = false;
      return "string";
    }
    if (stream.eatSpace()) return null;
    if (stream.match("#") || stream.match("//")) {
      stream.skipToEnd();
      return "comment";
    }
    if (stream.match('"')) {
      state.string = true;
      return "string";
    }
    if (stream.match(/^-?\d+(\.\d+)?/)) return "number";
    if (stream.match(/^(true|false|null)\b/)) return "atom";
    if (stream.match(/^(resource|data|variable|output|locals|module|provider|terraform)\b/)) return "keyword";
    if (stream.match(/^[A-Za-z_][\w-]*(?=\s*=)/)) return "propertyName";
    if (stream.match(/^[A-Za-z_][\w.-]*/)) return "variableName";
    stream.next();
    return "punctuation";
  },
};

function languageExtension(language: CodeLanguage): Extension {
  switch (language) {
    case "yaml":
      return [yaml(), yamlScalars];
    case "json":
      return json();
    case "xml":
      return xml();
    case "env":
    case "ini":
      return StreamLanguage.define(properties);
    case "toml":
      return StreamLanguage.define(toml);
    case "shell":
      return StreamLanguage.define(shell);
    case "nginx":
      return StreamLanguage.define(nginx);
    case "hcl":
      return StreamLanguage.define(hcl);
    default:
      return [];
  }
}

// The colours come from the page's own variables: they follow the light and
// the dark theme without a second set.
const highlight = HighlightStyle.define([
  { tag: [t.propertyName, t.definition(t.propertyName), t.definition(t.variableName), t.attributeName, t.tagName], color: "var(--color-code-key)" },
  { tag: [t.string, t.special(t.string), t.attributeValue, t.content, t.quote], color: "var(--color-code-string)" },
  { tag: t.heading, color: "var(--color-code-keyword)", fontWeight: "600" },
  { tag: [t.number, t.integer, t.float], color: "var(--color-code-number)" },
  { tag: [t.bool, t.null, t.atom, t.keyword, t.typeName, t.labelName], color: "var(--color-code-keyword)" },
  { tag: [t.comment, t.lineComment, t.blockComment], color: "var(--color-code-comment)", fontStyle: "italic" },
  { tag: [t.meta, t.separator, t.punctuation, t.squareBracket, t.brace, t.variableName], color: "var(--color-muted)" },
]);

// yamlScalars tells numbers and true/false apart from text: the YAML grammar
// marks every plain value the same.
const scalarMarks = {
  number: Decoration.mark({ class: "cm-code-number" }),
  keyword: Decoration.mark({ class: "cm-code-keyword" }),
};

function scalars(view: EditorView): DecorationSet {
  const builder = new RangeSetBuilder<Decoration>();
  for (const { from, to } of view.visibleRanges) {
    syntaxTree(view.state).iterate({
      from,
      to,
      enter(node) {
        if (node.name !== "Literal" || node.node.parent?.name === "Key") return;
        const value = view.state.doc.sliceString(node.from, node.to);
        if (/^-?\d+(\.\d+)?$/.test(value)) builder.add(node.from, node.to, scalarMarks.number);
        else if (/^(true|false|null|~)$/.test(value)) builder.add(node.from, node.to, scalarMarks.keyword);
      },
    });
  }
  return builder.finish();
}

const yamlScalars = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    constructor(view: EditorView) {
      this.decorations = scalars(view);
    }
    update(update: ViewUpdate) {
      if (update.docChanged || update.viewportChanged || syntaxTree(update.startState) !== syntaxTree(update.state)) {
        this.decorations = scalars(update.view);
      }
    }
  },
  { decorations: (plugin) => plugin.decorations },
);

// {{url}}, {{input:key}}, {{secret:name}}…: what MyInfra fills in at
// installation stands out from what is written as it is.
const tokenMatcher = new MatchDecorator({
  regexp: /\{\{[a-z]+(?::[A-Za-z0-9_-]+)?\}\}/g,
  decoration: Decoration.mark({ class: "cm-code-token" }),
});

const tokens = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    constructor(view: EditorView) {
      this.decorations = tokenMatcher.createDeco(view);
    }
    update(update: ViewUpdate) {
      this.decorations = tokenMatcher.updateDeco(update, this.decorations);
    }
  },
  { decorations: (plugin) => plugin.decorations },
);

const frame = EditorView.theme({
  "&": {
    backgroundColor: "var(--color-surface)",
    color: "var(--color-ink)",
    border: "1px solid var(--color-line)",
    borderRadius: "0.375rem",
    fontSize: "13px",
  },
  "&.cm-focused": { outline: "none", borderColor: "var(--color-focus)" },
  ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "1.6" },
  ".cm-content": { caretColor: "var(--color-ink)", padding: "8px 0" },
  ".cm-cursor": { borderLeftColor: "var(--color-ink)" },
  ".cm-placeholder": { color: "var(--color-faint)" },
  ".cm-gutters": {
    backgroundColor: "var(--color-canvas)",
    color: "var(--color-faint)",
    borderRight: "1px solid var(--color-line)",
    borderRadius: "0.375rem 0 0 0.375rem",
  },
  ".cm-activeLine": { backgroundColor: "var(--color-code-line)" },
  ".cm-activeLineGutter": { backgroundColor: "var(--color-code-line)", color: "var(--color-ink)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "var(--color-code-selection) !important" },
  ".cm-matchingBracket": { backgroundColor: "var(--color-code-selection)", outline: "1px solid var(--color-faint)" },
  ".cm-foldPlaceholder": { backgroundColor: "var(--color-status)", border: "1px solid var(--color-line)", color: "var(--color-muted)" },
  ".cm-tooltip": { backgroundColor: "var(--color-surface)", border: "1px solid var(--color-line)", color: "var(--color-ink)" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--color-bad)" },
  // A mark may sit outside the span the grammar coloured: its colour wins.
  ".cm-code-number, .cm-code-number *": { color: "var(--color-code-number)" },
  ".cm-code-keyword, .cm-code-keyword *": { color: "var(--color-code-keyword)" },
  ".cm-code-token *": { color: "var(--color-code-token)" },
  ".cm-code-token": {
    color: "var(--color-code-token)",
    backgroundColor: "var(--color-code-token-bg)",
    borderRadius: "3px",
  },
});

function diagnostics(state: EditorState, problems: CodeProblem[]): Diagnostic[] {
  return problems
    .filter((problem) => problem.line >= 1)
    .map((problem) => {
      const line = state.doc.line(Math.min(problem.line, state.doc.lines));
      // The underline starts at the text, not at the indentation.
      const start = line.from + (line.text.length - line.text.trimStart().length);
      return { from: start, to: Math.max(line.to, start), severity: "error", message: problem.message };
    });
}

// CodeEditor is a small IDE: colours by language, line numbers, folding,
// the current line and matching brackets, and the problems underlined.
export default function CodeEditor({
  value,
  onChange,
  language,
  problems = [],
  readOnly = false,
  label,
  placeholder,
  minHeight = "8rem",
  maxHeight = "32rem",
}: {
  value: string;
  onChange?: (value: string) => void;
  language: CodeLanguage;
  problems?: CodeProblem[];
  readOnly?: boolean;
  label: string;
  placeholder?: string;
  minHeight?: string;
  maxHeight?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const changed = useRef(onChange);
  changed.current = onChange;
  const lang = useRef(new Compartment());
  const editable = useRef(new Compartment());

  useEffect(() => {
    const created = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          foldGutter(),
          lintGutter(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          drawSelection(),
          history(),
          indentOnInput(),
          bracketMatching(),
          EditorState.tabSize.of(2),
          keymap.of([...defaultKeymap, ...historyKeymap, ...foldKeymap, indentWithTab]),
          syntaxHighlighting(highlight),
          tokens,
          frame,
          EditorView.theme({ ".cm-scroller": { minHeight, maxHeight }, "&": { maxHeight } }),
          EditorView.contentAttributes.of({ "aria-label": label }),
          placeholder ? hint(placeholder) : [],
          lang.current.of(languageExtension(language)),
          editable.current.of([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) changed.current?.(update.state.doc.toString());
          }),
        ],
      }),
    });
    view.current = created;
    return () => {
      created.destroy();
      view.current = null;
    };
    // The editor is made once; the effects below follow the props.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // A value set from outside, as by the form, replaces the text; the
  // person's own typing comes back equal and changes nothing.
  useEffect(() => {
    const current = view.current;
    if (!current || current.state.doc.toString() === value) return;
    current.dispatch({ changes: { from: 0, to: current.state.doc.length, insert: value } });
  }, [value]);

  useEffect(() => {
    view.current?.dispatch({ effects: lang.current.reconfigure(languageExtension(language)) });
  }, [language]);

  useEffect(() => {
    view.current?.dispatch({ effects: editable.current.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]) });
  }, [readOnly]);

  useEffect(() => {
    const current = view.current;
    if (!current) return;
    current.dispatch(setDiagnostics(current.state, diagnostics(current.state, problems)));
  }, [problems, value]);

  return <div ref={host} className="min-w-0" />;
}
