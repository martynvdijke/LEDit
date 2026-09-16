const TOKEN_KEY = "ledit_frame_secret";
const MAX_SIZE = 5 * 1024 * 1024;

function readSecret(): string {
  const hash = location.hash.replace(/^#/, "");
  if (hash) {
    sessionStorage.setItem(TOKEN_KEY, hash);
    history.replaceState(null, "", location.pathname);
    return hash;
  }
  return sessionStorage.getItem(TOKEN_KEY) ?? "";
}

let secret = readSecret();

const fileInput = document.querySelector("[data-frame-file]") as HTMLInputElement | null;
const preview = document.querySelector("[data-frame-preview]") as HTMLImageElement | null;
const uploadBtn = document.querySelector("[data-frame-upload]") as HTMLButtonElement | null;
const statusEl = document.querySelector("[data-frame-status]") as HTMLParagraphElement | null;

let selectedFile: File | null = null;
let inFlight = false;
let previewUrl: string | null = null;

function setStatus(text: string, kind: "error" | "ok" | "" = "") {
  if (!statusEl) return;
  statusEl.textContent = text;
  statusEl.className = kind ? kind : "";
}

function updateUploadEnabled() {
  if (!uploadBtn) return;
  uploadBtn.disabled = !selectedFile || !secret || inFlight;
}

if (!secret) {
  setStatus("This link is missing its access code.", "error");
}

fileInput?.addEventListener("change", () => {
  const file = fileInput.files?.[0] ?? null;
  if (!file) {
    selectedFile = null;
    if (previewUrl) URL.revokeObjectURL(previewUrl);
    previewUrl = null;
    if (preview) preview.removeAttribute("src");
    setStatus("");
    updateUploadEnabled();
    return;
  }
  if (!file.type.startsWith("image/")) {
    selectedFile = null;
    setStatus("Please select an image file.", "error");
    updateUploadEnabled();
    return;
  }
  if (file.size > MAX_SIZE) {
    selectedFile = null;
    setStatus("Image must be 5 MB or smaller.", "error");
    updateUploadEnabled();
    return;
  }
  selectedFile = file;
  if (previewUrl) URL.revokeObjectURL(previewUrl);
  previewUrl = URL.createObjectURL(file);
  if (preview) preview.src = previewUrl;
  setStatus("");
  updateUploadEnabled();
});

uploadBtn?.addEventListener("click", async () => {
  if (!selectedFile || !secret || inFlight) return;
  inFlight = true;
  updateUploadEnabled();
  setStatus("Uploading…");
  try {
    const form = new FormData();
    form.append("photo", selectedFile);
    const res = await fetch("/api/guest/photo", {
      method: "POST",
      headers: { "X-Guest-Token": secret },
      body: form,
    });
    if (res.status === 202) {
      setStatus("Submitted for approval", "ok");
      return;
    }
    if (res.status === 400) {
      setStatus("Invalid image. Try a different file.", "error");
      return;
    }
    if (res.status === 401) {
      setStatus("This link is invalid or expired.", "error");
      return;
    }
    if (res.status === 403) {
      setStatus("This link does not allow photo uploads.", "error");
      return;
    }
    if (res.status === 429) {
      setStatus("Too many uploads. Please wait and try again.", "error");
      return;
    }
    setStatus("Upload failed. Please try again.", "error");
  } catch {
    setStatus("Upload failed. Please try again.", "error");
  } finally {
    inFlight = false;
    updateUploadEnabled();
  }
});

updateUploadEnabled();
