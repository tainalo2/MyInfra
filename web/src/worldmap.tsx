import { useEffect, useRef } from "react";
import { Maximize, Minus, Plus } from "lucide-react";
import type { Server } from "./api";
import { text } from "./i18n";
import { landHeight, landMask, landWidth } from "./land";
import type { Place } from "./places";
import { useTheme } from "./theme";
import { IconButton } from "./ui";

export type Tone = "ok" | "warn" | "bad";

export type MapSite = {
  key: string;
  place: Place;
  count: number;
  tone: Tone;
  ring?: Tone[];
  dim?: boolean;
  selected?: boolean;
};

export type Inset = { top: number; right: number; bottom: number; left: number };

const toneColor: Record<Tone, string> = { ok: "#65a30d", warn: "#d97706", bad: "#dc2626" };

export function worst(tones: Tone[]): Tone {
  if (tones.includes("bad")) return "bad";
  if (tones.includes("warn")) return "warn";
  return "ok";
}

type Sample = { cpu: { used: number; total: number }; memory: { used: number; total: number }; disk: { used: number; total: number }; measured: boolean };

export function healthOf(server: Server, sample?: Sample): Tone {
  if (server.status === "failed" || server.reach) return "bad";
  if (server.status === "pending" || server.status === "deploying") return "warn";
  if (sample?.measured) {
    const pressure = Math.max(ratio(sample.cpu), ratio(sample.memory), ratio(sample.disk));
    if (pressure >= 0.97) return "bad";
    if (pressure >= 0.9) return "warn";
  }
  return "ok";
}

function ratio(gauge: { used: number; total: number }) {
  if (!gauge.total) return 0;
  return gauge.used / gauge.total;
}

function landAt(lat: number, lon: number) {
  const wrapped = ((((lon + 180) % 360) + 360) % 360) - 180;
  const col = Math.min(landWidth - 1, Math.max(0, Math.floor(((wrapped + 180) / 360) * landWidth)));
  const row = Math.floor(((lat + 90) / 180) * landHeight);
  if (row < 0 || row >= landHeight) return false;
  const index = row * landWidth + col;
  return (landMask[index >> 3] & (1 << (index & 7))) !== 0;
}

// The map uses Mercator: shapes keep their proportions at the latitudes where
// the servers usually are. `y` is the projected latitude, in degrees.
type View = { lon: number; y: number; k: number };

const toRad = Math.PI / 180;

function mercator(lat: number) {
  return Math.log(Math.tan(Math.PI / 4 + (lat * toRad) / 2)) / toRad;
}

const topY = mercator(80);
const bottomY = mercator(-62);

type Group = { keys: string[]; places: Place[]; x: number; y: number; count: number; tone: Tone; ring?: Tone[]; dim: boolean; selected: boolean; label: string };

const dotSpacing = 7;
const landStep = 360 / landWidth;
const maxScale = 90;

