"use client";

/**
 * La pantalla de "aquí no entras".
 *
 * No se pinta la vista y se le pone un aviso encima: NO SE PINTA. Media pantalla
 * cargada con datos que esa persona no debía ver, aunque sea un segundo mientras
 * aparece el mensaje, es una fuga. Y un error suelto sobre una tabla vacía no le
 * dice a nadie qué hacer: se queda mirando, prueba a recargar y acaba llamando.
 *
 * Por eso esto ocupa la pantalla entera, dice qué pasa en una frase y da las dos
 * salidas que hay: volver a lo suyo, o ir a Accesos —que es donde se reparten los
 * permisos y donde alguien puede dárselo.
 */

import Link from "next/link";
import { API, AUTH } from "@/lib/api";

export default function SinPermiso({
  que = "esta pantalla",
  detalle,
}: {
  /** Qué es lo que no puede ver, para nombrarlo en vez de decir "esto". */
  que?: string;
  /** La llave que falta, si se sabe: es lo que hay que marcarle en Accesos. */
  detalle?: string | null;
}) {
  return (
    <div className="sin-permiso">
      <span className="sin-permiso-icono" aria-hidden>
        🔒
      </span>
      <h1>No tienes permiso para ver {que}</h1>
      <p>
        Tu cuenta existe y la sesión está abierta: lo que falta es el permiso, y eso
        se da en Accesos. Si te hace falta para trabajar, pídeselo a quien administra.
      </p>

      {detalle && (
        <p className="sin-permiso-clave">
          Permiso que falta: <code>{detalle}</code>
        </p>
      )}

      <div className="sin-permiso-salidas">
        <Link href="/" className="pv-boton">
          Volver al inicio
        </Link>
        <a href={AUTH} className="pv-boton pv-boton-primario">
          Ir a Accesos
        </a>
        <a href={`${API}/api/auth/logout`} className="pv-boton">
          Cerrar sesión
        </a>
      </div>
    </div>
  );
}
