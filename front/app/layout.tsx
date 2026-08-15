import type { Metadata } from "next";
import { Archivo, IBM_Plex_Mono, IBM_Plex_Sans } from "next/font/google";
import Image from "next/image";
import Link from "next/link";
import Session from "@/components/Session";
import "./globals.css";

/**
 * The same three typefaces as Accesos, for the same reasons.
 *
 * · Archivo is the logotype's: narrow, flat-serifed, industrial. It titles and
 *   labels. Narrow matters here: full Cuban names are long and do not fit a
 *   table column in a wide face.
 * · Plex Sans reads running text without tiring the eye over eight hours.
 * · Plex Mono is for CODES — dates, kilometres, identifiers — which get compared
 *   character by character. In a proportional face a 1 and an l blur together.
 *
 * They are not picked again here: they are Procovar's, and this panel is opened
 * in the same morning as Accesos and PEDIDO.
 */
const archivo = Archivo({
  variable: "--font-archivo",
  subsets: ["latin"],
  weight: ["500", "600", "700"],
  display: "swap",
});

const plex = IBM_Plex_Sans({
  variable: "--font-plex",
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  display: "swap",
});

const plexMono = IBM_Plex_Mono({
  variable: "--font-plex-mono",
  subsets: ["latin"],
  weight: ["400", "500"],
  display: "swap",
});

export const metadata: Metadata = {
  title: "Rutas — Procovar",
  description: "Control de los recorridos de los vendedores",
  // The same isotype as every other Procovar application. On light it is the
  // blue square; on dark it inverts, because blue on a black tab disappears.
  // The only thing that changes between applications is the name.
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
    <html lang="es" className={`${archivo.variable} ${plex.variable} ${plexMono.variable}`}>
      <body>
        <header className="barra">
          {/* The wordmark exactly as Accesos uses it: 516×119 artwork, set by
              height with the width left free. Boxing it or forcing it square
              squashes the letters into an illegible smudge — which is what it did
              the first time round. The application's name goes beside it, because
              that is the only thing that changes between Procovar's panels. */}
          <Link href="/" className="marca">
            <Image src="/logo.png" alt="Procovar" width={150} height={30} className="marca-logo" priority />
            <span className="marca-app">Rutas</span>
          </Link>
          <nav>
            <Link href="/">Calendario</Link>
            <Link href="/bandeja">Bandeja</Link>
            <Link href="/administracion">Administración</Link>
          </nav>
          <Session />
        </header>
        <main>{children}</main>
      </body>
    </html>
  );
}
