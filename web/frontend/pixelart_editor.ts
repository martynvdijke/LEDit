// pixelart_editor.ts — admin pixel art maker/animator
// No external deps. Mounts on pixelart_editor.html / pixelart_form.html.

interface PixelFrame {
  duration: number;
  pixels: number[];
}
interface PixelDoc {
  palette: string[];
  transparent: number;
  background: string;
  frames: PixelFrame[];
}
interface BandRule {
  max?: number;
  colorIndex: number;
}
interface ColorSlot {
  slot: string;
  path: string;
  bands: BandRule[];
}
interface FrameRule {
  path: string;
  min?: number;
  max?: number;
  frameIndices: number[];
}
interface Overlay {
  path: string;
  x: number;
  y: number;
  color: string;
  fontSize: number;
  format: string;
}
interface Bindings {
  colorSlots: ColorSlot[];
  frameRules: FrameRule[];
  overlays: Overlay[];
}

const MAX_GRID = 128;
const MAX_FRAMES = 64;
const PREVIEW_TARGET_PX = 512;

let gridW = 32;
let gridH = 32;
let doc: PixelDoc = { palette: [], transparent: -1, background: "#000000", frames: [] };
let bindings: Bindings = { colorSlots: [], frameRules: [], overlays: [] };
let activeFrame = 0;
let selectedIdx = -1; // -1 = transparent / eraser
let tool: "pencil" | "eraser" | "fill" | "eyedropper" = "pencil";
let isDragging = false;
let playbackTimer: number | undefined;
let playbackFrame = 0;

function $(id: string): HTMLElement | null { return document.getElementById(id); }
function qs<T extends Element>(sel: string): T | null { return document.querySelector(sel); }

