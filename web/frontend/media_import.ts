// media_import.ts - shared import modal for gallery and editor
function setup(prefix: string, previewEndpoint: string, importEndpoint: string) {
  const btn = document.getElementById(prefix + "-btn") || document.getElementById("gallery-import-btn") || document.getElementById("media-import-btn");
  const modal = document.getElementById(prefix + "-modal") || document.getElementById("gallery-import-modal") || document.getElementById("media-import-modal");
  const fileEl = document.getElementById(prefix + "-file") as HTMLInputElement | null;
  const wEl = document.getElementById(prefix + "-w") as HTMLInputElement | null;
  const hEl = document.getElementById(prefix + "-h") as HTMLInputElement | null;
  const aspectEl = document.getElementById(prefix + "-aspect") as HTMLSelectElement | null;
  const colorsEl = document.getElementById(prefix + "-colors") as HTMLInputElement | null;
  const paletteEl = document.getElementById(prefix + "-palette") as HTMLInputElement | null;
  const previewBtn = document.getElementById(prefix + "-preview-btn");
  const submitBtn = document.getElementById(prefix + "-submit");
  const cancelBtn = document.getElementById(prefix + "-cancel");
  const previewImg = document.getElementById(prefix + "-preview") as HTMLImageElement | null;
  const previewWrap = document.getElementById(prefix + "-preview-wrap");
  const errEl = document.getElementById(prefix + "-error");

  if (!btn || !modal) return;

  function open() { modal!.style.display = "flex"; }
  function close() { modal!.style.display = "none"; if (errEl) errEl.textContent=""; if(previewWrap) previewWrap.style.display="none"; }
  btn.addEventListener("click", open);
  cancelBtn?.addEventListener("click", close);
  modal.addEventListener("click", (e)=>{ if(e.target===modal) close(); });

  // palette mode toggle
  const radios = document.querySelectorAll<HTMLInputElement>(`input[name="${prefix}-palette-mode"], input[name="gallery-palette-mode"], input[name="media-palette-mode"]`);
  radios.forEach(r=>{
    r.addEventListener("change", ()=>{
      const v = (document.querySelector('input[name="'+r.name+'"]:checked') as HTMLInputElement)?.value;
      if(paletteEl) paletteEl.style.display = v==="custom" ? "block":"none";
      if(colorsEl) colorsEl.style.display = v==="auto" ? "block":"none";
    });
  });

  fileEl?.addEventListener("change", ()=>{
    const f = fileEl.files?.[0];
    if(f && f.size > 5*1024*1024) { if(errEl) errEl.textContent="File too large (max 5MB)"; if(previewWrap) previewWrap.style.display="block"; }
  });

  async function doPreview(){
    const f = fileEl?.files?.[0];
    if(!f){ if(errEl) errEl.textContent="Select a file"; if(previewWrap) previewWrap.style.display="block"; return; }
    if(f.size>5*1024*1024){ if(errEl) errEl.textContent="File too large"; return; }
    const fd = new FormData();
    fd.set("file", f);
    fd.set("target_width", wEl?.value ?? "32");
    fd.set("target_height", hEl?.value ?? "32");
    fd.set("aspect", aspectEl?.value ?? "fit");
    const mode = (document.querySelector('input[name="'+(radios[0]?.name ?? 'media-palette-mode')+'"]:checked') as HTMLInputElement)?.value ?? "auto";
    if(mode==="auto"){ fd.set("auto_palette","true"); fd.set("auto_palette_colors", colorsEl?.value ?? "16"); }
    else { fd.set("palette_json", paletteEl?.value ?? "[]"); }
    if(errEl) errEl.textContent="Loading preview…";
    if(previewWrap) previewWrap.style.display="block";
    try{
      const res = await fetch(previewEndpoint, {method:"POST", body: fd});
      const j = await res.json();
      if(!res.ok){ if(errEl) errEl.textContent=j.error||"Preview failed"; return; }
      if(previewImg && j.png_b64){ previewImg.src="data:image/png;base64,"+j.png_b64; if(errEl) errEl.textContent=""; }
    }catch(e){ if(errEl) errEl.textContent=String(e); }
  }
  async function doImport(){
    const f = fileEl?.files?.[0];
    if(!f){ if(errEl) errEl.textContent="Select a file"; return; }
    const fd = new FormData();
    fd.set("file", f);
    fd.set("target_width", wEl?.value ?? "32");
    fd.set("target_height", hEl?.value ?? "32");
    fd.set("aspect", aspectEl?.value ?? "fit");
    const mode = (document.querySelector('input[name="'+(radios[0]?.name ?? 'media-palette-mode')+'"]:checked') as HTMLInputElement)?.value ?? "auto";
    if(mode==="auto"){ fd.set("auto_palette","true"); fd.set("auto_palette_colors", colorsEl?.value ?? "16"); }
    else { fd.set("palette_json", paletteEl?.value ?? "[]"); }
    if(errEl) errEl.textContent="Importing…";
    try{
      const res = await fetch(importEndpoint, {method:"POST", body: fd});
      const j = await res.json();
      if(!res.ok){ if(errEl) errEl.textContent=j.error||"Import failed"; if(previewWrap) previewWrap.style.display="block"; return; }
      window.location.href="/admin/pixelarts/"+j.id+"/edit";
    }catch(e){ if(errEl) errEl.textContent=String(e); if(previewWrap) previewWrap.style.display="block"; }
  }
  previewBtn?.addEventListener("click", ()=>{ void doPreview(); });
  submitBtn?.addEventListener("click", ()=>{ void doImport(); });
}

function init(){
  // gallery
  setup("gallery-import", "/admin/pixelarts/import/preview", "/admin/pixelarts/import");
  // editor
  setup("media-import", "/admin/pixelarts/import/preview", "/admin/pixelarts/import");
}
if(document.readyState==="loading") document.addEventListener("DOMContentLoaded", init); else init();
