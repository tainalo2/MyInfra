# Builds the phone screens of the MyInfra canvas: one .dc.html per screen,
# from small building blocks so every screen reads the same way.
import json, os, sys

OUT = sys.argv[1]

INK, MUTED, FAINT, LINE, CANVAS, STATUS = '#1c1917', '#57534e', '#78716c', '#e7e5e4', '#fafaf9', '#f5f5f4'
TONE = {'ok': '#3f6212', 'warn': '#92400e', 'bad': '#991b1b', 'idle': '#a8a29e', 'link': '#1d4ed8'}

I = {
 'globe': '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>',
 'grid': '<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>',
 'network': '<rect x="16" y="16" width="6" height="6" rx="1"/><rect x="2" y="16" width="6" height="6" rx="1"/><rect x="9" y="2" width="6" height="6" rx="1"/><path d="M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3"/><path d="M12 12V8"/>',
 'history': '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
 'menu': '<path d="M4 6h16"/><path d="M4 12h16"/><path d="M4 18h16"/>',
 'plus': '<path d="M5 12h14"/><path d="M12 5v14"/>',
 'chevR': '<path d="m9 18 6-6-6-6"/>', 'chevL': '<path d="m15 18-6-6 6-6"/>', 'chevD': '<path d="m6 9 6 6 6-6"/>',
 'search': '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
 'server': '<rect width="20" height="8" x="2" y="2" rx="2"/><rect width="20" height="8" x="2" y="14" rx="2"/><path d="M6 6h.01"/><path d="M6 18h.01"/>',
 'box': '<path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>',
 'db': '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14a9 3 0 0 0 18 0V5"/><path d="M3 12a9 3 0 0 0 18 0"/>',
 'disk': '<path d="M22 12H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><path d="M6 16h.01"/><path d="M10 16h.01"/>',
 'shield': '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>',
 'key': '<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/>',
 'users': '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
 'user': '<circle cx="12" cy="8" r="5"/><path d="M20 21a8 8 0 0 0-16 0"/>',
 'settings': '<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>',
 'play': '<polygon points="6 3 20 12 6 21 6 3"/>',
 'pause': '<rect x="14" y="4" width="4" height="16" rx="1"/><rect x="6" y="4" width="4" height="16" rx="1"/>',
 'open': '<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>',
 'refresh': '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
 'up': '<circle cx="12" cy="12" r="10"/><path d="m16 12-4-4-4 4"/><path d="M12 16V8"/>',
 'download': '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/><path d="M12 15V3"/>',
 'upload': '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m17 8-5-5-5 5"/><path d="M12 3v12"/>',
 'alert': '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
 'check': '<path d="M20 6 9 17l-5-5"/>',
 'clock': '<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>',
 'git': '<circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M6 21V9a9 9 0 0 0 9 9"/>',
 'image': '<rect width="18" height="18" x="3" y="3" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.09-3.09a2 2 0 0 0-2.82 0L6 21"/>',
 'cloud': '<path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>',
 'terminal': '<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>',
 'file': '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M16 13H8"/><path d="M16 17H8"/>',
 'layers': '<path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>',
 'cpu': '<rect width="16" height="16" x="4" y="4" rx="2"/><rect width="6" height="6" x="9" y="9" rx="1"/><path d="M15 2v2"/><path d="M15 20v2"/><path d="M2 15h2"/><path d="M2 9h2"/><path d="M20 15h2"/><path d="M20 9h2"/><path d="M9 2v2"/><path d="M9 20v2"/>',
 'mem': '<path d="M6 19v-3"/><path d="M10 19v-3"/><path d="M14 19v-3"/><path d="M18 19v-3"/><path d="M8 11V9"/><path d="M16 11V9"/><path d="M12 11V9"/><path d="M2 15h20"/><path d="M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1.1a2 2 0 0 0 0 3.837V17a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5.1a2 2 0 0 0 0-3.837Z"/>',
 'lock': '<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
 'moon': '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
 'lang': '<path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/>',
 'trash': '<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>',
 'copy': '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
 'link': '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
 'pin': '<path d="M20 10c0 4.993-5.539 10.193-7.399 11.799a1 1 0 0 1-1.202 0C9.539 20.193 4 14.993 4 10a8 8 0 0 1 16 0"/><circle cx="12" cy="10" r="3"/>',
 'bot': '<path d="M12 8V4H8"/><rect width="16" height="12" x="4" y="8" rx="2"/><path d="M2 14h2"/><path d="M20 14h2"/><path d="M15 13v2"/><path d="M9 13v2"/>',
 'code': '<path d="m16 18 6-6-6-6"/><path d="m8 6-6 6 6 6"/>',
 'archive': '<rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>',
 'power': '<path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.77.04"/>',
 'swap': '<path d="m16 3 4 4-4 4"/><path d="M20 7H4"/><path d="m8 21-4-4 4-4"/><path d="M4 17h16"/>',
 'eye': '<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>',
 'braces': '<path d="M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5c0 1.1.9 2 2 2h1"/><path d="M16 21h1a2 2 0 0 0 2-2v-5c0-1.1.9-2 2-2a2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1"/>',
 'dots': '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
 'heart': '<path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7Z"/>',
 'x': '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
 'loader': '<path d="M21 12a9 9 0 1 1-6.219-8.56"/>',
 'book': '<path d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H19a1 1 0 0 1 1 1v18a1 1 0 0 1-1 1H6.5a1 1 0 0 1 0-5H20"/>',
 'tag': '<path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z"/><circle cx="7.5" cy="7.5" r=".5" fill="currentColor"/>',
 'radio': '<path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9"/><path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5"/><circle cx="12" cy="12" r="2"/><path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5"/><path d="M19.1 4.9C23 8.8 23 15.1 19.1 19"/>',
 'spark': '<path d="M12 3v3"/><path d="M12 18v3"/><path d="m4.9 4.9 2.1 2.1"/><path d="m17 17 2.1 2.1"/><path d="M3 12h3"/><path d="M18 12h3"/><path d="m4.9 19.1 2.1-2.1"/><path d="m17 7 2.1-2.1"/>',
 'bell': '<path d="M10.268 21a2 2 0 0 0 3.464 0"/><path d="M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"/>',
 'list': '<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>',
}

def ico(name, size=20, color=MUTED, extra=''):
    return f'<svg viewBox="0 0 24 24" aria-hidden="true" style="width: {size}px; height: {size}px; flex: none; fill: none; stroke: {color}; stroke-width: 1.75; stroke-linecap: round; stroke-linejoin: round;{extra}">{I[name]}</svg>'

def esc(t):
    return str(t).replace('&', '&amp;').replace('<', '&lt;').replace('>', '&gt;').replace('{{', '{ {')