function isSlotColor(c: string): boolean { return c.startsWith("@"); }
function slotName(c: string): string { return c.startsWith("@") ? c.slice(1) : c; }
function isValidHex(s: string): boolean { return /^#[0-9a-fA-F]{6}$/.test(s); }

function clamp(n: number, lo: number, hi: number): number { return Math.max(lo, Math.min(hi, n)); }

function hexToRgba(hex: string): string {
  if (isValidHex(hex)) return hex;
  if (hex.startsWith("@")) {
    const stripped = hex.slice(1);
    if (/^[0-9a-fA-F]{6}$/.test(stripped)) return "#" + stripped;
  }
  return "#000000";
}

function createDefaultDoc(w: number, h: number): PixelDoc {
  return {
    palette: ["#1a1a2e", "#00ff9d", "#ff3366", "#ffcc00", "#00ccff"],
    transparent: -1,
    background: "#000000",
    frames: [{ duration: 500, pixels: new Array(w * h).fill(-1) }],
  };
}

function parseInitial(): void {
  const gwEl = $("grid_width") as HTMLInputElement | null;
  const ghEl = $("grid_height") as HTMLInputElement | null;
  const bgEl = $("background") as HTMLInputElement | null;
  const framesEl = $("frames-input") as HTMLInputElement | null;
  const bindingsEl = $("bindings-input") as HTMLInputElement | null;

  const rawW = gwEl ? parseInt(gwEl.value, 10) : 32;
  const rawH = ghEl ? parseInt(ghEl.value, 10) : 32;
  gridW = Number.isFinite(rawW) ? clamp(rawW, 1, MAX_GRID) : 32;
  gridH = Number.isFinite(rawH) ? clamp(rawH, 1, MAX_GRID) : 32;

  let parsed: PixelDoc | null = null;
  if (framesEl && framesEl.value.trim()) {
    try {
      const j = JSON.parse(framesEl.value) as PixelDoc;
      // validate minimally
      if (Array.isArray(j.palette) && Array.isArray(j.frames) && j.frames.length > 0) {
        parsed = j;
        if (typeof j.transparent !== "number") (parsed as PixelDoc).transparent = -1;
        if (typeof j.background !== "string") parsed.background = "#000000";
      }
    } catch { /* ignore */ }
  }
  if (parsed) {
    doc = parsed;
    // ensure each frame pixel count matches gridW*gridH, pad/truncate if needed
    const area = gridW * gridH;
    for (const f of doc.frames) {
      if (!Array.isArray(f.pixels)) f.pixels = new Array(area).fill(-1);
      if (f.pixels.length !== area) {
        const np = new Array(area).fill(-1);
        const copyLen = Math.min(f.pixels.length, area);
        for (let i = 0; i < copyLen; i++) np[i] = f.pixels[i];
        f.pixels = np;
      }
      if (typeof f.duration !== "number" || f.duration <= 0) f.duration = 500;
    }
    if (doc.frames.length > MAX_FRAMES) doc.frames = doc.frames.slice(0, MAX_FRAMES);
  } else {
    doc = createDefaultDoc(gridW, gridH);
  }
  if (bgEl) bgEl.value = doc.background || "#000000";

  if (bindingsEl && bindingsEl.value.trim()) {
    try {
      const b = JSON.parse(bindingsEl.value) as Bindings;
      bindings = {
        colorSlots: Array.isArray(b.colorSlots) ? b.colorSlots : [],
        frameRules: Array.isArray(b.frameRules) ? b.frameRules : [],
        overlays: Array.isArray(b.overlays) ? b.overlays : [],
      };
    } catch { bindings = { colorSlots: [], frameRules: [], overlays: [] }; }
  }
  // default selected is palette 0 if exists else eraser
  selectedIdx = doc.palette.length > 0 ? 0 : -1;
  activeFrame = 0;
}

function cellSizeForGrid(): number {
  const maxDim = Math.max(gridW, gridH);
  if (maxDim <= 0) return 16;
  const s = Math.floor(PREVIEW_TARGET_PX / maxDim);
  return clamp(s, 2, 32);
}

function drawCell(ctx: CanvasRenderingContext2D, x: number, y: number, cs: number, col: string, isSlot: boolean): void {
  const px = x * cs;
  const py = y * cs;
  if (isSlot) {
    // hatched: diagonal stripes
    ctx.fillStyle = "#2a4a3a";
    ctx.fillRect(px, py, cs, cs);
    ctx.strokeStyle = "#a6ff70";
    ctx.lineWidth = 1;
    ctx.beginPath();
    for (let k = -cs; k < cs * 2; k += 4) {
      ctx.moveTo(px + k, py);
      ctx.lineTo(px + k + cs, py + cs);
    }
    ctx.stroke();
    // small label
    ctx.fillStyle = "rgba(0,0,0,0.65)";
    ctx.fillRect(px, py, cs, Math.min(10, cs));
    ctx.fillStyle = "#a6ff70";
    ctx.font = `${Math.max(7, Math.floor(cs * 0.45))}px monospace`;
    ctx.fillText(slotName(col).slice(0, 4), px + 1, py + Math.min(9, cs - 1));
  } else {
    ctx.fillStyle = col;
    ctx.fillRect(px, py, cs, cs);
  }
  // grid border
  ctx.strokeStyle = "rgba(0,0,0,0.25)";
  ctx.lineWidth = 1;
  ctx.strokeRect(px + 0.5, py + 0.5, cs - 1, cs - 1);
}

function drawEditor(): void {
  const canvas = $("editor-canvas") as HTMLCanvasElement | null;
  if (!canvas) return;
  const cs = cellSizeForGrid();
  canvas.width = gridW * cs;
  canvas.height = gridH * cs;
  canvas.style.width = canvas.width + "px";
  canvas.style.height = canvas.height + "px";
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  // background
  ctx.fillStyle = doc.background || "#000000";
  ctx.fillRect(0, 0, canvas.width, canvas.height);

  const frame = doc.frames[activeFrame];
  if (!frame) return;
  for (let y = 0; y < gridH; y++) {
    for (let x = 0; x < gridW; x++) {
      const idx = frame.pixels[y * gridW + x];
      if (idx === -1 || idx === doc.transparent) {
        // transparent: show checker on top of background? keep background
        // draw subtle checker for transparent
        if ((x + y) % 2 === 0) {
          ctx.fillStyle = "rgba(255,255,255,0.06)";
          ctx.fillRect(x * cs, y * cs, cs, cs);
        }
        ctx.strokeStyle = "rgba(255,255,255,0.08)";
        ctx.strokeRect(x * cs + 0.5, y * cs + 0.5, cs - 1, cs - 1);
        continue;
      }
      if (idx < 0 || idx >= doc.palette.length) continue;
      const pal = doc.palette[idx];
      const slot = isSlotColor(pal);
      const col = slot ? pal : hexToRgba(pal);
      drawCell(ctx, x, y, cs, col, slot);
    }
  }

  // highlight selected frame indicator not needed
}

function drawPlaybackNow(idx: number): void {
  const canvas = $("playback-canvas") as HTMLCanvasElement | null;
  if (!canvas) return;
  const cs = Math.max(1, Math.floor(128 / Math.max(gridW, gridH)));
  canvas.width = gridW * cs;
  canvas.height = gridH * cs;
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  ctx.fillStyle = doc.background || "#000000";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const f = doc.frames[idx];
  if (!f) return;
  for (let y = 0; y < gridH; y++) {
    for (let x = 0; x < gridW; x++) {
      const pi = f.pixels[y * gridW + x];
      if (pi === -1 || pi === doc.transparent) continue;
      if (pi < 0 || pi >= doc.palette.length) continue;
      const pal = doc.palette[pi];
      if (isSlotColor(pal)) {
        ctx.fillStyle = "#a6ff70";
        // slot preview as hatched small
        ctx.fillRect(x * cs, y * cs, cs, cs);
        ctx.fillStyle = "rgba(0,0,0,0.35)";
        ctx.fillRect(x * cs, y * cs, cs, 1);
      } else {
        ctx.fillStyle = hexToRgba(pal);
        ctx.fillRect(x * cs, y * cs, cs, cs);
      }
    }
  }
}

function renderPalette(): void {
  const list = $("palette-list");
  if (!list) return;
  list.innerHTML = "";
  doc.palette.forEach((c, i) => {
    const wrap = document.createElement("div");
    wrap.className = "d-flex align-items-center gap-1";
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "swatch" + (i === selectedIdx ? " active" : "") + (isSlotColor(c) ? " slot" : "") + (c === "transparent" ? " transparent" : "");
    if (!isSlotColor(c)) {
      btn.style.background = hexToRgba(c);
    } else {
      btn.title = "@" + slotName(c) + " (slot)";
    }
    btn.title = c + (i === selectedIdx ? " (selected)" : "");
    btn.addEventListener("click", () => {
      selectedIdx = i;
      renderPalette();
    });
    wrap.appendChild(btn);

    if (isSlotColor(c)) {
      const label = document.createElement("span");
      label.className = "small mono";
      label.textContent = c;
      label.title = "Slot marker — paintable region bound via colorSlots";
      wrap.appendChild(label);
    } else {
      const inp = document.createElement("input");
      inp.type = "color";
      inp.value = isValidHex(c) ? c : "#000000";
      inp.className = "form-control form-control-color palette-hex";
      inp.style.padding = "2px";
      inp.style.width = "36px";
      inp.style.minHeight = "28px";
      inp.addEventListener("input", () => {
        doc.palette[i] = inp.value;
        btn.style.background = inp.value;
        drawEditor();
        renderTimeline();
        if (i === selectedIdx) { /* keep */ }
        renderBindingsColorSlots(); // slots may depend on palette
      });
      wrap.appendChild(inp);
      const hex = document.createElement("input");
      hex.type = "text";
      hex.value = c;
      hex.className = "palette-hex form-control";
      hex.style.width = "78px";
      hex.addEventListener("change", () => {
        let v = hex.value.trim();
        if (!v.startsWith("#")) v = "#" + v;
        if (isValidHex(v)) {
          doc.palette[i] = v;
          inp.value = v;
          btn.style.background = v;
          drawEditor();
          renderTimeline();
        } else {
          hex.value = doc.palette[i];
        }
      });
      wrap.appendChild(hex);
    }

    const del = document.createElement("button");
    del.type = "button";
    del.className = "btn btn-sm btn-ghost";
    del.textContent = "×";
    del.title = "Remove color";
    del.addEventListener("click", () => {
      // remap pixels that used this index -> transparent, and shift indices > i down by 1
      const removed = i;
      doc.palette.splice(removed, 1);
      for (const fr of doc.frames) {
        for (let p = 0; p < fr.pixels.length; p++) {
          const v = fr.pixels[p];
          if (v === removed) fr.pixels[p] = -1;
          else if (v > removed) fr.pixels[p] = v - 1;
        }
      }
      // fix bindings colorIndex references
      for (const cs of bindings.colorSlots) {
        for (const b of cs.bands) {
          if (b.colorIndex === removed) b.colorIndex = 0;
          else if (b.colorIndex > removed) b.colorIndex -= 1;
        }
      }
      if (selectedIdx === removed) selectedIdx = -1;
      else if (selectedIdx > removed) selectedIdx -= 1;
      if (doc.palette.length === 0) selectedIdx = -1;
      renderPalette();
      drawEditor();
      renderTimeline();
      renderBindingsColorSlots();
    });
    wrap.appendChild(del);
    list.appendChild(wrap);
  });

  // eraser swatch
  const eraserWrap = document.createElement("div");
  eraserWrap.className = "d-flex align-items-center gap-1";
  const eBtn = document.createElement("button");
  eBtn.type = "button";
  eBtn.className = "swatch transparent" + (selectedIdx === -1 ? " active" : "");
  eBtn.title = "Transparent / eraser (-1)";
  eBtn.addEventListener("click", () => { selectedIdx = -1; tool = "eraser"; syncToolButtons(); renderPalette(); });
  eraserWrap.appendChild(eBtn);
  const eLabel = document.createElement("span");
  eLabel.className = "small text-muted";
  eLabel.textContent = "eraser";
  eraserWrap.appendChild(eLabel);
  list.appendChild(eraserWrap);
}

function syncToolButtons(): void {
  document.querySelectorAll<HTMLButtonElement>("[data-tool]").forEach(b => {
    b.classList.toggle("active", b.dataset.tool === tool);
  });
}

function getCellFromEvent(e: MouseEvent, canvas: HTMLCanvasElement): { x: number; y: number } | null {
  const rect = canvas.getBoundingClientRect();
  const cs = cellSizeForGrid();
  // canvas style width may be scaled via CSS but we use bounding rect ratio
  const sx = (e.clientX - rect.left) / rect.width * canvas.width;
  const sy = (e.clientY - rect.top) / rect.height * canvas.height;
  const cx = Math.floor(sx / cs);
  const cy = Math.floor(sy / cs);
  if (cx < 0 || cx >= gridW || cy < 0 || cy >= gridH) return null;
  return { x: cx, y: cy };
}

function paintCell(x: number, y: number, colIdx: number): void {
  const fr = doc.frames[activeFrame];
  if (!fr) return;
  fr.pixels[y * gridW + x] = colIdx;
}

function floodFill(sx: number, sy: number, newIdx: number): void {
  const fr = doc.frames[activeFrame];
  if (!fr) return;
  const targetIdx = fr.pixels[sy * gridW + sx];
  if (targetIdx === newIdx) return;
  const q: Array<[number, number]> = [[sx, sy]];
  const visited = new Set<string>();
  while (q.length) {
    const cur = q.shift();
    if (!cur) break;
    const [x, y] = cur;
    if (x < 0 || x >= gridW || y < 0 || y >= gridH) continue;
    const key = x + "," + y;
    if (visited.has(key)) continue;
    visited.add(key);
    const curIdx = fr.pixels[y * gridW + x];
    if (curIdx !== targetIdx) continue;
    fr.pixels[y * gridW + x] = newIdx;
    q.push([x + 1, y], [x - 1, y], [x, y + 1], [x, y - 1]);
  }
}

function eyedropperAt(x: number, y: number): void {
  const fr = doc.frames[activeFrame];
  if (!fr) return;
  const idx = fr.pixels[y * gridW + x];
  if (idx === -1) {
    selectedIdx = -1;
  } else {
    selectedIdx = idx;
  }
  renderPalette();
}

function setupCanvasInteractions(): void {
  const canvas = $("editor-canvas") as HTMLCanvasElement | null;
  if (!canvas) return;
  canvas.addEventListener("contextmenu", e => e.preventDefault());
  const handle = (e: MouseEvent, isMove: boolean): void => {
    const cell = getCellFromEvent(e, canvas);
    if (!cell) return;
    if (tool === "eyedropper") {
      if (!isMove) eyedropperAt(cell.x, cell.y);
      return;
    }
    if (tool === "fill") {
      if (!isMove) {
        floodFill(cell.x, cell.y, selectedIdx);
        drawEditor();
        renderTimeline();
      }
      return;
    }
    // pencil / eraser
    let idx = selectedIdx;
    if (e.button === 2) idx = -1; // right-click eraser
    else if (tool === "eraser") idx = -1;
    paintCell(cell.x, cell.y, idx);
    drawEditor();
    renderTimeline();
  };
  canvas.addEventListener("mousedown", e => {
    if (e.button === 1) e.preventDefault();
    isDragging = true;
    handle(e, false);
  });
  canvas.addEventListener("mousemove", e => {
    if (!isDragging) return;
    handle(e, true);
  });
  const stop = (): void => { isDragging = false; };
  window.addEventListener("mouseup", stop);
  canvas.addEventListener("mouseleave", () => { /* keep dragging if still pressed */ });
}

function thumbForFrame(frameIdx: number, size: number): HTMLCanvasElement {
  const c = document.createElement("canvas");
  const thumbCs = Math.max(1, Math.floor(size / Math.max(gridW, gridH)));
  c.width = gridW * thumbCs;
  c.height = gridH * thumbCs;
  c.className = "timeline-thumb";
  const ctx = c.getContext("2d");
  if (!ctx) return c;
  ctx.fillStyle = doc.background || "#000000";
  ctx.fillRect(0, 0, c.width, c.height);
  const f = doc.frames[frameIdx];
  if (!f) return c;
  for (let y = 0; y < gridH; y++) {
    for (let x = 0; x < gridW; x++) {
      const pi = f.pixels[y * gridW + x];
      if (pi === -1) continue;
      if (pi < 0 || pi >= doc.palette.length) continue;
      const pal = doc.palette[pi];
      if (isSlotColor(pal)) {
        ctx.fillStyle = "#2a4a3a";
        ctx.fillRect(x * thumbCs, y * thumbCs, thumbCs, thumbCs);
        ctx.fillStyle = "#a6ff70";
        ctx.fillRect(x * thumbCs, y * thumbCs, thumbCs, 1);
      } else {
        ctx.fillStyle = hexToRgba(pal);
        ctx.fillRect(x * thumbCs, y * thumbCs, thumbCs, thumbCs);
      }
    }
  }
  return c;
}

function renderTimeline(): void {
  const wrap = $("timeline");
  if (!wrap) return;
  wrap.innerHTML = "";
  doc.frames.forEach((fr, idx) => {
    const item = document.createElement("div");
    item.className = "timeline-item text-center" + (idx === activeFrame ? " active" : "");
    item.style.flex = "0 0 auto";
    const thumb = thumbForFrame(idx, 64);
    thumb.addEventListener("click", () => { activeFrame = idx; drawEditor(); renderTimeline(); updatePlaybackLabel(); });
    if (idx === activeFrame) {
      thumb.style.outline = "2px solid var(--green)";
    }
    item.appendChild(thumb);
    const durWrap = document.createElement("div");
    durWrap.className = "mt-1 d-flex gap-1 align-items-center justify-content-center";
    const dur = document.createElement("input");
    dur.type = "number";
    dur.min = "50";
    dur.max = "10000";
    dur.step = "50";
    dur.value = String(fr.duration);
    dur.className = "form-control";
    dur.style.width = "78px";
    dur.style.minHeight = "28px";
    dur.title = "Duration ms";
    dur.addEventListener("change", () => {
      const v = parseInt(dur.value, 10);
      fr.duration = Number.isFinite(v) && v >= 50 ? v : 500;
      dur.value = String(fr.duration);
    });
    durWrap.appendChild(dur);
    const msLabel = document.createElement("span");
    msLabel.className = "small text-muted";
    msLabel.textContent = "ms";
    durWrap.appendChild(msLabel);
    item.appendChild(durWrap);

    const ctrls = document.createElement("div");
    ctrls.className = "d-flex gap-1 justify-content-center mt-1";
    const leftBtn = document.createElement("button");
    leftBtn.type = "button"; leftBtn.className = "btn btn-sm btn-ghost"; leftBtn.textContent = "◀"; leftBtn.title = "Move left";
    leftBtn.disabled = idx === 0;
    leftBtn.addEventListener("click", () => {
      if (idx === 0) return;
      const tmp = doc.frames[idx - 1];
      doc.frames[idx - 1] = doc.frames[idx];
      doc.frames[idx] = tmp;
      if (activeFrame === idx) activeFrame = idx - 1;
      else if (activeFrame === idx - 1) activeFrame = idx;
      renderTimeline(); drawEditor();
    });
    const rightBtn = document.createElement("button");
    rightBtn.type = "button"; rightBtn.className = "btn btn-sm btn-ghost"; rightBtn.textContent = "▶"; rightBtn.title = "Move right";
    rightBtn.disabled = idx === doc.frames.length - 1;
    rightBtn.addEventListener("click", () => {
      if (idx >= doc.frames.length - 1) return;
      const tmp = doc.frames[idx + 1];
      doc.frames[idx + 1] = doc.frames[idx];
      doc.frames[idx] = tmp;
      if (activeFrame === idx) activeFrame = idx + 1;
      else if (activeFrame === idx + 1) activeFrame = idx;
      renderTimeline(); drawEditor();
    });
    const delBtn = document.createElement("button");
    delBtn.type = "button"; delBtn.className = "btn btn-sm btn-ghost"; delBtn.textContent = "✕"; delBtn.title = "Delete frame";
    delBtn.disabled = doc.frames.length <= 1;
    delBtn.addEventListener("click", () => {
      if (doc.frames.length <= 1) return;
      doc.frames.splice(idx, 1);
      if (activeFrame >= doc.frames.length) activeFrame = doc.frames.length - 1;
      renderTimeline(); drawEditor();
    });
    ctrls.append(leftBtn, rightBtn, delBtn);
    item.appendChild(ctrls);

    const idxLabel = document.createElement("div");
    idxLabel.className = "small text-muted";
    idxLabel.textContent = "#" + (idx + 1);
    item.appendChild(idxLabel);

    wrap.appendChild(item);
  });
}

function updatePlaybackLabel(): void {
  const el = $("playback-label");
  if (!el) return;
  el.textContent = `frame ${activeFrame + 1}/${doc.frames.length} · ${doc.frames[activeFrame]?.duration ?? 0}ms`;
}

// ---------- Playback ----------
function startPlayback(): void {
  stopPlayback();
  playbackFrame = activeFrame;
  const loop = (): void => {
    drawPlaybackNow(playbackFrame);
    const dur = doc.frames[playbackFrame]?.duration ?? 500;
    playbackTimer = window.setTimeout(() => {
      playbackFrame = (playbackFrame + 1) % doc.frames.length;
      loop();
    }, dur);
  };
  loop();
}
function stopPlayback(): void {
  if (playbackTimer !== undefined) window.clearTimeout(playbackTimer);
  playbackTimer = undefined;
}
function setupPlayback(): void {
  const btn = $("playback-toggle") as HTMLButtonElement | null;
  if (!btn) return;
  btn.addEventListener("click", () => {
    if (playbackTimer !== undefined) {
      stopPlayback();
      btn.textContent = "Play";
    } else {
      btn.textContent = "Stop";
      startPlayback();
    }
  });
  drawPlaybackNow(activeFrame);
}

// ---------- Bindings UI ----------
function slotOptions(): string[] {
  return doc.palette.filter(isSlotColor).map(slotName);
}
function renderBindingsColorSlots(): void {
  const list = $("colorslots-list");
  if (!list) return;
  list.innerHTML = "";
  bindings.colorSlots.forEach((cs, idx) => {
    const row = document.createElement("div");
    row.className = "card mb-2";
    row.style.padding = "8px";
    const top = document.createElement("div");
    top.className = "d-flex gap-2 align-items-center mb-2";
    const slotSel = document.createElement("select");
    slotSel.className = "form-select form-select-sm";
    slotSel.style.maxWidth = "140px";
    const opts = slotOptions();
    if (opts.length === 0) {
      const o = document.createElement("option");
      o.value = ""; o.textContent = "(no @slots in palette)";
      slotSel.appendChild(o);
    } else {
      opts.forEach(s => {
        const o = document.createElement("option");
        o.value = s; o.textContent = "@" + s;
        if (s === cs.slot) o.selected = true;
        slotSel.appendChild(o);
      });
      // if current slot not in palette, add it
      if (cs.slot && !opts.includes(cs.slot)) {
        const o = document.createElement("option");
        o.value = cs.slot; o.textContent = "@" + cs.slot + " (missing)";
        o.selected = true;
        slotSel.appendChild(o);
      }
    }
    slotSel.addEventListener("change", () => { cs.slot = slotSel.value; });
    top.appendChild(slotSel);

    const path = document.createElement("input");
    path.type = "text"; path.className = "form-control form-control-sm"; path.placeholder = "dot path e.g. main.temp";
    path.value = cs.path;
    path.addEventListener("input", () => { cs.path = path.value; });
    top.appendChild(path);

    const del = document.createElement("button");
    del.type = "button"; del.className = "btn btn-sm btn-ghost"; del.textContent = "✕";
    del.addEventListener("click", () => { bindings.colorSlots.splice(idx, 1); renderBindingsColorSlots(); });
    top.appendChild(del);
    row.appendChild(top);

    const bandsWrap = document.createElement("div");
    cs.bands.forEach((b, bi) => {
      const br = document.createElement("div");
      br.className = "band-row";
      const maxInp = document.createElement("input");
      maxInp.type = "number"; maxInp.step = "any"; maxInp.className = "form-control form-control-sm"; maxInp.placeholder = "max (empty=catch-all)";
      if (b.max !== undefined) maxInp.value = String(b.max);
      maxInp.addEventListener("input", () => {
        const v = maxInp.value.trim();
        if (v === "") b.max = undefined;
        else { const n = Number(v); if (Number.isFinite(n)) b.max = n; }
      });
      const ciInp = document.createElement("input");
      ciInp.type = "number"; ciInp.min = "0"; ciInp.max = String(Math.max(0, doc.palette.length - 1)); ciInp.className = "form-control form-control-sm"; ciInp.placeholder = "colorIndex";
      ciInp.value = String(b.colorIndex);
      ciInp.addEventListener("input", () => {
        const n = parseInt(ciInp.value, 10);
        if (Number.isFinite(n)) b.colorIndex = clamp(n, 0, Math.max(0, doc.palette.length - 1));
      });
      const bDel = document.createElement("button");
      bDel.type = "button"; bDel.className = "btn btn-sm btn-ghost"; bDel.textContent = "×";
      bDel.addEventListener("click", () => { cs.bands.splice(bi, 1); renderBindingsColorSlots(); });
      br.append(maxInp, ciInp, bDel);
      bandsWrap.appendChild(br);
    });
    const addBand = document.createElement("button");
    addBand.type = "button"; addBand.className = "btn btn-sm btn-secondary mt-1";
    addBand.textContent = "Add band";
    addBand.addEventListener("click", () => { cs.bands.push({ colorIndex: 0 }); renderBindingsColorSlots(); });
    bandsWrap.appendChild(addBand);
    row.appendChild(bandsWrap);
    list.appendChild(row);
  });
}

function renderBindingsFrameRules(): void {
  const list = $("framerules-list");
  if (!list) return;
  list.innerHTML = "";
  bindings.frameRules.forEach((r, idx) => {
    const row = document.createElement("div");
    row.className = "card mb-2";
    row.style.padding = "8px";
    const top = document.createElement("div");
    top.className = "d-flex gap-2 align-items-center flex-wrap mb-2";
    const path = document.createElement("input");
    path.type = "text"; path.className = "form-control form-control-sm"; path.placeholder = "path";
    path.value = r.path; path.style.flex = "1 1 120px";
    path.addEventListener("input", () => { r.path = path.value; });
    const minInp = document.createElement("input");
    minInp.type = "number"; minInp.step = "any"; minInp.className = "form-control form-control-sm"; minInp.placeholder = "min";
    minInp.style.width = "90px";
    if (r.min !== undefined) minInp.value = String(r.min);
    minInp.addEventListener("input", () => {
      const v = minInp.value.trim();
      r.min = v === "" ? undefined : Number(v);
    });
    const maxInp = document.createElement("input");
    maxInp.type = "number"; maxInp.step = "any"; maxInp.className = "form-control form-control-sm"; maxInp.placeholder = "max";
    maxInp.style.width = "90px";
    if (r.max !== undefined) maxInp.value = String(r.max);
    maxInp.addEventListener("input", () => {
      const v = maxInp.value.trim();
      r.max = v === "" ? undefined : Number(v);
    });
    const fiInp = document.createElement("input");
    fiInp.type = "text"; fiInp.className = "form-control form-control-sm"; fiInp.placeholder = "frameIndices e.g. 1,2";
    fiInp.value = r.frameIndices.join(",");
    fiInp.addEventListener("input", () => {
      const parts = fiInp.value.split(",").map(s => parseInt(s.trim(), 10)).filter(n => Number.isFinite(n));
      r.frameIndices = parts;
    });
    const del = document.createElement("button");
    del.type = "button"; del.className = "btn btn-sm btn-ghost"; del.textContent = "✕";
    del.addEventListener("click", () => { bindings.frameRules.splice(idx, 1); renderBindingsFrameRules(); });
    top.append(path, minInp, maxInp, fiInp, del);
    row.appendChild(top);
    list.appendChild(row);
  });
}

function renderBindingsOverlays(): void {
  const list = $("overlays-list");
  if (!list) return;
  list.innerHTML = "";
  bindings.overlays.forEach((ov, idx) => {
    const row = document.createElement("div");
    row.className = "card mb-2";
    row.style.padding = "8px";
    const top = document.createElement("div");
    top.className = "d-flex gap-2 flex-wrap mb-2";
    const path = document.createElement("input");
    path.type = "text"; path.className = "form-control form-control-sm"; path.placeholder = "path";
    path.value = ov.path; path.addEventListener("input", () => { ov.path = path.value; });
    const xInp = document.createElement("input");
    xInp.type = "number"; xInp.className = "form-control form-control-sm"; xInp.placeholder = "x"; xInp.style.width = "70px"; xInp.value = String(ov.x);
    xInp.addEventListener("input", () => { ov.x = parseInt(xInp.value, 10) || 0; });
    const yInp = document.createElement("input");
    yInp.type = "number"; yInp.className = "form-control form-control-sm"; yInp.placeholder = "y"; yInp.style.width = "70px"; yInp.value = String(ov.y);
    yInp.addEventListener("input", () => { ov.y = parseInt(yInp.value, 10) || 0; });
    const colInp = document.createElement("input");
    colInp.type = "color"; colInp.className = "form-control form-control-color"; colInp.style.width = "44px"; colInp.value = isValidHex(ov.color) ? ov.color : "#ffffff";
    colInp.addEventListener("input", () => { ov.color = colInp.value; });
    top.append(path, xInp, yInp, colInp);
    row.appendChild(top);
    const bottom = document.createElement("div");
    bottom.className = "d-flex gap-2";
    const fsInp = document.createElement("input");
    fsInp.type = "number"; fsInp.className = "form-control form-control-sm"; fsInp.placeholder = "fontSize"; fsInp.style.width = "90px"; fsInp.value = String(ov.fontSize || 12);
    fsInp.addEventListener("input", () => { ov.fontSize = Number(fsInp.value) || 12; });
    const fmtInp = document.createElement("input");
    fmtInp.type = "text"; fmtInp.className = "form-control form-control-sm"; fmtInp.placeholder = 'format e.g. %.1f°C';
    fmtInp.value = ov.format; fmtInp.addEventListener("input", () => { ov.format = fmtInp.value; });
    const del = document.createElement("button");
    del.type = "button"; del.className = "btn btn-sm btn-ghost"; del.textContent = "✕";
    del.addEventListener("click", () => { bindings.overlays.splice(idx, 1); renderBindingsOverlays(); });
    bottom.append(fsInp, fmtInp, del);
    row.appendChild(bottom);
    list.appendChild(row);
  });
}

function renderAllBindings(): void {
  renderBindingsColorSlots();
  renderBindingsFrameRules();
  renderBindingsOverlays();
}

// ---------- Grid resize ----------
function resizeGrid(newW: number, newH: number): void {
  if (newW === gridW && newH === gridH) return;
  if (newW < 1 || newW > MAX_GRID || newH < 1 || newH > MAX_GRID) return;
  // preserve pixels where possible
  const oldW = gridW, oldH = gridH;
  for (const fr of doc.frames) {
    const oldPixels = fr.pixels;
    const np = new Array(newW * newH).fill(-1);
    const copyW = Math.min(oldW, newW);
    const copyH = Math.min(oldH, newH);
    for (let y = 0; y < copyH; y++) {
      for (let x = 0; x < copyW; x++) {
        np[y * newW + x] = oldPixels[y * oldW + x];
      }
    }
    fr.pixels = np;
  }
  gridW = newW; gridH = newH;
  drawEditor();
  renderTimeline();
  drawPlaybackNow(activeFrame);
}

function setupGridInputs(): void {
  const gw = $("grid_width") as HTMLInputElement | null;
  const gh = $("grid_height") as HTMLInputElement | null;
  if (!gw || !gh) return;
  const onChange = (): void => {
    const nw = parseInt(gw.value, 10);
    const nh = parseInt(gh.value, 10);
    if (!Number.isFinite(nw) || !Number.isFinite(nh)) return;
    if (nw === gridW && nh === gridH) return;
    // if shrinking, confirm if there's painted pixels that will be lost (simple check)
    const oldArea = gridW * gridH;
    const newArea = nw * nh;
    if (newArea < oldArea) {
      // check if any non-transparent pixels outside new bounds
      let hasLoss = false;
      for (const fr of doc.frames) {
        for (let y = 0; y < gridH; y++) {
          for (let x = 0; x < gridW; x++) {
            if (x >= nw || y >= nh) {
              if (fr.pixels[y * gridW + x] !== -1) { hasLoss = true; break; }
            }
          }
          if (hasLoss) break;
        }
        if (hasLoss) break;
      }
      if (hasLoss) {
        if (!window.confirm("Resizing will crop painted pixels outside the new bounds. Continue?")) {
          gw.value = String(gridW);
          gh.value = String(gridH);
          return;
        }
      }
    }
    resizeGrid(nw, nh);
  };
  gw.addEventListener("change", onChange);
  gh.addEventListener("change", onChange);
}

// ---------- Test render ----------
async function doTestRender(): Promise<void> {
  const wSel = $("preview-w") as HTMLSelectElement | null;
  const hSel = $("preview-h") as HTMLSelectElement | null;
  const errEl = $("test-error");
  const img = $("test-preview-img") as HTMLImageElement | null;
  const w = wSel ? parseInt(wSel.value, 10) : 64;
  const h = hSel ? parseInt(hSel.value, 10) : 64;
  const apiUrl = ($("api_url") as HTMLInputElement | null)?.value ?? "";
  const apiToken = ($("api_token") as HTMLInputElement | null)?.value ?? "";
  const form = new FormData();
  form.set("width", String(w));
  form.set("height", String(h));
  form.set("grid_width", String(gridW));
  form.set("grid_height", String(gridH));
  form.set("frames", JSON.stringify(doc));
  form.set("bindings", JSON.stringify(bindings));
  form.set("api_url", apiUrl);
  form.set("api_token", apiToken);
  if (errEl) errEl.textContent = "Rendering…";
  try {
    const res = await fetch("/admin/pixelarts/preview", { method: "POST", body: form });
    if (!res.ok) {
      const txt = await res.text();
      if (errEl) errEl.textContent = txt || `Error ${res.status}`;
      return;
    }
    const blob = await res.blob();
    if (img) {
      const url = URL.createObjectURL(blob);
      img.src = url;
    }
    if (errEl) errEl.textContent = "";
  } catch (e) {
    if (errEl) errEl.textContent = String(e);
  }
}

// ---------- Import/Export ----------
function doExport(): void {
  const payload = {
    grid_width: gridW,
    grid_height: gridH,
    frames: doc,
    bindings: bindings,
    api_url: ($("api_url") as HTMLInputElement | null)?.value ?? "",
    api_token: ($("api_token") as HTMLInputElement | null)?.value ?? "",
  };
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  const nameInput = $("name") as HTMLInputElement | null;
  const base = nameInput?.value.trim() ? nameInput.value.trim().replace(/\s+/g, "_") : "pixelart";
  a.download = base + ".json";
  a.click();
}
function doImport(file: File): void {
  const reader = new FileReader();
  reader.onload = () => {
    try {
      const raw = String(reader.result ?? "");
      const j = JSON.parse(raw) as { grid_width?: number; grid_height?: number; frames?: unknown; bindings?: unknown; api_url?: string; api_token?: string };
      // accept either wrapped {frames:{palette,...}} or direct doc
      let newDoc: PixelDoc | null = null;
      let newBindings: Bindings | null = null;
      let newW = gridW, newH = gridH;
      if (j.grid_width && j.grid_height) { newW = clamp(j.grid_width, 1, MAX_GRID); newH = clamp(j.grid_height, 1, MAX_GRID); }
      // frames may be in j.frames as doc, or j itself is doc
      const candidateFrames = (j.frames as PixelDoc) ?? (j as unknown as PixelDoc);
      if (candidateFrames && Array.isArray((candidateFrames as PixelDoc).palette) && Array.isArray((candidateFrames as PixelDoc).frames)) {
        newDoc = candidateFrames as PixelDoc;
        // also accept if j.frames is doc and j.bindings present
        if (j.bindings && typeof j.bindings === "object") newBindings = j.bindings as Bindings;
      } else if (j.bindings && Array.isArray((j.bindings as unknown as { palette: unknown }).palette)) {
        // maybe file is just doc alone
        newDoc = j as unknown as PixelDoc;
      }
      if (!newDoc) throw new Error("Invalid file: missing palette/frames");
      // validate area matches grid
      const area = newW * newH;
      for (const f of newDoc.frames) {
        if (!Array.isArray(f.pixels) || f.pixels.length !== area) {
          throw new Error(`Frame pixels length ${f.pixels.length} != grid ${newW}x${newH}=${area}`);
        }
      }
      if (newDoc.frames.length > MAX_FRAMES) throw new Error(`Too many frames max ${MAX_FRAMES}`);
      // apply
      doc = { palette: newDoc.palette, transparent: typeof newDoc.transparent === "number" ? newDoc.transparent : -1, background: newDoc.background || "#000000", frames: newDoc.frames };
      if (newBindings) {
        bindings = {
          colorSlots: Array.isArray(newBindings.colorSlots) ? newBindings.colorSlots : [],
          frameRules: Array.isArray(newBindings.frameRules) ? newBindings.frameRules : [],
          overlays: Array.isArray(newBindings.overlays) ? newBindings.overlays : [],
        };
      } else if ((j as unknown as Bindings).colorSlots) {
        // bindings directly in root
        const b = j as unknown as Bindings;
        bindings = { colorSlots: b.colorSlots ?? [], frameRules: b.frameRules ?? [], overlays: b.overlays ?? [] };
      }
      gridW = newW; gridH = newH;
      const gw = $("grid_width") as HTMLInputElement | null;
      const gh = $("grid_height") as HTMLInputElement | null;
      if (gw) gw.value = String(gridW);
      if (gh) gh.value = String(gridH);
      const bg = $("background") as HTMLInputElement | null;
      if (bg) bg.value = doc.background || "#000000";
      if (typeof j.api_url === "string") {
        const au = $("api_url") as HTMLInputElement | null;
        if (au) au.value = j.api_url;
      }
      if (typeof j.api_token === "string") {
        const at = $("api_token") as HTMLInputElement | null;
        if (at) at.value = j.api_token;
      }
      activeFrame = 0;
      selectedIdx = doc.palette.length > 0 ? 0 : -1;
      renderPalette();
      renderTimeline();
      drawEditor();
      drawPlaybackNow(activeFrame);
      renderAllBindings();
      updatePlaybackLabel();
    } catch (e) {
      window.alert("Import failed: " + String(e));
    }
  };
  reader.readAsText(file);
}

// ---------- Form serialize ----------
function setupFormSubmit(): void {
  const form = $("pixelart-form") as HTMLFormElement | null;
  if (!form) return;
  form.addEventListener("submit", () => {
    const framesEl = $("frames-input") as HTMLInputElement | null;
    const bindingsEl = $("bindings-input") as HTMLInputElement | null;
    const bgEl = $("background") as HTMLInputElement | null;
    if (bgEl) doc.background = bgEl.value || "#000000";
    if (framesEl) framesEl.value = JSON.stringify(doc);
    if (bindingsEl) bindingsEl.value = JSON.stringify(bindings);
    // let form submit normally
  });
}

function init(): void {
  parseInitial();
  drawEditor();
  renderPalette();
  renderTimeline();
  renderAllBindings();
  updatePlaybackLabel();
  drawPlaybackNow(activeFrame);
  setupCanvasInteractions();
  setupPlayback();
  setupGridInputs();
  setupFormSubmit();

  // tools
  document.querySelectorAll<HTMLButtonElement>("[data-tool]").forEach(b => {
    b.addEventListener("click", () => {
      tool = (b.dataset.tool as typeof tool) ?? "pencil";
      syncToolButtons();
    });
  });
  syncToolButtons();

  // palette add
  const addColorBtn = $("add-color");
  addColorBtn?.addEventListener("click", () => {
    const inp = $("new-color") as HTMLInputElement | null;
    const col = inp?.value ?? "#ff3366";
    if (!isValidHex(col)) return;
    doc.palette.push(col);
    selectedIdx = doc.palette.length - 1;
    renderPalette();
    renderBindingsColorSlots();
  });
  const addSlotBtn = $("add-slot");
  addSlotBtn?.addEventListener("click", () => {
    const name = window.prompt("Slot name (e.g. gauge):");
    if (!name) return;
    const clean = name.trim().replace(/[^a-zA-Z0-9_]/g, "_").replace(/^_+/, "");
    if (!clean) return;
    const marker = "@" + clean;
    if (doc.palette.includes(marker)) { window.alert("Slot already exists"); return; }
    doc.palette.push(marker);
    selectedIdx = doc.palette.length - 1;
    renderPalette();
    renderBindingsColorSlots();
  });

  const addFrameBtn = $("add-frame");
  addFrameBtn?.addEventListener("click", () => {
    if (doc.frames.length >= MAX_FRAMES) { window.alert(`Max ${MAX_FRAMES} frames`); return; }
    const blank = new Array(gridW * gridH).fill(-1);
    doc.frames.splice(activeFrame + 1, 0, { duration: 500, pixels: blank });
    activeFrame += 1;
    drawEditor(); renderTimeline(); updatePlaybackLabel();
  });
  const dupBtn = $("duplicate-frame");
  dupBtn?.addEventListener("click", () => {
    if (doc.frames.length >= MAX_FRAMES) { window.alert(`Max ${MAX_FRAMES} frames`); return; }
    const src = doc.frames[activeFrame];
    if (!src) return;
    const copy: PixelFrame = { duration: src.duration, pixels: [...src.pixels] };
    doc.frames.splice(activeFrame + 1, 0, copy);
    activeFrame += 1;
    drawEditor(); renderTimeline(); updatePlaybackLabel();
  });

  const bgEl = $("background") as HTMLInputElement | null;
  bgEl?.addEventListener("input", () => { doc.background = bgEl.value; drawEditor(); drawPlaybackNow(playbackFrame); renderTimeline(); });

  // bindings add buttons
  $("add-colorslot")?.addEventListener("click", () => {
    const slot = slotOptions()[0] ?? "gauge";
    bindings.colorSlots.push({ slot, path: "main.temp", bands: [{ max: 0, colorIndex: 0 }, { colorIndex: 1 }] });
    renderBindingsColorSlots();
  });
  $("add-framerule")?.addEventListener("click", () => {
    bindings.frameRules.push({ path: "main.temp", min: 20, frameIndices: [1] });
    renderBindingsFrameRules();
  });
  $("add-overlay")?.addEventListener("click", () => {
    bindings.overlays.push({ path: "main.temp", x: 2, y: 50, color: "#ffffff", fontSize: 12, format: "%.1f°C" });
    renderBindingsOverlays();
  });

  $("test-render")?.addEventListener("click", () => { void doTestRender(); });
  $("export-btn")?.addEventListener("click", doExport);
  const importFile = $("import-file") as HTMLInputElement | null;
  importFile?.addEventListener("change", () => {
    const f = importFile.files?.[0];
    if (f) doImport(f);
    importFile.value = "";
  });

  // background sync on load
  if (bgEl) doc.background = bgEl.value || doc.background;
}

if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
else init();
