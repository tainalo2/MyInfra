import { Suspense, lazy } from "react";

export type CodeLanguage = "yaml" | "json" | "xml" | "env" | "ini" | "toml" | "shell" | "nginx" | "hcl" | "text";

// A problem the editor underlines: its line, from 1, and what is wrong.
export type CodeProblem = { line: number; message: string };

// languageOf picks the colours of a file from its name, as an IDE does.
export function languageOf(path: string): CodeLanguage {
  const name = path.toLowerCase().split("/").pop() ?? "";
  const ext = name.includes(".") ? name.slice(name.lastIndexOf(".") + 1) : "";
  if (name === ".env" || ext === "env" || ext === "properties") return "env";
  if (ext === "yaml" || ext === "yml") return "yaml";
  if (ext === "json") return "json";
  if (ext === "xml" || ext === "html" || ext === "svg") return "xml";
  if (ext === "toml") return "toml";
  if (ext === "ini" || ext === "cfg" || ext === "cnf") return "ini";
  if (ext === "sh" || ext === "bash") return "shell";
  if (ext === "tf" || ext === "hcl" || ext === "tfvars") return "hcl";
  if (name === "nginx.conf" || name.endsWith(".nginx") || (ext === "conf" && name.includes("nginx"))) return "nginx";
  if (ext === "conf") return "ini";
  return "text";
}

// The editor weighs a few hundred kilobytes: it loads with the first page
// that shows code, and the text shows as it is meanwhile.
const Editor = lazy(() => import("./code"));

type CodeProps = {
  value: string;
  onChange?: (value: string) => void;
  language: CodeLanguage;
  problems?: CodeProblem[];
  readOnly?: boolean;
  label: string;
  minHeight?: string;
  maxHeight?: string;
};

export function Code(props: CodeProps) {
  return (
    <Suspense
      fallback={
        <pre className="min-h-32 overflow-auto rounded-md border border-line bg-surface p-3 font-mono text-[13px] text-muted" aria-label={props.label}>
          {props.value}
        </pre>
      }
    >
      <Editor {...props} />
    </Suspense>
  );
}
