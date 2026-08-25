/**
 * Los iconos, empotrados.
 *
 * Son de Lucide, servidos por Iconify, pero NO se piden a la API de Iconify: los
 * datos de los diez que se usan están aquí, en el paquete. Un panel que hay que
 * abrir en una sucursal cubana no puede quedarse sin flechas porque un CDN no
 * responda, y pedirle a un servidor de fuera un dibujo de veinte bytes es una
 * dependencia que no compra nada.
 *
 * Nada de emojis ni de flechas escritas a mano («←», «→»): un emoji lo dibuja cada
 * sistema como le parece —y en Windows salen de otro color y otro tamaño— y una
 * flecha de texto no se alinea con el texto que tiene al lado.
 *
 * Para añadir uno: sacarlo de `@iconify-json/lucide` (está en node_modules) y
 * pegarlo aquí. Se hace a mano a propósito, para que el paquete de iconos no acabe
 * entero en el navegador de nadie.
 */

import type { IconifyIcon } from "@iconify/react";

const caja = { width: 24, height: 24 } as const;

export const flechaIzquierda: IconifyIcon = {
  ...caja,
  body: "<path fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\" d=\"m15 18l-6-6l6-6\"/>",
};

export const flechaDerecha: IconifyIcon = {
  ...caja,
  body: "<path fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\" d=\"m9 18l6-6l-6-6\"/>",
};

export const capas: IconifyIcon = {
  ...caja,
  body: "<g fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\"><path d=\"M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83z\"/><path d=\"M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12\"/><path d=\"M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17\"/></g>",
};

export const imprimir: IconifyIcon = {
  ...caja,
  body: "<g fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\"><path d=\"M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2M6 9V3a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v6\"/><rect width=\"12\" height=\"8\" x=\"6\" y=\"14\" rx=\"1\"/></g>",
};

export const traer: IconifyIcon = {
  ...caja,
  body: "<g fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\"><path d=\"M3 12a9 9 0 0 1 9-9a9.75 9.75 0 0 1 6.74 2.74L21 8\"/><path d=\"M21 3v5h-5m5 4a9 9 0 0 1-9 9a9.75 9.75 0 0 1-6.74-2.74L3 16\"/><path d=\"M8 16H3v5\"/></g>",
};

export const aviso: IconifyIcon = {
  ...caja,
  body: "<path fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\" d=\"m21.73 18l-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3M12 9v4m0 4h.01\"/>",
};

export const ir: IconifyIcon = {
  ...caja,
  body: "<path fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\" d=\"M5 12h14m-7-7l7 7l-7 7\"/>",
};

export const calendario: IconifyIcon = {
  ...caja,
  body: "<g fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\"><path d=\"M8 2v3m8-3v3\"/><rect width=\"18\" height=\"18\" x=\"3\" y=\"3\" rx=\"2\"/><path d=\"M3 9h18M8 13h.01M12 13h.01M16 13h.01M8 17h.01M12 17h.01M16 17h.01\"/></g>",
};

export const chincheta: IconifyIcon = {
  ...caja,
  body: "<g fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\"><path d=\"M20 10c0 4.993-5.539 10.193-7.399 11.799a1 1 0 0 1-1.202 0C9.539 20.193 4 14.993 4 10a8 8 0 0 1 16 0\"/><circle cx=\"12\" cy=\"10\" r=\"3\"/></g>",
};

export const reloj: IconifyIcon = {
  ...caja,
  body: "<g fill=\"none\" stroke=\"currentColor\" stroke-linecap=\"round\" stroke-linejoin=\"round\" stroke-width=\"2\"><circle cx=\"12\" cy=\"12\" r=\"10\"/><path d=\"M12 6v6l4 2\"/></g>",
};
