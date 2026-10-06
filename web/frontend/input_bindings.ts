async function fetchJSON(url: string, opts: RequestInit = {}): Promise<any> {
  const r = await fetch(url, {
    ...opts,
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
  });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

function showModal(edit: any = null): void {
  const modal = document.getElementById("binding-modal") as HTMLElement;
  modal.style.display = "flex";
  (document.getElementById("modal-title") as HTMLElement).textContent = edit ? "Edit Input Binding" : "New Input Binding";
  (document.getElementById("f-id") as HTMLInputElement).value = edit?.id ? String(edit.id) : "";
  (document.getElementById("f-device") as HTMLInputElement).value = edit?.device_id ? String(edit.device_id) : "";
  (document.getElementById("f-source") as HTMLSelectElement).value = edit?.source || "";
  (document.getElementById("f-event") as HTMLSelectElement).value = edit?.event || "";
  (document.getElementById("f-match") as HTMLInputElement).value = edit?.match || "";
  (document.getElementById("f-action") as HTMLTextAreaElement).value = edit?.action || "";
  (document.getElementById("f-order") as HTMLInputElement).value =
    edit?.order !== undefined && edit?.order !== null ? String(edit.order) : "0";
  (document.getElementById("f-enabled") as HTMLInputElement).checked = edit ? !!edit.enabled : true;
}

function hideModal(): void {
  (document.getElementById("binding-modal") as HTMLElement).style.display = "none";
}

document.addEventListener("DOMContentLoaded", () => {
  document.getElementById("btn-add")?.addEventListener("click", () => showModal());
  document.getElementById("modal-close")?.addEventListener("click", hideModal);

  const form = document.getElementById("binding-form") as HTMLFormElement;
  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const id = (document.getElementById("f-id") as HTMLInputElement).value;
    const deviceRaw = (document.getElementById("f-device") as HTMLInputElement).value.trim();
    const body = {
      device_id: deviceRaw === "" ? null : parseInt(deviceRaw, 10),
      source: (document.getElementById("f-source") as HTMLSelectElement).value,
      event: (document.getElementById("f-event") as HTMLSelectElement).value,
      match: (document.getElementById("f-match") as HTMLInputElement).value.trim(),
      action: (document.getElementById("f-action") as HTMLTextAreaElement).value.trim(),
      order: parseInt((document.getElementById("f-order") as HTMLInputElement).value, 10) || 0,
      enabled: (document.getElementById("f-enabled") as HTMLInputElement).checked,
    };
    try {
      if (id) {
        await fetchJSON(`/admin/api/input-bindings/${id}`, { method: "PUT", body: JSON.stringify(body) });
      } else {
        await fetchJSON("/admin/api/input-bindings", { method: "POST", body: JSON.stringify(body) });
      }
      location.reload();
    } catch (err) {
      alert((err as Error).message);
    }
  });

  let cache: any[] = [];
  async function loadCache(): Promise<void> {
    try {
      cache = await fetchJSON("/admin/api/input-bindings");
    } catch {
      cache = [];
    }
  }
  loadCache();

  document.getElementById("input-bindings-body")?.addEventListener("click", async (ev) => {
    const target = ev.target as HTMLElement;
    const row = target.closest("tr") as HTMLTableRowElement | null;
    if (!row) return;
    const id = Number(row.dataset.id);
    if (target.classList.contains("btn-delete")) {
      if (!confirm("Delete this binding?")) return;
      await fetchJSON(`/admin/api/input-bindings/${id}`, { method: "DELETE" });
      location.reload();
    } else if (target.classList.contains("btn-toggle")) {
      await fetchJSON(`/admin/api/input-bindings/${id}/toggle`, { method: "POST" });
      location.reload();
    } else if (target.classList.contains("btn-edit")) {
      if (!cache.length) await loadCache();
      const found = cache.find((b) => String(b.id) === String(id));
      showModal(found || { id });
    }
  });
});
