import { useEffect, useRef, useState, type ReactNode } from "react";
import { ChevronDown, Server } from "lucide-react";
import { ProviderLogo } from "./logos";
import { Icon } from "./ui";
import { text } from "./i18n";

export type ProviderOption = { value: string; label: string; logo?: string };

function Mark({ id }: { id: string }) {
  if (id === "manual") {
    return <Icon icon={Server} className="text-muted" />;
  }
  return <ProviderLogo id={id} />;
}

export function ProviderSelect({
  label,
  aside,
  value,
  options,
  onChange,
}: {
  label: string;
  aside?: ReactNode;
  value: string;
  options: ProviderOption[];
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const selected = options.find((option) => option.value === value);

  useEffect(() => {
    if (!open) {
      return;
    }
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={root} className="relative">
      <div className="mb-2 flex items-center justify-between gap-4">
        <span className="text-muted">{label}</span>
        {aside}
      </div>
      <button
        type="button"
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setOpen((current) => !current)}
        className="flex w-full items-center gap-3 rounded-md border border-line bg-surface px-3 py-2 text-left outline-none focus:border-focus"
      >
        {selected ? <Mark id={selected.logo ?? selected.value} /> : null}
        <span className={`flex-1 ${selected ? "" : "text-faint"}`}>{selected ? selected.label : text("common.choose")}</span>
        <ChevronDown size={16} strokeWidth={1.5} className={open ? "rotate-180" : ""} aria-hidden />
      </button>
      {open ? (
        <ul role="listbox" className="absolute z-10 mt-1 max-h-72 w-full overflow-y-auto rounded-md border border-line bg-surface">
          {options.map((option) => (
            <li key={option.value}>
              <button
                type="button"
                role="option"
                aria-selected={option.value === value}
                onClick={() => {
                  onChange(option.value);
                  setOpen(false);
                }}
                className={`flex w-full items-center gap-3 px-3 py-2 text-left ${option.value === value ? "bg-status" : ""}`}
              >
                <Mark id={option.logo ?? option.value} />
                {option.label}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
