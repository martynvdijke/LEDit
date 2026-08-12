import './styles.css';

const imgEl = document.getElementById('media-display') as HTMLImageElement | null;
const videoEl = document.getElementById('video-display') as HTMLVideoElement | null;
const statusEl = document.getElementById('status-text');
const sourceEl = document.getElementById('source-label');
const nextEl = document.getElementById('next-label');
const canvas = document.getElementById('matrix-overlay') as HTMLCanvasElement | null;
const clockEl = document.getElementById('clock-overlay');
const marqueeEl = document.getElementById('marquee-text');
const btnRefresh = document.getElementById('btn-refresh') as HTMLButtonElement | null;
const feedPage = document.querySelector<HTMLElement>('[data-feed-page]');
const isEink = document.body.classList.contains('eink-mode');
let ws: WebSocket | null = null;
let reconnectAttempts = 0;
let paused = false;
let fullscreen = false;
let clockInterval: number | undefined;
let autoRefreshInterval: number | undefined;

function setStatus(text: string): void {
  if (!statusEl) return;
  statusEl.textContent = text;
  statusEl.dataset.state = text.toLowerCase().includes('connect') ? 'connecting' :
    text.toLowerCase().includes('receiv') ? 'receiving' :
    text.toLowerCase().includes('disconnect') || text.toLowerCase().includes('fail') ? 'error' : 'idle';
}

function updateClock(): void {
  if (!clockEl) return;
  const now = new Date();
  clockEl.textContent = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`;
}

function drawMatrix(displayWidth: number): void {
  if (!canvas || isEink) return;
  const size = Math.max(160, Math.min(displayWidth, 720));
  canvas.width = size; canvas.height = size;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;
  const cell = size / 64;
  ctx.strokeStyle = 'rgba(132, 255, 93, .18)'; ctx.lineWidth = .5;
  for (let i = 0; i <= 64; i++) {
    ctx.beginPath(); ctx.moveTo(i * cell, 0); ctx.lineTo(i * cell, size); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(0, i * cell); ctx.lineTo(size, i * cell); ctx.stroke();
  }
}

function showImage(format: string, image: string): void {
  if (!imgEl || !videoEl) return;
  videoEl.pause(); videoEl.style.display = 'none';
  imgEl.classList.add('fade-out'); imgEl.style.display = 'block';
  imgEl.src = `data:image/${format.toLowerCase()};base64,${image}`;
  imgEl.onload = () => { imgEl.classList.remove('fade-out'); drawMatrix(imgEl.clientWidth || 400); };
}

function connect(): void {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${protocol}//${window.location.host}/ws/feed`);
  setStatus('Connecting');
  ws.onopen = () => { reconnectAttempts = 0; setStatus('Connected'); };
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data) as { source?: string; next?: string; format?: string; image?: string };
    if (msg.source) { sourceEl && (sourceEl.textContent = msg.source); marqueeEl && (marqueeEl.textContent = ` ${msg.source}  /  LEDit LIVE FEED `); }
    if (msg.next) nextEl && (nextEl.textContent = `NEXT  ${msg.next}`);
    if (msg.format && msg.image) {
      if (msg.format === 'MP4' && videoEl && imgEl) {
        imgEl.style.display = 'none'; videoEl.style.display = 'block'; videoEl.src = `data:video/mp4;base64,${msg.image}`; void videoEl.play(); drawMatrix(videoEl.clientWidth || 400);
      } else showImage(msg.format, msg.image);
    }
    setStatus('Receiving');
  };
  ws.onclose = () => {
    setStatus('Disconnected'); sourceEl && (sourceEl.textContent = '--');
    if (reconnectAttempts < 5) {
      reconnectAttempts++; const delay = Math.min(1000 * 2 ** reconnectAttempts, 30000);
      setStatus(`Reconnecting in ${delay / 1000}s`); window.setTimeout(connect, delay);
    } else setStatus('Connection failed');
  };
  ws.onerror = () => ws?.close();
}

function send(action: 'pause' | 'resume' | 'next'): void { if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ action })); }
document.getElementById('btn-pause')?.addEventListener('click', () => { paused = !paused; document.getElementById('btn-pause')!.textContent = paused ? 'Resume' : 'Pause'; send(paused ? 'pause' : 'resume'); });
document.getElementById('btn-skip')?.addEventListener('click', () => send('next'));
btnRefresh?.addEventListener('click', () => send('next'));
document.getElementById('btn-fullscreen')?.addEventListener('click', () => {
  fullscreen = !fullscreen; feedPage?.classList.toggle('fullscreen-active', fullscreen);
  document.querySelector<HTMLElement>('[data-app-shell]')?.classList.toggle('fullscreen-nav-hidden', fullscreen);
  const button = document.getElementById('btn-fullscreen'); if (button) { button.textContent = fullscreen ? 'Exit fullscreen' : 'Fullscreen'; button.setAttribute('aria-pressed', String(fullscreen)); }
});

if (isEink) {
  updateClock(); autoRefreshInterval = window.setInterval(() => send('next'), Number(document.body.dataset.einkRefresh || 30) * 1000);
  if (btnRefresh) btnRefresh.hidden = false;
} else { updateClock(); clockInterval = window.setInterval(updateClock, 1000); }

if (document.querySelector('[data-feed-page]')) connect();
window.addEventListener('beforeunload', () => { if (clockInterval) clearInterval(clockInterval); if (autoRefreshInterval) clearInterval(autoRefreshInterval); ws?.close(); });
