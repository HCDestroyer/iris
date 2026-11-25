// Manifest de la PWA «Iris Campo» (I4). Logo de Iris (docs/marca/logo-mark.svg, servido en
// /icon.svg, en degradado índigo → cian) y colores del rediseño: índigo de marca y el lienzo #F8F9FA.
import { MANIFEST_CAMPO } from "@/lib/campo/manifest";

export const dynamic = "force-static";

export function GET() {
    return new Response(JSON.stringify(MANIFEST_CAMPO), {
        headers: { "Content-Type": "application/manifest+json; charset=utf-8", "Cache-Control": "public, max-age=3600" },
    });
}
