"use client";

import { Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { History } from "lucide-react";
import Link from "next/link";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import type { QueryAccesos } from "@/lib/accesos/esquemas";
import type { Acceso } from "@/lib/accesos/presentacion";
import { api, resolver } from "@/lib/api/cliente";
import { formatearFecha } from "@/lib/fechas";
import { usePaginado } from "@/lib/paginado";
import { Canal, InsigniaResultado } from "./insignias";

const LIMITE = 50;

/**
 * Historial de accesos con cursor: del más reciente al más antiguo. Con `usuarioId` lee
 * `GET /usuarios/{id}/accesos`; si no, `GET /accesos` con los filtros.
 */
export function ListaAccesos({ query, usuarioId, limite = LIMITE, conUsuario = true }: { query: QueryAccesos; usuarioId?: number; limite?: number; conUsuario?: boolean }) {
    const paginado = usePaginado<Acceso>((cursor, signal) => {
        const q = { ...query, limit: limite, ...(cursor ? { cursor } : {}) };
        const opciones = signal ? { signal } : {};
        return usuarioId
            ? resolver(api().GET("/api/v1/usuarios/{id}/accesos", { params: { path: { id: usuarioId }, query: q }, ...opciones }))
            : resolver(api().GET("/api/v1/accesos", { params: { query: q }, ...opciones }));
    });
    const { primera } = paginado;
    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando accesos…
                </span>
                <Esqueleto className="h-16" />
                <Esqueleto className="h-16" />
                <Esqueleto className="h-16" />
            </div>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} que="el historial de accesos" reintentar={primera.reintentar} />;
    }
    if (paginado.items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio icono={History} titulo="Sin accesos" descripcion="No hay accesos con estos filtros." />
            </Tarjeta>
        );
    }
    return (
        <div className="grid gap-3">
            <ul className="grid gap-2" aria-label="Accesos">
                {paginado.items.map((a) => (
                    <li key={a.id} data-acceso={a.id}>
                        <FilaAcceso acceso={a} conUsuario={conUsuario} />
                    </li>
                ))}
            </ul>
            <PieLista paginado={paginado} que="accesos" />
        </div>
    );
}

function FilaAcceso({ acceso: a, conUsuario }: { acceso: Acceso; conUsuario: boolean }) {
    return (
        <Tarjeta className="grid gap-2 p-3 sm:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)] sm:items-center sm:gap-4">
            <div className="grid min-w-0 gap-1">
                <time className="text-sm font-medium tabular-nums" dateTime={a.creadoEn}>
                    {formatearFecha(a.creadoEn)}
                </time>
                {conUsuario ? (
                    a.usuarioId ? (
                        <Link href={`/usuarios/${a.usuarioId}`} className="truncate text-sm text-primary underline-offset-2 hover:underline">
                            {a.usuarioNombre ?? `Usuario ${a.usuarioId}`}
                        </Link>
                    ) : (
                        <span className="text-sm text-muted-foreground">Correo que no existe</span>
                    )
                ) : null}
            </div>
            <div className="flex flex-wrap items-center gap-2">
                <InsigniaResultado resultado={a.resultado} />
                <Canal canal={a.canal} />
            </div>
            <div className="grid min-w-0 gap-0.5 text-sm text-muted-foreground">
                <span className="truncate" title={a.dispositivo || undefined}>
                    {a.dispositivo || "Dispositivo desconocido"}
                </span>
                <span className="truncate tabular-nums">IP {a.ip || "—"}</span>
            </div>
        </Tarjeta>
    );
}
