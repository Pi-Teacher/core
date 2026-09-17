// Post-build: inline JS/CSS into a single standalone index.html
// so the prototype can be opened directly via file:// (double-click) without a server.
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const distDir = join(__dirname, '..', 'dist');
const assetsDir = join(distDir, 'assets');

const html = readFileSync(join(distDir, 'index.html'), 'utf8');
const assets = readdirSync(assetsDir);

const jsFile = assets.find((f) => f.endsWith('.js'));
const cssFile = assets.find((f) => f.endsWith('.css'));

const js = readFileSync(join(assetsDir, jsFile), 'utf8');
const css = readFileSync(join(assetsDir, cssFile), 'utf8');

const inlined = html
  .replace(
    new RegExp(`<script type="module"[^>]*src="[^"]*${jsFile}"[^>]*></script>`),
    `<script type="module">\n${js}\n</script>`
  )
  .replace(
    new RegExp(`<link[^>]*href="[^"]*${cssFile}"[^>]*>`),
    `<style>\n${css}\n</style>`
  );

writeFileSync(join(distDir, 'index.html'), inlined);
console.log(`✓ Inlined ${jsFile} + ${cssFile} into dist/index.html (standalone)`);
