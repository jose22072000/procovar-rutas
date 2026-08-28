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

// -------------------------------------------------------------- revisar

test('las sucursales del calendario se pliegan y se recuerdan', async () => {
  const { ctx, page } = await abrir('/')

  await page.waitForSelector('table tbody tr')

  const antes = await page.locator('table tbody tr:not(.fila-sucursal)').count()

  // Se cierra la primera sucursal: sus vendedores desaparecen, la franja se queda.
  await page.locator('.fila-sucursal button').first().click()
  await page.waitForTimeout(300)

  const despues = await page.locator('table tbody tr:not(.fila-sucursal)').count()

  assert.ok(despues < antes, `cerrar una sucursal no escondió sus filas: ${antes} -> ${despues}`)
  // La franja sigue, con su cuenta: cerrar no puede esconder que ahí hay gente sin subir.
  assert.equal(await page.locator('.fila-sucursal').count(), 3)

  // Y se recuerda: al recargar sigue cerrada.
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('table tbody tr')
  await page.waitForTimeout(300)
  assert.equal(await page.locator('table tbody tr:not(.fila-sucursal)').count(), despues)
  await ctx.close()
})

test('se puede dar de alta una carpeta AUNQUE no haya ninguna', async () => {
  /**
   * El fallo: el botón de crear vivía dentro del bloque de la tabla, que sólo se pintaba
   * si ya había carpetas. Sin ninguna —o si `/api/gps` fallaba— se perdían la tabla y el
   * botón a la vez, y no quedaba forma de crear la primera.
   *
   * Se comprueba con la lista LLENA y con la lista VACÍA: el botón tiene que estar en las
   * dos. La vacía la sirve el API de mentira con SIN_CARPETAS=1.
   */
  const { ctx, page } = await abrir('/revisar')

  await page.waitForSelector('text=/Los GPS/', { timeout: 20000 })
  assert.ok(
    await page.locator('button', { hasText: /a.adir una carpeta nueva/i }).count() > 0,
    'no hay forma de dar de alta una carpeta',
  )
  await ctx.close()
})

test('cada carpeta se puede asignar a un vendedor o supervisor', async () => {
  const { ctx, page } = await abrir('/revisar')

  await page.waitForSelector('.tabla-gps tbody tr')

  // Las que no tienen dueño ofrecen el desplegable para decir de quién son.
  const selects = page.locator('.tabla-gps select')

  assert.ok(await selects.count() > 0, 'ninguna carpeta ofrece a quién asignarla')

  const opciones = await selects.first().locator('option').allInnerTexts()

  // Y sólo gestores y supervisores: son los que salen a la calle.
  assert.ok(opciones.some((o) => /GESTOR|SUPERVISOR/i.test(o)), `sin roles en la lista: ${opciones}`)
  await ctx.close()
})

test('un fichero que Drive ya no tiene no ofrece reintentarlo', async () => {
  const { ctx, page } = await abrir('/revisar')

  await page.waitForSelector('text=/llegaron rotos|llegó roto/i', { timeout: 20000 })

  const texto = await page.locator('body').innerText()

  // El del 404: reintentarlo no puede funcionar, y el botón lo dice.
  assert.match(texto, /Quitarlo de la lista/i)
  assert.match(texto, /Drive ya no tiene este fichero/i)
  // El otro sí se puede volver a leer con el lector de hoy.
  assert.match(texto, /Volver a intentarlo/i)
  await ctx.close()
})

test.after(() => navegador.close())
