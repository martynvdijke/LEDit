const form = document.querySelector<HTMLFormElement>('[data-theme-editor]');
if (form) {
  const img = document.querySelector<HTMLImageElement>('[data-live-preview-img]');
  const previewTarget = document.querySelector<HTMLSelectElement>('[data-theme-preview-target]');

  function debounce(fn: () => void, ms: number): () => void {
    let timer: number | undefined;
    return () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(fn, ms);
    };
  }

  async function doPreview(): Promise<void> {
    if (!img) return;
    const raw = previewTarget?.value ?? 'clock:0';
    const colon = raw.indexOf(':');
    const type = colon >= 0 ? raw.slice(0, colon) : raw;
    const id = colon >= 0 ? raw.slice(colon + 1) : '0';

    const bg = (form!.querySelector<HTMLInputElement>('input[name="bg_color"]')?.value ?? '').trim();
    const accent = (form!.querySelector<HTMLInputElement>('input[name="accent_color"]')?.value ?? '').trim();
    const text = (form!.querySelector<HTMLInputElement>('input[name="text_color"]')?.value ?? '').trim();
    const title = (form!.querySelector<HTMLInputElement>('input[name="title"]')?.value ?? '').trim();
    const fontSize = (form!.querySelector<HTMLInputElement>('input[name="font_size"]')?.value ?? '').trim();

    const url =
      `/admin/preview?type=${encodeURIComponent(type)}&id=${encodeURIComponent(id)}&w=256&h=256` +
      `&theme_bg=${encodeURIComponent(bg)}&theme_accent=${encodeURIComponent(accent)}&theme_text=${encodeURIComponent(text)}` +
      `&theme_title=${encodeURIComponent(title)}&theme_font_size=${encodeURIComponent(fontSize)}`;

    try {
      const res = await fetch(url, { credentials: 'same-origin' });
      if (!res.ok) return;
      const blob = await res.blob();
      img.src = URL.createObjectURL(blob);
    } catch {
      // silent
    }
  }

  const debounced = debounce(() => void doPreview(), 300);

  form.addEventListener('input', debounced);
  form.addEventListener('change', debounced);
  previewTarget?.addEventListener('change', () => void doPreview());

  void doPreview();
}
