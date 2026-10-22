import { ClipboardEvent, KeyboardEvent, useEffect, useRef } from "react";
import { focus } from "./ui";

export function TotpInput({ value, onChange, disabled }: { value: string; onChange: (next: string) => void; disabled?: boolean }) {
  const digits = value.padEnd(6, " ").slice(0, 6).split("");
  const refs = useRef<(HTMLInputElement | null)[]>([]);
  useEffect(() => {
    const index = Math.min(value.length, 5);
    refs.current[index]?.focus();
  }, [value.length]);

  function setAt(index: number, char: string) {
    const clean = char.replace(/\D/g, "").slice(-1);
    const chars = value.padEnd(index, " ").slice(0, 6).split("");
    while (chars.length < 6) chars.push(" ");
    chars[index] = clean || " ";
    const next = chars.join("").replace(/ /g, "").slice(0, 6);
    onChange(next);
    if (clean && index < 5) refs.current[index + 1]?.focus();
  }

  function onKey(index: number, event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Backspace") {
      event.preventDefault();
      if (value[index]) {
        onChange(value.slice(0, index) + value.slice(index + 1));
      } else if (index > 0) {
        refs.current[index - 1]?.focus();
        onChange(value.slice(0, index - 1) + value.slice(index));
      }
      return;
    }
    if (event.key === "ArrowLeft" && index > 0) {
      refs.current[index - 1]?.focus();
      return;
    }
    if (event.key === "ArrowRight" && index < 5) {
      refs.current[index + 1]?.focus();
    }
  }

  function onPaste(event: ClipboardEvent<HTMLInputElement>) {
    event.preventDefault();
    const pasted = event.clipboardData.getData("text").replace(/\D/g, "").slice(0, 6);
    if (pasted) onChange(pasted);
  }

  return (
    <div className="flex gap-2 sm:gap-3" role="group" aria-label="Code à six chiffres">
      {digits.map((digit, index) => (
        <input
          key={index}
          ref={(el) => {
            refs.current[index] = el;
          }}
          type="text"
          inputMode="numeric"
          autoComplete={index === 0 ? "one-time-code" : "off"}
          maxLength={1}
          disabled={disabled}
          value={digit === " " ? "" : digit}
          aria-label={`Chiffre ${index + 1}`}
          className={`size-12 rounded-md border border-line bg-surface text-center text-xl font-medium tabular-nums sm:size-14 sm:text-2xl ${focus}`}
          onChange={(event) => setAt(index, event.target.value)}
          onKeyDown={(event) => onKey(index, event)}
          onPaste={onPaste}
        />
      ))}
    </div>
  );
}
