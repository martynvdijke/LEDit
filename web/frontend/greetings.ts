async function fetchJSON(url: string, opts?: RequestInit) {
  const r = await fetch(url, { ...opts, headers: { "Content-Type": "application/json", ...(opts?.headers || {}) } });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
function showModal(edit: any = null) {
  const modal = document.getElementById("greeting-modal") as HTMLElement;
  modal.style.display = "flex";
  (document.getElementById("f-id") as HTMLInputElement).value = edit ? String(edit.id) : "";
  (document.getElementById("f-name") as HTMLInputElement).value = edit ? edit.name : "";
  (document.getElementById("f-entity") as HTMLInputElement).value = edit ? edit.entity_path : "";
  (document.getElementById("f-match") as HTMLInputElement).value = edit ? edit.match_value : "home";
  (document.getElementById("f-operator") as HTMLSelectElement).value = edit ? edit.match_operator : "eq";
  (document.getElementById("f-template") as HTMLInputElement).value = edit ? edit.message_template : "";
  (document.getElementById("f-ttl") as HTMLInputElement).value = edit ? String(edit.ttl_seconds) : "30";
  (document.getElementById("f-cooldown") as HTMLInputElement).value = edit ? String(edit.cooldown_minutes) : "30";
  (document.getElementById("f-qs") as HTMLInputElement).value = edit ? edit.quiet_hours_start || "" : "";
  (document.getElementById("f-qe") as HTMLInputElement).value = edit ? edit.quiet_hours_end || "" : "";
  (document.getElementById("f-enabled") as HTMLInputElement).checked = edit ? edit.enabled : true;
  (document.getElementById("modal-title") as HTMLElement).textContent = edit ? "Edit Greeting" : "New Greeting";
}
function hideModal() { (document.getElementById("greeting-modal") as HTMLElement).style.display = "none"; }

document.getElementById("btn-add")?.addEventListener("click", () => showModal());
document.getElementById("btn-add-person")?.addEventListener("click", () => showModal({ name: "Maria", entity_path: "person.maria", match_value: "home", match_operator: "eq", message_template: "Welcome home, {name}!", ttl_seconds: 30, cooldown_minutes: 30, enabled: true }));
document.getElementById("btn-add-room")?.addEventListener("click", () => showModal({ name: "Meeting Room", entity_path: "binary_sensor.room_occupied", match_value: "on", match_operator: "eq", message_template: "Room busy until {until}", ttl_seconds: 60, cooldown_minutes: 5, enabled: true }));
document.getElementById("modal-close")?.addEventListener("click", hideModal);

document.getElementById("greeting-form")?.addEventListener("submit", async (e) => {
  e.preventDefault();
  const id = (document.getElementById("f-id") as HTMLInputElement).value;
  const body: any = {
    name: (document.getElementById("f-name") as HTMLInputElement).value,
    entity_path: (document.getElementById("f-entity") as HTMLInputElement).value,
    match_value: (document.getElementById("f-match") as HTMLInputElement).value,
    match_operator: (document.getElementById("f-operator") as HTMLSelectElement).value,
    message_template: (document.getElementById("f-template") as HTMLInputElement).value,
    ttl_seconds: parseInt((document.getElementById("f-ttl") as HTMLInputElement).value, 10),
    cooldown_minutes: parseInt((document.getElementById("f-cooldown") as HTMLInputElement).value, 10),
    enabled: (document.getElementById("f-enabled") as HTMLInputElement).checked,
  };
  const qs = (document.getElementById("f-qs") as HTMLInputElement).value.trim();
  const qe = (document.getElementById("f-qe") as HTMLInputElement).value.trim();
  if (qs) body.quiet_hours_start = qs;
  if (qe) body.quiet_hours_end = qe;
  try {
    if (id) await fetchJSON(`/admin/api/greetings/${id}`, { method: "PUT", body: JSON.stringify(body) });
    else await fetchJSON(`/admin/api/greetings`, { method: "POST", body: JSON.stringify(body) });
    location.reload();
  } catch (err: any) { alert(err.message); }
});

// delegate table actions
let cache: any[] = [];
async function loadCache() { try { cache = await fetchJSON("/admin/api/greetings"); } catch {} }
loadCache();
document.getElementById("greetings-body")?.addEventListener("click", async (e) => {
  const target = e.target as HTMLElement;
  const row = target.closest("tr") as HTMLElement;
  if (!row) return;
  const id = row.dataset.id;
  if (target.classList.contains("btn-delete")) {
    if (!confirm("Delete?")) return;
    await fetch(`/admin/api/greetings/${id}`, { method: "DELETE" });
    location.reload();
  } else if (target.classList.contains("btn-test")) {
    const res = await fetch(`/admin/api/greetings/${id}/test`, { method: "POST" });
    alert(res.ok ? "Test sent" : await res.text());
  } else if (target.classList.contains("btn-edit")) {
    if (cache.length === 0) await loadCache();
    const found = cache.find((x) => String(x.id) === id);
    // fallback to row data if cache miss
    showModal(found || { id, name: row.children[0].textContent });
  }
});
