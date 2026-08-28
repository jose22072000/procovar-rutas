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
  // 'about:blank' = la pestaña con la sesión puesta, pero sin navegar: hace falta para
  // poder interceptar una llamada ANTES de que la pantalla la haga.
  if (ruta !== 'about:blank') await page.goto(`${BASE}${ruta}`, { waitUntil: 'domcontentloaded' })
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

test('la carpeta que se da de alta aparece en la lista', async () => {
  const { ctx, page } = await abrir('/revisar')

  await page.waitForSelector('text=/Los GPS/', { timeout: 20000 })
  await page.click('button:has-text("Añadir una carpeta nueva")')
  await page.fill('input[placeholder="Nombre del perfil del GPS"]', 'GPS Yoandy')
  await page.fill('input[placeholder="Identificador de la carpeta en Drive"]', 'carpeta-nueva-1')
  await page.click('button:has-text("Añadir")')

  // Que conteste 200 no basta: lo que hay que ver es la carpeta en la tabla.
  await page.waitForSelector('text=GPS Yoandy', { timeout: 20000 })
  await ctx.close()
})

test('una carpeta que YA tiene dueño se le puede cambiar', async () => {
  /**
   * Un teléfono cambia de manos. Antes el desplegable sólo salía cuando la carpeta no
   * tenía dueño, así que una vez asignada se quedaba para siempre a nombre de quien la
   * llevaba antes — y con ella todo lo que subiera a partir de ese día.
   */
  const { ctx, page } = await abrir('/revisar')

  await page.waitForSelector('.tabla-gps tbody tr')

  const fila = page.locator('.tabla-gps tbody tr', { hasText: 'GPS luis' })

  await fila.locator('button:has-text("Cambiar")').click()
  await fila.locator('select').selectOption({ index: 1 })
  await fila.locator('button:has-text("Asignar")').click()

  await page.waitForSelector('text=/ficheros colocados/', { timeout: 20000 })
  await ctx.close()
})

test('sin la gente de Accesos se dice, en vez de enseñar una lista vacía', async () => {
  const { ctx, page } = await abrir('about:blank')

  // Se rompe SÓLO /api/personas: es el fallo que se vio en producción —la API contestaba
  // 404 ahí— y la pantalla ofrecía un desplegable sin nadie dentro, que no dice nada.
  await page.route('**/api/personas', (r) => r.fulfill({ status: 404, body: '{"error":"no"}' }))
  await page.goto(`${BASE}/revisar`, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.tabla-gps tbody tr', { timeout: 20000 })

  const texto = await page.locator('.tabla-gps').innerText()

  assert.match(texto, /no se pudo traer la gente de Accesos/i)
  await ctx.close()
})

test('en un teléfono la página no se arrastra a lo ancho: la tabla sí', async () => {
  const { ctx, page } = await abrir('about:blank')

  await page.setViewportSize({ width: 390, height: 820 })
  await page.goto(`${BASE}/revisar`, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.tabla-gps tbody tr', { timeout: 20000 })

  /**
   * Las tablas de aquí son anchas a propósito —estrecharlas deja columnas donde no se
   * lee el dato que se viene a mirar—, pero lo que se arrastra tiene que ser LA TABLA.
   * Estaban sueltas en la página, así que se arrastraba el documento entero y la barra
   * de arriba se iba con él.
   */
  const desborda = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
  )

  assert.equal(desborda, false, 'la página entera se desplaza a lo ancho')

  const scrollable = await page.evaluate(() => {
    const t = document.querySelector('.tabla-gps')?.closest('.tabla-ancha')

    return t != null && t.scrollWidth > t.clientWidth
  })

  assert.equal(scrollable, true, 'la tabla no se puede arrastrar dentro de su caja')
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
