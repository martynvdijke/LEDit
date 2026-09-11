// Guest remote client. The secret arrives in the URL fragment, is immediately
// cleared from the address bar, and is retained only in sessionStorage. Every
// API call sends it as the X-Guest-Token header; no cookies are ever sent.

const TOKEN_KEY = 'ledit_guest_token';
type Scope = 'pause' | 'next' | 'message';

interface GuestStatus {
  paused: boolean;
  scopes: Scope[];
  expires_at: string | null;
}

function readToken(): string {
  const hash = window.location.hash.replace(/^#/, '');
  if (hash) {
    sessionStorage.setItem(TOKEN_KEY, hash);
    history.replaceState(null, '', window.location.pathname + window.location.search);
    return hash;
  }
  return sessionStorage.getItem(TOKEN_KEY) ?? '';
}

const token = readToken();

const pausedEl = document.getElementById('paused-state') as HTMLParagraphElement | null;
const noticeEl = document.getElementById('notice') as HTMLParagraphElement | null;
const btnPause = document.getElementById('btn-pause') as HTMLButtonElement | null;
const btnResume = document.getElementById('btn-resume') as HTMLButtonElement | null;
const btnNext = document.getElementById('btn-next') as HTMLButtonElement | null;
const messageForm = document.getElementById('message-form') as HTMLFormElement | null;
const messageInput = document.getElementById('message-text') as HTMLInputElement | null;
const btnMessage = document.getElementById('btn-message') as HTMLButtonElement | null;

function setNotice(text: string, kind: 'ok' | 'error' | '') {
  if (!noticeEl) return;
  noticeEl.textContent = text;
  noticeEl.className = 'notice' + (kind ? ' ' + kind : '');
}

function disableAll() {
  [btnPause, btnResume, btnNext, btnMessage].forEach((b) => {
    if (b) b.disabled = true;
  });
  if (messageInput) messageInput.disabled = true;
}

async function api(path: string, method = 'GET', body?: unknown): Promise<Response> {
  return fetch(path, {
    method,
    headers: {
      'X-Guest-Token': token,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    // Never send cookies: guest auth is header-only and must not carry a session.
    credentials: 'omit',
  });
}

function applyStatus(status: GuestStatus) {
  if (pausedEl) {
    pausedEl.textContent = status.paused ? 'Paused' : 'Playing';
  }
  const canPause = status.scopes.includes('pause');
  const canNext = status.scopes.includes('next');
  const canMessage = status.scopes.includes('message');
  if (btnPause) btnPause.disabled = !canPause;
  if (btnResume) btnResume.disabled = !canPause;
  if (btnNext) btnNext.disabled = !canNext;
  if (btnMessage) btnMessage.disabled = !canMessage;
  if (messageInput) messageInput.disabled = !canMessage;
}

function handleStatusError(code: number) {
  if (code === 401) {
    setNotice('This remote link is invalid, expired, or revoked.', 'error');
    if (pausedEl) pausedEl.textContent = 'Not authorised';
    disableAll();
  } else {
    setNotice('Unable to reach the wall.', 'error');
  }
}

async function refreshStatus() {
  if (!token) {
    setNotice('No access token. Open the share link again.', 'error');
    if (pausedEl) pausedEl.textContent = 'Not authorised';
    disableAll();
    return;
  }
  try {
    const res = await api('/api/guest/status');
    if (res.status === 401) {
      handleStatusError(401);
      return;
    }
    if (!res.ok) {
      setNotice('Unable to reach the wall.', 'error');
      return;
    }
    applyStatus((await res.json()) as GuestStatus);
  } catch {
    setNotice('Unable to reach the wall.', 'error');
  }
}

async function action(path: string) {
  setNotice('');
  try {
    const res = await api(path, 'POST');
    if (res.status === 401) {
      handleStatusError(401);
      return;
    }
    if (res.status === 429) {
      const retry = res.headers.get('Retry-After') ?? 'a few';
      setNotice(`Too many requests. Try again in ${retry}s.`, 'error');
      return;
    }
    if (!res.ok) {
      setNotice('Action failed.', 'error');
      return;
    }
    setNotice('Done.', 'ok');
    void refreshStatus();
  } catch {
    setNotice('Action failed.', 'error');
  }
}

btnPause?.addEventListener('click', () => void action('/api/guest/pause'));
btnResume?.addEventListener('click', () => void action('/api/guest/resume'));
btnNext?.addEventListener('click', () => void action('/api/guest/next'));

messageForm?.addEventListener('submit', async (event) => {
  event.preventDefault();
  setNotice('');
  const text = messageInput?.value.trim() ?? '';
  if (!text) {
    setNotice('Type a message first.', 'error');
    return;
  }
  try {
    const res = await api('/api/guest/message', 'POST', { text });
    if (res.status === 401) {
      handleStatusError(401);
      return;
    }
    if (res.status === 429) {
      const retry = res.headers.get('Retry-After') ?? 'a few';
      setNotice(`Too many messages. Try again in ${retry}s.`, 'error');
      return;
    }
    if (res.status === 400) {
      setNotice('Message must be 1-140 characters.', 'error');
      return;
    }
    if (res.status === 202) {
      if (messageInput) messageInput.value = '';
      setNotice('Message sent to the wall.', 'ok');
      return;
    }
    setNotice('Message failed.', 'error');
  } catch {
    setNotice('Message failed.', 'error');
  }
});

void refreshStatus();
setInterval(() => {
  if (!document.hidden) void refreshStatus();
}, 5000);
