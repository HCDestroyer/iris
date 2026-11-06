import { expect, type Page } from "@playwright/test";

/*
 * Comprobación automática de «nada cortado ni roto» (pedido del PM para la bandeja):
 *
 * - la página no se desplaza a lo ancho;
 * - ningún elemento visible queda fuera de la ventana (salvo recortado a propósito por su padre);
 * - ninguna caja se desplaza a lo ancho sin querer (las tiras deslizables, como las respuestas
 *   rápidas, llevan `data-desborde-permitido`);
 * - ningún texto se sale de su caja: si `scrollWidth > clientWidth`, la caja debe recortar con
 *   elipsis (`text-overflow: ellipsis`) o ser un contenedor que se desplaza.
 *
 * Devuelve la lista de problemas con un selector legible para que el fallo diga dónde.
 */
export type Desborde = { tipo: "pagina" | "fuera" | "texto"; donde: string; detalle: string };

export async function buscarDesbordes(page: Page, selector = "body"): Promise<Desborde[]> {
    return page.locator(selector).first().evaluate((elemento: Element) => {
        const problemas: { tipo: "pagina" | "fuera" | "texto"; donde: string; detalle: string }[] = [];
        const ancho = window.innerWidth;
        const alto = window.innerHeight;
        const doc = document.documentElement;
        if (doc.scrollWidth > ancho + 1) {
            problemas.push({ tipo: "pagina", donde: "html", detalle: `scrollWidth ${doc.scrollWidth} > ${ancho}` });
        }
        const nombre = (el: Element): string => {
            const partes: string[] = [];
            let actual: Element | null = el;
            for (let i = 0; actual && i < 4; i++) {
                const etiqueta = actual.getAttribute("aria-label") ?? actual.getAttribute("data-slot") ?? actual.getAttribute("data-testid");
                partes.unshift(`${actual.tagName.toLowerCase()}${etiqueta ? `[${etiqueta}]` : ""}`);
                actual = actual.parentElement;
            }
            const texto = (el.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 40);
            return `${partes.join(" > ")}${texto ? ` «${texto}»` : ""}`;
        };
        const visible = (el: Element, estilo: CSSStyleDeclaration) => {
            if (estilo.visibility === "hidden" || estilo.display === "none" || Number(estilo.opacity) === 0) return false;
            const r = el.getBoundingClientRect();
            return r.width > 1 && r.height > 1;
        };
        const soloLectores = (estilo: CSSStyleDeclaration, r: DOMRect) =>
            (estilo.position === "absolute" && r.width <= 1.5 && r.height <= 1.5) || estilo.clip === "rect(0px, 0px, 0px, 0px)" || estilo.clipPath === "inset(50%)";
        /** ¿Algún ancestro se desplaza o recorta en x (el elemento puede salirse a propósito)? */
        const contenidoPorAncestro = (el: Element) => {
            let p = el.parentElement;
            while (p && p !== document.body) {
                const e = getComputedStyle(p);
                if (["auto", "scroll", "hidden", "clip"].includes(e.overflowX)) {
                    const r = p.getBoundingClientRect();
                    const re = el.getBoundingClientRect();
                    // El elemento queda recortado dentro de un ancestro que sí está en pantalla.
                    if (r.left >= -1 && r.right <= ancho + 1) return true;
                    if (re.right <= r.right + 1 && re.left >= r.left - 1) return true;
                }
                p = p.parentElement;
            }
            return false;
        };
        const tieneTextoPropio = (el: Element) => [...el.childNodes].some((n) => n.nodeType === Node.TEXT_NODE && (n.textContent ?? "").trim() !== "");
        for (const el of [elemento, ...elemento.querySelectorAll("*")]) {
            if (el instanceof SVGElement && !(el instanceof SVGSVGElement)) continue;
            if (el.closest("[data-desborde-permitido], [aria-hidden='true'] svg")) continue;
            const estilo = getComputedStyle(el);
            if (!visible(el, estilo)) continue;
            const r = el.getBoundingClientRect();
            if (soloLectores(estilo, r)) continue;
            // Fuera de la ventana a lo ancho (lo de abajo se alcanza desplazando en vertical).
            if ((r.right > ancho + 1 || r.left < -1) && r.top < alto && !contenidoPorAncestro(el) && estilo.position !== "fixed") {
                problemas.push({ tipo: "fuera", donde: nombre(el), detalle: `left ${Math.round(r.left)} right ${Math.round(r.right)} > ${ancho}` });
                continue;
            }
            const html = el as HTMLElement;
            // Recortado por su caja: se sale del ancestro más cercano que recorta o se desplaza.
            const recorte = (() => {
                let p = el.parentElement;
                while (p && p !== document.body) {
                    const e = getComputedStyle(p);
                    if (e.overflowX !== "visible") return { p, e };
                    p = p.parentElement;
                }
                return null;
            })();
            if (recorte && !recorte.p.closest("[data-desborde-permitido]") && recorte.e.textOverflow !== "ellipsis") {
                const rp = recorte.p.getBoundingClientRect();
                // Un contenedor oculto a la vista (1 px, para lectores o el autocompletado) recorta a propósito.
                if (rp.width <= 1.5 || rp.height <= 1.5) continue;
                const bordes = parseFloat(recorte.e.borderRightWidth) || 0;
                if (r.width > 4 && (r.right > rp.right - bordes + 1.5 || r.left < rp.left - 1.5)) {
                    problemas.push({ tipo: "fuera", donde: nombre(el), detalle: `recortado por su caja: ${Math.round(r.left)}–${Math.round(r.right)} fuera de ${Math.round(rp.left)}–${Math.round(rp.right)}` });
                    continue;
                }
            }
            // Una caja que se desplaza a lo ancho sin querer (columnas, fichas): su contenido no cabe.
            // Las tiras deslizables a propósito llevan `data-desborde-permitido`.
            if ((estilo.overflowX === "auto" || estilo.overflowX === "scroll") && html.scrollWidth > html.clientWidth + 1 && html.clientWidth > 0) {
                problemas.push({ tipo: "fuera", donde: nombre(el), detalle: `se desplaza a lo ancho: scrollWidth ${html.scrollWidth} > clientWidth ${html.clientWidth}` });
                continue;
            }
            // Texto que no cabe en su caja.
            // Cuenta el ancho del texto propio (no el de hijos posicionados, como el punto del avatar).
            const anchoTexto = () => {
                let maximo = 0;
                for (const n of el.childNodes) {
                    if (n.nodeType !== Node.TEXT_NODE || !(n.textContent ?? "").trim()) continue;
                    const rango = document.createRange();
                    rango.selectNodeContents(n);
                    const rt = rango.getBoundingClientRect();
                    maximo = Math.max(maximo, Math.max(rt.right - r.left, r.right - rt.left));
                }
                return maximo;
            };
            if (html.scrollWidth > html.clientWidth + 1 && html.clientWidth > 0 && tieneTextoPropio(el) && anchoTexto() > r.width + 1) {
                const desplaza = estilo.overflowX === "auto" || estilo.overflowX === "scroll";
                const elipsis = estilo.textOverflow === "ellipsis" && (estilo.overflowX === "hidden" || estilo.overflowX === "clip");
                const multilinea = estilo.webkitLineClamp !== "none" && estilo.webkitLineClamp !== "";
                if (!desplaza && !elipsis && !multilinea) {
                    problemas.push({ tipo: "texto", donde: nombre(el), detalle: `scrollWidth ${html.scrollWidth} > clientWidth ${html.clientWidth}` });
                }
            }
        }
        return problemas;
    });
}

/** Falla con la lista de problemas si algo se desborda o queda fuera de la pantalla. */
export async function esperarSinDesbordes(page: Page, selector = "body"): Promise<void> {
    expect(await buscarDesbordes(page, selector), "nada se desborda ni queda fuera de pantalla").toEqual([]);
}
