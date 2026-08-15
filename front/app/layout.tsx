import type { Metadata } from "next";
import Link from "next/link";
import "./globales.css";

export const metadata: Metadata = {
  title: "Rutas — Procovar",
  description: "Control de los recorridos de los vendedores",
};

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="es">
      <body>
        <header className="barra">
          <Link href="/" className="marca">
            Rutas <span>Procovar</span>
          </Link>
          <nav>
            <Link href="/">Calendario</Link>
            <Link href="/bandeja">Bandeja</Link>
            <Link href="/administracion">Administración</Link>
          </nav>
        </header>
        <main>{children}</main>
      </body>
    </html>
  );
}
