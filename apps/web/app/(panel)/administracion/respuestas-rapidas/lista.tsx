"use client";

import { avisar, Aviso, Boton, CampoBusqueda, DialogoConfirmar, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { Pencil, Plus, Power, PowerOff, Zap } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { avisoEstado } from "@/lib/avisos";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import type { RespuestaRapida } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";

/** Respuestas rápidas de la empresa (`conversaciones.configurar`): las que los agentes usan con un toque o con su `/atajo`. */
export function ListaRespuestasRapidas() {
    const lista = useLectura(async (signal) => {
        const [r, colas] = await Promise.all([
            resolver(api().GET("/api/v1/mensajeria/respuestas-rapidas", { params: { query: { incluirInactivas: true } }, signal })),
            resolver(api().GET("/api/v1/mensajeria/colas", { signal })),
        ]);
        return { respuestas: r.data ?? [], colas: colas.data ?? [] };
    });
    const [q, setQ] = useState("");
    const [cambiar, setCambiar] = useState<RespuestaRapida | null>(null);
    const [error, setError] = useState<unknown>(null);

    async function alternar(r: RespuestaRapida) {
        setError(null);
        try {
            const opciones = { params: { path: { id: r.id }, header: cabeceraIdem(nuevaClaveIdempotencia()) }, body: { version: r.version } };
            await resolver(r.activa ? api().POST("/api/v1/mensajeria/respuestas-rapidas/{id}/desactivar", opciones) : api().POST("/api/v1/mensajeria/respuestas-rapidas/{id}/activar", opciones));
            avisar({ titulo: avisoEstado(r.titulo, !r.activa) });
            setCambiar(null);
            await lista.refrescar();
        } catch (e) {
            setError(e);
        }
    }

    const filtro = q.trim().toLowerCase();
    return (
        <>
            <EncabezadoPagina
                titulo="Respuestas rápidas"
                descripcion="Textos que los agentes mandan con un toque en la bandeja o escribiendo su atajo. {{nombre}} se cambia por el nombre del cliente."
                acciones={
                    <Boton asChild>
                        <Link href="/administracion/respuestas-rapidas/nueva">
                            <Plus />
                            Nueva respuesta
                        </Link>
                    </Boton>
                }
            />
            {lista.estado === "cargando" ? (
                <div className="grid gap-2" aria-busy="true">
                    <Esqueleto className="h-16" />
                    <Esqueleto className="h-16" />
                </div>
            ) : lista.estado === "error" ? (
                <ErrorLectura error={lista.error} reintentar={lista.reintentar} que="las respuestas rápidas" />
            ) : lista.datos.respuestas.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio icono={Zap} titulo="Aún no hay respuestas rápidas" descripcion="Crea las que más se repiten: visita agendada, pedir reinicio del módem, falla en la zona…" />
                </Tarjeta>
            ) : (
                <div className="grid gap-3">
                    <CampoBusqueda etiqueta="Buscar respuestas" etiquetaVisible={false} placeholder="Buscar por atajo, título o texto" valor={q} alCambiar={setQ} className="max-w-sm" />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo cambiar">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <ul className="grid gap-2" aria-label="Respuestas rápidas">
                        {lista.datos.respuestas
                            .filter((r) => !filtro || `${r.atajo} ${r.titulo} ${r.texto}`.toLowerCase().includes(filtro))
                            .map((r) => (
                                <li key={r.id} data-respuesta={r.atajo} className="flex min-w-0 flex-wrap items-center gap-3 rounded-xl border border-border bg-card p-3 sm:flex-nowrap">
                                    <div className="grid min-w-0 flex-1 gap-0.5">
                                        <span className="flex min-w-0 items-center gap-2">
                                            <span className="truncate font-semibold">{r.titulo}</span>
                                            <span className="shrink-0 font-mono text-xs text-muted-foreground">{r.atajo}</span>
                                        </span>
                                        <span className="line-clamp-2 text-sm text-muted-foreground">{r.texto}</span>
                                        <span className="truncate text-xs text-muted-foreground">
                                            {r.colaId ? `Cola ${lista.datos.colas.find((c) => c.id === r.colaId)?.nombre ?? r.colaId}` : "Todas las colas"}
                                        </span>
                                    </div>
                                    <InsigniaEstado tono={r.activa ? "success" : "neutral"} texto={r.activa ? "Activa" : "Inactiva"} />
                                    <div className="flex shrink-0 gap-1">
                                        <Boton asChild variante="contorno" tamano="sm">
                                            <Link href={`/administracion/respuestas-rapidas/${r.id}/editar`}>
                                                <Pencil />
                                                Editar
                                            </Link>
                                        </Boton>
                                        <Boton variante={r.activa ? "peligro" : "secundario"} tamano="sm" onClick={() => (r.activa ? setCambiar(r) : void alternar(r))}>
                                            {r.activa ? <PowerOff /> : <Power />}
                                            {r.activa ? "Desactivar" : "Activar"}
                                        </Boton>
                                    </div>
                                </li>
                            ))}
                    </ul>
                </div>
            )}
            <DialogoConfirmar
                open={cambiar !== null}
                onOpenChange={(a) => !a && setCambiar(null)}
                titulo={`¿Desactivar «${cambiar?.titulo ?? ""}»?`}
                descripcion="Deja de aparecer en la bandeja. Puedes activarla de nuevo cuando quieras."
                textoConfirmar="Desactivar"
                peligro
                alConfirmar={() => (cambiar ? alternar(cambiar) : undefined)}
            />
        </>
    );
}