def dot(tone, size=8):
    return f'<span aria-hidden="true" style="width: {size}px; height: {size}px; border-radius: 9999px; background: {TONE[tone]}; flex: none;"></span>'

def state(tone, text):
    return f'<span style="display: inline-flex; align-items: center; gap: 6px; font-size: 13px; color: {TONE[tone] if tone != "idle" else MUTED}; white-space: nowrap;">{dot(tone)}{esc(text)}</span>'

def tile(icon=None, letter=None, tone=None):
    inner = ico(icon, 20, TONE[tone] if tone else MUTED) if icon else f'<span style="font-weight: 500; color: {MUTED};">{esc(letter)}</span>'
    return f'<span aria-hidden="true" style="flex: none; width: 40px; height: 40px; border-radius: 6px; background: {STATUS}; display: inline-flex; align-items: center; justify-content: center;">{inner}</span>'

def row(title, sub=None, icon=None, letter=None, right=None, href=None, chev=True, mono=False, tone=None, sub_tone=None, danger=False):
    lead = tile(icon, letter, tone) if (icon or letter) else ''
    tcol = TONE['bad'] if danger else INK
    s = f'<span style="display: block; font-size: 15px; color: {tcol}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;{" font-family: Geist Mono, monospace; font-size: 14px;" if mono else ""}">{esc(title)}</span>'
    if sub:
        s += f'<span style="display: block; font-size: 13px; color: {TONE[sub_tone] if sub_tone else MUTED}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{esc(sub)}</span>'
    r = right if right else ''
    c = ico('chevR', 18, '#a8a29e') if chev else ''
    tag = 'a' if href is not None else 'div'
    h = f' href="{href}"' if href is not None else ''
    return f'<{tag}{h} style="display: flex; align-items: center; gap: 12px; min-height: 56px; padding: 8px 16px; color: {INK}; text-decoration: none;">{lead}<span style="flex: 1; min-width: 0;">{s}</span>{r}{c}</{tag}>'

def group(title, rows, action=None, note=None):
    head = ''
    if title:
        a = f'<span style="font-size: 14px; color: {TONE["link"]};">{esc(action)}</span>' if action else ''
        head = f'<div style="display: flex; justify-content: space-between; align-items: baseline; padding: 0 4px 8px;"><h2 style="margin: 0; font-size: 13px; font-weight: 500; color: {MUTED};">{esc(title)}</h2>{a}</div>'
    body = f'<div style="background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; overflow: hidden;">' + f'<div style="height: 1px; background: {LINE}; margin-left: 68px;"></div>'.join(rows) + '</div>'
    n = f'<p style="margin: 8px 4px 0; font-size: 13px; color: {FAINT};">{esc(note)}</p>' if note else ''
    return f'<section style="margin-bottom: 24px;">{head}{body}{n}</section>'

def notice(tone, title, sub=None, action=None, icon='alert', href=None):
    s = f'<span style="display: block; font-size: 15px; color: {TONE[tone]};">{esc(title)}</span>'
    if sub: s += f'<span style="display: block; font-size: 13px; color: {MUTED};">{esc(sub)}</span>'
    a = f'<span style="font-size: 14px; color: {INK}; white-space: nowrap;">{esc(action)}</span>' if action else ''
    tag = 'a' if href else 'div'
    h = f' href="{href}"' if href else ''
    return f'<{tag}{h} style="display: flex; align-items: center; gap: 12px; padding: 12px 16px; margin-bottom: 24px; background: #ffffff; border: 1px solid {LINE}; border-left: 3px solid {TONE[tone]}; border-radius: 6px; color: {INK}; text-decoration: none;">{ico(icon, 20, TONE[tone])}<span style="flex: 1; min-width: 0;">{s}</span>{a}</{tag}>'

def tiles(items):
    out = ''
    for icon, label, value, sub, tone in items:
        sb = f'<span style="font-size: 13px; color: {TONE[tone] if tone else MUTED};">{esc(sub)}</span>' if sub else ''
        out += f'<div style="background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; padding: 14px; display: flex; flex-direction: column; gap: 6px;"><span style="display: flex; align-items: center; gap: 8px; font-size: 13px; color: {MUTED};">{ico(icon, 16)}{esc(label)}</span><span style="font-size: 22px; font-weight: 500; line-height: 1.2;">{value}</span>{sb}</div>'
    return f'<div style="display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-bottom: 24px;">{out}</div>'

def meters(items):
    out = ''
    for icon, label, pct, value in items:
        out += f'<div style="flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px;"><span style="display: flex; align-items: center; gap: 6px; font-size: 12px; color: {MUTED};">{ico(icon, 14)}{esc(label)}</span><span aria-hidden="true" style="height: 4px; border-radius: 9999px; background: {LINE}; overflow: hidden;"><span style="display: block; height: 100%; width: {pct}%; background: {MUTED};"></span></span><span style="font-size: 13px;">{esc(value)}</span></div>'
    return f'<div style="display: flex; gap: 16px; padding: 14px 16px; background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; margin-bottom: 16px;">{out}</div>'

def actions(items):
    out = ''
    for icon, label, primary in items:
        bg, fg, bd = (INK, CANVAS, INK) if primary else ('#ffffff', INK, LINE)
        out += f'<a href="#" style="display: flex; flex-direction: column; align-items: center; gap: 6px; color: {INK}; text-decoration: none; font-size: 12px;"><span style="width: 48px; height: 48px; border-radius: 9999px; background: {bg}; border: 1px solid {bd}; display: inline-flex; align-items: center; justify-content: center;">{ico(icon, 20, fg)}</span>{esc(label)}</a>'
    return f'<div style="display: grid; grid-template-columns: repeat({len(items)}, minmax(0, 1fr)); gap: 8px; margin-bottom: 24px;">{out}</div>'

def segmented(items, active=0, scroll=False):
    out = ''
    for i, t in enumerate(items):
        on = i == active
        out += f'<span style="flex: {"none" if scroll else "1"}; text-align: center; padding: 7px 12px; border-radius: 6px; font-size: 14px; white-space: nowrap; background: {"#ffffff" if on else "transparent"}; color: {INK if on else MUTED}; box-shadow: {"0 1px 2px rgba(28,25,23,.08)" if on else "none"};">{esc(t)}</span>'
    return f'<div role="tablist" style="display: flex; gap: 2px; padding: 3px; background: {STATUS}; border-radius: 8px; margin-bottom: 20px; overflow-x: auto;">{out}</div>'

def chips(items, active=0):
    out = ''
    for i, t in enumerate(items):
        on = i == active
        out += f'<span style="flex: none; padding: 6px 12px; border-radius: 9999px; font-size: 14px; border: 1px solid {INK if on else LINE}; background: {INK if on else "#ffffff"}; color: {CANVAS if on else INK}; white-space: nowrap;">{esc(t)}</span>'
    return f'<div style="display: flex; gap: 8px; overflow-x: auto; margin: 0 -16px 20px; padding: 0 16px;">{out}</div>'

