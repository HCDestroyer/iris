import { NextResponse, type NextRequest } from "next/server";

// Sin cookie de sesión no se sirve el panel: se manda al inicio de sesión con la ruta de vuelta.
// La cookie es HttpOnly y aquí no se valida (eso lo hace la API en GET /yo; un 401 también lleva
// al inicio de sesión). /campo (I4) no pasa por aquí: su shell lo sirve el service worker sin
// señal y la propia app pide /entrar?siguiente=/campo cuando GET /yo responde 401. Tampoco el
// service worker (/sw.js): el navegador lo baja sin seguir redirecciones.
//
// El portal de soporte (/soporte) es otro actor: nunca exige `iris_sesion`. Sin la cookie del
// portal (`iris_portal`) manda a /soporte/entrar, que pide entrar desde el portal del proveedor.
// /soporte/entrar no pasa por aquí (lee el ticket del fragmento, que el servidor nunca ve).
// La encuesta pública (/encuesta#<token>) tampoco: es sin sesión y el token va en el fragmento.
const COOKIE_SESION = "iris_sesion";
const COOKIE_PORTAL = "iris_portal";

/** Origen que vio el navegador (o el proxy delante del panel), no el que Next cree tener. */
function origen(peticion: NextRequest): string {
    const h = peticion.headers;
    const host = h.get("x-forwarded-host") ?? h.get("host") ?? peticion.nextUrl.host;
    const protocolo = h.get("x-forwarded-proto") ?? peticion.nextUrl.protocol.replace(":", "");
    return `${protocolo}://${host}`;
}

export function middleware(peticion: NextRequest) {
    const { pathname, search } = peticion.nextUrl;
    if (pathname === "/soporte" || pathname.startsWith("/soporte/")) {
        if (peticion.cookies.get(COOKIE_PORTAL)?.value) {
            return NextResponse.next();
        }
        return NextResponse.redirect(new URL("/soporte/entrar", origen(peticion)));
    }
    if (peticion.cookies.get(COOKIE_SESION)?.value) {
        return NextResponse.next();
    }
    const ruta = `${pathname}${search}`;
    const destino = ruta === "/" ? "/entrar" : `/entrar?${new URLSearchParams({ siguiente: ruta })}`;
    return NextResponse.redirect(new URL(destino, origen(peticion)));
}

export const config = {
    matcher: [
        "/((?!api/|_next/|entrar(?:/|$)|campo(?:/|$)|encuesta(?:/|$)|sw\\.js|soporte/entrar(?:/|$)|icon\\.svg|favicon\\.ico|robots\\.txt|manifest\\.webmanifest).*)",
    ],
};
