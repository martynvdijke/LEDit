// Embed surface client. Renders the shared wall (or a single device panel for
// token links) into a chrome-free page suitable for OBS browser sources and
// iframes. The server pushes frames over /ws/embed; this script only decodes
// and draws them — no polling, no requestAnimationFrame loop, no re-encoding.
//
// Device links carry the guest secret in the URL fragment. It is exchanged once
// at POST /embed/session for a short-lived HttpOnly cookie and then removed from
// the address bar, so the secret never lands in request lines or logs.

const DEVICE_TARGET = document.body.dataset.embedTarget === "device";
const deviceID = document.body.dataset.deviceId ?? "0";
const SECRET_KEY = "ledit_embed_secret";
const MAX_ATTEMPTS = 5;

const params = new URLSearchParams(window.location.search);
const queryWidth = params.get("w") ?? "";
const queryHeight = params.get("h") ?? "";
const background = params.get("bg") ?? "";

const canvas = document.getElementById("embed-canvas") as HTMLCanvasElement | null;
const ctx = canvas ? canvas.getContext("2d") : null;
const video = document.getElementById("embed-video") as HTMLVideoElement | null;

// Presentation options come from the query string so the same page works in an
// iframe, an OBS source or a plain browser. bg=transparent (the default) lets
// the page composite over whatever is behind it.
function applyBackground() {
  let value = "transparent";
  if (background && background !== "transparent") {
    const hex = background.startsWith("#") ? background : `#${background}`;
    if (/^#[0-9a-fA-F]{6}$/.test(hex)) value = hex;
    else value = "transparent";
  }
  document.documentElement.style.background = value;
  document.body.style.background = value;
}

function setState(state: "connecting" | "live" | "video" | "error") {
  document.body.dataset.state = state;
}

function readSecret(): string {
  const hash = window.location.hash.replace(/^#/, "");
  if (hash) {
    sessionStorage.setItem(SECRET_KEY, hash);
    // Strip the fragment immediately: the secret stays in memory, not the URL.
    window.history.replaceState(null, "", window.location.pathname + window.location.search);
    return hash;
  }
  return sessionStorage.getItem(SECRET_KEY) ?? "";
}

// ensureSession exchanges the fragment secret for the embed cookie. Returns
// false only for an auth failure, which the caller renders as the static
// "invalid or expired" state instead of retrying forever.
async function ensureSession(): Promise<boolean> {
  if (!DEVICE_TARGET) return true;
  const secret = readSecret();
  if (!secret) return false;
  try {
    const res = await fetch("/embed/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: secret }),
    });
    if (res.ok) return true;
    if (res.status === 401 || res.status === 403) return false;
    // Transient failure (5xx/429): allow the socket attempt to retry.
    return true;
  } catch {
    return true;
  }
}

function socketURL(): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const path = DEVICE_TARGET
    ? `/ws/embed/device/${encodeURIComponent(deviceID)}`
    : "/ws/embed";
  const query = new URLSearchParams();
  if (queryWidth) query.set("w", queryWidth);
  if (queryHeight) query.set("h", queryHeight);
  const qs = query.toString();
  return `${protocol}//${window.location.host}${path}${qs ? `?${qs}` : ""}`;
}

interface EmbedFrame {
  format?: string;
  image?: string;
}

function drawPNG(dataURI: string) {
  if (!ctx) return;
  const image = new Image();
  image.onload = () => {
    if (!canvas) return;
    canvas.width = image.naturalWidth;
    canvas.height = image.naturalHeight;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(image, 0, 0);
    setState("live");
  };
  image.src = dataURI;
}

function playVideo(dataURI: string) {
  if (!video) return;
  video.src = dataURI;
  void video.play().catch(() => {});
  setState("video");
}

function renderFrame(frame: EmbedFrame) {
  if (!frame.format || !frame.image) return;
  const dataURI =
    frame.format === "MP4"
      ? `data:video/mp4;base64,${frame.image}`
      : `data:image/${frame.format.toLowerCase()};base64,${frame.image}`;
  if (frame.format === "MP4") {
    playVideo(dataURI);
    return;
  }
  drawPNG(dataURI);
}

let socket: WebSocket | null = null;
let attempts = 0;

function connect() {
  setState("connecting");
  socket = new WebSocket(socketURL());
  socket.onopen = () => {
    attempts = 0;
  };
  socket.onmessage = (event) => {
    try {
      renderFrame(JSON.parse(event.data) as EmbedFrame);
    } catch {
      /* ignore malformed frames */
    }
  };
  socket.onclose = () => {
    // Keep whatever frame is on screen; only the badge changes.
    if (attempts >= MAX_ATTEMPTS) {
      setState("error");
      return;
    }
    const delay = Math.min(1000 * 2 ** attempts, 30000);
    attempts += 1;
    setState("connecting");
    window.setTimeout(connect, delay);
  };
}

async function start() {
  applyBackground();
  const authorised = await ensureSession();
  if (!authorised) {
    setState("error");
    return;
  }
  connect();
}

void start();
