import type { ReactNode } from "react";

/*
 * Markdown del manual de Iris (docs/manual). Convertidor mínimo y seguro, sin dependencias: el
 * texto se arma como nodos de React y nunca como HTML (no hay `dangerouslySetInnerHTML`), así que
 * `<script>`, `<img onerror>` o cualquier etiqueta sale como texto.
 *
 * Acepta solo:
 *   - títulos `#`, `##` y `###`;
 *   - párrafos;
 *   - listas con `-`/`*` o `1.` (un nivel de sangría para sublistas);
 *   - bloques de código entre ``` y código en línea entre `;
 *   - **negrita**;
 *   - enlaces internos `[texto](/manual/clave)` y externos `[texto](https://…)`, estos con
 *     `target="_blank"` y `rel="noopener noreferrer"`.
 * Cualquier otro destino (javascript:, data:, rutas fuera del manual) queda como texto.
 */

export type Bloque =
    | { tipo: "titulo"; nivel: 1 | 2 | 3; texto: string }
    | { tipo: "parrafo"; texto: string }
    | { tipo: "lista"; ordenada: boolean; elementos: ElementoLista[] }
    | { tipo: "codigo"; texto: string };

export type ElementoLista = { texto: string; sublista: { ordenada: boolean; elementos: string[] } | null };

const VINETA = /^([-*]|\d+[.)])\s+/;
const VINETA_SANGRIA = /^ {2,}([-*]|\d+[.)])\s+/;
const TITULO = /^(#{1,3})\s+(.+)$/;
const SOLO_NEGRITA = /^\*\*[^*]+\*\*$/;

function esInicioDeBloque(linea: string): boolean {
    return linea.trim() === "" || TITULO.test(linea) || VINETA.test(linea) || linea.startsWith("```");
}

/** Divide la fuente en bloques. Una línea que no encaja en nada es parte de un párrafo. */
export function bloquesManual(fuente: string): Bloque[] {
    const lineas = fuente.replace(/\r\n?/g, "\n").split("\n");
    const bloques: Bloque[] = [];
    let i = 0;
    while (i < lineas.length) {
        const linea = lineas[i] ?? "";
        if (linea.trim() === "") {
            i++;
            continue;
        }
        if (linea.startsWith("```")) {
            const codigo: string[] = [];
            i++;
            while (i < lineas.length && !(lineas[i] ?? "").startsWith("```")) {
                codigo.push(lineas[i] ?? "");
                i++;
            }
            i++;
            bloques.push({ tipo: "codigo", texto: codigo.join("\n") });
            continue;
        }
        const titulo = TITULO.exec(linea);
        if (titulo) {
            bloques.push({ tipo: "titulo", nivel: (titulo[1]?.length ?? 1) as 1 | 2 | 3, texto: (titulo[2] ?? "").trim() });
            i++;
            continue;
        }
        if (VINETA.test(linea)) {
            const ordenada = /^\d/.test(linea);
            const elementos: ElementoLista[] = [];
            while (i < lineas.length) {
                const actual = lineas[i] ?? "";
                if (VINETA.test(actual)) {
                    elementos.push({ texto: actual.replace(VINETA, "").trim(), sublista: null });
                } else if (VINETA_SANGRIA.test(actual) && elementos.length > 0) {
                    const ultimo = elementos[elementos.length - 1];
                    if (ultimo) {
                        ultimo.sublista ??= { ordenada: /^\s*\d/.test(actual), elementos: [] };
                        ultimo.sublista.elementos.push(actual.replace(VINETA_SANGRIA, "").trim());
                    }
                } else if (/^\s{2,}\S/.test(actual) && elementos.length > 0) {
                    // Continuación con sangría del último elemento (o de su último subelemento).
                    const ultimo = elementos[elementos.length - 1];
                    const sub = ultimo?.sublista?.elementos;
                    if (sub && sub.length > 0) sub[sub.length - 1] += ` ${actual.trim()}`;
                    else if (ultimo) ultimo.texto += ` ${actual.trim()}`;
                } else {
                    break;
                }
                i++;
            }
            bloques.push({ tipo: "lista", ordenada, elementos });
            continue;
        }
        const parrafo: string[] = [];
        while (i < lineas.length && !esInicioDeBloque(lineas[i] ?? "")) {
            parrafo.push((lineas[i] ?? "").trim());
            i++;
            // Una línea toda en negrita (la pregunta de «Preguntas frecuentes») va sola.
            if (parrafo.length === 1 && SOLO_NEGRITA.test(parrafo[0] ?? "")) break;
        }
        bloques.push({ tipo: "parrafo", texto: parrafo.join(" ") });
    }
    return bloques;
}

export type DestinoEnlace = { tipo: "interno"; clave: string | null } | { tipo: "externo"; href: string };

/**
 * Qué es un enlace: `/manual` o `/manual/<clave>` es interno; `https://…` o `http://…`, externo.
 * Lo demás (javascript:, data:, mailto:, rutas relativas o del panel) no es un enlace.
 */
export function destinoEnlace(url: string): DestinoEnlace | null {
    const limpia = url.trim();
    if (limpia === "/manual") return { tipo: "interno", clave: null };
    const interno = /^\/manual\/([a-z0-9-]+\/[a-z0-9-]+)$/.exec(limpia);
    if (interno) return { tipo: "interno", clave: interno[1] ?? null };
    try {
        const u = new URL(limpia);
        return u.protocol === "https:" || u.protocol === "http:" ? { tipo: "externo", href: u.href } : null;
    } catch {
        return null;
    }
}

const EN_LINEA = /(\*\*([^*]+)\*\*)|(`([^`]+)`)|(\[([^\]]+)\]\(([^)\s]+)\))/;

/** Convierte una clave interna en la ruta donde se abre (en /campo cambia para funcionar sin señal). */
export type RutaClave = (clave: string | null) => string;

export const rutaPanel: RutaClave = (clave) => (clave ? `/manual/${clave}` : "/manual");

const CLASE_ENLACE = "font-medium text-primary underline underline-offset-2";

function enLinea(texto: string, clave: string, ruta: RutaClave): ReactNode[] {
    const salida: ReactNode[] = [];
    let resto = texto;
    let n = 0;
    while (resto.length > 0) {
        const m = EN_LINEA.exec(resto);
        if (!m) {
            salida.push(resto);
            break;
        }
        if (m.index > 0) salida.push(resto.slice(0, m.index));
        const k = `${clave}-${n++}`;
        if (m[1]) {
            salida.push(<strong key={k}>{enLinea(m[2] ?? "", k, ruta)}</strong>);
        } else if (m[3]) {
            salida.push(
                <code key={k} className="rounded bg-background-secondary px-1 font-mono text-[0.85em]">
                    {m[4]}
                </code>,
            );
        } else {
            const destino = destinoEnlace(m[7] ?? "");
            const etiqueta = m[6] ?? "";
            if (destino?.tipo === "interno") {
                salida.push(
                    <a key={k} href={ruta(destino.clave)} className={CLASE_ENLACE}>
                        {etiqueta}
                    </a>,
                );
            } else if (destino?.tipo === "externo") {
                salida.push(
                    <a key={k} href={destino.href} target="_blank" rel="noopener noreferrer" className={CLASE_ENLACE}>
                        {etiqueta}
                    </a>,
                );
            } else {
                // Destino no permitido: solo la etiqueta, como texto.
                salida.push(etiqueta);
            }
        }
        resto = resto.slice(m.index + m[0].length);
    }
    return salida;
}

const CLASES_TITULO = {
    1: "text-[1.375rem] font-semibold tracking-[-0.015em]",
    2: "mt-3 text-lg font-semibold",
    3: "mt-2 text-base font-semibold",
} as const;

function Lista({ ordenada, children }: { ordenada: boolean; children: ReactNode }) {
    const Etiqueta = ordenada ? "ol" : "ul";
    return <Etiqueta className={ordenada ? "grid list-decimal gap-1.5 pl-6" : "grid list-disc gap-1.5 pl-6"}>{children}</Etiqueta>;
}

export type PropsMarkdownManual = {
    fuente: string;
    /** Dónde abre un enlace interno; por defecto, /manual/<clave>. */
    ruta?: RutaClave;
    /** Sin el título `#` (cuando la pantalla ya lo pone). */
    sinTitulo?: boolean;
    className?: string;
};

