"use client";

import { avisar, Aviso, Boton, DialogoConfirmar, Esqueleto, InsigniaEstado } from "@iris/ui";
import { CalendarClock, Pencil, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustesAsistente, leerAjustesAsistente } from "@/lib/asistente/api";
import type { PerfilAsistente } from "@/lib/asistente/fase3";
import { entradaPerfiles, esDefecto, MAX_PERFILES, perfilesDe, perfilesSin, resumenPerfil } from "@/lib/asistente/fase3b";
import type { AjustesAsistente } from "@/lib/asistente/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const BASE = "/administracion/asistente/perfiles";

/** Horarios que nombran cada perfil en alguna franja; `null` si no se pudieron leer (otro permiso). */
type Usos = Map<string, string[]> | null;

async function leerUsos(signal: AbortSignal): Promise<Usos> {
    try {
        const r = await resolver(api().GET("/api/v1/mensajeria/horarios", { signal }));
        const usos = new Map<string, string[]>();
        for (const h of r.data ?? []) {
            const codigos = new Set([...(h.franjas ?? []), ...(h.feriados ?? []).flatMap((f) => f.franjas ?? [])].map((f) => f.perfil).filter((c): c is string => Boolean(c)));
            for (const c of codigos) usos.set(c, [...(usos.get(c) ?? []), h.nombre]);
        }
        return usos;
    } catch (e) {
        if (signal.aborted) throw e;
        return null;
    }
}

/**
 * `/administracion/asistente/perfiles`: los perfiles del asistente (contratos/conversaciones.md
 * §17.1). El general vale siempre; los demás se eligen por franja en los horarios, para que de
 * noche o en fin de semana haga otra cosa.
 */
export function ListaPerfiles() {
    const lectura = useLectura(async (signal) => {
        const [ajustes, usos] = await Promise.all([leerAjustesAsistente(signal), leerUsos(signal)]);
        return { ajustes, usos };
    });
    let contenido: ReactNode;
    let lleno = false;
    if (lectura.estado === "cargando") {
        contenido = (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando perfiles…
                </span>
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los perfiles del asistente" />;
    } else {
        const { ajustes, usos } = lectura.datos;
        const perfiles = perfilesDe(ajustes);
        lleno = perfiles.length >= MAX_PERFILES;
        contenido = (
            <>
                <ul aria-label="Perfiles" className="grid gap-3">
                    {perfiles.map((p) => (
                        <FilaPerfil key={p.codigo} perfil={p} ajustes={ajustes} usos={usos} alCambiar={() => void lectura.refrescar()} />
                    ))}
                </ul>
                <Aviso
                    tono="info"
                    icono={CalendarClock}
                    titulo="Un perfil se usa cuando lo eliges en un horario"
                    accion={
                        <Boton asChild variante="secundario" tamano="sm">
                            <Link href="/administracion/horarios">Ir a horarios</Link>
                        </Boton>
                    }
                >
                    En cada franja del horario eliges qué hace el asistente: lo de siempre, otro perfil o solo sugerir. Si dos reglas se cruzan, gana la más cuidadosa.
                </Aviso>
            </>
        );
    }
    return (
        <>
            <EncabezadoPagina
                titulo="Perfiles por horario"
                descripcion="Para que el asistente haga una cosa de día y otra de noche o en fin de semana."
                acciones={
                    lleno ? null : (
                        <Boton asChild>
                            <Link href={`${BASE}/nuevo`}>
                                <Plus />
                                Agregar perfil
                            </Link>
                        </Boton>
                    )
                }
            />
            {contenido}
        </>
    );
}

function FilaPerfil({ perfil: p, ajustes, usos, alCambiar }: { perfil: PerfilAsistente; ajustes: AjustesAsistente; usos: Usos; alCambiar: () => void }) {
    const clave = useClaveIdempotencia();
    const [confirmar, setConfirmar] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const general = esDefecto(p);
    const enHorarios = usos?.get(p.codigo) ?? [];

    async function quitar() {
        setError(null);
        try {
            await guardarAjustesAsistente(entradaPerfiles(ajustes, perfilesSin(ajustes, p.codigo)), clave.actual());
            clave.renovar();
            avisar({ titulo: `Perfil «${p.nombre}» quitado` });
            setConfirmar(false);
            alCambiar();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }

    return (
        <li data-perfil={p.codigo} className="surface-card grid min-w-0 gap-2 p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
            <div className="grid min-w-0 gap-1">
                <span className="flex min-w-0 flex-wrap items-center gap-2">
                    <span className="min-w-0 text-[0.9375rem] font-semibold [overflow-wrap:anywhere]">{p.nombre}</span>
                    {general ? <InsigniaEstado tono="neutral" texto="El de siempre" /> : null}
                </span>
                <p className="text-sm [overflow-wrap:anywhere]">{resumenPerfil(p)}</p>
                <p className="text-sm text-muted-foreground [overflow-wrap:anywhere]">
                    {general
                        ? "Vale en todas las franjas donde no elijas otro."
                        : usos === null
                          ? "Se elige en las franjas de un horario."
                          : enHorarios.length > 0
                            ? `En uso en: ${enHorarios.join(", ")}`
                            : "Todavía no se usa: elígelo en las franjas de un horario."}
                </p>
            </div>
            <div className="flex flex-wrap gap-2 md:justify-end">
                {general ? null : (
                    <Boton variante="fantasma" tamano="sm" className="text-error" onClick={() => setConfirmar(true)} aria-label={`Quitar ${p.nombre}`}>
                        <Trash2 />
                        Quitar
                    </Boton>
                )}
                <Boton asChild variante="contorno" tamano="sm">
                    <Link href={`${BASE}/${encodeURIComponent(p.codigo)}/editar`} aria-label={`Editar ${p.nombre}`}>
                        <Pencil />
                        Editar
                    </Link>
                </Boton>
            </div>
            <DialogoConfirmar
                open={confirmar}
                onOpenChange={setConfirmar}
                titulo={`¿Quitar el perfil «${p.nombre}»?`}
                descripcion={enHorarios.length > 0 ? `Lo usa ${enHorarios.join(", ")}: en esas franjas el asistente volverá a hacer lo de siempre.` : "En las franjas que lo usen, el asistente volverá a hacer lo de siempre."}
                textoConfirmar="Quitar"
                peligro
                alConfirmar={quitar}
            >
                {error ? (
                    <Aviso tono="error" titulo="No se pudo quitar">
                        {error instanceof ErrorApi && error.estado === 409 ? "Alguien más cambió los ajustes: recarga la página." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
            </DialogoConfirmar>
        </li>
    );
}
