"use client";

import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, CampoBusqueda, Cargando, Dialogo, DialogoContenido, DialogoConfirmar, Esqueleto, EstadoVacio, GrupoSegmentado, InsigniaEstado, Tarjeta } from "@iris/ui";
import { BookOpen, Eye, Pencil, Plus, Power, PowerOff, Users } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { MarkdownSeguro } from "@/componentes/manuales/markdown";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { avisoEstado } from "@/lib/avisos";
import { useDiferido } from "@/lib/diferido";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type Manual = Schema<"ManualResumen">;
type Visibilidad = "todas" | "interna" | "cliente";

/**
 * Biblioteca de manuales (§11): búsqueda de texto completo en el servidor, filtro por visibilidad,
 * «Ver» con la vista segura del Markdown y, con `manuales.editar`, crear, editar y desactivar.
 */
export function BibliotecaManuales() {
    const { puede } = useSesion();
    const edita = puede(["manuales.editar"]);
    const [ver, setVer] = useState<Manual | null>(null);
    const params = useSearchParams();
    const router = useRouter();
    const ruta = usePathname();
    const [texto, setTexto] = useState(params.get("q") ?? "");
    const q = useDiferido(texto.trim(), 300);
    const visibilidad: Visibilidad = params.get("visibilidad") === "interna" || params.get("visibilidad") === "cliente" ? (params.get("visibilidad") as Visibilidad) : "todas";
    useEffect(() => {
        const p = new URLSearchParams(params.toString());
        if (q) p.set("q", q);
        else p.delete("q");
        if (p.toString() !== params.toString()) router.replace(`${ruta}${p.toString() ? `?${p}` : ""}`, { scroll: false });
    }, [q, params, router, ruta]);

    return (
        <>
            <EncabezadoPagina
                titulo="Biblioteca de manuales"
                descripcion="Guías para el equipo y artículos que se mandan al cliente desde la bandeja. La bandeja sugiere los que comparten etiqueta, equipo o asunto con la conversación."
                acciones={
                    edita ? (
                        <Boton asChild>
                            <Link href="/administracion/manuales/nuevo">
                                <Plus />
                                Nuevo manual
                            </Link>
                        </Boton>
                    ) : null
                }
            />
            <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
                <CampoBusqueda etiqueta="Buscar en los manuales" etiquetaVisible={false} placeholder="Buscar: ONU, reinicio, potencia…" valor={texto} alCambiar={setTexto} maxLength={100} className="w-full min-w-0 sm:max-w-sm sm:flex-1" />
                <GrupoSegmentado
                    etiqueta="Visibilidad"
                    valor={visibilidad}
                    alCambiar={(v) => {
                        const p = new URLSearchParams(params.toString());
                        if (v === "todas") p.delete("visibilidad");
                        else p.set("visibilidad", v);
                        router.replace(`${ruta}${p.toString() ? `?${p}` : ""}`, { scroll: false });
                    }}
                    opciones={[
                        { valor: "todas", etiqueta: "Todos" },
                        { valor: "interna", etiqueta: "Del equipo" },
                        { valor: "cliente", etiqueta: "Para el cliente" },
                    ]}
                />
            </div>
            <Resultados key={`${q}|${visibilidad}`} q={q} visibilidad={visibilidad} edita={edita} alVer={setVer} />
            {/* Fuera de los resultados: cambiar la búsqueda no cierra el manual abierto. */}
            <VerManual manual={ver} cerrar={() => setVer(null)} />
        </>
    );
}

