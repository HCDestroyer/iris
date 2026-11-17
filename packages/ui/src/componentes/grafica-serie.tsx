"use client";

import { Table2 } from "lucide-react";
import { useEffect, useId, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "./tabla";

/*
 * Serie en el tiempo (una o dos líneas, un solo eje). Reglas de gráficas de Horus:
 *
 * - Un eje: dos medidas solo si comparten unidad y escala (p. ej. dos porcentajes).
 * - Líneas de 2 px con los colores de gráfico del tema (`--hx-grafico-1` y `-2`, validados para
 *   daltonismo en claro y oscuro); el texto va siempre en color de texto, nunca en el de la serie.
 * - Con dos series hay leyenda; cada línea lleva además su último valor al final (identidad nunca
 *   solo por color).
 * - Un tramo sin dato (`null`) no se une ni se rellena: se sombrea y dice «Sin datos».
 * - Cursor: al pasar o tocar muestra el instante y el valor de cada serie; con el teclado, flechas.
 * - «Ver tabla» cambia a los mismos datos en tabla.
 */

export type SerieGrafica = {
    nombre: string;
    /** Color del tema: 1 (principal) o 2. Sigue a la serie, no a su posición. */
    color: 1 | 2;
    /** Un valor por punto; `null`: no hay dato en ese instante. */
    valores: readonly (number | null)[];
};

export type PropsGraficaSerie = {
    titulo: string;
    descripcion?: string;
    /** Una o dos series con la misma unidad. */
    series: readonly SerieGrafica[];
    /** Texto de cada punto (el instante), para el cursor y la tabla. */
    instantes: readonly string[];
    /** Tres marcas del eje horizontal: inicio, medio y fin. */
    marcas: readonly [string, string, string];
    /** Cómo se escribe un valor («64 %», «2.0 s»). */
    formato: (valor: number) => string;
    /** Tope del eje; si no se da, el máximo de los datos redondeado hacia arriba. */
    maximo?: number;
    /** Rellena bajo la línea (solo con una serie). */
    area?: boolean;
    /** Encabezado de la columna del instante en la tabla. */
    tituloInstante?: string;
    className?: string;
};

const ALTO = 190;
const MARGEN = { arriba: 12, abajo: 26, izquierda: 44, derecha: 58 };
const COLOR = { 1: "var(--hx-grafico-1)", 2: "var(--hx-grafico-2)" } as const;

/** Tope «redondo» para el eje: 1, 2, 2.5, 5 o 10 por una potencia de diez. */
export function topeRedondo(maximo: number): number {
    if (!(maximo > 0)) return 1;
    const potencia = 10 ** Math.floor(Math.log10(maximo));
    const fraccion = maximo / potencia;
    const paso = fraccion <= 1 ? 1 : fraccion <= 2 ? 2 : fraccion <= 2.5 ? 2.5 : fraccion <= 5 ? 5 : 10;
    return paso * potencia;
}

/** Tramos seguidos sin ningún dato en ninguna serie: `[desde, hasta]` (índices, inclusive). */
export function tramosSinDatos(series: readonly SerieGrafica[], puntos: number): [number, number][] {
    const tramos: [number, number][] = [];
    let inicio = -1;
    for (let i = 0; i < puntos; i++) {
        const vacio = series.every((s) => s.valores[i] === null || s.valores[i] === undefined);
        if (vacio && inicio < 0) inicio = i;
        if (!vacio && inicio >= 0) {
            tramos.push([inicio, i - 1]);
            inicio = -1;
        }
    }
    if (inicio >= 0) tramos.push([inicio, puntos - 1]);
    return tramos;
}

/** El último punto con dato de una serie, o `null` si no tiene ninguno. */
export function ultimoConDato(valores: readonly (number | null)[]): { indice: number; valor: number } | null {
    for (let i = valores.length - 1; i >= 0; i--) {
        const v = valores[i];
        if (v !== null && v !== undefined) return { indice: i, valor: v };
    }
    return null;
}

function useAncho(): [React.RefObject<HTMLDivElement | null>, number] {
    const ref = useRef<HTMLDivElement | null>(null);
    const [ancho, setAncho] = useState(320);
    useEffect(() => {
        const nodo = ref.current;
        if (!nodo) return;
        const medir = () => setAncho(Math.max(240, Math.floor(nodo.getBoundingClientRect().width)));
        medir();
        if (typeof ResizeObserver === "undefined") return;
        const observador = new ResizeObserver(medir);
        observador.observe(nodo);
        return () => observador.disconnect();
    }, []);
    return [ref, ancho];
}

export function GraficaSerie({ titulo, descripcion, series, instantes, marcas, formato, maximo, area = false, tituloInstante = "Momento", className }: PropsGraficaSerie) {
    const [ref, ancho] = useAncho();
    const [tabla, setTabla] = useState(false);
    const [cursor, setCursor] = useState<number | null>(null);
    const idTitulo = useId();
    const puntos = instantes.length;
    const conDatos = series.some((s) => s.valores.some((v) => v !== null && v !== undefined));
    const mayor = Math.max(0, ...series.flatMap((s) => s.valores.filter((v): v is number => v !== null && v !== undefined)));
    const tope = maximo ?? topeRedondo(mayor);

    const anchoTrazo = ancho - MARGEN.izquierda - MARGEN.derecha;
    const altoTrazo = ALTO - MARGEN.arriba - MARGEN.abajo;
    const x = (i: number) => MARGEN.izquierda + (puntos <= 1 ? anchoTrazo : (i / (puntos - 1)) * anchoTrazo);
    const y = (v: number) => MARGEN.arriba + altoTrazo - (Math.min(Math.max(v, 0), tope) / tope) * altoTrazo;

    /** La línea, cortada donde falta el dato. */
    const trazo = (valores: readonly (number | null)[]) => {
        let d = "";
        let abierto = false;
        valores.forEach((v, i) => {
            if (v === null || v === undefined) {
                abierto = false;
                return;
            }
            d += `${abierto ? "L" : "M"}${x(i).toFixed(1)} ${y(v).toFixed(1)} `;
            abierto = true;
        });
        return d.trim();
    };
    /** El relleno bajo cada tramo continuo. */
    const relleno = (valores: readonly (number | null)[]) => {
        const base = MARGEN.arriba + altoTrazo;
        let d = "";
        let inicio = -1;
        const cerrar = (fin: number) => {
            if (inicio >= 0) d += `L${x(fin).toFixed(1)} ${base} L${x(inicio).toFixed(1)} ${base} Z `;
            inicio = -1;
        };
        valores.forEach((v, i) => {
            if (v === null || v === undefined) return cerrar(i - 1);
            d += `${inicio < 0 ? "M" : "L"}${x(i).toFixed(1)} ${y(v).toFixed(1)} `;
            if (inicio < 0) inicio = i;
        });
        cerrar(valores.length - 1);
        return d.trim();
    };

    const finales = series.map((s) => ({ serie: s, fin: ultimoConDato(s.valores) })).filter((f): f is { serie: SerieGrafica; fin: { indice: number; valor: number } } => f.fin !== null);
    // Las etiquetas del final no se pisan: si dos quedan a menos de 14 px, se separan.
    const posiciones = finales.map((f) => y(f.fin.valor));
    if (posiciones.length === 2 && Math.abs((posiciones[0] ?? 0) - (posiciones[1] ?? 0)) < 14) {
        const medio = ((posiciones[0] ?? 0) + (posiciones[1] ?? 0)) / 2;
        const arriba = (posiciones[0] ?? 0) <= (posiciones[1] ?? 0) ? 0 : 1;
        posiciones[arriba] = medio - 7;
        posiciones[1 - arriba] = medio + 7;
    }

    const sinDatos = tramosSinDatos(series, puntos);
    const mover = (indice: number) => setCursor(Math.min(Math.max(indice, 0), puntos - 1));
    const alPuntero = (e: PointerEvent<SVGRectElement>) => {
        const caja = e.currentTarget.getBoundingClientRect();
        if (caja.width <= 0 || puntos === 0) return;
        mover(Math.round(((e.clientX - caja.left) / caja.width) * (puntos - 1)));
    };
    const alTeclado = (e: KeyboardEvent<SVGSVGElement>) => {
        if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
            e.preventDefault();
            mover((cursor ?? puntos - 1) + (e.key === "ArrowLeft" ? -1 : 1));
        } else if (e.key === "Escape") setCursor(null);
    };
    const xCursor = cursor === null ? 0 : x(cursor);

    return (
        <section aria-labelledby={idTitulo} data-slot="grafica-serie" className={cn("surface-card grid min-w-0 gap-3 p-4 sm:p-5", className)}>
            <header className="flex min-w-0 items-start justify-between gap-3">
                <div className="grid min-w-0 gap-0.5">
                    <h3 id={idTitulo} className="text-[0.9375rem] font-semibold">
                        {titulo}
                    </h3>
                    {descripcion ? <p className="text-[0.8125rem] text-muted-foreground">{descripcion}</p> : null}
                </div>
                <Boton variante="fantasma" tamano="sm" className="-mr-2 shrink-0" aria-pressed={tabla} onClick={() => setTabla((t) => !t)}>
                    <Table2 />
                    {tabla ? "Ver gráfica" : "Ver tabla"}
                </Boton>
            </header>
            {series.length > 1 ? (
                <ul aria-label="Series" className="flex flex-wrap gap-x-4 gap-y-1 text-[0.8125rem] text-muted-foreground">
                    {series.map((s) => (
                        <li key={s.nombre} className="flex items-center gap-1.5">
                            <span aria-hidden className="h-0.5 w-4 rounded-full" style={{ background: COLOR[s.color] }} />
                            {s.nombre}
                        </li>
                    ))}
                </ul>
            ) : null}
            <div ref={ref} className="relative min-w-0">
                {tabla ? (
                    <div className="max-h-64 overflow-y-auto" data-slot="grafica-tabla">
                        <Tabla>
                            <TablaEncabezado>
                                <TablaFila>
                                    <TablaTitulo>{tituloInstante}</TablaTitulo>
                                    {series.map((s) => (
                                        <TablaTitulo key={s.nombre} className="text-right">
                                            {s.nombre}
                                        </TablaTitulo>
                                    ))}
                                </TablaFila>
                            </TablaEncabezado>
                            <TablaCuerpo>
                                {instantes.map((instante, i) => (
                                    <TablaFila key={i}>
                                        <TablaCelda>{instante}</TablaCelda>
                                        {series.map((s) => {
                                            const v = s.valores[i];
                                            return (
                                                <TablaCelda key={s.nombre} className="text-right">
                                                    {v === null || v === undefined ? "Sin datos" : formato(v)}
                                                </TablaCelda>
                                            );
                                        })}
                                    </TablaFila>
                                ))}
                            </TablaCuerpo>
                        </Tabla>
                    </div>
                ) : !conDatos ? (
                    <p className="flex items-center justify-center rounded-lg bg-elevated text-sm text-muted-foreground" style={{ height: ALTO }} data-slot="grafica-sin-datos">
                        Sin datos en este periodo
                    </p>
                ) : (
                    <>
                        <svg
                            width={ancho}
                            height={ALTO}
                            viewBox={`0 0 ${ancho} ${ALTO}`}
                            role="img"
                            aria-label={`${titulo}. ${finales.map((f) => `${f.serie.nombre}: ${formato(f.fin.valor)}`).join(", ")}. Usa las flechas para recorrerla o «Ver tabla».`}
                            tabIndex={0}
                            onKeyDown={alTeclado}
                            onBlur={() => setCursor(null)}
                            className="block max-w-full rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                            {/* Rejilla y eje: discretos. */}
                            {[0, 0.5, 1].map((parte) => {
                                const yy = MARGEN.arriba + altoTrazo * (1 - parte);
                                // La marca del medio no se escribe si diría lo mismo que la de arriba o la de abajo («0, 1, 1»).
                                const repetida = parte === 0.5 && (formato(tope * 0.5) === formato(tope) || formato(tope * 0.5) === formato(0));
                                return (
                                    <g key={parte}>
                                        <line x1={MARGEN.izquierda} x2={ancho - MARGEN.derecha} y1={yy} y2={yy} stroke="var(--hx-border)" strokeWidth={1} strokeDasharray={parte === 0 ? undefined : "2 4"} />
                                        {repetida ? null : (
                                            <text x={MARGEN.izquierda - 8} y={yy + 4} textAnchor="end" fontSize={11} fill="var(--hx-text-muted)" data-slot="marca-eje">
                                                {formato(tope * parte)}
                                            </text>
                                        )}
                                    </g>
                                );
                            })}
                            {marcas.map((m, i) => (
                                <text key={i} x={i === 0 ? MARGEN.izquierda : i === 1 ? MARGEN.izquierda + anchoTrazo / 2 : ancho - MARGEN.derecha} y={ALTO - 6} textAnchor={i === 0 ? "start" : i === 1 ? "middle" : "end"} fontSize={11} fill="var(--hx-text-muted)">
                                    {m}
                                </text>
                            ))}
                            {/* Tramos sin respuesta: sombreados, sin unir la línea. */}
                            {sinDatos.map(([desde, hasta]) => {
                                const x0 = x(Math.max(desde - 1, 0));
                                const x1 = x(Math.min(hasta + (hasta === puntos - 1 ? 0 : 1), puntos - 1));
                                return (
                                    <g key={desde} data-slot="tramo-sin-datos">
                                        <rect x={x0} y={MARGEN.arriba} width={Math.max(x1 - x0, 2)} height={altoTrazo} fill="var(--hx-bg-elevated)" />
                                        {x1 - x0 > 64 ? (
                                            <text x={(x0 + x1) / 2} y={MARGEN.arriba + altoTrazo / 2 + 4} textAnchor="middle" fontSize={12} fill="var(--hx-text-muted)">
                                                Sin datos
                                            </text>
                                        ) : null}
                                    </g>
                                );
                            })}
                            {area && series.length === 1 && series[0] ? <path d={relleno(series[0].valores)} fill={COLOR[series[0].color]} opacity={0.12} /> : null}
                            {series.map((s) => (
                                <path key={s.nombre} d={trazo(s.valores)} fill="none" stroke={COLOR[s.color]} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
                            ))}
                            {/* Último valor de cada serie: punto con anillo de la superficie y su cifra en color de texto. */}
                            {finales.map((f, i) => (
                                <g key={f.serie.nombre}>
                                    <circle cx={x(f.fin.indice)} cy={y(f.fin.valor)} r={4} fill={COLOR[f.serie.color]} stroke="var(--hx-card)" strokeWidth={2} />
                                    {f.fin.indice === puntos - 1 ? (
                                        <text x={ancho - MARGEN.derecha + 9} y={(posiciones[i] ?? 0) + 4} fontSize={12} fontWeight={600} fill="var(--hx-text)">
                                            {formato(f.fin.valor)}
                                        </text>
                                    ) : null}
                                </g>
                            ))}
                            {cursor !== null ? (
                                <g aria-hidden>
                                    <line x1={xCursor} x2={xCursor} y1={MARGEN.arriba} y2={MARGEN.arriba + altoTrazo} stroke="var(--hx-text-muted)" strokeWidth={1} />
                                    {series.map((s) => {
                                        const v = s.valores[cursor];
                                        return v === null || v === undefined ? null : <circle key={s.nombre} cx={xCursor} cy={y(v)} r={4} fill={COLOR[s.color]} stroke="var(--hx-card)" strokeWidth={2} />;
                                    })}
                                </g>
                            ) : null}
                            {/* Zona sensible: más grande que las marcas. */}
                            <rect x={MARGEN.izquierda} y={0} width={anchoTrazo} height={ALTO} fill="transparent" onPointerMove={alPuntero} onPointerDown={alPuntero} onPointerLeave={() => setCursor(null)} />
                        </svg>
                        {cursor !== null ? (
                            <div
                                role="status"
                                data-slot="grafica-cursor"
                                className="pointer-events-none absolute top-0 z-10 grid min-w-28 gap-0.5 rounded-lg border border-border bg-card px-2.5 py-1.5 text-xs shadow-md"
                                style={xCursor > ancho / 2 ? { right: ancho - xCursor + 10 } : { left: xCursor + 10 }}
                            >
                                <span className="font-medium text-muted-foreground">{instantes[cursor]}</span>
                                {series.map((s) => {
                                    const v = s.valores[cursor];
                                    return (
                                        <span key={s.nombre} className="flex items-center gap-1.5 tabular-nums">
                                            <span aria-hidden className="h-0.5 w-3 rounded-full" style={{ background: COLOR[s.color] }} />
                                            <span className="text-muted-foreground">{s.nombre}</span>
                                            <span className="ml-auto font-semibold">{v === null || v === undefined ? "Sin datos" : formato(v)}</span>
                                        </span>
                                    );
                                })}
                            </div>
                        ) : null}
                    </>
                )}
            </div>
        </section>
    );
}