def searchbox(ph):
    return f'<div style="display: flex; align-items: center; gap: 10px; height: 44px; padding: 0 14px; background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; margin-bottom: 16px; color: {FAINT};">{ico("search", 18, FAINT)}<span style="font-size: 15px;">{esc(ph)}</span></div>'

def field(label, value='', ph='', mono=False, suffix=None, hint=None):
    v = f'<span style="color: {INK if value else FAINT};{" font-family: Geist Mono, monospace; font-size: 14px;" if mono else ""}">{esc(value or ph)}</span>'
    s = f'<span style="color: {MUTED};">{esc(suffix)}</span>' if suffix else ''
    hn = f'<span style="font-size: 13px; color: {FAINT};">{esc(hint)}</span>' if hint else ''
    return f'<label style="display: flex; flex-direction: column; gap: 6px; margin-bottom: 16px;"><span style="font-size: 14px; font-weight: 500;">{esc(label)}</span><span style="display: flex; align-items: center; justify-content: space-between; gap: 8px; height: 44px; padding: 0 14px; background: #ffffff; border: 1px solid #d6d3d1; border-radius: 6px; font-size: 15px;">{v}{s}</span>{hn}</label>'

def choice(title, opts, active=0, note=None):
    rows = []
    for i, o in enumerate(opts):
        icon, t, sub = o[0], o[1], o[2]
        on = i == active
        radio = f'<span aria-hidden="true" style="flex: none; width: 20px; height: 20px; border-radius: 9999px; border: {"6px solid " + INK if on else "1.5px solid #a8a29e"}; box-sizing: border-box;"></span>'
        rows.append(row(t, sub, icon=icon if icon and len(icon) > 1 else None, letter=icon if icon and len(icon) == 1 else None, right=radio, chev=False))
    return group(title, rows, note=note)

def primary(label, icon=None, secondary=None):
    i = ico(icon, 18, CANVAS) if icon else ''
    s = f'<a href="#" style="display: flex; justify-content: center; padding: 12px; color: {INK}; text-decoration: none; font-size: 15px;">{esc(secondary)}</a>' if secondary else ''
    return f'<div style="margin: 8px 0 24px;"><a href="#" style="display: flex; align-items: center; justify-content: center; gap: 8px; height: 48px; border-radius: 6px; background: {INK}; color: {CANVAS}; text-decoration: none; font-size: 15px; font-weight: 500;">{i}{esc(label)}</a>{s}</div>'

def steps(items):
    out = ''
    for i, (st, title, sub) in enumerate(items):
        mark = {'done': f'<span style="width: 24px; height: 24px; border-radius: 9999px; background: {INK}; display: inline-flex; align-items: center; justify-content: center;">{ico("check", 14, CANVAS)}</span>',
                'run': f'<span style="width: 24px; height: 24px; border-radius: 9999px; border: 2px solid {TONE["warn"]}; box-sizing: border-box; display: inline-flex; align-items: center; justify-content: center;">{dot("warn", 8)}</span>',
                'wait': f'<span style="width: 24px; height: 24px; border-radius: 9999px; border: 1.5px dashed #a8a29e; box-sizing: border-box;"></span>'}[st]
        line = f'<span aria-hidden="true" style="flex: 1; width: 1.5px; background: {LINE}; margin: 4px 0;"></span>' if i < len(items) - 1 else ''
        col = {'done': INK, 'run': TONE['warn'], 'wait': MUTED}[st]
        out += f'<div style="display: flex; gap: 12px;"><span style="display: flex; flex-direction: column; align-items: center;">{mark}{line}</span><span style="flex: 1; padding-bottom: 18px;"><span style="display: block; font-size: 15px; color: {col};">{esc(title)}</span><span style="display: block; font-size: 13px; color: {MUTED};">{esc(sub)}</span></span></div>'
    return f'<div style="background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; padding: 16px 16px 0; margin-bottom: 24px;">{out}</div>'

def progress(label, pct, right):
    return f'<div style="margin-bottom: 20px;"><div style="display: flex; justify-content: space-between; font-size: 13px; color: {MUTED}; margin-bottom: 6px;"><span>{esc(label)}</span><span>{esc(right)}</span></div><div style="height: 6px; border-radius: 9999px; background: {LINE}; overflow: hidden;"><div style="height: 100%; width: {pct}%; background: {INK};"></div></div></div>'

def toggle(on):
    return f'<span aria-hidden="true" style="flex: none; width: 44px; height: 26px; border-radius: 9999px; background: {INK if on else "#d6d3d1"}; position: relative;"><span style="position: absolute; top: 3px; left: {21 if on else 3}px; width: 20px; height: 20px; border-radius: 9999px; background: #ffffff;"></span></span>'

def text_right(t, tone=None):
    return f'<span style="font-size: 14px; color: {TONE[tone] if tone else MUTED}; white-space: nowrap;">{esc(t)}</span>'

def para(t, small=False):
    return f'<p style="margin: 0 4px 20px; font-size: {13 if small else 15}px; color: {MUTED};">{esc(t)}</p>'

def header(title, sub=None, letter=None, icon=None, status=None, extra=''):
    lead = tile(icon, letter) if (letter or icon) else ''
    st = state(*status) if status else ''
    sb = f'<span style="display: block; font-size: 14px; color: {MUTED};">{sub}</span>' if sub else ''
    return f'<div style="display: flex; gap: 12px; align-items: center; margin-bottom: 16px;">{lead}<div style="flex: 1; min-width: 0;"><div style="display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px;"><span style="font-size: 20px; font-weight: 500;">{esc(title)}</span>{st}</div>{sb}{extra}</div></div>'

TABS = [('Accueil', 'globe', 'Mobile-Ensemble.dc.html'), ('Apps', 'grid', 'Mobile-Applications.dc.html'), ('Serveurs', 'network', 'Mobile-Serveurs.dc.html'), ('Activité', 'history', 'Mobile-Activite.dc.html'), ('Plus', 'menu', 'Mobile-Plus.dc.html')]

def nav(active):
    out = ''
    for i, (lab, icon, href) in enumerate(TABS):
        on = i == active
        badge = ''
        if lab == 'Activité': badge = f'<span style="position: absolute; top: -4px; right: -10px; min-width: 16px; height: 16px; padding: 0 4px; box-sizing: border-box; border-radius: 9999px; background: {TONE["warn"]}; color: {CANVAS}; font-size: 10px; line-height: 16px; text-align: center;">1</span>'
        if lab == 'Plus': badge = f'<span style="position: absolute; top: -2px; right: -4px; width: 8px; height: 8px; border-radius: 9999px; background: {TONE["link"]};"></span>'
        out += f'<a href="{href}"{" aria-current=\"page\"" if on else ""} style="display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; min-height: 58px; font-size: 11px; text-decoration: none; color: {INK if on else FAINT};{" font-weight: 500;" if on else ""}"><span style="position: relative; display: inline-flex;">{ico(icon, 22, INK if on else FAINT)}{badge}</span>{lab}</a>'
    return f'<nav aria-label="Navigation principale" style="position: sticky; bottom: 0; z-index: 5; display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); background: #ffffff; border-top: 1px solid {LINE};">{out}</nav>'

