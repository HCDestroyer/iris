import { Fragment, type ReactNode } from "react";

/*
 * Markdown seguro de los manuales (contratos/conversaciones.md §11): el cuerpo se guarda tal cual y
 * el panel lo muestra **sin HTML crudo**. No hay `dangerouslySetInnerHTML`: el texto se arma como
 * nodos de React, así que `<script>`, `<img onerror>` o `<a href="javascript:">` salen como texto.
 *
 * Subconjunto: títulos `#`…`###`, párrafos, listas `-`/`*`/`1.`, citas `>`, bloques ``` y en línea
 * **negrita**, *cursiva* / _cursiva_, `código` y [enlaces](https://…) (solo http, https y mailto;
 * cualquier otro esquema queda como texto). Las imágenes `![alt](url)` se muestran como enlace.
 */

type Bloque =
    | { tipo: "titulo"; nivel: 1 | 2 | 3; texto: string }
    | { tipo: "parrafo"; texto: string }
    | { tipo: "lista"; ordenada: boolean; elementos: string[] }
    | { tipo: "cita"; texto: string }
    | { tipo: "codigo"; texto: string };

export function bloquesMarkdown(fuente: string): Bloque[] {
    const lineas = fuente.replace(/\r\n?/g, "\n").split("\n");
    const bloques: Bloque[] = [];
    let i = 0;
    while (i < lineas.length) {
        const linea = lineas[i] ?? "";
        if (linea.trim() === "") {
            i++;
            continue;
        }
        if (linea.trimStart().startsWith("```")) {
            const codigo: string[] = [];
            i++;
            while (i < lineas.length && !(lineas[i] ?? "").trimStart().startsWith("```")) {
                codigo.push(lineas[i] ?? "");
                i++;
            }
            i++;
            bloques.push({ tipo: "codigo", texto: codigo.join("\n") });
            continue;
        }
        const titulo = /^(#{1,6})\s+(.*)$/.exec(linea);
        if (titulo) {
            const nivel = Math.min(titulo[1]?.length ?? 1, 3) as 1 | 2 | 3;
            bloques.push({ tipo: "titulo", nivel, texto: (titulo[2] ?? "").replace(/\s#+\s*$/, "") });
            i++;
            continue;
        }
        if (/^\s*>/.test(linea)) {
            const cita: string[] = [];
            while (i < lineas.length && /^\s*>/.test(lineas[i] ?? "")) {
                cita.push((lineas[i] ?? "").replace(/^\s*>\s?/, ""));
                i++;
            }
            bloques.push({ tipo: "cita", texto: cita.join(" ") });
            continue;
        }
        const viñeta = /^\s*([-*+]|\d+[.)])\s+/;
        if (viñeta.test(linea)) {
            const ordenada = /^\s*\d/.test(linea);
            const elementos: string[] = [];
            while (i < lineas.length && viñeta.test(lineas[i] ?? "")) {
                elementos.push((lineas[i] ?? "").replace(viñeta, ""));
                i++;
                // Continuación con sangría.
                while (i < lineas.length && /^\s{2,}\S/.test(lineas[i] ?? "") && !viñeta.test(lineas[i] ?? "")) {
                    elementos[elementos.length - 1] += ` ${(lineas[i] ?? "").trim()}`;
                    i++;
                }
            }
            bloques.push({ tipo: "lista", ordenada, elementos });
            continue;
        }
        const parrafo: string[] = [];
        while (
            i < lineas.length &&
            (lineas[i] ?? "").trim() !== "" &&
            !/^(#{1,6})\s/.test(lineas[i] ?? "") &&
            !/^\s*>/.test(lineas[i] ?? "") &&
            !viñeta.test(lineas[i] ?? "") &&
            !(lineas[i] ?? "").trimStart().startsWith("```")
        ) {
            parrafo.push((lineas[i] ?? "").trim());
            i++;
        }
        bloques.push({ tipo: "parrafo", texto: parrafo.join(" ") });
    }
    return bloques;
}

/** Solo enlaces web y correo; lo demás (javascript:, data:, rutas relativas) no es enlace. */
export function urlSegura(url: string): string | null {
    const limpia = url.trim();
    try {
        const u = new URL(limpia);
        return u.protocol === "https:" || u.protocol === "http:" || u.protocol === "mailto:" ? u.href : null;
    } catch {
        return null;
    }
}

const EN_LINEA = /(\*\*([^*]+)\*\*)|(`([^`]+)`)|(!?\[([^\]]*)\]\(([^)\s]+)\))|(\*([^*\s][^*]*)\*)|(_([^_\s][^_]*)_)/;

function enLinea(texto: string, clave: string): ReactNode[] {
    const salida: ReactNode[] = [];
    let resto = texto;
    let n = 0;
    while (resto.length > 0) {
        const m = EN_LINEA.exec(resto);
        if (!m) {
            salida.push(resto);
            break;
        }
        if (m.index > 0) {
            salida.push(resto.slice(0, m.index));
        }
        const k = `${clave}-${n++}`;
        if (m[1]) {
            salida.push(<strong key={k}>{enLinea(m[2] ?? "", k)}</strong>);
        } else if (m[3]) {
            salida.push(
                <code key={k} className="rounded bg-background-secondary px-1 font-mono text-[0.85em]">
                    {m[4]}
                </code>,
            );
        } else if (m[5]) {
            const href = urlSegura(m[7] ?? "");
            const etiqueta = m[6] || m[7] || "";
            salida.push(
                href ? (
                    <a key={k} href={href} target="_blank" rel="noopener noreferrer nofollow" className="text-primary underline underline-offset-2">
                        {etiqueta}
                    </a>
                ) : (
                    <Fragment key={k}>{m[0]}</Fragment>
                ),
            );
        } else if (m[8]) {
            salida.push(<em key={k}>{enLinea(m[9] ?? "", k)}</em>);
        } else if (m[10]) {
            salida.push(<em key={k}>{enLinea(m[11] ?? "", k)}</em>);
        }
        resto = resto.slice(m.index + m[0].length);
    }
    return salida;
}

/** El cuerpo de un manual como nodos de React (nunca HTML). */
export function MarkdownSeguro({ fuente, className }: { fuente: string; className?: string }) {
    const bloques = bloquesMarkdown(fuente);
    return (
        <div className={["grid gap-3 text-sm leading-relaxed [overflow-wrap:anywhere]", className].filter(Boolean).join(" ")}>
            {bloques.map((b, i) => {
                const k = `b${i}`;
                switch (b.tipo) {
                    case "titulo": {
                        const clases = b.nivel === 1 ? "text-lg font-semibold" : b.nivel === 2 ? "text-base font-semibold" : "text-sm font-semibold";
                        const Etiqueta = b.nivel === 1 ? "h3" : b.nivel === 2 ? "h4" : "h5";
                        return (
                            <Etiqueta key={k} className={clases}>
                                {enLinea(b.texto, k)}
                            </Etiqueta>
                        );
                    }
                    case "lista": {
                        const Lista = b.ordenada ? "ol" : "ul";
                        return (
                            <Lista key={k} className={b.ordenada ? "grid list-decimal gap-1 pl-5" : "grid list-disc gap-1 pl-5"}>
                                {b.elementos.map((e, j) => (
                                    <li key={`${k}-${j}`}>{enLinea(e, `${k}-${j}`)}</li>
                                ))}
                            </Lista>
                        );
                    }
                    case "cita":
                        return (
                            <blockquote key={k} className="border-l-2 border-border pl-3 text-muted-foreground">
                                {enLinea(b.texto, k)}
                            </blockquote>
                        );
                    case "codigo":
                        return (
                            <pre key={k} className="overflow-x-auto rounded-lg bg-background-secondary p-3 font-mono text-xs">
                                {b.texto}
                            </pre>
                        );
                    default:
                        return <p key={k}>{enLinea(b.texto, k)}</p>;
                }
            })}
        </div>
    );
}

/**
 * El cuerpo en texto plano para «Usar en la respuesta» (WhatsApp no muestra Markdown): sin
 * marcas de título ni de cita, viñetas como «• », enlaces como «texto (url)».
 */
export function markdownATexto(fuente: string): string {
    const limpiarEnLinea = (t: string) =>
        t
            .replace(/!?\[([^\]]*)\]\(([^)\s]+)\)/g, (_m, txt: string, url: string) => (txt && txt !== url ? `${txt} (${url})` : url))
            .replace(/\*\*([^*]+)\*\*/g, "$1")
            .replace(/`([^`]+)`/g, "$1")
            .replace(/(^|\W)\*([^*\s][^*]*)\*/g, "$1$2")
            .replace(/(^|\W)_([^_\s][^_]*)_/g, "$1$2");
    return bloquesMarkdown(fuente)
        .map((b) => {
            switch (b.tipo) {
                case "lista":
                    return b.elementos.map((e, i) => `${b.ordenada ? `${i + 1}.` : "•"} ${limpiarEnLinea(e)}`).join("\n");
                case "codigo":
                    return b.texto;
                default:
                    return limpiarEnLinea(b.texto);
            }
        })
        .join("\n\n");
}
