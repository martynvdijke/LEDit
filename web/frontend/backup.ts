const includeSecrets = document.getElementById('include-secrets') as HTMLInputElement;
const includeMedia = document.getElementById('include-media') as HTMLInputElement;
const secretsWarning = document.getElementById('secrets-warning') as HTMLElement;
const downloadBtn = document.getElementById('download-btn') as HTMLButtonElement;
const fileInput = document.getElementById('backup-file') as HTMLInputElement;
const previewBtn = document.getElementById('preview-btn') as HTMLButtonElement;
const confirmBtn = document.getElementById('confirm-btn') as HTMLButtonElement;
const diffTable = document.getElementById('diff-table') as HTMLElement;
const importResult = document.getElementById('import-result') as HTMLElement;

includeSecrets?.addEventListener('change', ()=> { secretsWarning.style.display = includeSecrets.checked ? 'block':'none'; });
downloadBtn?.addEventListener('click', async ()=>{
  const params = new URLSearchParams();
  if (includeSecrets.checked) params.set('include_secrets','true');
  if (includeMedia.checked) params.set('include_media','true');
  const res = await fetch(`/admin/api/backup/export?${params.toString()}`,{credentials:'same-origin'});
  if (!res.ok) { alert('export failed'); return; }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a=document.createElement('a');
  const cd=res.headers.get('Content-Disposition')||'';
  const m=cd.match(/filename="?([^"]+)"?/);
  a.download=m?m[1]:'backup.json';
  a.href=url; a.click(); URL.revokeObjectURL(url);
});

async function doImport(dryRun:boolean){
  const file=fileInput.files?.[0];
  if(!file){ alert('select file'); return; }
  const fd=new FormData();
  fd.append('file',file);
  // also send raw body fallback
  const buf=await file.arrayBuffer();
  const isZip=file.name.endsWith('.zip');
  const params=new URLSearchParams();
  if(dryRun) params.set('dry_run','true');
  const res=await fetch(`/admin/api/backup/import?${params.toString()}`,{method:'POST', body: isZip? buf : new Blob([buf],{type:'application/json'}), credentials:'same-origin', headers: isZip? {'Content-Type':'application/zip'}:{'Content-Type':'application/json'}});
  const data=await res.json();
  if(dryRun){
    renderDiff(data);
    confirmBtn.disabled=false;
  } else {
    importResult.textContent=JSON.stringify(data,null,2);
  }
}
function renderDiff(data:any){
  const diff=data.diff||data;
  let html='<table><tr><th>type</th><th>create</th><th>update</th><th>skip</th><th>conflict</th></tr>';
  const perType=diff.per_type||diff.PerType||{};
  for(const [k,v] of Object.entries(perType as any)){
    html+=`<tr><td>${k}</td><td>${(v as any).create}</td><td>${(v as any).update}</td><td>${(v as any).skip}</td><td>${(v as any).conflict}</td></tr>`;
  }
  html+='</table>';
  if(diff.dangling_refs?.length) html+=`<p>Dangling: ${diff.dangling_refs.join(', ')}</p>`;
  diffTable.innerHTML=html;
}
previewBtn?.addEventListener('click', ()=> doImport(true));
confirmBtn?.addEventListener('click', ()=> doImport(false));
