import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import Sesion from "@/components/Sesion";
import "./globales.css";

export const metadata: Metadata = {
  title: "Rutas — Procovar",
  description: "Control de los recorridos de los vendedores",
  // El mismo isotipo que el resto de las aplicaciones de Procovar. En claro sale
  // el cuadrado azul; en oscuro se invierte, porque sobre una pestaña negra el
  // azul se pierde. Lo único que cambia entre aplicaciones es el nombre.
  icons: {
    icon: [
      { url: "/favicon-oscuro.svg", type: "image/svg+xml", media: "(prefers-color-scheme: dark)" },
      { url: "/favicon-claro.svg", type: "image/svg+xml" },
    ],
    apple: "/logo-512.png",
  },
};

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="es">
      <body>
        <header className="barra">
          <Link href="/" className="marca">
            <Image src="/logo.png" alt="Procovar" width={28} height={28} priority />
            Rutas <span>Procovar</span>
          </Link>
          <nav>
            <Link href="/">Calendario</Link>
            <Link href="/bandeja">Bandeja</Link>
            <Link href="/administracion">Administración</Link>
          </nav>
          <Sesion />
        </header>
        <main>{children}</main>
      </body>
    </html>
  );
}
