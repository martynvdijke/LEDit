import { resolve } from 'node:path';
import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    outDir: resolve(__dirname, 'web/static/assets'),
    emptyOutDir: true,
    rollupOptions: {
      input: {
        app: resolve(__dirname, 'web/frontend/app.ts'),
        feed: resolve(__dirname, 'web/frontend/feed.ts'),
        pixelart_editor: resolve(__dirname, 'web/frontend/pixelart_editor.ts'),
        media_import: resolve(__dirname, 'web/frontend/media_import.ts'),
        pixelart_generate: resolve(__dirname, 'web/frontend/pixelart_generate.ts'),
        timelapse: resolve(__dirname, 'web/frontend/timelapse.ts'),
        events: resolve(__dirname, 'web/frontend/events.ts'),
        greetings: resolve(__dirname, 'web/frontend/greetings.ts'),
        backup: resolve(__dirname, 'web/frontend/backup.ts'),
        analytics: resolve(__dirname, 'web/frontend/analytics.ts'),
      },
      output: {
        entryFileNames: '[name].js',
        assetFileNames: '[name][extname]',
      },
    },
  },
});
