const provider = document.getElementById("provider") as HTMLSelectElement | null;
const url = document.getElementById("url") as HTMLInputElement | null;
const form = document.getElementById("nowplaying-form") as HTMLFormElement | null;
const errorBox = document.getElementById("nowplaying-error") as HTMLElement | null;

function requiresURL(): boolean {
  return provider?.value === "plex" || provider?.value === "jellyfin";
}

function updateURLRequirement(): void {
  if (url) url.required = requiresURL();
}

provider?.addEventListener("change", updateURLRequirement);
updateURLRequirement();

form?.addEventListener("submit", (event) => {
  if (!requiresURL() || !url || url.value.trim() !== "") return;
  event.preventDefault();
  if (errorBox) {
    errorBox.textContent = "Server URL is required for Plex and Jellyfin.";
    errorBox.classList.remove("d-none");
  }
});
