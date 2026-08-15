"use client";

/**
 * Quién ha entrado, y el botón de salir.
 *
 * Faltaba: se entraba por procovar-auth pero el panel no enseñaba en ningún sitio
 * con qué usuario, ni cómo cerrar. Y no poder salir importa de verdad aquí, porque
 * esto se abre en el ordenador de la sucursal, que usa más de una persona: quien
 * termina se levanta creyendo que ha salido y la siguiente se encuentra la sesión
 * abierta con los datos de todos los vendedores.
 *
 * Salir no se resuelve aquí: se va a Accesos. La sesión vive allí, así que allí
 * está el cartel de "¿seguro?" y allí se cierra. Un panel que borrara solo su
 * cookie estaría mintiendo: la sesión de Accesos seguiría abierta y el botón de
 * entrar devolvería adentro sin preguntar.
 */

import { useEffect, useState } from "react";
import { pedir, API } from "@/lib/api";

interface Yo {
  user: string;
  email: string;
  role: string;
  branchId: string;
  isAdmin: boolean;
}

export default function Sesion() {
  const [yo, setYo] = useState<Yo | null>(null);

  useEffect(() => {
    // Si no hay sesión, `pedir` ya manda al login: aquí no hay nada que hacer.
    pedir<Yo>("/api/me")
      .then(setYo)
      .catch(() => {});
  }, []);

  if (!yo) return null;

  return (
    <div className="sesion">
      <span className="sesion-quien">
        {yo.user}
        <span className="sesion-rol">{yo.role}</span>
      </span>
      {/* Un enlace, no un botón con fetch: salir es un viaje a Accesos, que es
          donde se pregunta "¿seguro?" y donde se cierra de verdad. */}
      <a href={`${API}/api/auth/logout`} className="sesion-salir">
        Cerrar sesión
      </a>
    </div>
  );
}
