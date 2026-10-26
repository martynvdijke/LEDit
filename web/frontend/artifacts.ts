// Share Artifacts admin page: dry-run validation preview and import feedback.
// The page never fetches a remote bundle until the admin presses a button.

interface ArtifactWarning {
  code: string;
  path?: string;
  message: string;
}

interface ArtifactConflict {
  kind: string;
  name: string;
  existing_id: number;
}

interface ArtifactResult {
  imported?: boolean;
  kind?: string;
  name?: string;
  id?: number;
  replaced?: boolean;
  skipped?: boolean;
  action?: string;
  conflict?: ArtifactConflict;
  warnings?: ArtifactWarning[];
  error?: string;
  message?: string;
  details?: Record<string, unknown>;
}

function el<T extends HTMLElement>(id: string): T | null {
  return document.getElementById(id) as T | null;
}

function renderResult(panel: HTMLElement, result: ArtifactResult, dryRun: boolean): void {
  const parts: string[] = [];
  const title = dryRun ? "Validation result" : "Import result";

  if (result.error) {
    parts.push(`<div class="alert alert-danger mb-2"><strong>${result.error}</strong>: ${result.message ?? ""}</div>`);
    if (result.details && Object.keys(result.details).length > 0) {
      parts.push(`<pre class="small mb-2">${JSON.stringify(result.details, null, 2)}</pre>`);
    }
    if (result.error === "name_conflict") {
      parts.push(
        '<div class="alert alert-warning mb-2">An artifact with this name already exists. Choose <code>Rename</code> in the "On name conflict" dropdown, provide a new name, and try again.</div>',
      );
    }
  } else {
    const kind = result.kind ?? "artifact";
    const name = result.name ?? "";
    const action = result.action ?? (result.imported ? "create" : "");
    parts.push(
      `<div class="alert alert-success mb-2">${title}: <strong>${kind}</strong> ${name}${action ? ` (${action})` : ""}</div>`,
    );
  }

  const warnings = result.warnings ?? [];
  if (warnings.length > 0) {
    const items = warnings
      .map((w) => `<li><code>${w.code}</code>${w.path ? ` <span class="text-muted">${w.path}</span>` : ""}: ${w.message}</li>`)
      .join("");
    parts.push(`<div class="alert alert-warning mb-2"><strong>Warnings</strong><ul class="mb-0">${items}</ul></div>`);
  }

  if (dryRun && !result.error) {
    parts.push(
      '<p class="text-muted small mb-0">Nothing was written. Press <strong>Import</strong> to apply this bundle.</p>',
    );
  }

  panel.innerHTML = parts.join("");
  panel.hidden = false;
}

function formPayload(form: HTMLFormElement): FormData {
  const data = new FormData(form);
  // Drop empty optional fields so the server sees clean defaults.
  for (const key of ["url", "sha256", "name", "on_conflict"]) {
    const value = data.get(key);
    if (typeof value === "string" && value.trim() === "") {
      data.delete(key);
    }
  }
  return data;
}

function init(): void {
  const form = el<HTMLFormElement>("artifact-import-form");
  const validate = el<HTMLButtonElement>("artifact-validate");
  const panel = el<HTMLDivElement>("artifact-result");
  if (!form || !validate || !panel) {
    return;
  }

  const submit = async (dryRun: boolean): Promise<void> => {
    const data = formPayload(form);
    if (dryRun) {
      data.set("dry_run", "1");
    }
    const file = data.get("file");
    if (!dryRun && file instanceof File && file.size === 0) {
      data.delete("file");
    }
    panel.hidden = false;
    panel.innerHTML = '<div class="text-muted">Working…</div>';
    try {
      const response = await fetch("/admin/api/artifacts/import", {
        method: "POST",
        body: data,
        credentials: "same-origin",
      });
      const body = (await response.json()) as ArtifactResult;
      renderResult(panel, body, dryRun);
      if (!dryRun && response.ok && body.imported) {
        window.setTimeout(() => {
          window.location.href = "/admin/artifacts";
        }, 600);
      }
    } catch (err) {
      panel.innerHTML = `<div class="alert alert-danger mb-0">Request failed: ${String(err)}</div>`;
    }
  };

  validate.addEventListener("click", () => {
    void submit(true);
  });

  form.addEventListener("submit", (event) => {
    // Progressive enhancement: the plain form still works without JS.
    event.preventDefault();
    void submit(false);
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
