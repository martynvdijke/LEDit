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
      },
      output: {
        entryFileNames: '[name].js',
        assetFileNames: '[name][extname]',
      },
    },
  },
});
