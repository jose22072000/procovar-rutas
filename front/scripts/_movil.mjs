/** Comprueba, en 390px, si alguna pantalla desborda a lo ancho. */
import { chromium } from 'playwright'
const BASE = process.env.BASE
const RUTAS = (process.env.RUTAS || '/').split(',')
const b = await chromium.launch()
const ctx = await b.newContext({ viewport: { width: 390, height: 820 } })
const page = await ctx.newPage()
for (const r of RUTAS) {
  await page.goto(`${BASE}${r}`, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2500)
  const info = await page.evaluate(() => {
    const d = document.documentElement
    const culpables = [...document.querySelectorAll('*')]
      .filter((e) => e.getBoundingClientRect().right > d.clientWidth + 1)
      .slice(0, 5)
      .map((e) => `${e.tagName.toLowerCase()}.${(e.className || '').toString().split(' ').slice(0, 3).join('.')} w=${Math.round(e.getBoundingClientRect().width)}`)
    return { desborda: d.scrollWidth - d.clientWidth, titulo: document.title, culpables }
  })
  console.log(r, JSON.stringify(info, null, 1))
}
await b.close()
