"use client";

import { avisar, Aviso, Boton, Cargando, Casilla, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie } from "@iris/ui";
import { BookPlus, RotateCw } from "lucide-react";
import { useRef, useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { claveManualInicio, entradaDe, MANUALES_INICIO, NOMBRE_TECNOLOGIA, textoResultadoCarga, yaExistentes, type PlantillaManual } from "@/lib/manuales/inicio";
import { useSesion } from "@/lib/sesion";

const LIMITE_LISTA = 100;

/**
 * Títulos de la biblioteca (activos o no) que coinciden con los manuales de inicio. La lista sin
 * búsqueda trae hasta 100; si la biblioteca es más grande, cada plantilla que no apareció se busca
 * por su título (búsqueda de texto completo del servidor).
 */
async function titulosEnBiblioteca(signal?: AbortSignal): Promise<string[]> {
    const opciones = (query: { q?: string }) => ({ params: { query: { ...query, incluirInactivos: true, limit: LIMITE_LISTA } }, ...(signal ? { signal } : {}) });
    const todos = await resolver(api().GET("/api/v1/mensajeria/manuales", opciones({})));
    const titulos = todos.data.map((m) => m.titulo);
    if (todos.data.length < LIMITE_LISTA) return titulos;
    const vistos = yaExistentes(MANUALES_INICIO, titulos);
    for (const p of MANUALES_INICIO) {
        if (vistos.has(p.codigo)) continue;
        const r = await resolver(api().GET("/api/v1/mensajeria/manuales", opciones({ q: p.titulo })));
        titulos.push(...r.data.map((m) => m.titulo));
    }
    return titulos;
}

type Avance = { hechos: number; total: number; actual: string | null };

/**
 * «Cargar manuales de inicio» (`manuales.editar`): diálogo con los ejemplos para un ISP con fibra
 * (FTTH) e inalámbrico (WISP), todos marcados salvo los que ya están en la biblioteca. Los crea uno
 * por llamada con su `Idempotency-Key` estable; vuelve a revisar los títulos justo antes, así que
 * cargarlos otra vez no duplica nada.
 */
export function CargarManualesInicio({ alCargar }: { alCargar: () => void }) {
    const [abierto, setAbierto] = useState(false);
    const [ocupado, setOcupado] = useState(false);
    return (
        <>
            <Boton variante="secundario" onClick={() => setAbierto(true)}>
                <BookPlus />
                Cargar manuales de inicio
            </Boton>
            <Dialogo open={abierto} onOpenChange={(a) => !ocupado && setAbierto(a)}>
                {abierto ? (
                    <ContenidoCarga
                        alOcupar={setOcupado}
                        alTerminar={() => {
                            setAbierto(false);
                            alCargar();
                        }}
                    />
                ) : null}
            </Dialogo>
        </>
    );
}

function ContenidoCarga({ alOcupar, alTerminar }: { alOcupar: (ocupado: boolean) => void; alTerminar: () => void }) {
    const { yo } = useSesion();
    const empresaId = yo.empresaActiva?.id;
    const existentes = useLectura(async (signal) => yaExistentes(MANUALES_INICIO, await titulosEnBiblioteca(signal)));
    // Desmarcados a mano (por código): lo demás que falta en la biblioteca va marcado.
    const [quitados, setQuitados] = useState<ReadonlySet<string>>(new Set());
    const [avance, setAvance] = useState<Avance | null>(null);
    const [error, setError] = useState<unknown>(null);
    // Lo ya hecho en esta carga (sobrevive a «Reintentar» para el aviso final).
    const hecho = useRef<{ creados: number; omitidos: string[]; listos: Set<string> }>({ creados: 0, omitidos: [], listos: new Set() });

    const ocupado = avance !== null && error === null;
    const disponibles = existentes.estado === "listo" ? MANUALES_INICIO.filter((p) => !existentes.datos.has(p.codigo)) : [];
    const elegidos = disponibles.filter((p) => !quitados.has(p.codigo));

    async function cargar(lista: readonly PlantillaManual[]) {
        setError(null);
        alOcupar(true);
        setAvance({ hechos: hecho.current.listos.size, total: lista.length, actual: null });
        try {
            // Justo antes: alguien pudo crear alguno mientras el diálogo estaba abierto.
            const ya = yaExistentes(lista, await titulosEnBiblioteca());
            for (const p of lista) {
                if (hecho.current.listos.has(p.codigo)) continue;
                setAvance({ hechos: hecho.current.listos.size, total: lista.length, actual: p.titulo });
                if (ya.has(p.codigo)) {
                    hecho.current.omitidos.push(p.titulo);
                } else {
                    const llamada = api().POST("/api/v1/mensajeria/manuales", {
                        params: { header: { "Idempotency-Key": claveManualInicio(empresaId, p) } },
                        body: entradaDe(p),
                    });
                    await resolver(llamada);
                    // Respuesta repetida (misma clave en las últimas 24 h): ya se había creado y luego
                    // se le cambió el título. No es uno nuevo.
                    if ((await llamada).response.headers.get("Idempotent-Replayed")) hecho.current.omitidos.push(p.titulo);
                    else hecho.current.creados += 1;
                }
                hecho.current.listos.add(p.codigo);
            }
            const { creados, omitidos } = hecho.current;
            avisar({ tono: creados > 0 ? "success" : "info", ...textoResultadoCarga(creados, omitidos), duracion: omitidos.length > 0 ? 8_000 : 5_000 });
            alOcupar(false);
            alTerminar();
        } catch (e) {
            setError(e);
            alOcupar(false);
        }
    }

    const lista = avance ? elegidos : null;
    return (
        <DialogoContenido
            titulo="Cargar manuales de inicio"
            descripcion="Ejemplos editables para un ISP con fibra (FTTH) e inalámbrico (WISP), escritos para el cliente. Se crean «Para el cliente»: antes de enviarlos, edítalos y cambia lo que va entre corchetes ([Nombre del ISP], [número de soporte]…) por los datos de tu empresa."
            cerrable={!ocupado}
        >
            {existentes.estado === "cargando" ? (
                <Cargando texto="Revisando la biblioteca…" />
            ) : existentes.estado === "error" ? (
                <ErrorLectura error={existentes.error} reintentar={existentes.reintentar} que="la biblioteca" />
            ) : (
                <>
                    {disponibles.length === 0 ? (
                        <Aviso tono="info" titulo="Ya están todos en la biblioteca">
                            Los {MANUALES_INICIO.length} manuales de inicio ya se cargaron. Puedes editarlos desde la lista.
                        </Aviso>
                    ) : null}
                    <fieldset className="grid min-w-0 gap-2" disabled={ocupado}>
                        <legend className="mb-2 flex w-full flex-wrap items-center justify-between gap-2 text-sm font-medium">
                            <span>
                                {elegidos.length} de {disponibles.length} {disponibles.length === 1 ? "elegido" : "elegidos"}
                            </span>
                            {disponibles.length > 0 ? (
                                <Boton
                                    variante="fantasma"
                                    tamano="sm"
                                    disabled={ocupado}
                                    onClick={() => setQuitados(elegidos.length === disponibles.length ? new Set(disponibles.map((p) => p.codigo)) : new Set())}
                                >
                                    {elegidos.length === disponibles.length ? "Quitar todos" : "Marcar todos"}
                                </Boton>
                            ) : null}
                        </legend>
                        <ul className="grid gap-1" aria-label="Manuales de inicio">
                            {MANUALES_INICIO.map((p) => {
                                const esta = existentes.datos.has(p.codigo);
                                return (
                                    <li key={p.codigo} data-manual-inicio={p.codigo} className="rounded-lg border border-border px-3 py-2">
                                        <Casilla
                                            etiqueta={p.titulo}
                                            ayuda={esta ? `${NOMBRE_TECNOLOGIA[p.tecnologia]} · Ya está en la biblioteca` : NOMBRE_TECNOLOGIA[p.tecnologia]}
                                            checked={!esta && !quitados.has(p.codigo)}
                                            disabled={esta || ocupado}
                                            onChange={(e) => {
                                                const marcar = e.target.checked;
                                                setQuitados((q) => {
                                                    const s = new Set(q);
                                                    if (marcar) s.delete(p.codigo);
                                                    else s.add(p.codigo);
                                                    return s;
                                                });
                                            }}
                                        />
                                    </li>
                                );
                            })}
                        </ul>
                    </fieldset>
                    {avance && lista ? (
                        <div className="grid gap-1.5">
                            <p role="status" className="text-sm" data-slot="avance-carga">
                                {error ? `Se detuvo en ${avance.hechos + 1} de ${avance.total}.` : avance.actual ? `Creando ${Math.min(avance.hechos + 1, avance.total)} de ${avance.total}: «${avance.actual}»` : "Revisando la biblioteca…"}
                            </p>
                            <span
                                role="progressbar"
                                aria-label="Avance de la carga"
                                aria-valuemin={0}
                                aria-valuemax={avance.total}
                                aria-valuenow={avance.hechos}
                                className="h-1.5 overflow-hidden rounded-full bg-elevated"
                            >
                                <span className="block h-full rounded-full bg-primary transition-[width] motion-reduce:transition-none" style={{ width: `${avance.total > 0 ? (avance.hechos / avance.total) * 100 : 0}%` }} />
                            </span>
                        </div>
                    ) : null}
                    {error && lista ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudieron cargar todos"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={() => void cargar(lista)}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        >
                            <p>{mensajeDeError(error).titulo}</p>
                            <p>Los que ya se crearon no se repiten al reintentar.</p>
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="fantasma" disabled={ocupado}>
                                {disponibles.length === 0 ? "Cerrar" : "Cancelar"}
                            </Boton>
                        </DialogoCerrar>
                        {disponibles.length > 0 && !error ? (
                            <Boton disabled={ocupado || elegidos.length === 0} onClick={() => void cargar(elegidos)}>
                                <BookPlus />
                                {ocupado ? "Cargando…" : elegidos.length === 1 ? "Cargar 1 manual" : `Cargar ${elegidos.length} manuales`}
                            </Boton>
                        ) : null}
                    </DialogoPie>
                </>
            )}
        </DialogoContenido>
    );
}
