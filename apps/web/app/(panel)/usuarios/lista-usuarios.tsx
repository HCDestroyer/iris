"use client";

import type { Schema } from "@iris/contratos";
import { Boton, DialogoProtegido, Esqueleto, EstadoVacio, FilaLista, InsigniaEstado, ListaFilas, MenuAcciones } from "@iris/ui";
import { Bot, KeyRound, RotateCcw, ShieldCheck, ShieldOff, SquareArrowOutUpRight, User, UserCheck, UserPlus, Users, UserX } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { pieListaFilas } from "@/componentes/pie-lista";
import { puedeReiniciarMfa, puedeRestablecerContrasena } from "@/lib/cuenta/sesion";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { DialogoReiniciarMfa } from "./reiniciar-mfa";
import { DialogoRestablecerContrasena } from "@/componentes/usuarios/restablecer-contrasena";
import Link from "next/link";

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

export function textoRoles(u: Usuario): string {
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

export function Tipo({ u }: { u: Usuario }) {
    const Icono = u.tipo === "integracion" ? Bot : User;
    return (
        <span className="inline-flex items-center gap-1.5 text-sm">
            <Icono aria-hidden className="size-4 text-muted-foreground" />
            {u.tipo === "integracion" ? "Integración" : "Persona"}
        </span>
    );
}

export function Mfa({ u }: { u: Usuario }) {
    return u.mfaActivo ? (
        <InsigniaEstado tono="success" icono={ShieldCheck} texto="MFA activo" />
    ) : (
        <InsigniaEstado tono="neutral" icono={ShieldOff} texto="Sin MFA" />
    );
}

export function Activo({ u }: { u: Usuario }) {
    return u.activo ? (
        <InsigniaEstado tono="neutral" icono={UserCheck} texto="Activo" />
    ) : (
        <InsigniaEstado tono="neutral" icono={UserX} texto="Inactivo" className="text-dimmed" />
    );
}

/**
 * Usuarios en `ListaFilas`: cada fila abre la ficha; su menú `⋯` tiene «Reiniciar MFA» y
 * «Restablecer contraseña» cuando se pueden (con el permiso, sobre otra persona).
 */
export function ListaUsuarios() {
    const primera = useLectura((signal) => leerPagina(undefined, signal));
    const [extra, setExtra] = useState<{ usuarios: Usuario[]; cursor: string | null } | null>(null);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const { yo, permisos, puede } = useSesion();
    const router = useRouter();
    // Lo que devolvieron las acciones (versión y MFA nuevos) sobre lo leído.
    const [cambios, setCambios] = useState<Record<number, Usuario>>({});
    const [reinicio, setReinicio] = useState<Usuario | null>(null);
    const [restablecer, setRestablecer] = useState<Usuario | null>(null);
    const actualizar = (u: Usuario) => setCambios((c) => ({ ...c, [u.id]: u }));

    const encabezado = (
        <EncabezadoPagina
            titulo="Usuarios"
            descripcion="Personas e integraciones con acceso a Iris."
            acciones={
                puede(["usuarios.crear"]) ? (
                    <Boton asChild>
                        <Link href="/usuarios/nuevo">
                            <UserPlus />
                            Nuevo usuario
                        </Link>
                    </Boton>
                ) : null
            }
        />
    );

    if (primera.estado === "cargando") {
        return (
            <>
                {encabezado}
                <ListaFilas
                    etiqueta="Usuarios"
                    vacio={
                        <div className="grid gap-3" aria-busy="true">
                            <span className="sr-only" role="status">
                                Cargando usuarios…
                            </span>
                            {Array.from({ length: 5 }, (_, i) => (
                                <Esqueleto key={i} className="h-10" />
                            ))}
                        </div>
                    }
                >
                    {[]}
                </ListaFilas>
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

    const leidos = [...(primera.datos.data ?? []), ...(extra?.usuarios ?? [])];
    const usuarios = leidos.map((u) => cambios[u.id] ?? u);

    function recargarLista() {
        setReinicio(null);
        setRestablecer(null);
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

    return (
        <>
            {encabezado}
            <ListaFilas
                etiqueta="Usuarios"
                {...pieListaFilas({ cuantos: usuarios.length, siguiente, cargarMas, cargandoMas, errorMas }, "usuarios")}
                vacio={<EstadoVacio icono={Users} titulo="Aún no hay usuarios" descripcion="Cuando se creen usuarios aparecerán aquí." />}
            >
                {usuarios.map((u) => (
                    <FilaLista
                        key={u.id}
                        data-usuario={u.id}
                        icono={u.tipo === "integracion" ? Bot : User}
                        tonoIcono={u.activo ? "marca" : "neutro"}
                        titulo={u.nombre}
                        detalle={`${u.correo} · ${u.tipo === "integracion" ? "Integración" : "Persona"} · ${textoRoles(u)}`}
                        href={`/usuarios/${u.id}`}
                        extremo={
                            <>
                                <Mfa u={u} />
                                <Activo u={u} />
                            </>
                        }
                        extremoTelefono="debajo"
                        menu={
                            <MenuAcciones
                                etiqueta={`Acciones de ${u.nombre}`}
                                acciones={[
                                    { id: "abrir", texto: "Abrir ficha", icono: SquareArrowOutUpRight, alElegir: () => router.push(`/usuarios/${u.id}`) },
                                    {
                                        id: "reiniciar-mfa",
                                        texto: "Reiniciar MFA",
                                        icono: RotateCcw,
                                        seccion: "Acceso",
                                        permitida: puedeReiniciarMfa(permisos, yo.usuario.id, u),
                                        alElegir: () => setReinicio(u),
                                    },
                                    {
                                        id: "restablecer",
                                        texto: "Restablecer contraseña",
                                        icono: KeyRound,
                                        seccion: "Acceso",
                                        permitida: puedeRestablecerContrasena(permisos, yo.usuario.id, u),
                                        alElegir: () => setRestablecer(u),
                                    },
                                ]}
                            />
                        }
                    />
                ))}
            </ListaFilas>
            <DialogoProtegido open={reinicio !== null} onOpenChange={(abierto) => (abierto ? undefined : setReinicio(null))}>
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
            </DialogoProtegido>
            {restablecer ? (
                <DialogoRestablecerContrasena
                    key={restablecer.id}
                    usuario={restablecer}
                    abierto
                    alCerrar={() => setRestablecer(null)}
                    alCambiar={actualizar}
                    alPerderAlcance={recargarLista}
                />
            ) : null}
        </>
    );
}