export type PropsMedidor = {
    /** Parte usada, de 0 a 1; `null`: no hay dato (no se dibuja relleno). */
    parte: number | null;
    /** Qué mide, para lectores de pantalla («Memoria de video»). */
    etiqueta: string;
    /** Cómo se lee el valor («13.1 de 24 GB»). */
    texto: string;
    /** Color por estado: normal (marca), aviso o error. Va siempre con su texto al lado. */
    tono?: "normal" | "aviso" | "error";
    className?: string;
};

/** Barra de «cuánto de cuánto»: un `meter` con el relleno anclado a la izquierda. */
export function Medidor({ parte, etiqueta, texto, tono = "normal", className }: PropsMedidor) {
    const valor = parte === null ? null : Math.min(Math.max(parte, 0), 1);
    return (
        <div
            role="meter"
            aria-label={etiqueta}
            aria-valuemin={0}
            aria-valuemax={100}
            {...(valor === null ? {} : { "aria-valuenow": Math.round(valor * 100) })}
            aria-valuetext={texto}
            data-slot="medidor"
            data-tono={tono}
            className={cn("h-1.5 w-full overflow-hidden rounded-full bg-elevated", className)}
        >
            {valor === null ? null : <div className={cn("h-full rounded-full", tono === "error" ? "bg-error" : tono === "aviso" ? "bg-warning" : "bg-primary")} style={{ width: `${valor * 100}%` }} />}
        </div>
    );
}
