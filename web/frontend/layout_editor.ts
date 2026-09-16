// layout_editor.ts — visual layout editor (DOM + Pointer Events, no deps)
type Theme = { accent?: string; text?: string; background?: string; font_size?: number };
type Region = {
  id: string;
  x: number; y: number; w: number; h: number;
  source_type?: string; source_id?: number;
  theme?: Theme;
  inset?: number; border?: boolean;
  row?: number; col?: number; row_span?: number; col_span?: number;
};
type Device = { id: number; name: string; width: number; height: number; refresh: number; panel_cols: number; panel_gap: number };
type SnapShot = { rows: number; cols: number; gap: number; regions: Region[] };

declare global {
  interface Window {
    LAYOUT_BINDING_OPTS?: Record<string, { id: number; label: string }[]>;
    LAYOUT_DEVICES?: Device[];
    LAYOUT_MIN_REGION?: number;
    __layoutDebug?: { convert(deltaPx: number, scale: number): number; getRegions(): Region[]; setScale(s: number): void };
  }
}

const form = document.querySelector<HTMLFormElement>("[data-layout-editor]");
if (form) {
  const surface = document.querySelector<HTMLElement>("[data-canvas-surface]")!;
  const canvasWrap = document.querySelector<HTMLElement>("[data-canvas]")!;
  const regionListEl = document.querySelector<HTMLElement>("[data-region-list]")!;
  const inspectorEl = document.querySelector<HTMLElement>("[data-inspector]")!;
  const snapInput = document.querySelector<HTMLInputElement>("[data-snap]")!;
  const deviceSelect = document.querySelector<HTMLSelectElement>("[data-device-select]")!;
  const previewImg = document.querySelector<HTMLImageElement>("[data-live-preview-img]")!;
  const validationWarning = document.querySelector<HTMLElement>("[data-validation-warning]")!;
  const previewSizeEl = document.querySelector<HTMLElement>("[data-preview-size]")!;
  const regionCountEl = document.querySelector<HTMLElement>("[data-region-count]")!;

  const nameInput = form.querySelector<HTMLInputElement>('[name="name"]')!;
  const modeInput = form.querySelector<HTMLInputElement>('[name="mode"]')!;
  const rowsInput = form.querySelector<HTMLInputElement>('[name="rows"]')!;
  const colsInput = form.querySelector<HTMLInputElement>('[name="cols"]')!;
  const gapInput = form.querySelector<HTMLInputElement>('[name="gap"]')!;
  const paddingInput = form.querySelector<HTMLInputElement>('[name="padding"]')!;
  const backgroundInput = form.querySelector<HTMLInputElement>('[name="background"]')!;
  const enabledInput = form.querySelector<HTMLInputElement>('[name="enabled"]')!;
  const ttlInput = form.querySelector<HTMLInputElement>('[name="ttl_seconds"]')!;
  const regionsInput = form.querySelector<HTMLInputElement>('[name="regions"]')!;
  const canvasWInput = form.querySelector<HTMLInputElement>('[name="canvas_w"]')!;
  const canvasHInput = form.querySelector<HTMLInputElement>('[name="canvas_h"]')!;

  // visible mirrors
  const nameVis = document.querySelector<HTMLInputElement>('[data-field="name-visible"]');
  const rowsVis = document.querySelector<HTMLInputElement>('[data-field="rows-visible"]');
  const colsVis = document.querySelector<HTMLInputElement>('[data-field="cols-visible"]');
  const gapVis = document.querySelector<HTMLInputElement>('[data-field="gap-visible"]');
  const bgVis = document.querySelector<HTMLInputElement>('[data-field="background-visible"]');
  const enabledVis = document.querySelector<HTMLInputElement>('[data-field="enabled-visible"]');

  const bindingOpts: Record<string, { id: number; label: string }[]> = (window as unknown as { LAYOUT_BINDING_OPTS?: Record<string, { id: number; label: string }[]> }).LAYOUT_BINDING_OPTS ?? (window as unknown as { BINDING_OPTS?: Record<string, { id: number; label: string }[]> }).BINDING_OPTS as unknown as Record<string, { id: number; label: string }[]> ?? {};
  const devices: Device[] = (window as unknown as { LAYOUT_DEVICES?: Device[] }).LAYOUT_DEVICES ?? [];
  const minRegion: number = (window as unknown as { LAYOUT_MIN_REGION?: number }).LAYOUT_MIN_REGION ?? 2;

  let canvasW = Math.max(1, parseInt(canvasWInput.value || "64", 10) || 64);
  let canvasH = Math.max(1, parseInt(canvasHInput.value || "64", 10) || 64);
  let rows = Math.max(1, parseInt(rowsInput.value || "1", 10) || 1);
  let cols = Math.max(1, parseInt(colsInput.value || "1", 10) || 1);
  let gap = Math.max(0, parseInt(gapInput.value || "0", 10) || 0);

  let regions: Region[] = [];
  try { const p = JSON.parse(regionsInput.value || "[]"); if (Array.isArray(p)) regions = p; } catch { regions = []; }
  // normalize: ensure ids and numbers
  let idCounter = 1;
  const ensureIds = () => {
    const seen = new Set<string>();
    for (const r of regions) {
      if (!r.id || seen.has(r.id)) r.id = `r${idCounter++}`;
      else {
        const n = parseInt(r.id.replace(/\D/g, ""), 10);
        if (!isNaN(n) && n >= idCounter) idCounter = n + 1;
      }
      seen.add(r.id);
      r.x = Number(r.x) || 0; r.y = Number(r.y) || 0; r.w = Number(r.w) || minRegion; r.h = Number(r.h) || minRegion;
      if (r.w < minRegion) r.w = minRegion;
      if (r.h < minRegion) r.h = minRegion;
    }
    // if any region lacks id after loop, already fixed
    if (regions.some(r => !r.id)) {
      regions.forEach(r => { if (!r.id) r.id = `r${idCounter++}`; });
    }
  };
  ensureIds();

  let selectedId: string | null = regions[0]?.id ?? null;
  let snap = gap > 0 ? gap : 1;
  if (snapInput) { snapInput.value = String(snap); snapInput.addEventListener("input", () => { snap = Math.max(1, parseInt(snapInput.value, 10) || 1); }); }

  // device selector
  let activeDeviceId = 0;
  const defaultW = canvasW, defaultH = canvasH;
  const refreshDeviceSelect = () => {
    if (!deviceSelect) return;
    deviceSelect.innerHTML = "";
    const custom = document.createElement("option");
    custom.value = "0";
    custom.textContent = `Custom / default (${defaultW}×${defaultH})`;
    deviceSelect.appendChild(custom);
    for (const d of devices) {
      const o = document.createElement("option");
      o.value = String(d.id);
      o.textContent = `${d.name} (${d.width}×${d.height}${d.panel_cols > 1 ? ` ${d.panel_cols} panels` : ""})`;
      deviceSelect.appendChild(o);
    }
    // default from window
    const defId = (window as unknown as { LAYOUT_DEFAULT_DEVICE?: number }).LAYOUT_DEFAULT_DEVICE ?? 0;
    if (defId) activeDeviceId = defId;
    deviceSelect.value = String(activeDeviceId);
  };
  refreshDeviceSelect();
  deviceSelect?.addEventListener("change", () => {
    activeDeviceId = parseInt(deviceSelect.value, 10) || 0;
    renderBezels();
    updatePreviewSizeLabel();
    schedulePreview();
  });

  const getDevice = (): Device | undefined => devices.find(d => d.id === activeDeviceId);
  const previewDims = (): { w: number; h: number } => {
    const d = getDevice();
    if (!d) return { w: canvasW, h: canvasH };
    // logical width includes bezel gaps
    const lw = d.panel_cols > 1 && d.panel_gap > 0 ? d.width + (d.panel_cols - 1) * d.panel_gap : d.width;
    return { w: lw, h: d.height };
  };

  // scale handling
  let debugScale: number | null = null;
  const convert = (deltaPx: number, scale: number): number => deltaPx / (scale || 1);
  const currentScale = (): number => {
    if (debugScale !== null) return debugScale;
    if (!surface) return 1;
    const rect = surface.getBoundingClientRect();
    if (canvasW === 0) return 1;
    // if CSS scaling via transform, rect width != canvasW ; otherwise 1
    const s = rect.width / canvasW;
    return s > 0.05 && s < 10 ? s : 1;
  };

  // undo/redo
  const MAX_STACK = 50;
  let stack: SnapShot[] = [];
  let cursor = -1;
  const cloneRegions = (): Region[] => JSON.parse(JSON.stringify(regions));
  const pushSnapshot = () => {
    const snapShot: SnapShot = { rows, cols, gap, regions: cloneRegions() };
    // truncate tail
    if (cursor < stack.length - 1) stack = stack.slice(0, cursor + 1);
    stack.push(snapShot);
    if (stack.length > MAX_STACK) stack.shift();
    else cursor++;
    // when shifted, cursor stays at end
    if (stack.length > MAX_STACK) cursor = stack.length - 1;
    else if (cursor >= stack.length) cursor = stack.length - 1;
    syncUndoRedoButtons();
  };
  const applySnapshot = (s: SnapShot) => {
    rows = s.rows; cols = s.cols; gap = s.gap;
    regions = JSON.parse(JSON.stringify(s.regions));
    ensureIds();
    // sync inputs
    rowsInput.value = String(rows); colsInput.value = String(cols); gapInput.value = String(gap);
    if (rowsVis) rowsVis.value = String(rows);
    if (colsVis) colsVis.value = String(cols);
    if (gapVis) gapVis.value = String(gap);
    if (selectedId && !regions.find(r => r.id === selectedId)) selectedId = regions[0]?.id ?? null;
    syncAll();
  };
  const undo = () => { if (cursor > 0) { cursor--; applySnapshot(stack[cursor]); } };
  const redo = () => { if (cursor < stack.length - 1) { cursor++; applySnapshot(stack[cursor]); } };
  const syncUndoRedoButtons = () => {
    const u = document.querySelector<HTMLButtonElement>("[data-undo]");
    const r = document.querySelector<HTMLButtonElement>("[data-redo]");
    if (u) u.disabled = cursor <= 0;
    if (r) r.disabled = cursor >= stack.length - 1;
  };

  // init stack with initial state
  pushSnapshot();

  // helpers
  const clamp = (n: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, n));
  const snapVal = (v: number, step: number) => Math.round(v / step) * step;

  const sourceLabelFor = (r: Region): string => {
    if (!r.source_type) return "unbound";
    const key = `${r.source_type}:${r.source_id}`;
    // search opts
    for (const t of Object.keys(bindingOpts)) {
      for (const o of bindingOpts[t] ?? []) {
        if (`${t}:${o.id}` === key) return o.label;
      }
    }
    // also check if source_type exists at all? If not in opts, still show key
    return key;
  };
  const hasSource = (r: Region): boolean => {
    if (!r.source_type) return true; // unbound is valid, not warning
    const key = `${r.source_type}:${r.source_id}`;
    for (const t of Object.keys(bindingOpts)) {
      if (t !== r.source_type) continue;
      for (const o of bindingOpts[t] ?? []) if (`${t}:${o.id}` === key) return true;
    }
    // if type not in opts at all, it's unknown -> warning
    // if type present but id not found -> deleted -> warning
    return false;
  };

  const syncHiddenInputs = () => {
    nameInput.value = nameVis?.value ?? nameInput.value;
    rowsInput.value = String(rows);
    colsInput.value = String(cols);
    gapInput.value = String(gap);
    paddingInput.value = paddingInput.value; // keep as is
    backgroundInput.value = bgVis?.value ?? backgroundInput.value;
    if (enabledVis) enabledInput.checked = enabledVis.checked;
    canvasWInput.value = String(canvasW);
    canvasHInput.value = String(canvasH);
    regionsInput.value = JSON.stringify(regions);
  };

  const validate = (): string | null => {
    for (const r of regions) {
      if (r.w < minRegion || r.h < minRegion) return `Region ${r.id} below minimum size ${minRegion}px`;
      if (r.w <= 0 || r.h <= 0) return `Region ${r.id} has non-positive size`;
      if (r.x < 0 || r.y < 0 || r.x + r.w > canvasW || r.y + r.h > canvasH) return `Region ${r.id} out of bounds`;
    }
    return null;
  };
  const syncValidation = () => {
    const msg = validate();
    if (msg) {
      if (validationWarning) { validationWarning.textContent = msg; validationWarning.style.display = "block"; }
      (form as unknown as { dataset: DOMStringMap }).dataset.previewValid = "false";
    } else {
      if (validationWarning) validationWarning.style.display = "none";
      (form as unknown as { dataset: DOMStringMap }).dataset.previewValid = "true";
    }
    return !msg;
  };

  const updatePreviewSizeLabel = () => {
    if (previewSizeEl) {
      const d = previewDims();
      previewSizeEl.textContent = `${d.w}×${d.h}`;
    }
  };

  // preview debounced + in-flight guard (single in-flight + latest-wins)
  const endpoint = form.dataset.previewEndpoint ?? "/admin/preview/layout";
  let debounceTimer: number | undefined;
  let inFlight = false;
  let pending = false;
  const doPreview = async () => {
    if ((form as unknown as { dataset: DOMStringMap }).dataset.previewValid === "false") return;
    if (inFlight) { pending = true; return; }
    inFlight = true;
    try {
      const fd = new FormData(form);
      const dims = previewDims();
      fd.set("w", String(dims.w));
      fd.set("h", String(dims.h));
      // ensure canvas_w/h sent as dims? spec says w/h set to current canvas size (preview resolution) — use dims
      const res = await fetch(endpoint, { method: "POST", body: fd });
      if (!res.ok) return;
      const blob = await res.blob();
      if (previewImg) {
        const url = URL.createObjectURL(blob);
        // revoke old after load? short leak ok; revoke previous if needed
        const old = previewImg.src;
        previewImg.src = url;
        if (old.startsWith("blob:")) URL.revokeObjectURL(old);
      }
    } catch { /* ignore */ } finally {
      inFlight = false;
      if (pending) { pending = false; schedulePreviewImmediate(); }
    }
  };
  const schedulePreviewImmediate = () => { window.clearTimeout(debounceTimer); debounceTimer = window.setTimeout(() => { void doPreview(); }, 300) as unknown as number; };
  const schedulePreview = () => { window.clearTimeout(debounceTimer); debounceTimer = window.setTimeout(() => { void doPreview(); }, 300) as unknown as number; };

  // rendering
  const renderBezels = () => {
    // remove old
    surface.querySelectorAll("[data-bezel]").forEach(e => e.remove());
    const d = getDevice();
    if (!d || d.panel_cols < 2 || d.panel_gap <= 0) return;
    const panelW = Math.floor(d.width / d.panel_cols);
    for (let g = 0; g < d.panel_cols - 1; g++) {
      const x = (g + 1) * panelW + g * d.panel_gap;
      const el = document.createElement("div");
      el.setAttribute("data-bezel", "");
      el.setAttribute("role", "separator");
      el.setAttribute("aria-label", "bezel gap");
      el.style.left = x + "px";
      el.style.width = d.panel_gap + "px";
      el.style.pointerEvents = "none";
      surface.appendChild(el);
    }
  };

  let guides: HTMLElement[] = [];
  const clearGuides = () => { guides.forEach(g => g.remove()); guides = []; };
  const showGuides = (xLines: number[], yLines: number[]) => {
    clearGuides();
    for (const x of xLines) {
      const g = document.createElement("div");
      g.className = "guide-line v";
      g.style.left = x + "px";
      surface.appendChild(g); guides.push(g);
    }
    for (const y of yLines) {
      const g = document.createElement("div");
      g.className = "guide-line h";
      g.style.top = y + "px";
      surface.appendChild(g); guides.push(g);
    }
  };

  const focusRegion = (id: string) => {
    const el = surface?.querySelector<HTMLElement>(`[data-region][data-region-id="${CSS.escape(id)}"]`);
    el?.focus({ preventScroll: true });
  };

  const renderCanvas = () => {
    if (!surface) return;
    // preserve keyboard focus across re-render (innerHTML wipe would drop it)
    const active = document.activeElement as HTMLElement | null;
    const focusedId = active && surface.contains(active)
      ? active.closest("[data-region]")?.getAttribute("data-region-id") ?? null
      : null;
    // size surface
    surface.style.width = canvasW + "px";
    surface.style.height = canvasH + "px";
    // keep existing bezels? re-render after clearing regions but before bezels
    // remove old regions/guides but keep bezels to re-add later? simpler clear all then re-add bezels
    const keepBezels = Array.from(surface.querySelectorAll("[data-bezel]"));
    surface.innerHTML = "";
    // re-append bezels after? We'll renderBezels after regions so bezels overlay
    // background from input
    const bg = backgroundInput.value || bgVis?.value || "#000000";
    surface.style.background = bg;

    for (const r of regions) {
      const el = document.createElement("div");
      el.setAttribute("data-region", "");
      el.setAttribute("data-region-id", r.id);
      el.tabIndex = 0;
      el.setAttribute("role", "button");
      el.setAttribute("aria-label", `Region ${r.id} ${r.w}x${r.h} ${sourceLabelFor(r)}`);
      el.style.left = r.x + "px";
      el.style.top = r.y + "px";
      el.style.width = r.w + "px";
      el.style.height = r.h + "px";
      if (selectedId === r.id) el.classList.add("selected");
      // label
      const lab = document.createElement("span");
      lab.className = "region-label";
      lab.textContent = `${r.id} ${sourceLabelFor(r)}`;
      el.appendChild(lab);
      if (!hasSource(r)) {
        const w = document.createElement("span");
        w.className = "region-warning";
        w.textContent = "⚠ missing source";
        el.appendChild(w);
      }
      // handles (only for selected)
      if (selectedId === r.id) {
        for (const h of ["nw", "n", "ne", "e", "se", "s", "sw", "w"] as const) {
          const hd = document.createElement("div");
          hd.setAttribute("data-handle", h);
          el.appendChild(hd);
        }
      }
      surface.appendChild(el);
    }
    renderBezels();
    if (focusedId) focusRegion(focusedId);
    if (regionCountEl) regionCountEl.textContent = String(regions.length);
  };

  const renderRegionList = () => {
    if (!regionListEl) return;
    regionListEl.innerHTML = "";
    regions.forEach((r, idx) => {
      const item = document.createElement("div");
      item.setAttribute("data-region-item", "");
      item.setAttribute("data-region-id", r.id);
      item.className = "d-flex gap-2 align-items-center p-2 border rounded" + (selectedId === r.id ? " border-warning" : "");
      // select button
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "btn btn-sm btn-ghost flex-grow-1 text-start";
      btn.textContent = `${idx + 1}. ${r.id} ${r.w}×${r.h} ${sourceLabelFor(r)}`;
      btn.addEventListener("click", () => { selectedId = r.id; syncAll(); });
      item.appendChild(btn);
      // up/down
      const up = document.createElement("button");
      up.type = "button"; up.className = "btn btn-sm btn-ghost"; up.textContent = "↑"; up.title = "Move up";
      up.disabled = idx === 0;
      up.addEventListener("click", () => {
        if (idx === 0) return;
        const tmp = regions[idx - 1]; regions[idx - 1] = regions[idx]; regions[idx] = tmp;
        pushSnapshot(); syncAll(); schedulePreview();
      });
      const down = document.createElement("button");
      down.type = "button"; down.className = "btn btn-sm btn-ghost"; down.textContent = "↓"; down.title = "Move down";
      down.disabled = idx === regions.length - 1;
      down.addEventListener("click", () => {
        if (idx >= regions.length - 1) return;
        const tmp = regions[idx + 1]; regions[idx + 1] = regions[idx]; regions[idx] = tmp;
        pushSnapshot(); syncAll(); schedulePreview();
      });
      const del = document.createElement("button");
      del.type = "button"; del.className = "btn btn-sm btn-danger"; del.textContent = "×"; del.title = "Delete";
      del.addEventListener("click", () => {
        regions = regions.filter(x => x.id !== r.id);
        if (selectedId === r.id) selectedId = regions[0]?.id ?? null;
        pushSnapshot(); syncAll(); schedulePreview();
      });
      item.append(up, down, del);
      regionListEl.appendChild(item);
    });
  };

  const populateSourceSelect = () => {
    const sel = inspectorEl.querySelector<HTMLSelectElement>('[data-field="source"]');
    if (!sel) return;
    const cur = regions.find(r => r.id === selectedId);
    sel.innerHTML = "";
    const empty = document.createElement("option");
    empty.value = ""; empty.textContent = "unbound";
    sel.appendChild(empty);
    const types = Object.keys(bindingOpts).sort();
    for (const t of types) {
      const grp = document.createElement("optgroup");
      grp.label = t;
      for (const o of bindingOpts[t] ?? []) {
        const opt = document.createElement("option");
        opt.value = `${t}:${o.id}`;
        opt.textContent = o.label;
        grp.appendChild(opt);
      }
      sel.appendChild(grp);
    }
    if (cur?.source_type) {
      const v = `${cur.source_type}:${cur.source_id}`;
      // check if exists; if not, add warning option
      let exists = false;
      for (const o of sel.querySelectorAll("option")) if (o.value === v) exists = true;
      if (!exists) {
        const opt = document.createElement("option");
        opt.value = v; opt.textContent = `${v} (missing)`;
        sel.appendChild(opt);
      }
      sel.value = v;
    } else sel.value = "";
  };

  const renderInspector = () => {
    const cur = regions.find(r => r.id === selectedId);
    const insetEl = inspectorEl.querySelector<HTMLInputElement>('[data-field="inset"]');
    const borderEl = inspectorEl.querySelector<HTMLInputElement>('[data-field="border"]');
    const accentEl = inspectorEl.querySelector<HTMLInputElement>('[data-field="theme-accent"]');
    const textEl = inspectorEl.querySelector<HTMLInputElement>('[data-field="theme-text"]');
    const bgEl = inspectorEl.querySelector<HTMLInputElement>('[data-field="theme-background"]');
    const fsEl = inspectorEl.querySelector<HTMLInputElement>('[data-field="theme-font-size"]');
    const hasSelection = !!cur;
    inspectorEl.style.opacity = hasSelection ? "1" : "0.5";
    // disable when none
    [insetEl, borderEl, accentEl, textEl, bgEl, fsEl].forEach(e => { if (e) (e as HTMLInputElement).disabled = !hasSelection; });
    populateSourceSelect();
    if (!cur) return;
    if (insetEl) insetEl.value = String(cur.inset ?? 0);
    if (borderEl) borderEl.checked = !!cur.border;
    if (accentEl) accentEl.value = cur.theme?.accent ?? "#000000";
    if (textEl) textEl.value = cur.theme?.text ?? "#000000";
    if (bgEl) bgEl.value = cur.theme?.background ?? "#000000";
    if (fsEl) fsEl.value = cur.theme?.font_size ? String(cur.theme.font_size) : "";
  };

  const syncAll = () => {
    syncHiddenInputs();
    syncValidation();
    renderCanvas();
    renderRegionList();
    renderInspector();
    syncUndoRedoButtons();
  };

  // grid preset helper (largest remainder)
  const applyGridPreset = () => {
    if (regions.length > 0 && !window.confirm("Replace current regions with grid preset?")) return;
    const cw = canvasW, ch = canvasH;
    const totalGapW = (cols - 1) * gap;
    const totalGapH = (rows - 1) * gap;
    const availW = Math.max(0, cw - totalGapW);
    const availH = Math.max(0, ch - totalGapH);
    const baseW = Math.floor(availW / cols);
    const baseH = Math.floor(availH / rows);
    const remW = availW % cols;
    const remH = availH % rows;
    const newRegions: Region[] = [];
    let idx = 0;
    // distribute remainder: first remW cols get +1 width, first remH rows get +1 height
    // compute x offsets cumulative
    for (let r = 0; r < rows; r++) {
      for (let c = 0; c < cols; c++) {
        const w = baseW + (c < remW ? 1 : 0);
        const h = baseH + (r < remH ? 1 : 0);
        // compute x = c*baseW + min(c,remW) + c*gap
        const x = c * baseW + Math.min(c, remW) + c * gap;
        const y = r * baseH + Math.min(r, remH) + r * gap;
        newRegions.push({ id: `r${idx + 1}`, x, y, w, h, source_type: "", source_id: 0 });
        idx++;
      }
    }
    regions = newRegions;
    idCounter = newRegions.length + 1;
    selectedId = regions[0]?.id ?? null;
    pushSnapshot(); syncAll(); schedulePreview();
  };

  // templates
  const applyTemplate = (kind: string) => {
    if (regions.length > 0 && !window.confirm("Apply template and replace regions?")) return;
    const cw = canvasW, ch = canvasH;
    let out: Region[] = [];
    if (kind === "blank") out = [];
    else if (kind === "split") {
      const halfW = Math.floor(cw / 2), halfH = Math.floor(ch / 2);
      out = [
        { id: "r1", x: 0, y: 0, w: halfW, h: halfH, source_type: "" },
        { id: "r2", x: halfW, y: 0, w: cw - halfW, h: halfH, source_type: "" },
        { id: "r3", x: 0, y: halfH, w: halfW, h: ch - halfH, source_type: "" },
        { id: "r4", x: halfW, y: halfH, w: cw - halfW, h: ch - halfH, source_type: "" },
      ];
    } else if (kind === "header-footer") {
      const headerH = Math.max(minRegion, Math.floor(ch * 0.22));
      const footerH = Math.max(minRegion, Math.floor(ch * 0.18));
      const midH = ch - headerH - footerH;
      out = [
        { id: "r1", x: 0, y: 0, w: cw, h: headerH, source_type: "" },
        { id: "r2", x: 0, y: headerH, w: cw, h: midH > 0 ? midH : minRegion, source_type: "" },
        { id: "r3", x: 0, y: headerH + (midH > 0 ? midH : minRegion), w: cw, h: footerH, source_type: "" },
      ];
      // clamp last if overflow
      if (out[2].y + out[2].h > ch) out[2].h = ch - out[2].y;
    } else if (kind === "sidebar") {
      const sideW = Math.max(minRegion, Math.floor(cw * 0.28));
      out = [
        { id: "r1", x: 0, y: 0, w: sideW, h: ch, source_type: "" },
        { id: "r2", x: sideW, y: 0, w: cw - sideW, h: ch, source_type: "" },
      ];
    }
    regions = out as Region[];
    idCounter = out.length + 1;
    selectedId = regions[0]?.id ?? null;
    pushSnapshot(); syncAll(); schedulePreview();
  };

  // inspector bindings
  inspectorEl.querySelector<HTMLSelectElement>('[data-field="source"]')?.addEventListener("change", (e) => {
    const cur = regions.find(r => r.id === selectedId);
    if (!cur) return;
    const v = (e.target as HTMLSelectElement).value;
    if (!v) { cur.source_type = ""; cur.source_id = 0; }
    else { const [t, idStr] = v.split(":"); cur.source_type = t; cur.source_id = Number(idStr); }
    pushSnapshot(); syncAll(); schedulePreview();
  });
  inspectorEl.querySelector<HTMLInputElement>('[data-field="inset"]')?.addEventListener("input", (e) => {
    const cur = regions.find(r => r.id === selectedId); if (!cur) return;
    cur.inset = Math.max(0, parseInt((e.target as HTMLInputElement).value, 10) || 0);
    syncHiddenInputs(); schedulePreview();
  });
  inspectorEl.querySelector<HTMLInputElement>('[data-field="inset"]')?.addEventListener("change", () => { pushSnapshot(); syncAll(); });
  inspectorEl.querySelector<HTMLInputElement>('[data-field="border"]')?.addEventListener("change", (e) => {
    const cur = regions.find(r => r.id === selectedId); if (!cur) return;
    cur.border = (e.target as HTMLInputElement).checked;
    pushSnapshot(); syncAll(); schedulePreview();
  });
  for (const f of ["theme-accent", "theme-text", "theme-background"] as const) {
    inspectorEl.querySelector<HTMLInputElement>(`[data-field="${f}"]`)?.addEventListener("input", (e) => {
      const cur = regions.find(r => r.id === selectedId); if (!cur) return;
      if (!cur.theme) cur.theme = {};
      const v = (e.target as HTMLInputElement).value;
      if (f === "theme-accent") cur.theme.accent = v;
      if (f === "theme-text") cur.theme.text = v;
      if (f === "theme-background") cur.theme.background = v;
      syncHiddenInputs(); schedulePreview();
    });
    inspectorEl.querySelector<HTMLInputElement>(`[data-field="${f}"]`)?.addEventListener("change", () => { pushSnapshot(); syncAll(); });
  }
  inspectorEl.querySelector<HTMLInputElement>('[data-field="theme-font-size"]')?.addEventListener("input", (e) => {
    const cur = regions.find(r => r.id === selectedId); if (!cur) return;
    const v = (e.target as HTMLInputElement).value.trim();
    if (!cur.theme) cur.theme = {};
    if (v === "") delete cur.theme.font_size;
    else cur.theme.font_size = Number(v) || undefined;
    syncHiddenInputs(); schedulePreview();
  });
  inspectorEl.querySelector<HTMLInputElement>('[data-field="theme-font-size"]')?.addEventListener("change", () => { pushSnapshot(); syncAll(); });
  inspectorEl.querySelector<HTMLButtonElement>("[data-clear-theme]")?.addEventListener("click", () => {
    const cur = regions.find(r => r.id === selectedId); if (!cur) return;
    delete cur.theme; pushSnapshot(); syncAll(); schedulePreview();
  });

  // toolbar
  document.querySelector("[data-undo]")?.addEventListener("click", () => { undo(); schedulePreview(); });
  document.querySelector("[data-redo]")?.addEventListener("click", () => { redo(); schedulePreview(); });
  document.querySelector("[data-add-region]")?.addEventListener("click", () => {
    const id = `r${idCounter++}`;
    const w = Math.max(minRegion, Math.min(32, canvasW - 4));
    const h = Math.max(minRegion, Math.min(16, canvasH - 4));
    regions.push({ id, x: 2, y: 2, w, h, source_type: "" });
    selectedId = id; pushSnapshot(); syncAll(); schedulePreview();
  });
  document.querySelector("[data-delete-region]")?.addEventListener("click", () => {
    if (!selectedId) return;
    regions = regions.filter(r => r.id !== selectedId);
    selectedId = regions[0]?.id ?? null;
    pushSnapshot(); syncAll(); schedulePreview();
  });
  document.querySelector("[data-duplicate-region]")?.addEventListener("click", () => {
    const cur = regions.find(r => r.id === selectedId); if (!cur) return;
    const id = `r${idCounter++}`;
    const dup: Region = { ...cur, id, x: Math.min(cur.x + 8, canvasW - cur.w), y: Math.min(cur.y + 8, canvasH - cur.h) };
    if (dup.theme) dup.theme = { ...cur.theme! };
    regions.push(dup); selectedId = id; pushSnapshot(); syncAll(); schedulePreview();
  });
  document.querySelectorAll<HTMLElement>("[data-template]").forEach(b => {
    b.addEventListener("click", () => { const k = b.getAttribute("data-template") ?? "blank"; applyTemplate(k); });
  });
  document.querySelector("[data-grid-apply]")?.addEventListener("click", applyGridPreset);

  // visible meta sync
  nameVis?.addEventListener("input", () => { nameInput.value = nameVis.value; });
  const syncRowsColsGap = () => {
    rows = Math.max(1, parseInt(rowsVis?.value ?? String(rows), 10) || 1);
    cols = Math.max(1, parseInt(colsVis?.value ?? String(cols), 10) || 1);
    gap = Math.max(0, parseInt(gapVis?.value ?? String(gap), 10) || 0);
    rowsInput.value = String(rows); colsInput.value = String(cols); gapInput.value = String(gap);
    if (snapInput && gap > 0) { /* keep snap unless user changed */ }
  };
  rowsVis?.addEventListener("input", syncRowsColsGap);
  colsVis?.addEventListener("input", syncRowsColsGap);
  gapVis?.addEventListener("input", syncRowsColsGap);
  rowsVis?.addEventListener("change", () => { pushSnapshot(); syncAll(); });
  colsVis?.addEventListener("change", () => { pushSnapshot(); syncAll(); });
  gapVis?.addEventListener("change", () => { pushSnapshot(); syncAll(); });
  bgVis?.addEventListener("input", () => { backgroundInput.value = bgVis.value; renderCanvas(); schedulePreview(); });
  bgVis?.addEventListener("change", () => { pushSnapshot(); });
  enabledVis?.addEventListener("change", () => { enabledInput.checked = enabledVis.checked; });

  // pointer drag/resize
  let drag: { id: string; mode: "move" | "resize"; handle: string | null; startX: number; startY: number; orig: Region; pointerId: number } | null = null;
  const onPointerDown = (e: PointerEvent) => {
    const target = e.target as HTMLElement;
    const regionEl = target.closest<HTMLElement>("[data-region]");
    if (!regionEl) {
      // click on surface -> deselect?
      if (target === surface || target.closest("[data-canvas]")) { selectedId = null; syncAll(); }
      return;
    }
    const id = regionEl.getAttribute("data-region-id")!;
    // select
    selectedId = id;
    syncAll();
    // keep keyboard focus on the (freshly rendered) region element.
    // synchronous for immediate key handling; rAF re-asserts after the
    // browser's native mousedown focus targets the detached old element.
    focusRegion(id);
    requestAnimationFrame(() => { if (selectedId === id) focusRegion(id); });
    const handleEl = (e.target as HTMLElement).closest<HTMLElement>("[data-handle]");
    const isResize = !!handleEl;
    const r = regions.find(x => x.id === id);
    if (!r) return;
    drag = { id, mode: isResize ? "resize" : "move", handle: handleEl?.getAttribute("data-handle") ?? null, startX: e.clientX, startY: e.clientY, orig: { ...r, theme: r.theme ? { ...r.theme } : undefined }, pointerId: e.pointerId };
    (e.target as Element).setPointerCapture?.(e.pointerId);
    e.preventDefault();
  };
  const onPointerMove = (e: PointerEvent) => {
    if (!drag) return;
    const r = regions.find(x => x.id === drag!.id);
    if (!r) return;
    const scale = currentScale();
    const dx = convert(e.clientX - drag.startX, scale);
    const dy = convert(e.clientY - drag.startY, scale);
    // zero-movement drag should just select (no move). But we already selected; skip tiny delta? No, treat as select already.
    const useSnap = !e.shiftKey;
    const step = snap;
    let nx = drag.orig.x, ny = drag.orig.y, nw = drag.orig.w, nh = drag.orig.h;

    if (drag.mode === "move") {
      nx = drag.orig.x + dx;
      ny = drag.orig.y + dy;
      if (useSnap) { nx = snapVal(nx, step); ny = snapVal(ny, step); }
      // guides: align edges to other regions or canvas centre
      if (useSnap) {
        const guidesX: number[] = [];
        const guidesY: number[] = [];
        const cx = canvasW / 2, cy = canvasH / 2;
        // centre guides
        if (Math.abs(nx + nw / 2 - cx) < 4) { nx = cx - nw / 2; guidesX.push(cx); }
        if (Math.abs(ny + nh / 2 - cy) < 4) { ny = cy - nh / 2; guidesY.push(cy); }
        for (const o of regions) {
          if (o.id === r.id) continue;
          if (Math.abs(nx - o.x) < 4) { nx = o.x; guidesX.push(o.x); }
          if (Math.abs(nx + nw - (o.x + o.w)) < 4) { nx = o.x + o.w - nw; guidesX.push(o.x + o.w); }
          if (Math.abs(nx - (o.x + o.w)) < 4) { nx = o.x + o.w; guidesX.push(o.x + o.w); }
          if (Math.abs(ny - o.y) < 4) { ny = o.y; guidesY.push(o.y); }
          if (Math.abs(ny + nh - (o.y + o.h)) < 4) { ny = o.y + o.h - nh; guidesY.push(o.y + o.h); }
        }
        if (guidesX.length || guidesY.length) showGuides(guidesX, guidesY); else clearGuides();
      } else clearGuides();
      // clamp
      nx = clamp(nx, 0, canvasW - nw);
      ny = clamp(ny, 0, canvasH - nh);
      r.x = Math.round(nx); r.y = Math.round(ny);
    } else {
      // resize via handle
      const h = drag.handle ?? "se";
      // For each handle, adjust edges: nw: x+w fixed at orig.x+orig.w , y+h fixed
      // Compute new x/y/w/h based on delta
      let x1 = drag.orig.x, y1 = drag.orig.y, x2 = drag.orig.x + drag.orig.w, y2 = drag.orig.y + drag.orig.h;
      // Apply deltas per handle
      if (h.includes("n")) y1 = drag.orig.y + dy;
      if (h.includes("s")) y2 = drag.orig.y + drag.orig.h + dy;
      if (h.includes("w")) x1 = drag.orig.x + dx;
      if (h.includes("e")) x2 = drag.orig.x + drag.orig.w + dx;
      // For edge handles, alternative: n => y1 moves, s => y2 moves etc already handled
      if (useSnap) { x1 = snapVal(x1, step); y1 = snapVal(y1, step); x2 = snapVal(x2, step); y2 = snapVal(y2, step); }
      // ensure x1<x2 and y1<y2, clamp to canvas and min size
      // clamp to canvas first
      x1 = clamp(x1, 0, canvasW); x2 = clamp(x2, 0, canvasW);
      y1 = clamp(y1, 0, canvasH); y2 = clamp(y2, 0, canvasH);
      // enforce min size: if x2 - x1 < minRegion, push edge back
      if (h.includes("w")) { if (x2 - x1 < minRegion) x1 = x2 - minRegion; }
      else if (h.includes("e")) { if (x2 - x1 < minRegion) x2 = x1 + minRegion; }
      else { // shouldn't happen for move; for resize ensure still
        if (x2 - x1 < minRegion) x2 = x1 + minRegion;
      }
      if (h.includes("n")) { if (y2 - y1 < minRegion) y1 = y2 - minRegion; }
      else if (h.includes("s")) { if (y2 - y1 < minRegion) y2 = y1 + minRegion; }
      else { if (y2 - y1 < minRegion) y2 = y1 + minRegion; }
      // final clamp inside canvas after min adjust
      if (x1 < 0) { x2 -= x1; x1 = 0; }
      if (y1 < 0) { y2 -= y1; y1 = 0; }
      if (x2 > canvasW) { x1 -= (x2 - canvasW); x2 = canvasW; if (x1 < 0) x1 = 0; }
      if (y2 > canvasH) { y1 -= (y2 - canvasH); y2 = canvasH; if (y1 < 0) y1 = 0; }
      r.x = Math.round(x1); r.y = Math.round(y1); r.w = Math.round(x2 - x1); r.h = Math.round(y2 - y1);
      // enforce min finally
      if (r.w < minRegion) r.w = minRegion;
      if (r.h < minRegion) r.h = minRegion;
    }
    // live render without pushing snapshot yet
    syncHiddenInputs(); renderCanvas(); renderInspector(); renderRegionList(); syncValidation();
  };
  const onPointerUp = (e: PointerEvent) => {
    if (!drag) return;
    clearGuides();
    // zero-movement drag already selected; still push if moved? Check if region changed vs orig
    const r = regions.find(x => x.id === drag!.id);
    if (r) {
      const changed = r.x !== drag.orig.x || r.y !== drag.orig.y || r.w !== drag.orig.w || r.h !== drag.orig.h;
      if (changed) { pushSnapshot(); syncAll(); schedulePreview(); }
    }
    drag = null;
  };
  surface.addEventListener("pointerdown", onPointerDown);
  window.addEventListener("pointermove", onPointerMove);
  window.addEventListener("pointerup", onPointerUp);
  surface.addEventListener("pointercancel", onPointerUp);

  // keyboard
  surface.addEventListener("keydown", (e: KeyboardEvent) => {
    const cur = regions.find(r => r.id === selectedId);
    if (!cur) return;
    const step = snap;
    let handled = false;
    if (e.key.startsWith("Arrow")) {
      e.preventDefault();
      handled = true;
      if (e.shiftKey) {
        // resize
        if (e.key === "ArrowLeft") cur.w = Math.max(minRegion, cur.w - step);
        if (e.key === "ArrowRight") cur.w = Math.min(canvasW - cur.x, cur.w + step);
        if (e.key === "ArrowUp") cur.h = Math.max(minRegion, cur.h - step);
        if (e.key === "ArrowDown") cur.h = Math.min(canvasH - cur.y, cur.h + step);
      } else {
        if (e.key === "ArrowLeft") cur.x = clamp(cur.x - step, 0, canvasW - cur.w);
        if (e.key === "ArrowRight") cur.x = clamp(cur.x + step, 0, canvasW - cur.w);
        if (e.key === "ArrowUp") cur.y = clamp(cur.y - step, 0, canvasH - cur.h);
        if (e.key === "ArrowDown") cur.y = clamp(cur.y + step, 0, canvasH - cur.h);
      }
      if (handled) { pushSnapshot(); syncAll(); schedulePreview(); }
    } else if (e.key === "Delete" || e.key === "Backspace") {
      regions = regions.filter(r => r.id !== selectedId);
      selectedId = regions[0]?.id ?? null;
      pushSnapshot(); syncAll(); schedulePreview();
    }
  });

  // form submit sync
  form.addEventListener("submit", () => { syncHiddenInputs(); });

  // initial render
  syncAll();
  updatePreviewSizeLabel();
  schedulePreview();

  // expose debug
  window.__layoutDebug = {
    convert,
    getRegions: () => JSON.parse(JSON.stringify(regions)),
    setScale: (s: number) => { debugScale = s; },
  };

  // click on region list container delegation already handled via buttons
  // also allow selecting via canvas region click (already pointerdown selects)
}
