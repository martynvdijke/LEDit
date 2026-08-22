import type { Page } from '@playwright/test';

export interface FeedFrame {
  format?: string;
  image?: string;
  source?: string;
  next?: string;
  title?: string;
  message?: string;
  stale?: boolean;
  [k: string]: unknown;
}

export class WsFeed {
  private buffer: FeedFrame[] = [];
  private waiters: Array<{ resolve: (v: FeedFrame) => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }> = [];
  private jsHandle: unknown = null;

  constructor(private page: Page, private url: string) {}

  async connect() {
    // Create WS in browser context and buffer messages
    await this.page.evaluate((wsUrl: string) => {
      // @ts-ignore global
      (window as unknown as Record<string, unknown>).__pwWsBuffers = (window as unknown as Record<string, unknown>).__pwWsBuffers || {};
      const buffers = (window as unknown as Record<string, unknown>).__pwWsBuffers as Record<string, unknown>;
      if (buffers[wsUrl]) {
        try { ((buffers[wsUrl] as { ws: WebSocket }).ws).close(); } catch {}
      }
      const ws = new WebSocket(wsUrl);
      const buf: string[] = [];
      ws.addEventListener('message', (ev) => {
        buf.push(ev.data as string);
        // dispatch custom event for Node side polling
        window.dispatchEvent(new CustomEvent('__pwWsMessage', { detail: { url: wsUrl, data: ev.data } }));
      });
      buffers[wsUrl] = { ws, buf };
    }, this.absUrl());

    // Listen for custom events and push to buffer
    await this.page.exposeFunction('__pwWsPush', (data: { url: string; raw: string }) => {
      if (data.url !== this.absUrl()) return;
      try {
        const parsed = JSON.parse(data.raw) as FeedFrame;
        if (this.waiters.length) {
          const w = this.waiters.shift()!;
          clearTimeout(w.timer);
          w.resolve(parsed);
        } else {
          this.buffer.push(parsed);
        }
      } catch {
        // ignore non-json
      }
    }).catch(() => {});

    await this.page.evaluate((wsUrl: string) => {
      window.addEventListener('__pwWsMessage', (e: Event) => {
        const ce = e as CustomEvent<{ url: string; data: string }>;
        if (ce.detail.url === wsUrl) {
          // @ts-ignore
          (window as unknown as { __pwWsPush?: (d: unknown)=>void }).__pwWsPush?.({ url: ce.detail.url, raw: ce.detail.data });
        }
      });
    }, this.absUrl());

    // Also drain any buffered messages already received before listener
    const existing: string[] = await this.page.evaluate((wsUrl: string) => {
      const buffers = (window as unknown as Record<string, unknown>).__pwWsBuffers as Record<string, { ws: WebSocket; buf: string[] }> | undefined;
      const entry = buffers?.[wsUrl];
      if (!entry) return [];
      const out = [...entry.buf];
      entry.buf.length = 0;
      return out;
    }, this.absUrl());
    for (const raw of existing) {
      try {
        const parsed = JSON.parse(raw) as FeedFrame;
        this.buffer.push(parsed);
      } catch {}
    }
  }

  private absUrl(): string {
    if (this.url.startsWith('ws://') || this.url.startsWith('wss://')) return this.url;
    // page url base handled by evaluate WebSocket which resolves relative? But we passed absolute via page.evaluate: need to resolve via page.url
    return this.url;
  }

  async nextFrame(timeoutMs = 8000): Promise<FeedFrame> {
    if (this.buffer.length) return this.buffer.shift()!;
    return new Promise<FeedFrame>((resolve, reject) => {
      const timer = setTimeout(() => {
        const idx = this.waiters.findIndex((w) => w.resolve === resolve);
        if (idx >= 0) this.waiters.splice(idx, 1);
        reject(new Error(`WsFeed nextFrame timeout after ${timeoutMs}ms for ${this.url}`));
      }, timeoutMs);
      this.waiters.push({ resolve, reject, timer });
    });
  }

  /** Send a control message (e.g. {action:'pause'}) over the feed WebSocket. */
  async send(payload: Record<string, unknown>): Promise<void> {
    await this.page.evaluate(({ wsUrl, data }) => {
      const buffers = (window as unknown as Record<string, unknown>).__pwWsBuffers as
        | Record<string, { ws: WebSocket }>
        | undefined;
      const entry = buffers?.[wsUrl];
      if (!entry) throw new Error(`no open ws for ${wsUrl}`);
      entry.ws.send(JSON.stringify(data));
    }, { wsUrl: this.absUrl(), data: payload });
  }

  drain(): FeedFrame[] {
    const out = [...this.buffer];
    this.buffer.length = 0;
    return out;
  }

  async close() {
    await this.page.evaluate((wsUrl: string) => {
      const buffers = (window as unknown as Record<string, unknown>).__pwWsBuffers as Record<string, { ws: WebSocket; buf: string[] }> | undefined;
      const entry = buffers?.[wsUrl];
      if (entry) {
        try { entry.ws.close(); } catch {}
        delete buffers[wsUrl];
      }
    }, this.absUrl()).catch(() => {});
    for (const w of this.waiters) {
      clearTimeout(w.timer);
      w.reject(new Error('WsFeed closed'));
    }
    this.waiters.length = 0;
  }

  // Poll any existing buffered in browser that hasn't been dispatched
  async pollBrowserBuffer(): Promise<void> {
    const raws: string[] = await this.page.evaluate((wsUrl: string) => {
      const buffers = (window as unknown as Record<string, unknown>).__pwWsBuffers as Record<string, { ws: WebSocket; buf: string[] }> | undefined;
      const entry = buffers?.[wsUrl];
      if (!entry) return [];
      const out = [...entry.buf];
      entry.buf.length = 0;
      return out;
    }, this.absUrl());
    for (const raw of raws) {
      try {
        const parsed = JSON.parse(raw) as FeedFrame;
        if (this.waiters.length) {
          const w = this.waiters.shift()!;
          clearTimeout(w.timer);
          w.resolve(parsed);
        } else {
          this.buffer.push(parsed);
        }
      } catch {}
    }
  }
}

export async function createWsFeed(page: Page, url: string): Promise<WsFeed> {
  // Resolve relative url to ws://...
  let wsUrl = url;
  if (url.startsWith('/')) {
    const origin = new URL(page.url() || 'http://127.0.0.1:8080');
    const proto = origin.protocol === 'https:' ? 'wss:' : 'ws:';
    wsUrl = `${proto}//${origin.host}${url}`;
    // If page hasn't navigated yet, fallback to baseURL
    if (wsUrl.includes('127.0.0.1') === false && wsUrl.includes('localhost') === false) {
      wsUrl = `ws://127.0.0.1:8080${url}`;
    }
  }
  const feed = new WsFeed(page, wsUrl);
  await feed.connect();
  return feed;
}
