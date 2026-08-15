"use client";

/**
 * Who is signed in, and the way out.
 *
 * This was missing: you came in through procovar-auth but the panel never showed
 * which user, nor how to close. And not being able to sign out matters here,
 * because this is opened on the branch computer, which more than one person uses:
 * whoever finishes gets up believing they left, and the next person finds the
 * session open with every seller's data in it.
 *
 * Signing out is not resolved here: it goes to Accesos. The session lives there,
 * so that is where the "are you sure?" prompt is and where it is closed. A panel
 * that merely cleared its own cookie would be lying: the Accesos session would
 * stay open and the sign-in button would let you back in without asking.
 */

import { useEffect, useState } from "react";
import { API, ask } from "@/lib/api";

interface Me {
  user: string;
  email: string;
  role: string;
  branchId: string;
  isAdmin: boolean;
}

export default function Session() {
  const [me, setMe] = useState<Me | null>(null);

  useEffect(() => {
    // With no session, `ask` already sends to the login: nothing to do here.
    ask<Me>("/api/me")
      .then(setMe)
      .catch(() => {});
  }, []);

  if (!me) return null;

  return (
    <div className="sesion">
      <span className="sesion-quien">
        {me.user}
        <span className="sesion-rol">{me.role}</span>
      </span>
      {/* A link, not a button with fetch: signing out is a trip to Accesos, which
          is where the prompt lives and where the session is really closed. */}
      <a href={`${API}/api/auth/logout`} className="sesion-salir">
        Cerrar sesión
      </a>
    </div>
  );
}