/** Resultados de una búsqueda: se remonta con cada búsqueda o filtro (y vuelve a leer). */
function Resultados({ q, visibilidad, edita, alVer: setVer }: { q: string; visibilidad: Visibilidad; edita: boolean; alVer: (m: Manual) => void }) {
    const lista = useLectura(async (signal) => {
        const r = await resolver(
            api().GET("/api/v1/mensajeria/manuales", { params: { query: { ...(q ? { q } : {}), ...(visibilidad !== "todas" ? { visibilidad } : {}), incluirInactivos: true } }, signal }),
        );
        return r.data ?? [];
    });
    const [desactivar, setDesactivar] = useState<Manual | null>(null);
    const [error, setError] = useState<unknown>(null);

    async function alternar(m: Manual) {
        setError(null);
        try {
            const opciones = { params: { path: { id: m.id }, header: { "Idempotency-Key": nuevaClaveIdempotencia() } } };
            await resolver(m.activo ? api().POST("/api/v1/mensajeria/manuales/{id}/desactivar", opciones) : api().POST("/api/v1/mensajeria/manuales/{id}/activar", opciones));
            avisar({ titulo: avisoEstado(m.titulo, !m.activo) });
            setDesactivar(null);
            await lista.refrescar();
        } catch (e) {
            setError(e);
        }
    }

    return (
        <>
            {error ? (
                <Aviso tono="error" titulo="No se pudo cambiar" className="mb-3">
                    {mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            {lista.estado === "cargando" ? (
                <div className="grid gap-2" aria-busy="true">
                    <Esqueleto className="h-20" />
                    <Esqueleto className="h-20" />
                </div>
            ) : lista.estado === "error" ? (
                <ErrorLectura error={lista.error} reintentar={lista.reintentar} que="los manuales" />
            ) : lista.datos.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio icono={BookOpen} titulo={q ? `Nada coincide con «${q}»` : "La biblioteca está vacía"} descripcion={q ? "Prueba con otra palabra o con el modelo del equipo." : "Escribe el primer manual: diagnóstico sin servicio, reiniciar la ONU, cuándo agendar visita…"} />
                </Tarjeta>
            ) : (
                <ul className="grid gap-2" aria-label="Manuales">
                    {lista.datos.map((m) => (
                        <li key={m.id} data-manual={m.id} className="flex min-w-0 flex-wrap items-start gap-3 rounded-xl border border-border bg-card p-3 sm:flex-nowrap">
                            <BookOpen aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                            <div className="grid min-w-0 flex-1 gap-1">
                                <span className="flex min-w-0 flex-wrap items-center gap-2">
                                    <span className="min-w-0 truncate font-semibold">{m.titulo}</span>
                                    <InsigniaEstado tono={m.visibilidad === "cliente" ? "info" : "neutral"} icono={m.visibilidad === "cliente" ? Users : BookOpen} texto={m.visibilidad === "cliente" ? "Para el cliente" : "Del equipo"} />
                                    {!m.activo ? <InsigniaEstado tono="neutral" texto="Inactivo" /> : null}
                                </span>
                                <span className="line-clamp-2 text-sm text-muted-foreground">{m.resumen}</span>
                                <span className="flex flex-wrap gap-1">
                                    {m.equipo ? <span className="rounded-md bg-elevated px-1.5 font-mono text-xs">{m.equipo}</span> : null}
                                    {(m.etiquetas ?? []).map((e) => (
                                        <span key={e} className="rounded-md bg-elevated px-1.5 text-xs">
                                            {e}
                                        </span>
                                    ))}
                                </span>
                            </div>
                            <div className="flex shrink-0 flex-wrap gap-1">
                                <Boton variante="contorno" tamano="sm" onClick={() => setVer(m)}>
                                    <Eye />
                                    Ver
                                </Boton>
                                {edita ? (
                                    <>
                                        <Boton asChild variante="contorno" tamano="sm">
                                            <Link href={`/administracion/manuales/${m.id}/editar`} aria-label={`Editar ${m.titulo}`}>
                                                <Pencil />
                                                Editar
                                            </Link>
                                        </Boton>
                                        <Boton variante={m.activo ? "peligro" : "secundario"} tamano="sm" onClick={() => (m.activo ? setDesactivar(m) : void alternar(m))} aria-label={`${m.activo ? "Desactivar" : "Activar"} ${m.titulo}`}>
                                            {m.activo ? <PowerOff /> : <Power />}
                                        </Boton>
                                    </>
                                ) : null}
                            </div>
                        </li>
                    ))}
                </ul>
            )}
            <DialogoConfirmar
                open={desactivar !== null}
                onOpenChange={(a) => !a && setDesactivar(null)}
                titulo={`¿Desactivar «${desactivar?.titulo ?? ""}»?`}
                descripcion="Deja de sugerirse en la bandeja y sus enlaces enviados al cliente dejan de abrir."
                textoConfirmar="Desactivar"
                peligro
                alConfirmar={() => (desactivar ? alternar(desactivar) : undefined)}
            />
        </>
    );
}

/** Leer un manual: lo lee entero (`GET /manuales/{id}`) y muestra el cuerpo con la vista segura del Markdown (sin HTML crudo). */
export function VerManual({ manual, cerrar }: { manual: Pick<Manual, "id" | "titulo" | "resumen"> | null; cerrar: () => void }) {
    return (
        <Dialogo open={manual !== null} onOpenChange={(a) => !a && cerrar()}>
            {manual ? (
                <DialogoContenido titulo={manual.titulo} descripcion={manual.resumen} className="sm:max-w-2xl">
                    <CuerpoManual id={manual.id} />
                </DialogoContenido>
            ) : null}
        </Dialogo>
    );
}

function CuerpoManual({ id }: { id: number }) {
    const lectura = useLectura(async (signal) => resolver(api().GET("/api/v1/mensajeria/manuales/{id}", { params: { path: { id } }, signal })));
    if (lectura.estado === "cargando") return <Cargando texto="Cargando el manual…" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el manual" />;
    return <MarkdownSeguro fuente={lectura.datos.cuerpo} />;
}
