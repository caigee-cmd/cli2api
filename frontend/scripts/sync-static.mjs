import { cpSync, mkdirSync, readdirSync, rmSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const dist = join(root, 'dist')
const target = join(root, '..', 'internal', 'webui', 'static')
const assetsTarget = join(target, 'assets')

mkdirSync(assetsTarget, { recursive: true })
for (const name of readdirSync(assetsTarget)) {
  rmSync(join(assetsTarget, name), { force: true })
}
for (const name of readdirSync(join(dist, 'assets'))) {
  cpSync(join(dist, 'assets', name), join(assetsTarget, name))
}
for (const name of [
  'index.html',
  'favicon.svg',
  'favicon-dark.svg',
  'apple-touch-icon.svg',
  'og-card.svg',
  'site.webmanifest',
]) {
  const src = join(dist, name)
  if (existsSync(src)) cpSync(src, join(target, name))
}
console.log('synced frontend/dist -> internal/webui/static')
