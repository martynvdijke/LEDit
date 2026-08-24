const dateEl = document.getElementById('tl-date') as HTMLInputElement | null;
const deviceEl = document.getElementById('tl-device') as HTMLInputElement | null;
const strip = document.getElementById('filmstrip') as HTMLElement | null;
const emptyEl = document.getElementById('tl-empty') as HTMLElement | null;
const hintEl = document.getElementById('tl-hint') as HTMLElement | null;
const exportBtn = document.getElementById('tl-export') as HTMLButtonElement | null;

function todayStr(): string {
  return new Date().toISOString().slice(0, 10);
}
if (dateEl && !dateEl.value) dateEl.value = todayStr();

async function loadFrames() {
  if (!dateEl || !deviceEl || !strip) return;
  const date = dateEl.value || todayStr();
  const device_id = deviceEl.value.trim();
  if (!device_id) {
    if (emptyEl) { emptyEl.textContent = 'Enter a device ID.'; emptyEl.classList.remove('d-none'); }
    return;
  }
  // Try admin API first, fallback to /api
  let res = await fetch(`/admin/api/timelapse/frames?device_id=${device_id}&date=${date}`);
  if (!res.ok) res = await fetch(`/api/timelapse/frames?device_id=${device_id}&date=${date}`);
  if (!res.ok) {
    if (emptyEl) { emptyEl.textContent = 'Failed to load frames.'; emptyEl.classList.remove('d-none'); }
    return;
  }
  const data = await res.json() as { frames: { file_path: string; captured_at: string; source_type: string; source_id: number; source_label: string }[] };
  strip.innerHTML = '';
  if (!data.frames || data.frames.length === 0) {
    if (emptyEl) { emptyEl.textContent = 'No captures for this device/date.'; emptyEl.classList.remove('d-none'); }
    return;
  }
  if (emptyEl) emptyEl.classList.add('d-none');
  for (const f of data.frames) {
    const card = document.createElement('div');
    card.className = 'film-card';
    const img = document.createElement('img');
    img.loading = 'lazy';
    img.src = f.file_path;
    img.alt = f.source_label;
    const meta = document.createElement('div');
    meta.className = 'small text-muted';
    meta.textContent = new Date(f.captured_at).toLocaleTimeString() + ' ' + f.source_type + ':' + f.source_id;
    card.appendChild(img);
    card.appendChild(meta);
    card.addEventListener('click', () => {
      const modal = document.getElementById('tl-modal');
      const mImg = document.getElementById('tl-modal-img') as HTMLImageElement | null;
      const mMeta = document.getElementById('tl-modal-meta');
      if (mImg) mImg.src = f.file_path;
      if (mMeta) mMeta.textContent = new Date(f.captured_at).toLocaleString() + ' — ' + f.source_type + ':' + f.source_id + ' ' + f.source_label;
      modal?.classList.remove('d-none');
    });
    strip.appendChild(card);
  }
  if (hintEl) hintEl.textContent = `${data.frames.length} frames`;
}

document.getElementById('timelapse-filter')?.addEventListener('submit', (e) => {
  e.preventDefault();
  void loadFrames();
});

exportBtn?.addEventListener('click', async () => {
  const date = dateEl?.value || todayStr();
  const device_id = deviceEl?.value.trim();
  if (!device_id) return;
  exportBtn.textContent = 'Exporting…';
  exportBtn.disabled = true;
  try {
    let res = await fetch(`/admin/api/timelapse/export?device_id=${device_id}&date=${date}`, { method: 'POST' });
    if (!res.ok) res = await fetch(`/api/timelapse/export?device_id=${device_id}&date=${date}`, { method: 'POST' });
    if (!res.ok) {
      const j = await res.json().catch(() => ({})) as { error?: string };
      alert(j.error || 'Export failed');
      return;
    }
    const blob = await res.blob();
    const cd = res.headers.get('Content-Disposition') || '';
    let filename = `timelapse-${date}-${device_id}`;
    const m = /filename="?([^"]+)"?/.exec(cd);
    if (m) filename = m[1];
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  } finally {
    exportBtn.textContent = 'Export day';
    exportBtn.disabled = false;
  }
});

// Hint if ffmpeg missing: probe via HEAD? Just show generic hint.
if (hintEl) hintEl.textContent = 'ffmpeg not found — will export as GIF/ZIP';
