"use client";

import type { Schema } from "@iris/contratos";
import {
    Aviso,
    Boton,
    Dialogo,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Tabla,
    TablaCelda,
    TablaCuerpo,
    TablaEncabezado,
    TablaFila,
    TablaTitulo,
    Tarjeta,
} from "@iris/ui";
import { Bot, ChevronDown, RotateCcw, RotateCw, ShieldCheck, ShieldOff, User, UserCheck, Users, UserX } from "lucide-react";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { puedeReiniciarMfa } from "@/lib/cuenta/sesion";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { DialogoReiniciarMfa } from "./reiniciar-mfa";

type Usuario = Schema<"Usuario">;
type Pagina = Schema<"PaginaUsuario">;

const TAMANO_PAGINA = 25;

function leerPagina(cursor: string | undefined, signal?: AbortSignal): Promise<Pagina> {
    return resolver(
        api().GET("/api/v1/usuarios", {
            params: { query: { limit: TAMANO_PAGINA, ...(cursor ? { cursor } : {}) } },
            ...(signal ? { signal } : {}),
        }),
    );
}

function textoRoles(u: Usuario): string {
    const roles = u.roles ?? [];
    if (roles.length === 0) {
        return "Sin roles";
    }
    return roles
        .map((r) => {
            const alcance = [r.zonaId ? `zona ${r.zonaId}` : null, r.cuadrillaId ? `cuadrilla ${r.cuadrillaId}` : null].filter(Boolean);
            return alcance.length ? `${r.rol} (${alcance.join(", ")})` : r.rol;
        })
        .join(", ");
}

function Tipo({ u }: { u: Usuario }) {
    const Icono = u.tipo === "integracion" ? Bot : User;
    return (
        <span className="inline-flex items-center gap-1.5 text-sm">
            <Icono aria-hidden className="size-4 text-muted-foreground" />
            {u.tipo === "integracion" ? "Integración" : "Persona"}
        </span>
    );
}

function Mfa({ u }: { u: Usuario }) {
    return u.mfaActivo ? (
        <InsigniaEstado tono="success" icono={ShieldCheck} texto="MFA activo" />
    ) : (
        <InsigniaEstado tono="neutral" icono={ShieldOff} texto="Sin MFA" />
    );
}

/** «Reiniciar MFA» de la fila: solo con el permiso, sobre otro usuario y con MFA activo. */
function BotonReiniciar({ u, alAbrir }: { u: Usuario; alAbrir: (u: Usuario) => void }) {
    const { yo, permisos } = useSesion();
    if (!puedeReiniciarMfa(permisos, yo.usuario.id, u)) {
        return null;
    }
    return (
        <Boton variante="contorno" tamano="sm" onClick={() => alAbrir(u)} aria-label={`Reiniciar MFA de ${u.nombre}`}>
            <RotateCcw />
            Reiniciar MFA
        </Boton>
    );
}

function Activo({ u }: { u: Usuario }) {
    return u.activo ? (
        <InsigniaEstado tono="neutral" icono={UserCheck} texto="Activo" />
    ) : (
        <InsigniaEstado tono="neutral" icono={UserX} texto="Inactivo" className="text-dimmed" />
    );
}

