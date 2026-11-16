"use client";

import { avisar, Aviso, Boton, DialogoConfirmar, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { Pencil, Plus, Power, Tags } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { ChipCategoria } from "@/componentes/asistente/chip-categoria";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { activarCategoria, leerCategorias } from "@/lib/asistente/api-fase3";
import { minutosLegibles, resumenModo, type Categoria } from "@/lib/asistente/fase3";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { leerApoyo, type Apoyo } from "./datos";

const BASE = "/administracion/asistente/categorias";

/**
 * `/administracion/asistente/categorias`: los tipos de mensaje que el asistente reconoce
 * (contratos/conversaciones.md §17), cada uno con su cola, su tiempo de primera respuesta, sus
 * manuales base y lo que el asistente hace solo.
 */
export function ListaCategorias() {
    const lectura = useLectura(async (signal) => {
        const [categorias, apoyo] = await Promise.all([leerCategorias(true, signal), leerApoyo(signal)]);
        return { categorias, apoyo };
    });
    let contenido: ReactNode;
    if (lectura.estado === "cargando") {
        contenido = (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando categorías…
                </span>
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
            </div>
        );
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las categorías" />;
    } else if (lectura.datos.categorias.length === 0) {
        contenido = (
            <Tarjeta>
                <EstadoVacio icono={Tags} titulo="Aún no hay categorías" descripcion="Con categorías, el asistente clasifica cada mensaje nuevo y lo manda a la cola que toca." />
            </Tarjeta>
        );
    } else {
        const { categorias, apoyo } = lectura.datos;
        contenido = (
            <ul aria-label="Categorías" className="grid gap-3">
                {categorias.map((c) => (
                    <FilaCategoria key={c.id} categoria={c} apoyo={apoyo} alCambiar={() => void lectura.refrescar()} />
                ))}
            </ul>
        );
    }
    return (
        <>
            <EncabezadoPagina
                titulo="Categorías de mensajes"
                descripcion="El asistente clasifica cada mensaje nuevo. Cada categoría tiene su cola, su tiempo de respuesta y lo que el asistente hace solo."
                acciones={
                    <Boton asChild>
                        <Link href={`${BASE}/nueva`}>
                            <Plus />
                            Agregar categoría
                        </Link>
                    </Boton>
                }
            />
            {contenido}
        </>
    );
}

function FilaCategoria({ categoria: c, apoyo, alCambiar }: { categoria: Categoria; apoyo: Apoyo; alCambiar: () => void }) {
    const clave = useClaveIdempotencia();
    const [confirmar, setConfirmar] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const cola = apoyo.colas.find((x) => x.id === c.colaId)?.nombre;
    const manuales = (c.manualIds ?? []).map((id) => apoyo.manuales.find((m) => m.id === id)?.titulo).filter((t): t is string => Boolean(t));

    async function cambiar(activa: boolean) {
        setError(null);
        try {
            await activarCategoria(c.id, activa, c.version, clave.actual());
            clave.renovar();
            avisar({ titulo: activa ? `Categoría «${c.nombre}» activada` : `Categoría «${c.nombre}» desactivada` });
            setConfirmar(false);
            alCambiar();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }
    const motivo = (e: unknown) => (e instanceof ErrorApi && e.codigo === "demasiadas_categorias" ? "Ya hay 30 categorías activas: desactiva otra primero." : mensajeDeError(e).titulo);

    return (
        <li data-categoria={c.id} className="surface-card grid min-w-0 gap-2 p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
            <div className="grid min-w-0 gap-1.5">
                <span className="flex min-w-0 flex-wrap items-center gap-2">
                    <ChipCategoria categoria={c} />
                    {c.activa ? null : <InsigniaEstado tono="neutral" texto="Inactiva" />}
                    {c.esOtro ? <span className="text-xs text-muted-foreground">Aquí cae lo que no se pudo clasificar</span> : null}
                </span>
                <p className="text-sm [overflow-wrap:anywhere]">
                    {cola ? (
                        <>
                            Cola <span className="font-semibold">{cola}</span>
                        </>
                    ) : (
                        "La cola del número"
                    )}
                    {" · "}
                    {c.slaPrimeraRespuestaMin ? (
                        <>
                            Primera respuesta en <span className="font-semibold">{minutosLegibles(c.slaPrimeraRespuestaMin)}</span>
                        </>
                    ) : (
                        "Sin tiempo de primera respuesta"
                    )}
                </p>
                <p className="text-sm text-muted-foreground [overflow-wrap:anywhere]">Base: {manuales.length ? manuales.map((t) => `Manual «${t}»`).join(", ") : "—"}</p>
                <p className="text-sm font-medium [overflow-wrap:anywhere]">{resumenModo(c)}</p>
                {error && !confirmar ? (
                    <Aviso tono="error" titulo="No se pudo cambiar">
                        {motivo(error)}
                    </Aviso>
                ) : null}
            </div>
            <div className="flex flex-wrap gap-2 md:justify-end">
                {/* «Otro» recibe lo que no se pudo clasificar: no se desactiva (422 `categoria_otro`). */}
                {c.esOtro ? null : (
                    <Boton variante="fantasma" tamano="sm" onClick={() => (c.activa ? setConfirmar(true) : void cambiar(true))} aria-label={`${c.activa ? "Desactivar" : "Activar"} ${c.nombre}`}>
                        <Power />
                        {c.activa ? "Desactivar" : "Activar"}
                    </Boton>
                )}
                <Boton asChild variante="contorno" tamano="sm">
                    <Link href={`${BASE}/${c.id}/editar`} aria-label={`Editar ${c.nombre}`}>
                        <Pencil />
                        Editar
                    </Link>
                </Boton>
            </div>
            <DialogoConfirmar
                open={confirmar}
                onOpenChange={setConfirmar}
                titulo={`¿Desactivar «${c.nombre}»?`}
                descripcion="El asistente deja de clasificar mensajes en esta categoría. Las conversaciones que ya la tienen la conservan."
                textoConfirmar="Desactivar"
                peligro
                alConfirmar={() => cambiar(false)}
            >
                {error ? (
                    <Aviso tono="error" titulo="No se pudo desactivar">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
            </DialogoConfirmar>
        </li>
    );
}
