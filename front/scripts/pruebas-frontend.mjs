/**
 * La pantalla, con un navegador de verdad.
 *
 * El calendario lo pinta el navegador entero: el HTML que manda el servidor viene vacío,
 * así que con `curl` no se comprueba si las sucursales salen separadas o revueltas.
 *
 * La API de verdad es un servicio en Go con Postgres y la sesión de Accesos detrás.
 * Levantar todo eso para mirar una tabla es desproporcionado, así que se usa
 * `api-de-mentira.mjs`, que devuelve vendedores de tres sucursales — el caso que había
 * que separar.
 *
 * Cómo se corre entero, desde cero:
 *   node scripts/api-de-mentira.mjs 3600 &
 *   NEXT_PUBLIC_API_URL=http://localhost:3600 npm run build
 *   NEXT_PUBLIC_API_URL=http://localhost:3600 npx next start -p 3601 &
 *   node --test scripts/pruebas-frontend.mjs
 *
 * (El navegador necesita librerías del sistema que aquí no están; va en la imagen de
 * Playwright — ver cómo se lanza en procovar-delivery/PRUEBAS.md.)
 */

import test from 'node:test'
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const BASE = process.env.BASE || 'http://localhost:3601'

const navegador = await chromium.launch()

async function abrir(ruta) {
  const ctx = await navegador.newContext({ viewport: { width: 1500, height: 1000 } })

  // El middleware sólo mira que la cookie de Accesos EXISTA: validarla es cosa de la API.
  await ctx.addCookies([{ name: 'qb.session_token', value: 'de-mentira', url: BASE }])

  const page = await ctx.newPage()
  const errores = []

  page.on('pageerror', (e) => errores.push(String(e)))
  await page.goto(`${BASE}${ruta}`, { waitUntil: 'domcontentloaded' })
  return { ctx, page, errores }
}

test('el calendario separa los vendedores por sucursal', async () => {
  const { ctx, page, errores } = await abrir('/')

  await page.waitForSelector('table tbody tr', { timeout: 20000 })

  const franjas = await page.locator('.fila-sucursal').allInnerTexts()

  assert.ok(franjas.length >= 3, `salieron ${franjas.length} sucursales, se esperaban 3`)
  assert.ok(franjas.some((f) => /CAMAG/i.test(f)))
  assert.ok(franjas.some((f) => /HABANA/i.test(f)))

  // La franja lleva lo único que hace falta decidir: cuántos de ahí llevan días callados.
  assert.ok(franjas.every((f) => /vendedores/.test(f)), `una franja sin la cuenta: ${franjas}`)
  assert.ok(franjas.some((f) => /sin subir/.test(f)))

  assert.equal(errores.length, 0, `errores en el navegador: ${errores.join(' | ')}`)
  await ctx.close()
})

test('cada vendedor cae DEBAJO de su sucursal, no en cualquier sitio', async () => {
  const { ctx, page } = await abrir('/')

  await page.waitForSelector('table tbody tr')

  // Se recorre la tabla de arriba abajo: cada fila de vendedor pertenece a la última
  // franja que se vio. Si el agrupado estuviera mal, un CMG aparecería bajo La Habana.
  const filas = await page.locator('table tbody tr').evaluateAll((trs) =>
    trs.map((tr) => ({
      esFranja: tr.classList.contains('fila-sucursal'),
      texto: tr.textContent || '',
    })),
  )

  let sucursal = ''
  let comprobadas = 0

  for (const f of filas) {
    if (f.esFranja) {
      sucursal = /CAMAG/i.test(f.texto) ? 'CMG' : /HOLGU/i.test(f.texto) ? 'HOL' : 'HAB'
      continue
    }

    const m = f.texto.match(/Vendedor (CMG|HOL|HAB)\d/)

    if (m) {
      assert.equal(m[1], sucursal, `${m[0]} está bajo la sucursal equivocada`)
      comprobadas++
    }
  }
  assert.ok(comprobadas >= 9, `sólo se comprobaron ${comprobadas} vendedores`)
  await ctx.close()
})

test('agrupada, la sucursal NO se repite en cada fila', async () => {
  const { ctx, page } = await abrir('/')

  await page.waitForSelector('table tbody tr')

  // La franja ya lo dice; repetirlo bajo cada nombre es decir dos veces lo mismo.
  assert.equal(await page.locator('td.seller .seller-sucursal').count(), 0)
  await ctx.close()
})

test.after(() => navegador.close())
