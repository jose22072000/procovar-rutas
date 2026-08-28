/**
 * Que `import './pricing'` encuentre `pricing.ts`, y que `@/lib/x` sea `src/lib/x`.
 *
 * Node ejecuta TypeScript directamente desde la 22, pero resuelve como ESM: exige la
 * extensión en las rutas relativas y no sabe nada de los alias de `tsconfig`. El código
 * de la aplicación lo compila Next, que sí sabe las dos cosas; obligarle a escribir
 * `./pricing.ts` sólo para poder probarlo sería cambiar el código por la herramienta.
 *
 * Este gancho hace la traducción y no toca nada más.
 */

import { existsSync } from 'node:fs'
import { fileURLToPath, pathToFileURL } from 'node:url'
import path from 'node:path'

const RAIZ = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const CANDIDATOS = ['.ts', '.tsx', '/index.ts', '/index.tsx']

function conExtension(base) {
  for (const suf of CANDIDATOS) {
    const p = base + suf
    if (existsSync(p)) return pathToFileURL(p).href
  }
  return null
}

export async function resolve(especificador, contexto, siguiente) {
  // Alias del tsconfig: "@/lib/auth" -> "<raíz>/src/lib/auth".
  if (especificador.startsWith('@/')) {
    const url = conExtension(path.join(RAIZ, 'src', especificador.slice(2)))
    if (url) return { url, shortCircuit: true }
  }

  /**
   * Relativos sin extensión, que es como los escribe el código de la aplicación.
   *
   * No vale con mirar si el nombre "tiene extensión": `orderRecord.dto` la tiene según
   * `path.extname` —cree que es `.dto`— y sin embargo el fichero es `orderRecord.dto.ts`.
   * Lo que decide es si el fichero existe tal cual; si no, se prueban las extensiones.
   */
  if (especificador.startsWith('.')) {
    const desde = contexto.parentURL ? path.dirname(fileURLToPath(contexto.parentURL)) : RAIZ
    const destino = path.resolve(desde, especificador)

    if (!existsSync(destino) || !path.extname(destino)) {
      const url = conExtension(destino)
      if (url) return { url, shortCircuit: true }
    }
  }

  return siguiente(especificador, contexto)
}
