import './styles.css';

const shell = document.querySelector<HTMLElement>('[data-app-shell]');
const toggle = document.querySelector<HTMLButtonElement>('[data-nav-toggle]');
const nav = document.querySelector<HTMLElement>('[data-nav]');

toggle?.addEventListener('click', () => {
  const open = shell?.classList.toggle('nav-open') ?? false;
  toggle.setAttribute('aria-expanded', String(open));
  nav?.setAttribute('aria-hidden', String(!open));
});

document.querySelectorAll<HTMLElement>('[data-dismiss]').forEach((element) => {
  element.addEventListener('click', () => element.closest('[role="alert"]')?.remove());
});

// ---------------------------------------------------------------------------
// Live previews: debounced PNG previews for datasource + matrix forms
// ---------------------------------------------------------------------------

interface Binding {
  row: number;
  col: number;
  source_type?: string;
  source_id?: number;
}

interface BindingOption {
  id: number;
  label: string;
}

const PREVIEW_SIZE = 256;
const DEBOUNCE_MS = 300;

function debounce(fn: () => void, ms: number): () => void {
  let timer: number | undefined;
  return () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(fn, ms);
  };
}

async function postPreview(endpoint: string, data: FormData): Promise<void> {
  const img = document.querySelector<HTMLImageElement>('[data-live-preview-img]');
  if (!img) return;
  try {
    const res = await fetch(endpoint, { method: 'POST', body: data });
    if (!res.ok) return;
    const blob = await res.blob();
    img.src = URL.createObjectURL(blob);
  } catch {
    // Preview failures are non-fatal
  }
}

// Datasource forms (token/url/name/config) -> /admin/preview/datasource
const liveForms = document.querySelectorAll<HTMLFormElement>(
  '[data-live-preview-form]:not([data-matrix-editor])',
);
liveForms.forEach((form) => {
  const endpoint = form.dataset.previewEndpoint ?? '/admin/preview/datasource';
  const type = form.dataset.previewType ?? '';
  const schedule = debounce(() => {
    if (form.dataset.previewValid === 'false') return;
    const data = new FormData(form);
    if (type) data.set('type', type);
    data.set('w', String(PREVIEW_SIZE));
    data.set('h', String(PREVIEW_SIZE));
    void postPreview(endpoint, data);
  }, DEBOUNCE_MS);
  form.addEventListener('input', schedule);
  form.addEventListener('change', schedule);
  // Initial render (edit pages show the saved source immediately)
  window.setTimeout(schedule, 400);
});

// ---------------------------------------------------------------------------
// Matrix layout editor: grid selectors -> bindings JSON -> live composite preview
// ---------------------------------------------------------------------------