export function ListaUsuarios() {
    const primera = useLectura((signal) => leerPagina(undefined, signal));
    const [extra, setExtra] = useState<{ usuarios: Usuario[]; cursor: string | null } | null>(null);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const { permisos } = useSesion();
    const conAcciones = permisos.includes("usuarios.reiniciar_mfa");
    // Lo que devolvieron las acciones (versión y MFA nuevos) sobre lo leído.
    const [cambios, setCambios] = useState<Record<number, Usuario>>({});
    const [reinicio, setReinicio] = useState<Usuario | null>(null);
    const actualizar = (u: Usuario) => setCambios((c) => ({ ...c, [u.id]: u }));

    const encabezado = <EncabezadoPagina titulo="Usuarios" descripcion="Personas e integraciones con acceso a Iris." />;

    if (primera.estado === "cargando") {
        return (
            <>
                {encabezado}
                <Tarjeta className="grid gap-3 p-5" aria-busy="true">
                    <span className="sr-only" role="status">
                        Cargando usuarios…
                    </span>
                    {Array.from({ length: 5 }, (_, i) => (
                        <Esqueleto key={i} className="h-10" />
                    ))}
                </Tarjeta>
            </>
        );
    }
    if (primera.estado === "error") {
        return (
            <>
                {encabezado}
                <ErrorLectura
                    error={primera.error}
                    que="los usuarios"
                    reintentar={() => {
                        setExtra(null);
                        primera.reintentar();
                    }}
                />
            </>
        );
    }

    const usuarios = [...(primera.datos.data ?? []), ...(extra?.usuarios ?? [])].map((u) => cambios[u.id] ?? u);

    function recargarLista() {
        setReinicio(null);
        setExtra(null);
        setCambios({});
        primera.reintentar();
    }
    const siguiente = extra ? extra.cursor : primera.datos.nextCursor;

    async function cargarMas() {
        if (!siguiente) {
            return;
        }
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const pagina = await leerPagina(siguiente);
            setExtra((prev) => ({ usuarios: [...(prev?.usuarios ?? []), ...(pagina.data ?? [])], cursor: pagina.nextCursor }));
        } catch (error) {
            setErrorMas(error);
        } finally {
            setCargandoMas(false);
        }
    }

    if (usuarios.length === 0) {
        return (
            <>
                {encabezado}
                <Tarjeta>
                    <EstadoVacio icono={Users} titulo="Aún no hay usuarios" descripcion="Cuando se creen usuarios aparecerán aquí." />
                </Tarjeta>
            </>
        );
    }

    return (
        <>
            {encabezado}
            <Tarjeta className="overflow-hidden">
                {/* Teléfono: lista */}
                <ul className="divide-y sm:hidden" aria-label="Usuarios">
                    {usuarios.map((u) => (
                        <li key={u.id} className="grid gap-2 p-4">
                            <div className="grid min-w-0">
                                <span className="truncate font-medium">{u.nombre}</span>
                                <span className="truncate text-sm text-muted-foreground">{u.correo}</span>
                            </div>
                            <div className="flex flex-wrap items-center gap-2">
                                <Tipo u={u} />
                                <Mfa u={u} />
                                <Activo u={u} />
                            </div>
                            <p className="text-sm text-muted-foreground">{textoRoles(u)}</p>
                            <div className="empty:hidden">
                                <BotonReiniciar u={u} alAbrir={setReinicio} />
                            </div>
                        </li>
                    ))}
                </ul>
                {/* Escritorio: tabla */}
                <div className="hidden sm:block">
                    <Tabla aria-label="Usuarios">
                        <TablaEncabezado>
                            <TablaFila className="hover:bg-transparent">
                                <TablaTitulo>Nombre</TablaTitulo>
                                <TablaTitulo>Tipo</TablaTitulo>
                                <TablaTitulo>Roles</TablaTitulo>
                                <TablaTitulo>MFA</TablaTitulo>
                                <TablaTitulo>Estado</TablaTitulo>
                                {conAcciones ? (
                                    <TablaTitulo>
                                        <span className="sr-only">Acciones</span>
                                    </TablaTitulo>
                                ) : null}
                            </TablaFila>
                        </TablaEncabezado>
                        <TablaCuerpo>
                            {usuarios.map((u) => (
                                <TablaFila key={u.id}>
                                    <TablaCelda>
                                        <div className="grid min-w-0">
                                            <span className="font-medium">{u.nombre}</span>
                                            <span className="text-muted-foreground">{u.correo}</span>
                                        </div>
                                    </TablaCelda>
                                    <TablaCelda>
                                        <Tipo u={u} />
                                    </TablaCelda>
                                    <TablaCelda className="text-muted-foreground">{textoRoles(u)}</TablaCelda>
                                    <TablaCelda>
                                        <Mfa u={u} />
                                    </TablaCelda>
                                    <TablaCelda>
                                        <Activo u={u} />
                                    </TablaCelda>
                                    {conAcciones ? (
                                        <TablaCelda className="text-right">
                                            <BotonReiniciar u={u} alAbrir={setReinicio} />
                                        </TablaCelda>
                                    ) : null}
                                </TablaFila>
                            ))}
                        </TablaCuerpo>
                    </Tabla>
                </div>
            </Tarjeta>
            <div className="mt-4 grid justify-items-center gap-3">
                {errorMas ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudo cargar la página siguiente"
                        className="w-full"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={cargarMas}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        {mensajeDeError(errorMas).detalle}
                    </Aviso>
                ) : siguiente ? (
                    <Boton variante="contorno" onClick={cargarMas} disabled={cargandoMas}>
                        <ChevronDown />
                        {cargandoMas ? "Cargando…" : "Cargar más"}
                    </Boton>
                ) : (
                    <p className="text-sm text-muted-foreground">No hay más usuarios.</p>
                )}
            </div>
            <Dialogo open={reinicio !== null} onOpenChange={(abierto) => (abierto ? undefined : setReinicio(null))}>
                {reinicio ? (
                    <DialogoReiniciarMfa
                        key={reinicio.id}
                        usuario={reinicio}
                        alReiniciar={actualizar}
                        alRecargar={actualizar}
                        alRecargarLista={recargarLista}
                        alCerrar={() => setReinicio(null)}
                    />
                ) : null}
            </Dialogo>
        </>
    );
}
