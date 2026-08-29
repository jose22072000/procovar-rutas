import { chromium } from 'playwright'
const b = await chromium.launch()
const ctx = await b.newContext({ viewport: { width: 390, height: 820 } })
await ctx.addCookies([{ name: '__Secure-qb.session_token', value: process.env.TOKEN, domain: 'localhost', path: '/', secure: true, httpOnly: true, sameSite: 'Lax' }])
const page = await ctx.newPage()
await page.goto('http://localhost:3701/dashboard/organizations', { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(2500)
await page.getByRole('button', { name: /Nueva sucursal/i }).click()
await page.waitForTimeout(1200)
await page.screenshot({ path: '/fotos/auth-cajon.png' })
const d = await page.evaluate(() => {
  const el = document.querySelector('[role="dialog"], section[data-slot="base"], .heroui-modal')
  const r = el?.getBoundingClientRect()
  return { hay: !!el, w: r && Math.round(r.width), h: r && Math.round(r.height), desborda: document.documentElement.scrollWidth - document.documentElement.clientWidth }
})
console.log(JSON.stringify(d))
await b.close()