def topbar(title, back=None, action=None, big=True):
    b = f'<a href="{back[1]}" style="display: inline-flex; align-items: center; gap: 2px; margin-left: -6px; color: {TONE["link"]}; text-decoration: none; font-size: 15px;">{ico("chevL", 22, TONE["link"])}{esc(back[0])}</a>' if back else '<span></span>'
    a = ''
    if action:
        a = ''.join(f'<a href="{h}" aria-label="{esc(lab)}" style="width: 40px; height: 40px; display: inline-flex; align-items: center; justify-content: center; border-radius: 9999px; color: {INK};">{ico(icon, 22, INK)}</a>' for icon, lab, h in action)
    bar = f'<div style="display: flex; align-items: center; justify-content: space-between; height: 48px; padding: 0 10px 0 16px;">{b}<span style="display: flex;">{a}</span></div>'
    t = f'<h1 style="margin: 0; padding: 0 16px 12px; font-size: 28px; font-weight: 500; line-height: 1.2;">{esc(title)}</h1>' if big else ''
    return f'<header style="position: sticky; top: 0; z-index: 5; background: {CANVAS};">{bar}{t}</header>'

def page(name, title, body, tab, back=None, action=None, big=True, interactive=False):
    html = f'''<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<title>{esc(title)} · mobile</title>
<script src="./support.js"></script>
</head>
<body>
<x-dc>
<helmet>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500&amp;family=Geist+Mono:wght@400;500&amp;display=swap">
<style>body{{margin:0;background:{CANVAS}}} *{{-webkit-tap-highlight-color:transparent}} a{{color:{TONE["link"]}}}</style>
</helmet>
<div style="min-height: 844px; display: flex; flex-direction: column; background: {CANVAS}; color: {INK}; font-family: 'Geist', ui-sans-serif, system-ui, sans-serif; font-size: 15px; line-height: 1.45;">
{topbar(title, back, action, big)}
<main style="flex: 1; padding: 4px 16px 16px;">
{body}
</main>
{nav(tab)}
</div>
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{{"$preview":{{"width":390,"height":844}}}}'>
class Component extends DCLogic {{ renderVals() {{ return {{}}; }} }}
</script>
</body>
</html>
'''
    open(os.path.join(OUT, f'Mobile-{name}.dc.html'), 'w').write(html)

M = lambda n: f'Mobile-{n}.dc.html'
OK = lambda t='En ligne': state('ok', t)
WARN = lambda t: state('warn', t)

# ---------- Accueil
page('Ensemble', "Vue d'ensemble", ''.join([
    tiles([('grid', 'Apps', '7<span style="font-size: 15px; color: #57534e;"> / 8</span>', '1 démarre', 'warn'),
           ('network', 'Serveurs', '4<span style="font-size: 15px; color: #57534e;"> / 4</span>', '3 lieux', None),
           ('box', 'Pods', '13<span style="font-size: 15px; color: #57534e;"> / 14</span>', '1 démarre', 'warn'),
           ('tag', 'Coût', '104 €', 'par mois', None)]),
    group('À traiter · 2', [row('Uptime Kuma', 'Un seul serveur', icon='alert', tone='warn', href=M('Applications')),
                            row('MeTube', 'Un seul serveur', icon='alert', tone='warn', href=M('Applications'))]),
    group('Activité', [row('Billing API déployée', 'il y a 4 min', icon='refresh', href=M('Activite')),
                       row('Site vitrine en 2.3.0', 'il y a 3 jours', icon='up', href=M('Activite')),
                       row('fsn-1 mis à jour', 'il y a 5 jours', icon='server', href=M('Activite'))], action='Tout'),
]), 0, action=[('search', 'Rechercher', '#')])

# ---------- Apps
apps = [('U', 'Uptime Kuma', 'status.example.com', 'ok', 'single'), ('V', 'Vikunja', 'tasks.example.com', 'ok', ''), ('N', 'nextcloud', 'cloud.example.com', 'warn', 'pod'),
        ('H', 'HedgeDoc', 'notes.example.com', 'ok', ''), ('E', 'Excalidraw', 'draw.example.com', 'ok', ''), ('M', 'MeTube', 'videos.example.com', 'ok', 'single'),
        ('S', 'Site vitrine', 'www.example.com', 'ok', ''), ('B', 'Billing API', 'api.example.com', 'warn', 'prep'), ('w', 'whoami', 'edge.example.com', 'ok', '')]
def app_right(t, x):
    if x == 'prep': return WARN('Démarre')
    if x == 'pod': return WARN('1 pod')
    if x == 'single': return f'<span title="Un seul serveur" style="display: inline-flex;">{ico("alert", 16, TONE["warn"])}</span>{dot(t)}'
    return dot(t)
page('Applications', 'Applications', ''.join([
    searchbox('Nom ou adresse'),
    chips(['Toutes 9', 'En ligne 7', 'À surveiller 2', 'En pause 0']),
    group(None, [row(n, d, letter=l, right=f'<span style="display: inline-flex; align-items: center; gap: 8px;">{app_right(t, x)}</span>', href=M('AppPlan') if n == 'nextcloud' else '#') for l, n, d, t, x in apps]),
    para('Le triangle signale une app sur un seul serveur : si le serveur tombe, elle s’arrête.', True),
]), 1, action=[('plus', 'Nouvelle application', M('Nouvelle'))])

