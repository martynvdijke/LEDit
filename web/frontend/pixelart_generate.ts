function q<T extends Element>(sel: string): T | null { return document.querySelector(sel) as T | null; }
function qid(id: string): HTMLElement | null { return document.getElementById(id); }

function initGenerate(): void {
  const form = qid("generate-panel") as HTMLElement | null;
  if (!form) return;
  const promptEl = qid("gen-prompt") as HTMLTextAreaElement | null;
  const widthEl = qid("gen-width") as HTMLInputElement | null;
  const heightEl = qid("gen-height") as HTMLInputElement | null;
  const paletteHintEl = qid("gen-palette-hint") as HTMLInputElement | null;
  const frameCountEl = qid("gen-frames") as HTMLSelectElement | null;
  const genBtn = qid("gen-btn") as HTMLButtonElement | null;
  const errorEl = qid("gen-error") as HTMLElement | null;
  const spinner = qid("gen-spinner") as HTMLElement | null;
  const refineInput = qid("gen-refine-prompt") as HTMLTextAreaElement | null;
  const refineBtn = qid("gen-refine-btn") as HTMLButtonElement | null;
  const publishBtn = qid("gen-publish-btn") as HTMLButtonElement | null;

  const aiConfigured = (form.dataset.aiConfigured === "true");

  function setError(msg: string): void { if (errorEl) { errorEl.textContent = msg; errorEl.style.display = msg ? "block" : "none"; } }
  function setLoading(v: boolean): void {
    if (spinner) spinner.style.display = v ? "inline-block" : "none";
    if (genBtn) genBtn.disabled = v || !aiConfigured;
    if (refineBtn) refineBtn.disabled = v || !aiConfigured;
  }

  if (!aiConfigured) {
    if (genBtn) { genBtn.disabled = true; genBtn.title = "AI not configured — set provider in Admin → AI"; }
    if (refineBtn) { refineBtn.disabled = true; }
    setError("AI not configured — set provider in Admin → AI");
  }

  genBtn?.addEventListener("click", async () => {
    const prompt = promptEl?.value.trim() ?? "";
    if (!prompt) { setError("Prompt is required"); return; }
    const width = parseInt(widthEl?.value ?? "32", 10);
    const height = parseInt(heightEl?.value ?? "32", 10);
    const frameCount = parseInt(frameCountEl?.value ?? "1", 10);
    const paletteHint = paletteHintEl?.value.trim() ?? "";
    setError("");
    setLoading(true);
    try {
      const res = await fetch("/api/pixelart/generate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt, width, height, palette_hint: paletteHint, frame_count: frameCount }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setError((data.error as string) || `Error ${res.status}` + (data.details ? `: ${data.details}` : ""));
        return;
      }
      const id = (data as { id: number }).id;
      if (id) window.location.href = `/admin/pixelarts/${id}/edit`;
    } catch (e) {
      setError(String(e));
    } finally { setLoading(false); }
  });

  refineBtn?.addEventListener("click", async () => {
    const draftId = form.dataset.draftId;
    if (!draftId) { setError("No draft to refine"); return; }
    const prompt = refineInput?.value.trim() ?? "";
    if (!prompt) { setError("Refine prompt required"); return; }
    setError("");
    setLoading(true);
    try {
      const res = await fetch(`/api/pixelart/${draftId}/refine`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setError((data.error as string) || `Error ${res.status}` + (data.details ? `: ${data.details}` : ""));
        return;
      }
      window.location.reload();
    } catch (e) { setError(String(e)); } finally { setLoading(false); }
  });

  publishBtn?.addEventListener("click", async () => {
    const draftId = form.dataset.draftId;
    if (!draftId) return;
    setError("");
    try {
      const res = await fetch(`/api/pixelart/${draftId}/publish`, { method: "POST" });
      if (!res.ok) { const d = await res.json().catch(() => ({})); setError((d.error as string) || `Error ${res.status}`); return; }
      window.location.href = "/admin/pixelarts";
    } catch (e) { setError(String(e)); }
  });
}

if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initGenerate);
else initGenerate();
