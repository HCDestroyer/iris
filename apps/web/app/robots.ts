import type { MetadataRoute } from "next";
import { RUTAS_PUBLICAS } from "@/lib/legal/paginas";

// El panel, la app de campo y el portal piden sesión: nada de eso se rastrea. Las páginas legales
// públicas sí (el rastreador de Meta las revisa y respeta robots.txt).
export default function robots(): MetadataRoute.Robots {
    return { rules: [{ userAgent: "*", allow: [...RUTAS_PUBLICAS], disallow: "/" }] };
}