# ---------- Fiche app (no plan on phones)
page('AppPlan', 'nextcloud', ''.join([
    header('nextcloud', '<a href="#" style="font-family: Geist Mono, monospace; font-size: 13px;">cloud.example.com</a>', letter='N', status=('ok', 'En ligne')),
    meters([('cpu', 'CPU', 22, '1,3 / 6'), ('mem', 'RAM', 19, '2,3 / 12 Go'), ('disk', 'Disque', 11, '19 / 180 Go')]),
    actions([('open', 'Ouvrir', True), ('pause', 'Pause', False), ('up', 'Mettre à jour', False), ('dots', 'Plus', False)]),
    notice('warn', 'cron redémarre', '3 fois depuis 10:12, sur fr-par-3', 'Voir', href=M('AppService')),
    group('Services · 5', [row('nextcloud', '2 copies · public', icon='globe', right=dot('ok'), href=M('AppService')),
                           row('turn', '3 copies · port 3478 direct', icon='radio', right=dot('ok'), href=M('AppService')),
                           row('cron', 'Redémarre', icon='clock', sub_tone='warn', right=dot('warn'), href=M('AppService')),
                           row('redis', '1 copie · interne', icon='box', right=dot('ok'), href=M('AppService')),
                           row('db', '2 copies répliquées', icon='db', right=dot('ok'), href=M('AppService'))]),
    group('Stockage', [row('fichiers', '18 / 50 Go · 3 exemplaires', icon='disk'),
                       row('db-data', '3,1 / 10 Go · 2 disques', icon='disk'),
                       row('Sauvegardes', 'Cette nuit, 04:02', icon='archive', right=dot('ok'))]),
    group('Adresses', [row('cloud.example.com', None, icon='link', right=OK('Prête'), mono=True),
                       row('files.example.com', None, icon='link', right=WARN('En attente'), mono=True)], action='Modifier'),
    group('Réglages', [row('Variables', None, icon='braces', right=text_right('4')),
                       row('Secrets', None, icon='lock', right=text_right('1')),
                       row('Activité', None, icon='history')]),
    group(None, [row('Enregistrer comme paquet', None, icon='box', chev=False),
                 row('Supprimer l’application', None, icon='trash', tone='bad', danger=True, chev=False)]),
]), 1, back=('Apps', M('Applications')), action=[('dots', 'Plus d’actions', '#')], big=False)

# ---------- Service of an app
pod = lambda name, srv, tone, st, cpu, ram: f'''<div style="padding: 14px 16px; display: flex; flex-direction: column; gap: 10px;"><div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;"><span style="font-family: Geist Mono, monospace; font-size: 14px;">{name}</span>{state(tone, st)}</div><span style="display: flex; align-items: center; gap: 6px; font-size: 13px; color: {MUTED};">{ico('server', 14)}{srv}</span>{meters_inline(cpu, ram)}<div style="display: flex; gap: 8px;">{pill('file', 'Journal')}{pill('refresh', 'Relancer')}{pill('terminal', 'Terminal')}</div></div>'''
def meters_inline(cpu, ram):
    def m(icon, lab, p, v): return f'<span style="flex: 1; display: flex; flex-direction: column; gap: 4px;"><span style="display: flex; align-items: center; gap: 4px; font-size: 12px; color: {MUTED};">{ico(icon, 13)}{lab} · {v}</span><span style="height: 4px; border-radius: 9999px; background: {LINE}; overflow: hidden;"><span style="display: block; height: 100%; width: {p}%; background: {MUTED};"></span></span></span>'
    return f'<div style="display: flex; gap: 16px;">{m("cpu", "CPU", cpu[0], cpu[1])}{m("mem", "RAM", ram[0], ram[1])}</div>'
def pill(icon, label):
    return f'<a href="#" style="display: inline-flex; align-items: center; gap: 6px; height: 34px; padding: 0 12px; border: 1px solid {LINE}; border-radius: 9999px; color: {INK}; text-decoration: none; font-size: 13px;">{ico(icon, 15, INK)}{label}</a>'
page('AppService', 'cron', ''.join([
    header('cron', 'Service de nextcloud', icon='clock', status=('warn', 'Redémarre')),
    segmented(['Pods', 'Image', 'Réseau', 'Disques']),
    f'<div style="background: #ffffff; border: 1px solid {LINE}; border-left: 3px solid {TONE["warn"]}; border-radius: 6px; margin-bottom: 12px;">{pod("cron-5d2-q", "fr-par-3", "warn", "3 redémarrages", (2, "0 cœur"), (7, "18 Mo"))}</div>',
    f'<pre style="margin: 0 0 24px; padding: 12px; background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; font-family: Geist Mono, monospace; font-size: 12px; line-height: 1.5; white-space: pre-wrap; color: {INK};">10:41 redis:6379 injoignable\n10:41 Sortie (code 1)\n10:41 Redémarrage (3)</pre>',
    group('Image', [row('nextcloud:30-apache', 'Mise à jour : 30.0.4', icon='image', mono=True, sub_tone='link')]),
]), 1, back=('nextcloud', M('AppPlan')), big=False)

# ---------- Nouvelle application
page('Nouvelle', 'Nouvelle app', ''.join([
    group('D’où vient-elle ?', [row('Catalogue', 'Prête à installer', icon='book', href=M('Article')),
                                row('Mes paquets', 'Plusieurs services', icon='layers', href=M('PaquetPlan')),
                                row('Une image', 'De vos registres', icon='image', href=M('SourceImage')),
                                row('Un dépôt Git', 'Construite par MyInfra', icon='git')]),
    searchbox('Rechercher dans le catalogue'),
    chips(['Sites', 'Collaboration', 'Code', 'Médias', 'Outils']),
    group('Populaires', [row('WordPress', 'Site et blog', letter='W', href=M('Article')), row('Ghost', 'Publication', letter='G'),
                         row('Wiki.js', 'Wiki partagé', letter='W'), row('BookStack', 'Notes en étagères', letter='B')]),
]), 1, back=('Apps', M('Applications')))

# ---------- Article
page('Article', 'WordPress', ''.join([
    header('WordPress', 'Site et blog · v6.6', letter='W'),
    para('Pages, articles, thèmes et extensions, avec une interface d’administration.'),
    group('MyInfra installe', [row('Le site', 'Avec son adresse HTTPS', icon='globe', chev=False),
                               row('Sa base de données', 'Interne', icon='db', chev=False),
                               row('Deux disques', 'Gardés aux mises à jour', icon='disk', chev=False)]),
    group('En bref', [row('Taille conseillée', None, icon='server', right=text_right('Petit'), chev=False),
                      row('Catégorie', None, icon='tag', right=text_right('Sites'), chev=False),
                      row('Site officiel', None, icon='open', right=text_right('wordpress.org', 'link'), chev=False)]),
    primary('Installer', 'download', 'Copier dans mes paquets'),
]), 1, back=('Catalogue', M('Nouvelle')), big=False)

# ---------- Installer (form)
page('Formulaire', 'Installer', ''.join([
    header('WordPress', '6.6 · conseillée', letter='W'),
    field('Adresse', 'blog', suffix='.example.com', hint='https://blog.example.com · libre'),
    choice('Où', [('network', 'Cluster production', '3 serveurs · conseillé'), ('server', 'edge-hel', 'Machine seule · Helsinki')]),
    f'<h2 style="margin: 0 4px 8px; font-size: 13px; font-weight: 500; color: {MUTED};">Copies</h2>',
    segmented(['1', '2', '3'], 1),
    para('2 copies sur 2 serveurs : si l’un tombe, l’autre répond.', True),
    group('Récapitulatif', [row('Application', None, icon='box', right=text_right('WordPress 6.6'), chev=False),
                            row('Disques', None, icon='disk', right=text_right('2, gardés'), chev=False)]),
    primary('Installer', 'download'),
]), 1, back=('WordPress', M('Article')))