// WorldMap is a flat dotted map of the sites, framed on them. It is a visual
// aid: every action it offers is also reachable from the list beside it.
export function WorldMap({
  sites,
  frame,
  inset,
  fitKey,
  label,
  onSelect,
}: {
  sites: MapSite[];
  frame: string[];
  inset: Inset;
  fitKey: string;
  label: string;
  onSelect: (key: string | null) => void;
}) {
  const { theme } = useTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const sitesRef = useRef(sites);
  sitesRef.current = sites;
  const frameRef = useRef(frame);
  frameRef.current = frame;
  const insetRef = useRef(inset);
  insetRef.current = inset;
  const selectRef = useRef(onSelect);
  selectRef.current = onSelect;
  const controls = useRef<{ draw: () => void; fit: () => void; zoom: (factor: number) => void } | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    const parent = canvas?.parentElement;
    if (!canvas || !ctx || !parent) return;

    let width = 0;
    let height = 0;
    let view: View = { lon: 10, y: mercator(25), k: 1 };
    let moved = false;
    let dragging = false;
    let dragged = false;
    let lastX = 0;
    let lastY = 0;
    const hits: { group: Group; r: number }[] = [];

    function size() {
      width = parent!.clientWidth;
      height = parent!.clientHeight;
      const scale = window.devicePixelRatio || 1;
      canvas!.width = Math.floor(width * scale);
      canvas!.height = Math.floor(height * scale);
      ctx!.setTransform(scale, 0, 0, scale, 0, 0);
    }

    function visible() {
      const edge = insetRef.current;
      let x0 = edge.left;
      let x1 = width - edge.right;
      let y0 = edge.top;
      let y1 = height - edge.bottom;
      if (x1 - x0 < 160) [x0, x1] = [0, width];
      if (y1 - y0 < 120) [y0, y1] = [0, height];
      return { x0, x1, y0, y1 };
    }

    function minScale() {
      return Math.max(width / 360, 0.5);
    }

    function fit(only?: Place[]) {
      const area = visible();
      const keys = new Set(frameRef.current);
      const places = only ?? sitesRef.current.filter((site) => keys.size === 0 || keys.has(site.key)).map((site) => site.place);
      const areaW = area.x1 - area.x0;
      const areaH = area.y1 - area.y0;
      let lon = 10;
      let y = mercator(30);
      let k = Math.max(areaW / 360, minScale());
      if (places.length > 0) {
        const lons = places.map((place) => place.lon);
        const ys = places.map((place) => mercator(place.lat));
        const spanLon = Math.max(Math.max(...lons) - Math.min(...lons), 18);
        const spanY = Math.max(Math.max(...ys) - Math.min(...ys), 12);
        lon = (Math.max(...lons) + Math.min(...lons)) / 2;
        y = (Math.max(...ys) + Math.min(...ys)) / 2;
        k = Math.min(Math.max(areaW - 120, 80) / spanLon, Math.max(areaH - 120, 60) / spanY);
        k = Math.min(maxScale, Math.max(minScale(), k));
      }
      view = {
        k,
        lon: lon - ((area.x0 + area.x1) / 2 - width / 2) / k,
        y: clampY(y + ((area.y0 + area.y1) / 2 - height / 2) / k),
      };
      moved = only !== undefined;
      draw();
    }

    function screen(lon: number, lat: number) {
      let delta = lon - view.lon;
      delta -= Math.round(delta / 360) * 360;
      return { x: width / 2 + delta * view.k, y: height / 2 - (mercator(lat) - view.y) * view.k };
    }

    function color(name: string, fallback: string) {
      return getComputedStyle(canvas!).getPropertyValue(name).trim() || fallback;
    }

    function draw() {
      const canvasColor = color("--color-canvas", "#fafaf9");
      const surface = color("--color-surface", "#ffffff");
      const ink = color("--color-ink", "#1c1917");
      const muted = color("--color-muted", "#57534e");
      const faint = color("--color-faint", "#78716c");
      ctx!.fillStyle = canvasColor;
      ctx!.fillRect(0, 0, width, height);

      // The dots sit on a geographic grid, so they travel with the map when
      // it moves; the grid only changes at each doubling of the zoom. Rows get
      // closer in latitude away from the equator to stay evenly spaced.
      const wanted = dotSpacing / view.k;
      const step = Math.max(landStep, landStep * 2 ** Math.round(Math.log2(wanted / landStep)));
      const radius = Math.max(0.8, Math.min(1.6, (step * view.k) / 4.5));
      const west = view.lon - width / 2 / view.k;
      const east = view.lon + width / 2 / view.k;
      ctx!.fillStyle = faint;
      ctx!.globalAlpha = theme === "dark" ? 0.5 : 0.38;
      const row = (lat: number) => {
        const y = height / 2 - (mercator(lat) - view.y) * view.k;
        if (y < -4 || y > height + 4) return;
        for (let lon = Math.floor(west / step) * step; lon <= east; lon += step) {
          if (!landAt(lat, lon + step / 2)) continue;
          const x = width / 2 + (lon - view.lon) * view.k;
          ctx!.fillRect(x - radius, y - radius, radius * 2, radius * 2);
        }
      };
      for (let lat = 0; lat <= 84; lat += step * Math.max(0.2, Math.cos(lat * toRad))) row(lat);
      for (let lat = -step; lat >= -60; lat -= step * Math.max(0.2, Math.cos(lat * toRad))) row(lat);
      ctx!.globalAlpha = 1;

      hits.length = 0;
      const groups: Group[] = [];
      for (const site of sitesRef.current) {
        const { x, y } = screen(site.place.lon, site.place.lat);
        if (x < -40 || x > width + 40 || y < -40 || y > height + 40) continue;
        const near = groups.find((group) => (group.x - x) ** 2 + (group.y - y) ** 2 < 34 ** 2);
        if (!near) {
          groups.push({ keys: [site.key], places: [site.place], x, y, count: site.count, tone: site.tone, ring: site.ring, dim: !!site.dim, selected: !!site.selected, label: site.place.label });
          continue;
        }
        const n = near.keys.length;
        near.x = (near.x * n + x) / (n + 1);
        near.y = (near.y * n + y) / (n + 1);
        near.keys.push(site.key);
        near.places.push(site.place);
        near.count += site.count;
        near.tone = worst([near.tone, site.tone]);
        if (near.ring || site.ring) near.ring = [...(near.ring ?? []), ...(site.ring ?? [])];
        near.dim = near.dim && !!site.dim;
        near.selected = near.selected || !!site.selected;
        near.label = `${near.places[0].label} +${n}`;
      }
      groups.sort((a, b) => Number(a.selected) - Number(b.selected) || Number(b.dim) - Number(a.dim));
      for (const group of groups) {
        const { x, y } = group;
        const r = group.keys.length > 1 ? 15 : 13;
        ctx!.globalAlpha = group.dim ? 0.3 : 1;
        if (group.selected) {
          ctx!.beginPath();
          ctx!.arc(x, y, r + 11, 0, Math.PI * 2);
          ctx!.fillStyle = toneColor[group.tone];
          ctx!.globalAlpha = 0.16;
          ctx!.fill();
          ctx!.globalAlpha = 1;
          ctx!.beginPath();
          ctx!.arc(x, y, r + 8, 0, Math.PI * 2);
          ctx!.strokeStyle = ink;
          ctx!.lineWidth = 1.5;
          ctx!.stroke();
        }
        ctx!.beginPath();
        ctx!.arc(x, y, r, 0, Math.PI * 2);
        ctx!.fillStyle = surface;
        ctx!.fill();
        if (group.ring) {
          ctx!.strokeStyle = faint;
          ctx!.lineWidth = 1;
          ctx!.stroke();
          ring(x, y, r + 4, group.ring, faint);
        } else {
          ctx!.strokeStyle = toneColor[group.tone];
          ctx!.lineWidth = 2.5;
          ctx!.stroke();
        }
        ctx!.fillStyle = ink;
        ctx!.font = "600 12px 'Geist Variable', ui-sans-serif, system-ui, sans-serif";
        ctx!.textAlign = "center";
        ctx!.textBaseline = "middle";
        ctx!.fillText(String(group.count), x, y + 0.5);
        ctx!.font = "12px 'Geist Variable', ui-sans-serif, system-ui, sans-serif";
        ctx!.lineWidth = 4;
        ctx!.strokeStyle = canvasColor;
        ctx!.strokeText(group.label, x, y + r + 15);
        ctx!.fillStyle = muted;
        ctx!.fillText(group.label, x, y + r + 15);
        ctx!.globalAlpha = 1;
        hits.push({ group, r: r + 8 });
      }
    }

    function ring(x: number, y: number, r: number, tones: Tone[], empty: string) {
      ctx!.lineWidth = 4;
      ctx!.lineCap = "butt";
      if (tones.length === 0) {
        ctx!.beginPath();
        ctx!.setLineDash([2, 4]);
        ctx!.arc(x, y, r, 0, Math.PI * 2);
        ctx!.strokeStyle = empty;
        ctx!.lineWidth = 1.5;
        ctx!.stroke();
        ctx!.setLineDash([]);
        return;
      }
      const share = (Math.PI * 2) / tones.length;
      const gap = tones.length > 1 ? Math.min(0.18, share * 0.25) : 0;
      tones.forEach((tone, index) => {
        const start = -Math.PI / 2 + index * share + gap / 2;
        ctx!.beginPath();
        ctx!.arc(x, y, r, start, start + share - gap);
        ctx!.strokeStyle = toneColor[tone];
        ctx!.stroke();
      });
    }

    function hitAt(x: number, y: number) {
      for (let i = hits.length - 1; i >= 0; i--) {
        const hit = hits[i];
        if ((hit.group.x - x) ** 2 + (hit.group.y - y) ** 2 <= hit.r ** 2) return hit.group;
      }
      return null;
    }

    function zoomAt(x: number, y: number, factor: number) {
      const k = Math.min(maxScale, Math.max(minScale(), view.k * factor));
      const lon = view.lon + (x - width / 2) / view.k;
      const my = view.y - (y - height / 2) / view.k;
      view = { k, lon: lon - (x - width / 2) / k, y: clampY(my + (y - height / 2) / k) };
      moved = true;
      draw();
    }

    function local(event: PointerEvent | WheelEvent | MouseEvent) {
      const rect = canvas!.getBoundingClientRect();
      return { x: event.clientX - rect.left, y: event.clientY - rect.top };
    }

    // Two fingers on a phone zoom, as the wheel does.
    const touches = new Map<number, { x: number; y: number }>();
    let spread = 0;

    function pinchSpread() {
      const [a, b] = [...touches.values()];
      return Math.hypot(a.x - b.x, a.y - b.y);
    }

    function onDown(event: PointerEvent) {
      touches.set(event.pointerId, { x: event.clientX, y: event.clientY });
      canvas!.setPointerCapture(event.pointerId);
      if (touches.size === 2) {
        spread = pinchSpread();
        dragged = true;
        return;
      }
      dragging = true;
      dragged = false;
      lastX = event.clientX;
      lastY = event.clientY;
    }

    function onMove(event: PointerEvent) {
      if (touches.has(event.pointerId)) touches.set(event.pointerId, { x: event.clientX, y: event.clientY });
      if (touches.size === 2) {
        const next = pinchSpread();
        const [a, b] = [...touches.values()];
        const rect = canvas!.getBoundingClientRect();
        if (spread > 0) zoomAt((a.x + b.x) / 2 - rect.left, (a.y + b.y) / 2 - rect.top, next / spread);
        spread = next;
        return;
      }
      if (!dragging) {
        const { x, y } = local(event);
        canvas!.style.cursor = hitAt(x, y) ? "pointer" : "grab";
        return;
      }
      const dx = event.clientX - lastX;
      const dy = event.clientY - lastY;
      if (!dragged && Math.abs(dx) + Math.abs(dy) < 4) return;
      dragged = true;
      canvas!.style.cursor = "grabbing";
      lastX = event.clientX;
      lastY = event.clientY;
      view = { ...view, lon: view.lon - dx / view.k, y: clampY(view.y + dy / view.k) };
      moved = true;
      draw();
    }

    function onUp(event: PointerEvent) {
      touches.delete(event.pointerId);
      if (touches.size > 0) {
        // One finger left after a pinch: it drags on from where it is.
        spread = 0;
        const [rest] = [...touches.values()];
        lastX = rest.x;
        lastY = rest.y;
        return;
      }
      spread = 0;
      if (!dragging) return;
      dragging = false;
      canvas!.style.cursor = "grab";
      if (dragged) return;
      const { x, y } = local(event);
      const group = hitAt(x, y);
      // A group of nearby places opens by zooming on it; a single place is picked.
      if (group && group.keys.length > 1) fit(group.places);
      else selectRef.current(group ? group.keys[0] : null);
    }

    function onWheel(event: WheelEvent) {
      event.preventDefault();
      const { x, y } = local(event);
      zoomAt(x, y, Math.exp(-event.deltaY * 0.0015));
    }

    function onDouble() {
      fit();
    }


    size();
    fit();
    const observer = new ResizeObserver(() => {
      size();
      if (moved) draw();
      else fit();
    });
    observer.observe(parent);
    canvas.addEventListener("pointerdown", onDown);
    canvas.addEventListener("pointermove", onMove);
    canvas.addEventListener("pointerup", onUp);
    canvas.addEventListener("pointercancel", onUp);
    canvas.addEventListener("wheel", onWheel, { passive: false });
    canvas.addEventListener("dblclick", onDouble);
    controls.current = { draw, fit: () => fit(), zoom: (factor) => zoomAt(width / 2, height / 2, factor) };
    return () => {
      observer.disconnect();
      canvas.removeEventListener("pointerdown", onDown);
      canvas.removeEventListener("pointermove", onMove);
      canvas.removeEventListener("pointerup", onUp);
      canvas.removeEventListener("pointercancel", onUp);
      canvas.removeEventListener("wheel", onWheel);
      canvas.removeEventListener("dblclick", onDouble);
      controls.current = null;
    };
  }, [theme]);

  useEffect(() => {
    controls.current?.draw();
  }, [sites]);

  useEffect(() => {
    controls.current?.fit();
  }, [fitKey]);

  return (
    <>
      <canvas ref={canvasRef} role="img" aria-label={label} className="block size-full touch-none" />
      <div
        className="absolute flex flex-col gap-1 rounded-md border border-line bg-surface p-1"
        style={{ right: inset.right, bottom: inset.bottom }}
      >
        <MapButton icon={Plus} label={text("overview.zoomIn")} onClick={() => controls.current?.zoom(1.5)} />
        <MapButton icon={Minus} label={text("overview.zoomOut")} onClick={() => controls.current?.zoom(1 / 1.5)} />
        <MapButton icon={Maximize} label={text("overview.refit")} onClick={() => controls.current?.fit()} />
      </div>
    </>
  );
}

function MapButton({ icon, label, onClick }: { icon: typeof Plus; label: string; onClick: () => void }) {
  return (
    <span className="flex size-7 items-center justify-center" title={label}>
      <IconButton type="button" icon={icon} label={label} onClick={onClick} />
    </span>
  );
}

function clampY(y: number) {
  return Math.max(bottomY, Math.min(topY, y));
}
