import { NextResponse, type NextRequest } from "next/server";

// Sin cookie de sesión no se sirve el panel: se manda al inicio de sesión con la ruta de vuelta.
// La cookie es HttpOnly y aquí no se valida (eso lo hace la API en GET /yo; un 401 también lleva
// al inicio de sesión). /campo (I4) y /soporte (portal) tienen su propia entrada.
const COOKIE_SESION = "iris_sesion";

export function middleware(peticion: NextRequest) {
    if (peticion.cookies.get(COOKIE_SESION)?.value) {
        return NextResponse.next();
    }
    const ruta = `${peticion.nextUrl.pathname}${peticion.nextUrl.search}`;
    const destino = ruta === "/" ? "/entrar" : `/entrar?${new URLSearchParams({ siguiente: ruta })}`;
    // Origen que vio el navegador (o el proxy delante del panel), no el que Next cree tener.
    const h = peticion.headers;
    const host = h.get("x-forwarded-host") ?? h.get("host") ?? peticion.nextUrl.host;
    const protocolo = h.get("x-forwarded-proto") ?? peticion.nextUrl.protocol.replace(":", "");
    return NextResponse.redirect(new URL(destino, `${protocolo}://${host}`));
}

export const config = {
    matcher: [
        "/((?!api/|_next/|entrar(?:/|$)|campo(?:/|$)|soporte(?:/|$)|icon\\.svg|favicon\\.ico|robots\\.txt|manifest\\.webmanifest).*)",
    ],
};