# ---------- Depuis une image
page('SourceImage', 'Une image', ''.join([
    field('Image', 'billing', mono=True),
    group('Résultats', [row('billing-api:1.8.0', 'ghcr.io/acme · il y a 2 j', icon='image', mono=True, chev=False, right=f'<span style="display: inline-flex;">{ico("check", 18, INK)}</span>'),
                        row('billing-worker:1.8.0', 'ghcr.io/acme', icon='image', mono=True, chev=False),
                        row('billing-api:1.7.2', 'Importée', icon='upload', mono=True, chev=False)]),
    group('Lu dans l’image', [row('Port', None, icon='radio', right=text_right('8080'), chev=False),
                              row('Données', None, icon='disk', right=text_right('/data'), chev=False),
                              row('DATABASE_URL', 'À renseigner', icon='braces', sub_tone='warn', mono=True)]),
    field('Nom', 'billing-api', mono=True),
    primary('Continuer'),
]), 1, back=('Nouvelle', M('Nouvelle')))

# ---------- Installation en cours
page('Installation', 'Installation', ''.join([
    header('blog', 'WordPress 6.6 · production', letter='W', status=('warn', 'En cours')),
    progress('Étape 3 sur 4', 62, 'environ 2 min'),
    steps([('done', 'Disques prêts', 'Site et base'), ('done', 'Base démarrée', 'Interne'), ('run', 'Site en démarrage', '1 copie sur 2 prête'), ('wait', 'Adresse publiée', 'blog.example.com')]),
    para('Vous pouvez quitter cette page : l’installation continue.', True),
    primary('Aller à la fiche'),
]), 1, back=('Apps', M('Applications')), big=False)

# ---------- Serveurs
page('Serveurs', 'Serveurs', ''.join([
    notice('warn', 'Un seul pilote', 'Si par-1 tombe, plus rien ne se déploie', 'Choisir'),
    header('production', '3 serveurs · 0,13 €/h', icon='network', status=('ok', 'En service')),
    meters([('cpu', 'CPU', 18, '2,1 / 12'), ('mem', 'RAM', 28, '11 / 40 Go'), ('disk', 'Disque', 15, '48 / 320 Go')]),
    group(None, [row('par-1', 'Paris · Pilote', icon='server', right=dot('ok')),
                 row('ams-1', 'Amsterdam', icon='server', right=dot('ok')),
                 row('fsn-1', 'Falkenstein', icon='server', right=dot('ok')),
                 row('Ajouter un serveur', None, icon='plus', href=M('NouveauServeur'))]),
    group('Machines seules', [row('edge-hel', 'Helsinki · 1 pod', icon='server', right=dot('ok'))]),
]), 2, action=[('plus', 'Nouveau serveur', M('NouveauServeur'))])

# ---------- Nouveau serveur
page('NouveauServeur', 'Nouveau serveur', ''.join([
    choice('1 · Hébergeur', [('H', 'Hetzner', 'Compte perso'), ('S', 'Scaleway', 'Compte pro'), ('plus', 'Serveur existant', 'Le vôtre')]),
    choice('2 · Lieu', [('pin', 'Nuremberg', 'Nouveau lieu'), ('pin', 'Falkenstein', 'Déjà 1 serveur'), ('pin', 'Helsinki', 'Nouveau lieu')]),
    choice('3 · Taille', [('server', 'Petit', '2 cœurs · 4 Go · [prix]/mois'), ('server', 'Moyen', '4 cœurs · 8 Go · [prix]/mois'), ('server', 'Grand', '8 cœurs · 16 Go · [prix]/mois')], 1),
    field('4 · Nom', 'nbg-1', mono=True, hint='Proposé d’après le lieu'),
    primary('Créer le serveur', 'plus'),
]), 2, back=('Serveurs', M('Serveurs')))

# ---------- Templates
page('Templates', 'Templates', ''.join([
    notice('warn', 'Espace d’équipe', 'Étape 2 sur 4', 'Suivre', icon='loader', href=M('Suivi')),
    group('Mes templates', [row('Espace d’équipe', '3 apps · 3 serveurs', icon='layers'), row('Site et tableau blanc', '2 apps · jamais déployé', icon='layers')]),
    group('Bibliothèque', [row('Premiers pas', 'Accueil, suivi, mots de passe', icon='spark'), row('Site et audience', 'WordPress et Umami', icon='globe'),
                           row('Espace d’équipe', 'Fichiers, agenda, messages', icon='users'), row('Base de connaissances', 'Wiki et notes', icon='book')]),
]), 4, back=('Plus', M('Plus')), action=[('plus', 'Nouveau template', '#')])

# ---------- Suivi
page('Suivi', 'Déploiement', ''.join([
    header('Espace d’équipe', 'Cluster team · il y a 12 min', icon='layers', status=('warn', 'En cours')),
    progress('Étape 2 sur 4', 45, ''),
    steps([('done', 'Cluster créé', 'En 1 min'), ('run', 'Serveurs en préparation', 'team-1 prêt · team-2 4/5 · team-3 2/5'), ('wait', 'Applications', 'Homepage, Vaultwarden, Uptime Kuma'), ('wait', 'Adresses publiées', '')]),
    group(None, [row('Journal détaillé', None, icon='file'), row('Arrêter le déploiement', None, icon='x', tone='bad', danger=True, chev=False)]),
]), 4, back=('Templates', M('Templates')), big=False)

# ---------- Activité
page('Activite', 'Activité', ''.join([
    chips(['Tout', 'Problèmes 2', 'Déploiements 3', 'Mises à jour 2', 'Serveurs 1']),
    notice('warn', 'Billing API se déploie', '2e essai · il y a 4 min', 'Suivre', icon='loader'),
    group('Aujourd’hui', [row('Billing API relancée', '10:42 · Assistant MCP', icon='refresh'),
                          row('Billing API ne démarre pas', '10:40 · Échec', icon='x', tone='bad', sub_tone='bad'),
                          row('Uptime Kuma sur un seul serveur', '09:12', icon='alert', tone='warn')]),
    group('Vendredi', [row('Site vitrine en 2.3.0', '16:05 · Vous', icon='up')]),
    group('Mercredi', [row('fsn-1 mis à jour', '03:18 · nuit', icon='server'), row('ams-1 mis à jour', '03:02 · nuit', icon='server')]),
]), 3, action=[('search', 'Rechercher', '#')])

