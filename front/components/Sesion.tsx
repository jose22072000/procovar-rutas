"use client";

/**
 * Quién entró, qué puede, y la salida.
 *
 * Se pregunta UNA vez, al cargar la aplicación, y de ahí vive todo lo demás: el
 * menú esconde lo que esa persona no tiene y cada pantalla sabe si le toca. No se
 * va a Accesos en cada clic — los permisos vienen con la sesión y no cambian
 * mientras se está dentro.
 *
 * Que la pantalla esconda cosas es comodidad, no seguridad: la API exige la llave
 * en cada ruta y contesta 403 aunque alguien escriba la dirección a mano. Lo de
 * aquí es para que nadie vea botones que no puede usar.
 *
 * Cerrar sesión no se resuelve aquí: se va a Accesos, que es donde vive la sesión,
 * donde está el "¿seguro?" y donde de verdad se cierra. Un panel que solo borrara
 * su galleta estaría mintiendo.
 */

import { createContext, useContext, useEffect, useState } from "react";
import Link from "next/link";
import { API, ask, ApiError, type Yo } from "@/lib/api";

interface Estado {
  yo: Yo | null;
  cargando: boolean;
  /** Si la sesión existe pero no puede ni entrar, la llave que le falta. */
  vetado: string | null;
  puede: (clave: string) => boolean;
}

const Contexto = createContext<Estado>({
  yo: null,
  cargando: true,
  vetado: null,
  puede: () => false,
});

export const useSesion = () => useContext(Contexto);

export function Sesion({ children }: { children: React.ReactNode }) {
  const [yo, setYo] = useState<Yo | null>(null);
  const [cargando, setCargando] = useState(true);
  const [vetado, setVetado] = useState<string | null>(null);

  useEffect(() => {
    ask<Yo>("/api/me")
      .then(setYo)
      .catch((e) => {
        // 403 con sesión buena: entró, pero esta aplicación no es suya. Se guarda la
        // llave que falta para poder decírselo, en vez de dejar la pantalla muda.
        if (e instanceof ApiError && e.status === 403) setVetado(e.message);
      })
      .finally(() => setCargando(false));
  }, []);

  const puede = (clave: string) => Boolean(yo?.permisos?.[clave]);

  return (
    <Contexto.Provider value={{ yo, cargando, vetado, puede }}>
      <header className="barra">
        <Link href="/" className="marca">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/logo.png" alt="Procovar" width={150} height={30} className="marca-logo" />
          <span className="marca-app">Rutas</span>
        </Link>

        {/* Un solo enlace, porque hay una sola pantalla.
            Había tres —Calendario, Bandeja y Administración— y las dos últimas eran
            listas a las que nadie entraba: lo suyo (qué fichero llegó sin dueño, qué
            vendedor lleva días sin subir) es la EXPLICACIÓN de un hueco del
            calendario, y puesto en otra pestaña se queda sin leer. Ahora sale encima
            de la cuadrícula, y solo cuando hay algo que hacer.
            El reporte imprimible no va aquí: necesita un vendedor y un día, así que
            se entra desde el recorrido, que es donde se sabe cuáles. */}
        <nav>
          {puede("rutas.calendario") && <Link href="/">Calendario</Link>}
        </nav>

        {/* Cerrar sesión sale SIEMPRE, aunque no se sepa quién es o no tenga
            permisos. Esto se abre en la computadora de la sucursal, que usa más de
            una persona: quien no pueda salir se levanta creyendo que salió, y el
            siguiente se encuentra la sesión abierta. */}
        <div className="sesion">
          {yo && (
            <span className="sesion-quien">
              {yo.user}
              <span className="sesion-rol">{yo.role}</span>
            </span>
          )}
          <a href={`${API}/api/auth/logout`} className="sesion-salir">
            Cerrar sesión
          </a>
        </div>
      </header>

      <main>{children}</main>
    </Contexto.Provider>
  );
}
