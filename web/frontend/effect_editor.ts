interface Descriptor {
  name: string;
  speed: number;
  intensity: number;
  density: number;
  direction: string;
  bounds: {
    speed: [number, number];
    intensity: [number, number];
    density: [number, number];
    direction: string[];
  };
}

const form = document.querySelector<HTMLFormElement>('[data-effect-editor]');
if (form) {
  const img = document.querySelector<HTMLImageElement>('[data-live-preview-img]');
  const effectSel = form.querySelector<HTMLSelectElement>('select[name="effect"]');
  const descriptors = (window as unknown as { EFFECT_DESCRIPTORS?: Descriptor[] }).EFFECT_DESCRIPTORS ?? [];
  const byName = new Map(descriptors.map((d) => [d.name, d]));

  function debounce(fn: () => void, ms: number): () => void {
    let timer: number | undefined;
    return () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(fn, ms);
    };
  }

  function field(name: string): string {
    return (form!.querySelector<HTMLInputElement>(`[name="${name}"]`)?.value ?? '').trim();
  }

  // Applying defaults when switching effects keeps the preview honest.
  effectSel?.addEventListener('change', () => {
    const desc = byName.get(effectSel.value);
    if (!desc) return;
    const speed = form.querySelector<HTMLInputElement>('input[name="speed"]');
    const intensity = form.querySelector<HTMLInputElement>('input[name="intensity"]');
    const density = form.querySelector<HTMLInputElement>('input[name="density"]');
    const direction = form.querySelector<HTMLSelectElement>('select[name="direction"]');
    if (speed) speed.value = String(desc.speed);
    if (intensity) intensity.value = String(desc.intensity);
    if (density) density.value = String(desc.density);
    if (direction && desc.direction) direction.value = desc.direction;
    void doPreview();
  });

  async function doPreview(): Promise<void> {
    if (!img) return;
    const body = new FormData();
    body.set('effect', field('effect'));
    body.set('speed', field('speed'));
    body.set('intensity', field('intensity'));
    body.set('density', field('density'));
    body.set('direction', field('direction'));
    body.set('seed', field('seed'));
    body.set('w', '256');
    body.set('h', '256');
    const id = form!.getAttribute('action')?.match(/effects\/(\d+)\/edit/)?.[1];
    if (id) body.set('id', id);

    try {
      const res = await fetch('/admin/preview/effect', {
        method: 'POST',
        body,
        credentials: 'same-origin',
      });
      if (!res.ok) return;
      const blob = await res.blob();
      img.src = URL.createObjectURL(blob);
    } catch {
      // A failed preview must never block editing.
    }
  }

  const debounced = debounce(() => void doPreview(), 300);
  form.addEventListener('input', debounced);
  form.addEventListener('change', debounced);
  void doPreview();
}
