import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { ChevronDown, Server } from "lucide-react";
import { ProviderLogo } from "./logos";
import { Icon, focus } from "./ui";
import { text } from "./i18n";

export type ProviderOption = { value: string; label: string; logo?: string };

function Mark({ id }: { id: string }) {
  if (id === "manual") {
    return <Icon icon={Server} className="text-muted" />;
  }
  return <ProviderLogo id={id} />;
}

// ProviderSelect is a list box with logos: it opens with the arrows, Enter or
// Space, moves with the arrows, Home and End, and closes with Escape or Tab.
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
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const id = useId();
  const selected = options.find((option) => option.value === value);

  useEffect(() => {
    if (!open) return;
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onPointer);
    list.current?.focus();
    return () => document.removeEventListener("mousedown", onPointer);
  }, [open]);

  useEffect(() => {
    if (open) document.getElementById(`${id}-${active}`)?.scrollIntoView({ block: "nearest" });
  }, [open, active, id]);

  function show(index = Math.max(0, options.findIndex((option) => option.value === value))) {
    setActive(index);
    setOpen(true);
  }

  function choose(index: number) {
    const option = options[index];
    if (option) onChange(option.value);
    setOpen(false);
    trigger.current?.focus();
  }

  function onListKey(event: KeyboardEvent<HTMLUListElement>) {
    const last = options.length - 1;
    if (event.key === "ArrowDown") setActive((current) => Math.min(last, current + 1));
    else if (event.key === "ArrowUp") setActive((current) => Math.max(0, current - 1));
    else if (event.key === "Home") setActive(0);
    else if (event.key === "End") setActive(last);
    else if (event.key === "Enter" || event.key === " ") choose(active);
    else if (event.key === "Escape") {
      setOpen(false);
      trigger.current?.focus();
    } else if (event.key === "Tab") {
      setOpen(false);
      return;
    } else return;
    event.preventDefault();
    event.stopPropagation();
  }

  return (
    <div ref={root} className="relative">
      <div className="mb-2 flex items-center justify-between gap-4">
        <span id={`${id}-label`} className="text-muted">
          {label}
        </span>
        {aside}
      </div>
      <button
        ref={trigger}
        type="button"
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-labelledby={`${id}-label ${id}-value`}
        aria-controls={open ? `${id}-list` : undefined}
        onClick={() => (open ? setOpen(false) : show())}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            show();
          }
        }}
        className={`flex min-h-11 w-full items-center gap-3 rounded-md border border-line bg-surface px-3 py-2 text-left sm:min-h-0 ${focus}`}
      >
        {selected ? <Mark id={selected.logo ?? selected.value} /> : null}
        <span id={`${id}-value`} className={`flex-1 ${selected ? "" : "text-faint"}`}>
          {selected ? selected.label : text("common.choose")}
        </span>
        <ChevronDown size={16} strokeWidth={1.5} className={open ? "rotate-180" : ""} aria-hidden />
      </button>
      {open ? (
        <ul
          ref={list}
          id={`${id}-list`}
          role="listbox"
          tabIndex={-1}
          aria-labelledby={`${id}-label`}
          aria-activedescendant={`${id}-${active}`}
          onKeyDown={onListKey}
          className="absolute z-10 mt-1 max-h-72 w-full overflow-y-auto rounded-md border border-line bg-surface outline-none"
        >
          {options.map((option, index) => (
            <li
              key={option.value}
              id={`${id}-${index}`}
              role="option"
              aria-selected={option.value === value}
              onMouseEnter={() => setActive(index)}
              onClick={() => choose(index)}
              className={`flex min-h-11 cursor-pointer items-center gap-3 px-3 py-2 sm:min-h-0 ${index === active ? "bg-status" : ""} ${option.value === value ? "font-medium" : ""}`}
            >
              <Mark id={option.logo ?? option.value} />
              {option.label}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