const matrixForm = document.querySelector<HTMLFormElement>('[data-matrix-editor]');
if (matrixForm) {
  const opts =
    (window as unknown as { BINDING_OPTS?: Record<string, BindingOption[]> }).BINDING_OPTS ?? {};
  const grid = matrixForm.querySelector<HTMLElement>('[data-matrix-grid]');
  const bindingsInput = matrixForm.querySelector<HTMLInputElement>('[name="bindings"]');
  const rowsInput = matrixForm.querySelector<HTMLInputElement>('[name="rows"]');
  const colsInput = matrixForm.querySelector<HTMLInputElement>('[name="cols"]');
  const warning = matrixForm.querySelector<HTMLElement>('[data-matrix-warning]');
  const templateBtn = matrixForm.querySelector<HTMLButtonElement>('[data-matrix-template]');
  const endpoint = matrixForm.dataset.previewEndpoint ?? '/admin/preview/matrix';

  if (grid && bindingsInput && rowsInput && colsInput) {
    let bindings: Binding[] = [];
    try {
      bindings = JSON.parse(bindingsInput.value || '[]');
    } catch {
      bindings = [];
    }

    const bindingAt = (row: number, col: number): Binding | undefined =>
      bindings.find((b) => b.row === row && b.col === col);

    const dimsValid = (): boolean => {
      const r = Number(rowsInput.value);
      const c = Number(colsInput.value);
      const ok =
        Number.isInteger(r) &&
        Number.isInteger(c) &&
        r >= 1 &&
        r <= 8 &&
        c >= 1 &&
        c <= 8;
      if (warning) warning.style.display = ok ? 'none' : 'block';
      matrixForm.dataset.previewValid = ok ? 'true' : 'false';
      return ok;
    };

    const schedulePreview = debounce(() => {
      if (matrixForm.dataset.previewValid === 'false') return;
      const data = new FormData(matrixForm);
      data.set('w', String(PREVIEW_SIZE));
      data.set('h', String(PREVIEW_SIZE));
      void postPreview(endpoint, data);
    }, DEBOUNCE_MS);

    const renderGrid = (): void => {
      const rows = Number(rowsInput.value);
      const cols = Number(colsInput.value);
      grid.innerHTML = '';
      const types = Object.keys(opts).sort();
      for (let r = 0; r < rows; r++) {
        const rowEl = document.createElement('div');
        rowEl.className = 'd-flex gap-2 mb-2 align-items-center';
        const badge = document.createElement('span');
        badge.className = 'badge bg-secondary';
        badge.textContent = `R${r + 1}`;
        rowEl.appendChild(badge);
        for (let c = 0; c < cols; c++) {
          const sel = document.createElement('select');
          sel.className = 'form-select form-select-sm flex-grow-1';
          const unbound = document.createElement('option');
          unbound.value = '';
          unbound.textContent = 'unbound';
          sel.appendChild(unbound);
          for (const t of types) {
            const grp = document.createElement('optgroup');
            grp.label = t;
            for (const o of opts[t] ?? []) {
              const opt = document.createElement('option');
              opt.value = `${t}:${o.id}`;
              opt.textContent = o.label;
              grp.appendChild(opt);
            }
            sel.appendChild(grp);
          }
          const cur = bindingAt(r, c);
          if (cur?.source_type !== undefined && cur.source_id !== undefined) {
            sel.value = `${cur.source_type}:${cur.source_id}`;
          }
          sel.addEventListener('change', () => {
            const [t, idStr] = sel.value.split(':');
            bindings = bindings.filter((b) => !(b.row === r && b.col === c));
            if (t && idStr) {
              bindings.push({ row: r, col: c, source_type: t, source_id: Number(idStr) });
            }
            bindingsInput.value = JSON.stringify(bindings);
            schedulePreview();
          });
          rowEl.appendChild(sel);
        }
        grid.appendChild(rowEl);
      }
    };

    const refresh = (): void => {
      if (dimsValid()) {
        renderGrid();
        schedulePreview();
      }
    };

    rowsInput.addEventListener('input', refresh);
    colsInput.addEventListener('input', refresh);

    templateBtn?.addEventListener('click', async () => {
      if (matrixForm.dataset.previewValid === 'false') return;
      const data = new FormData(matrixForm);
      data.set('w', String(PREVIEW_SIZE));
      data.set('h', String(PREVIEW_SIZE));
      data.set('template', '1');
      try {
        const res = await fetch(endpoint, { method: 'POST', body: data });
        if (!res.ok) return;
        const blob = await res.blob();
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = 'matrix-template.png';
        a.click();
      } catch {
        // Export failures are non-fatal
      }
    });

    refresh();
    window.setTimeout(schedulePreview, 400);
  }
}

// ---------------------------------------------------------------------------
// Test-action buttons (Custom API): POST the form to the test endpoint and
// surface the extracted rows or the error.
// ---------------------------------------------------------------------------
document.querySelectorAll<HTMLFormElement>('[data-test-endpoint]').forEach((form) => {
  const btn = form.querySelector<HTMLButtonElement>('[data-test-action]');
  const result = form.querySelector<HTMLElement>('[data-test-result]');
  if (!btn || !result) return;
  btn.addEventListener('click', async () => {
    result.textContent = 'Testing\u2026';
    result.className = 'mb-3 alert alert-info';
    try {
      const res = await fetch(form.dataset.testEndpoint ?? '', { method: 'POST', body: new FormData(form) });
      const json = (await res.json().catch(() => ({}))) as {
        ok?: boolean;
        error?: string;
        title?: string;
        rows?: { label: string; value: string }[];
      };
      if (json.ok) {
        const lines = (json.rows ?? []).map((r) => `${r.label}: ${r.value}`);
        result.textContent = json.title && json.title !== ''
          ? `${json.title}\n${lines.join('\n')}`
          : lines.join('\n') || 'OK';
        result.className = 'mb-3 alert alert-success';
      } else {
        result.textContent = json.error ?? 'Test failed';
        result.className = 'mb-3 alert alert-danger';
      }
    } catch {
      result.textContent = 'Request failed';
      result.className = 'mb-3 alert alert-danger';
    }
  });
});
