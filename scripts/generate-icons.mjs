import { mkdir, readFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import sharp from 'sharp';

const root = resolve(import.meta.dirname, '..');
const source = resolve(root, 'web/static/pwa/icon.svg');
const output = resolve(root, 'web/static/pwa');
const svg = await readFile(source);

await mkdir(output, { recursive: true });
for (const size of [16, 32, 180, 192, 512]) {
  const filename = size === 16 ? 'favicon.png' : size === 32 ? 'favicon-32.png' : `icon-${size}.png`;
  await sharp(svg).resize(size, size).png().toFile(resolve(output, filename));
}

console.log('Generated LEDit icons from', source);
