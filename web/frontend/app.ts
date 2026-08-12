import './styles.css';

const shell = document.querySelector<HTMLElement>('[data-app-shell]');
const toggle = document.querySelector<HTMLButtonElement>('[data-nav-toggle]');
const nav = document.querySelector<HTMLElement>('[data-nav]');

toggle?.addEventListener('click', () => {
  const open = shell?.classList.toggle('nav-open') ?? false;
  toggle.setAttribute('aria-expanded', String(open));
  nav?.setAttribute('aria-hidden', String(!open));
});

document.querySelectorAll<HTMLElement>('[data-dismiss]').forEach((element) => {
  element.addEventListener('click', () => element.closest('[role="alert"]')?.remove());
});