# ---------- Mises à jour
page('MAJ', 'Mises à jour', ''.join([
    f'<div style="background: #ffffff; border: 1px solid {LINE}; border-radius: 6px; padding: 16px; margin-bottom: 24px;"><span style="display: block; font-size: 15px; margin-bottom: 4px;">3 sans interruption</span><span style="display: block; font-size: 13px; color: {MUTED}; margin-bottom: 12px;">MyInfra, Vikunja, Premiers pas</span>{primary("Appliquer les 3").replace("margin: 8px 0 24px", "margin: 0")}</div>',
    group('Sécurité · 2', [row('par-1', 'Correctifs · courte coupure', icon='shield', tone='warn', sub_tone='warn'),
                           row('ams-1', 'Redémarrage requis', icon='shield', tone='warn', sub_tone='warn')]),
    group('Nouvelles versions · 4', [row('MyInfra', '0.14.2 → 0.15.0', icon='up', right=dot('ok')),
                                     row('Vikunja', '0.24.4 → 0.24.6', icon='up', right=dot('ok')),
                                     row('Uptime Kuma', '1.23.15 → 1.23.16 · ~30 s', icon='up', right=dot('warn')),
                                     row('edge-hel', 'Paquets · coupure', icon='server', right=dot('warn'))]),
    group(None, [row('Correctifs chaque nuit', 'Sans interruption seulement · 03:00', icon='clock', right=toggle(False), chev=False)]),
    para('Point vert : sans interruption. Point ambre : courte coupure.', True),
]), 4, back=('Plus', M('Plus')), action=[('refresh', 'Vérifier', '#')])

# ---------- Plus (sheet of the bottom bar)
page('Plus', 'Plus', ''.join([
    group(None, [row('Mon compte', 'Administrateur', icon='user', href='#')]),
    group(None, [row('Templates', None, icon='layers', href=M('Templates')),
                 row('Mises à jour', None, icon='up', right=f'<span style="min-width: 20px; height: 20px; padding: 0 6px; box-sizing: border-box; border-radius: 9999px; background: {TONE["link"]}; color: #fff; font-size: 12px; line-height: 20px; text-align: center;">7</span>', href=M('MAJ')),
                 row('Réglages', None, icon='settings', href=M('Reglages'))]),
    group(None, [row('Langue', None, icon='lang', right=text_right('Français')), row('Thème', None, icon='moon', right=text_right('Clair'))]),
]), 4)

# ---------- Réglages
page('Reglages', 'Réglages', ''.join([
    searchbox('Chercher un réglage'),
    group('Connexions', [row('Hébergeurs', None, icon='cloud', right=text_right('2'), href=M('Providers')),
                         row('Domaines', None, icon='globe', right=f'<span style="display: inline-flex; align-items: center; gap: 6px;">{dot("warn")}{text_right("2")}</span>', href=M('DNS')),
                         row('Images', None, icon='image', right=text_right('3'), href=M('Images')),
                         row('Git', None, icon='git', right=text_right('1'), href=M('Git')),
                         row('API et MCP', None, icon='bot', right=text_right('2'), href=M('APIMCP'))]),
    group('Sécurité', [row('Mon compte', None, icon='user', right=text_right('2FA', 'ok')),
                       row('Clés SSH', None, icon='key', right=text_right('3')),
                       row('Secrets', None, icon='lock', right=text_right('5')),
                       row('Utilisateurs', None, icon='users', right=text_right('2'))]),
    group('Préférences', [row('Langue', None, icon='lang', right=text_right('Français')),
                          row('Thème', None, icon='moon', right=text_right('Clair')),
                          row('Listes', None, icon='list', right=text_right('Cartes'))]),
    group('MyInfra', [row('Version', None, icon='up', right=text_right('0.14.2 · maj', 'link')),
                      row('Transfert', None, icon='swap'), row('Copies', None, icon='copy', right=text_right('0')),
                      row('Arrêter MyInfra', None, icon='power', tone='bad', danger=True, chev=False)]),
]), 4, back=('Plus', M('Plus')))

# ---------- Hébergeurs
page('Providers', 'Hébergeurs', ''.join([
    group(None, [row('Hetzner · perso', '2 serveurs', letter='H', right=dot('ok')), row('Scaleway · pro', '2 serveurs', letter='S', right=dot('ok'))]),
    group('Ajouter', [row('OVH', None, letter='O', href=M('ProviderAjout')), row('DigitalOcean', None, letter='D', href=M('ProviderAjout')),
                      row('Infomaniak', None, letter='I', href=M('ProviderAjout')), row('Voir les 9 autres', None, icon='dots', href=M('ProviderAjout'))]),
    para('Un serveur chez vous ou ailleurs ? Ajoutez-le comme serveur existant.', True),
]), 4, back=('Réglages', M('Reglages')), action=[('plus', 'Connecter', M('ProviderAjout'))])

def keysteps(items):
    out = ''.join(f'<li style="display: flex; gap: 12px; padding: 10px 0;"><span style="flex: none; width: 24px; height: 24px; border-radius: 9999px; background: {STATUS}; font-size: 13px; display: inline-flex; align-items: center; justify-content: center;">{i + 1}</span><span style="font-size: 15px;">{t}</span></li>' for i, t in enumerate(items))
    return f'<ol style="list-style: none; margin: 0 0 24px; padding: 6px 16px; background: #ffffff; border: 1px solid {LINE}; border-radius: 6px;">{out}</ol>'
code = lambda t: f'<code style="font-family: Geist Mono, monospace; font-size: 13px; background: {STATUS}; padding: 1px 4px; border-radius: 4px;">{t}</code>'

page('ProviderAjout', 'Hetzner', ''.join([
    header('Connecter Hetzner', 'Pour louer des serveurs', letter='H'),
    keysteps([f'Ouvrez {code("Security")} puis {code("API tokens")}', f'Créez un jeton {code("Read &amp; Write")}', 'Copiez-le : il ne s’affiche qu’une fois']),
    f'<a href="#" style="display: inline-flex; align-items: center; gap: 6px; margin: -12px 4px 24px; font-size: 15px;">Ouvrir la console Hetzner {ico("open", 16, TONE["link"])}</a>',
    field('Clé', '••••••••••••••••••••', mono=True),
    notice('ok', 'Connexion réussie', '6 lieux proposés', icon='check'),
    field('Nom du compte', 'Hetzner perso'),
    primary('Enregistrer'),
]), 4, back=('Hébergeurs', M('Providers')), big=False)

