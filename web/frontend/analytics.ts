async function loadWeights(){
  const el=document.getElementById('weights-body');
  const sub=document.getElementById('weights-subtitle');
  const notice=document.getElementById('collecting-notice');
  if(!el) return;
  try{
    const r=await fetch('/api/analytics/weights');
    if(!r.ok){ el.innerHTML='<tr><td colspan="6">unauthorized</td></tr>'; return; }
    const j=await r.json();
    const w=j.weights||{}; const d=j.displays||{}; const s=j.skips||{};
    const cfg=j.config||{window_days:14,half_life_days:7,floor:0.05,epsilon:0.15};
    if(sub) sub.textContent=`Window ${cfg.window_days}d · half-life ${cfg.half_life_days}d · floor ${Math.round(cfg.floor*100)}% · ε ${cfg.epsilon} · updated just now`;
    if(j.collectingData && notice) (notice as HTMLElement).style.display='block';
    const rows=Object.keys(w).map(k=>{
      const disp=d[k]||0; const sk=s[k]||0; const rate=disp? (sk/disp).toFixed(2):'0.00'; const weight=(w[k] as number).toFixed(3);
      const bar=`<div style="background:#0d6efd;height:12px;width:${(w[k]*100).toFixed(0)}%"></div>`;
      return `<tr><td>${k}</td><td>${disp}</td><td>${sk}</td><td>${rate}</td><td>${weight}</td><td style="width:120px">${bar}</td></tr>`;
    }).join('') || '<tr><td colspan="6">no data</td></tr>';
    el.innerHTML=rows;
  }catch(e){ el.innerHTML='<tr><td colspan="6">error</td></tr>';}
}
loadWeights();
