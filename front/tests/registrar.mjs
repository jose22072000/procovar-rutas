/**
 * Engancha el resolvedor de módulos antes de que Node cargue las pruebas.
 *
 * Se hace con `register()` y no con `--experimental-loader` porque ese flag avisa de que
 * va a desaparecer, y una prueba que empieza con una advertencia acaba ignorándose entera.
 */

import { register } from 'node:module';

register('./resolver.mjs', import.meta.url);
