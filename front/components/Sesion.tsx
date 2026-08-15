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
 * Cerrar se hace en el servidor (POST /api/auth/logout). La cookie es httpOnly, o
 * sea que el JavaScript no puede borrarla — y debe ser así: un token que puede leer
 * el navegador puede leerlo cualquier script de la página.
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
  const [saliendo, setSaliendo] = useState(false);

  useEffect(() => {
    // Si no hay sesión, `pedir` ya manda al login: aquí no hay nada que hacer.
    pedir<Yo>("/api/me")
      .then(setYo)
      .catch(() => {});
  }, []);

  async function salir() {
    setSaliendo(true);
    try {
      await fetch(`${API}/api/auth/logout`, {
        method: "POST",
        credentials: "include",
      });
    } finally {
      // Al login pase lo que pase: si la llamada falló, la cookie puede seguir
      // viva, y dejar a la persona en el panel le haría creer que ya salió.
      window.location.href = `${API}/api/auth/login`;
    }
  }

  if (!yo) return null;

  return (
    <div className="sesion">
      <span className="sesion-quien">
        {yo.user}
        <span className="sesion-rol">{yo.role}</span>
      </span>
      <button onClick={salir} disabled={saliendo} className="sesion-salir">
        {saliendo ? "Saliendo…" : "Cerrar sesión"}
      </button>
    </div>
  );
}