# ---------- Domaines
page('DNS', 'Domaines', ''.join([
    group('Comptes', [row('Cloudflare', 'Connecté · il y a 2 min', letter='C', right=dot('ok'))]),
    group('Domaines', [row('example.com', '11 adresses · 4 à vérifier', icon='globe', sub_tone='warn', mono=True),
                       row('example.org', 'Aucune adresse', icon='globe', mono=True)]),
    chips(['Toutes 11', 'À vérifier 4', 'MyInfra 8']),
    group('Adresses', [row('api.example.com', 'Billing API · IP à corriger', icon='link', mono=True, sub_tone='warn', right=dot('warn')),
                       row('cloud.example.com', 'Aucune app', icon='link', mono=True, right=dot('idle')),
                       row('www.example.com', 'Site vitrine', icon='link', mono=True, right=dot('ok')),
                       row('status.example.com', 'Uptime Kuma', icon='link', mono=True, right=dot('ok')),
                       row('Voir les 7 autres', None, icon='dots')]),
]), 4, back=('Réglages', M('Reglages')), action=[('plus', 'Connecter', M('DNSAjout'))])

page('DNSAjout', 'Compte DNS', ''.join([
    choice('Qui gère vos domaines ?', [('C', 'Cloudflare', None), ('O', 'OVH', None)]),
    keysteps([f'Ouvrez Cloudflare : le jeton est prérempli', f'Gardez {code("DNS Write")}', 'Copiez-le tout de suite']),
    field('Clé', '••••••••••••••••••••', mono=True),
    notice('ok', '2 domaines trouvés', 'example.com, example.org', icon='check'),
    field('Nom', 'Cloudflare'),
    primary('Enregistrer'),
]), 4, back=('Domaines', M('DNS')))

page('Git', 'Git', ''.join([
    group('Comptes', [row('GitHub · acme', 'Lecture seule · 12 dépôts', letter='G', right=dot('ok'))]),
    group('Utilisés par vos apps', [row('acme/site-vitrine', 'Site vitrine · main', icon='git', mono=True), row('acme/billing-api', 'Billing API · en construction', icon='git', mono=True, sub_tone='warn')]),
    group(None, [row('Créer une app depuis un dépôt', None, icon='plus')]),
]), 4, back=('Réglages', M('Reglages')), action=[('plus', 'Connecter', M('GitAjout'))])

page('GitAjout', 'Compte Git', ''.join([
    choice('Où est votre code ?', [('G', 'GitHub', 'github.com'), ('G', 'GitLab', 'gitlab.com ou le vôtre')]),
    keysteps([f'Créez un jeton {code("fine-grained")}', f'{code("Contents")} en lecture seule', 'Copiez-le tout de suite']),
    field('Clé', '••••••••••••••••••••', mono=True),
    notice('ok', '12 dépôts visibles', 'Expire le 27 janvier 2027', icon='check'),
    field('Nom', 'GitHub acme'),
    primary('Enregistrer'),
]), 4, back=('Git', M('Git')))

page('Images', 'Images', ''.join([
    group('Registres privés', [row('GitHub packages', 'ghcr.io · Billing API', letter='G', right=dot('ok'))], action='Ajouter'),
    group('Importées', [row('site-vitrine:2.3.0', 'Site vitrine · 2 versions', icon='image', mono=True), row('billing-api:1.7.2', 'Inutilisée', icon='image', mono=True)]),
    primary('Importer une image', 'upload'),
    para('Fichier .tar ou .tar.gz de docker save, 4 Go au plus.', True),
]), 4, back=('Réglages', M('Reglages')))

page('APIMCP', 'API et MCP', ''.join([
    group('Adresses', [row('MCP', 'myinfra.example.com/mcp', icon='bot', right=ico('copy', 18), chev=False),
                       row('API', 'myinfra.example.com/api', icon='code', right=ico('copy', 18), chev=False)]),
    group('Jetons · 2', [row('Claude Code', 'MCP · utilisé à 10:42', icon='key'), row('Script de sauvegarde', 'API · lecture seule', icon='key')], action='Nouveau'),
    group('Dernières actions', [row('Billing API relancée', 'Claude Code · 10:42', icon='refresh', chev=False), row('Sauvegardes lues', 'Script · hier', icon='archive', chev=False)]),
    para('Avant d’acheter, supprimer ou toucher au DNS, l’assistant attend votre accord.', True),
]), 4, back=('Réglages', M('Reglages')))

# ---------- Paquets
page('PaquetDebut', 'Nouveau paquet', ''.join([
    para('La recette d’une application : ses services, disques et variables. Rien ne tourne avant l’installation.'),
    f'<div style="background: #ffffff; border: 1px dashed #a8a29e; border-radius: 6px; padding: 16px; margin-bottom: 12px; display: flex; flex-direction: column; align-items: center; gap: 8px; text-align: center;">{ico("file", 28)}<span style="font-size: 15px;">Coller un docker-compose</span><span style="font-size: 13px; color: {MUTED};">MyInfra en fait un paquet</span></div>',
    notice('ok', '4 services, 2 disques', 'turn ouvre 3478 en direct', icon='check'),
    group('Ou partir de', [row('Une app du catalogue', None, icon='book'), row('Un chart Helm', None, icon='layers'),
                           row('Un paquet exporté', None, icon='download'), row('Une page blanche', None, icon='plus', href=M('PaquetPlan'))]),
]), 1, back=('Apps', M('Applications')))

page('PaquetPlan', 'nextcloud', ''.join([
    header('nextcloud', 'Paquet · brouillon', icon='box'),
    group('Entrée', [row('Adresse', 'cloud.example.com', icon='globe', mono=False), row('Port direct', '3478 TCP + UDP · turn', icon='radio', tone='warn')]),
    group('Services · 5', [row('nextcloud', 'Public · :80 · 2 copies', icon='globe', href=M('PaquetElement')), row('turn', '3478 direct', icon='radio'),
                           row('cron', 'Interne', icon='clock'), row('redis', 'Interne · :6379', icon='box'), row('db', 'Base répliquée', icon='db')], action='Ajouter'),
    group('Stockage · 3', [row('fichiers', 'Partagé · 50 Go', icon='disk'), row('db-data', '10 Go × 2', icon='disk'), row('Sauvegardes', 'Chaque nuit · S3', icon='archive')], action='Ajouter'),
    primary('Enregistrer', None, 'Exporter en YAML'),
]), 1, back=('Paquets', M('PaquetDebut')), big=False)

page('PaquetElement', 'nextcloud', ''.join([
    header('nextcloud', 'Service public', icon='globe'),
    segmented(['Général', 'Réseau', 'Disques', 'Variables', 'Santé'], 0, scroll=True),
    field('Image', 'nextcloud:30-apache', mono=True, hint='Lu : port 80, dossier /var/www/html'),
    choice('Accès', [('globe', 'Public', 'Domaine et HTTPS'), ('lock', 'Interne', 'Autres services seulement')]),
    field('Domaine', 'cloud.example.com', mono=True),
    field('Copies', '2, sur 2 serveurs'),
    group(None, [row('Retirer le service', None, icon='trash', tone='bad', danger=True, chev=False)]),
]), 1, back=('Paquet', M('PaquetPlan')), big=False)

print('ok')