/** Una página del manual como nodos de React (nunca HTML crudo). */
export function MarkdownManual({ fuente, ruta = rutaPanel, sinTitulo = false, className }: PropsMarkdownManual) {
    const bloques = bloquesManual(fuente).filter((b) => !(sinTitulo && b.tipo === "titulo" && b.nivel === 1));
    return (
        <div className={["grid gap-3 text-[0.9375rem] leading-relaxed [overflow-wrap:anywhere]", className].filter(Boolean).join(" ")}>
            {bloques.map((b, i) => {
                const k = `b${i}`;
                switch (b.tipo) {
                    case "titulo": {
                        const Etiqueta = b.nivel === 1 ? "h1" : b.nivel === 2 ? "h2" : "h3";
                        return (
                            <Etiqueta key={k} className={CLASES_TITULO[b.nivel]}>
                                {enLinea(b.texto, k, ruta)}
                            </Etiqueta>
                        );
                    }
                    case "lista":
                        return (
                            <Lista key={k} ordenada={b.ordenada}>
                                {b.elementos.map((e, j) => (
                                    <li key={`${k}-${j}`}>
                                        {enLinea(e.texto, `${k}-${j}`, ruta)}
                                        {e.sublista ? (
                                            <div className="mt-1.5">
                                                <Lista ordenada={e.sublista.ordenada}>
                                                    {e.sublista.elementos.map((s, h) => (
                                                        <li key={`${k}-${j}-${h}`}>{enLinea(s, `${k}-${j}-${h}`, ruta)}</li>
                                                    ))}
                                                </Lista>
                                            </div>
                                        ) : null}
                                    </li>
                                ))}
                            </Lista>
                        );
                    case "codigo":
                        return (
                            <pre key={k} className="overflow-x-auto rounded-lg bg-background-secondary p-3 font-mono text-xs">
                                {b.texto}
                            </pre>
                        );
                    default:
                        return <p key={k}>{enLinea(b.texto, k, ruta)}</p>;
                }
            })}
        </div>
    );
}

/** El texto de una página sin marcas (para buscar y para el extracto de un resultado). */
export function textoPlano(fuente: string): string {
    const limpiar = (t: string) =>
        t
            .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, "$1")
            .replace(/\*\*([^*]+)\*\*/g, "$1")
            .replace(/`([^`]+)`/g, "$1");
    return bloquesManual(fuente)
        .flatMap((b) => {
            switch (b.tipo) {
                case "lista":
                    return b.elementos.flatMap((e) => [limpiar(e.texto), ...(e.sublista?.elementos.map(limpiar) ?? [])]);
                case "codigo":
                    return [b.texto];
                default:
                    return [limpiar(b.texto)];
            }
        })
        .join(" ");
}

/** Claves internas que enlaza una página (para comprobar que todas existen). */
export function enlacesInternos(fuente: string): (string | null)[] {
    const claves: (string | null)[] = [];
    for (const m of fuente.matchAll(/\[[^\]]+\]\(([^)\s]+)\)/g)) {
        const d = destinoEnlace(m[1] ?? "");
        if (d?.tipo === "interno") claves.push(d.clave);
    }
    return claves;
}
