"use client";

import { Aviso, Boton, CampoArea, Dialogo, DialogoContenido, DialogoPie, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowLeft, CircleCheck, CirclePause, Lock, Power, PowerOff, RotateCw, SearchX, ShieldAlert } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useRef, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { FormularioRol, useCatalogoPermisos } from "@/componentes/roles/formulario-rol";
import { cuerpoEditarRol } from "@/lib/accesos/esquemas";
import { agruparPermisos, type Rol } from "@/lib/accesos/presentacion";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/** `/administracion/roles/[id]`: un rol base se ve; uno propio se edita, activa o desactiva. */
export function DetalleRol() {
    const { id: crudo } = useParams<{ id: string }>();
    const id = Number(crudo);
    const valido = Number.isSafeInteger(id) && id > 0;
    const { puede } = useSesion();
    const lectura = useLectura<Rol | null>(async (signal) => (valido ? resolver(api().GET("/api/v1/roles/{id}", { params: { path: { id } }, signal })) : null));
    const clave = useRef(nuevaClaveIdempotencia());
    const [guardado, setGuardado] = useState(false);

    const volver = (
        <Boton variante="fantasma" tamano="sm" asChild className="mb-3 -ml-2">
            <Link href="/administracion/roles">
                <ArrowLeft />
                Roles y permisos
            </Link>
        </Boton>
    );
    if (lectura.estado === "cargando") {
        return (
            <>
                {volver}
                <Esqueleto className="mb-4 h-10 w-64" />
                <Esqueleto className="h-64" />
            </>
        );
    }
    if (!valido || (lectura.estado === "error" && lectura.error instanceof ErrorApi && lectura.error.estado === 404) || (lectura.estado === "listo" && !lectura.datos)) {
        return (
            <>
                {volver}
                <Tarjeta>
                    <EstadoVacio icono={SearchX} titulo="Rol no encontrado" descripcion="No existe o ya no está disponible." />
                </Tarjeta>
            </>
        );
    }
    if (lectura.estado === "error") {
        return (
            <>
                {volver}
                <ErrorLectura error={lectura.error} que="el rol" reintentar={lectura.reintentar} />
            </>
        );
    }
    const rol = lectura.datos;
    if (!rol) return null;
    const editable = !rol.sistema && puede(["roles.editar"]);
    return (
        <>
            {volver}
            <EncabezadoPagina
                titulo={rol.nombre}
                descripcion={
                    <span className="inline-flex flex-wrap items-center gap-2">
                        <code>{rol.codigo}</code>
                        {rol.sistema ? (
                            <InsigniaEstado tono="neutral" icono={Lock} texto="Rol base" />
                        ) : rol.activo ? (
                            <InsigniaEstado tono="success" icono={CircleCheck} texto="Activo" />
                        ) : (
                            <InsigniaEstado tono="warning" icono={CirclePause} texto="Desactivado" />
                        )}
                        <span>{rol.usuarios === 1 ? "1 usuario lo tiene" : `${rol.usuarios} usuarios lo tienen`}</span>
                    </span>
                }
            />
            <div className="grid gap-4">
                {rol.sistema ? (
                    <Aviso tono="info" titulo="Rol base" icono={Lock}>
                        Lo mantiene Iris: sus permisos se actualizan con cada versión y no se edita ni se desactiva. Para otra combinación, crea un rol propio.
                    </Aviso>
                ) : null}
                {!rol.sistema && !rol.activo ? (
                    <Aviso tono="warning" titulo="Rol desactivado" icono={CirclePause}>
                        No da permisos a quienes lo tienen asignado ni se puede asignar. Al activarlo, sus permisos vuelven a valer.
                    </Aviso>
                ) : null}
                {guardado ? (
                    <Aviso tono="success" titulo="Rol guardado">
                        Los cambios valen desde la siguiente acción de cada usuario con este rol.
                    </Aviso>
                ) : null}
                {editable ? (
                    <FormularioRol
                        key={rol.version}
                        edicion
                        inicial={{ codigo: rol.codigo, nombre: rol.nombre, descripcion: rol.descripcion, permisos: [...(rol.permisos ?? [])] }}
                        textoGuardar="Guardar cambios"
                        alGuardar={async (d) => {
                            setGuardado(false);
                            try {
                                const r = await resolver(
                                    api().POST("/api/v1/roles/{id}/editar", {
                                        params: { path: { id: rol.id }, header: { "Idempotency-Key": clave.current } },
                                        body: cuerpoEditarRol(d, rol.version),
                                    }),
                                );
                                clave.current = nuevaClaveIdempotencia();
                                lectura.reemplazar(r);
                                setGuardado(true);
                            } catch (e) {
                                clave.current = claveTrasError(e, clave.current);
                                throw e;
                            }
                        }}
                    />
                ) : (
                    <PermisosDeRol rol={rol} />
                )}
                {!rol.sistema && (rol.activo ? puede(["roles.desactivar"]) : puede(["roles.activar"])) ? (
                    <CambiarActivo rol={rol} alCambiar={(r) => lectura.reemplazar(r)} recargar={() => void lectura.refrescar()} />
                ) : null}
            </div>
        </>
    );
}

function PermisosDeRol({ rol }: { rol: Rol }) {
    const catalogo = useCatalogoPermisos();
    if (catalogo.estado === "cargando") return <Esqueleto className="h-64" />;
    if (catalogo.estado === "error") return <ErrorLectura error={catalogo.error} que="el catálogo de permisos" reintentar={catalogo.reintentar} />;
    const propios = new Set<string>(rol.permisos ?? []);
    const grupos = agruparPermisos(catalogo.datos.filter((p) => propios.has(p.codigo)));
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Permisos ({propios.size})</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4">
                {grupos.map((g) => (
                    <section key={g.modulo} className="grid gap-1" aria-label={g.nombre}>
                        <h3 className="font-medium">{g.nombre}</h3>
                        <ul className="grid gap-1 text-sm">
                            {g.permisos.map((p) => (
                                <li key={p.codigo} className="flex flex-wrap items-center gap-2">
                                    <span>{p.descripcion}</span>
                                    <code className="text-xs text-muted-foreground">{p.codigo}</code>
                                    {p.sensible ? <InsigniaEstado tono="warning" icono={ShieldAlert} texto="Sensible" /> : null}
                                </li>
                            ))}
                        </ul>
                    </section>
                ))}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function CambiarActivo({ rol, alCambiar, recargar }: { rol: Rol; alCambiar: (r: Rol) => void; recargar: () => void }) {
    const [abierto, setAbierto] = useState(false);
    const [motivo, setMotivo] = useState("");
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());
    const desactivar = rol.activo;

    async function confirmar() {
        setEnviando(true);
        setError(null);
        try {
            const r = await resolver(
                api().POST(desactivar ? "/api/v1/roles/{id}/desactivar" : "/api/v1/roles/{id}/activar", {
                    params: { path: { id: rol.id }, header: { "Idempotency-Key": clave.current } },
                    body: { version: rol.version, ...(motivo.trim() ? { motivo: motivo.trim() } : {}) },
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            setAbierto(false);
            setMotivo("");
            alCambiar(r);
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <Tarjeta>
            <TarjetaContenido className="flex flex-wrap items-center justify-between gap-3 pt-5">
                <p className="text-sm text-muted-foreground">
                    {desactivar
                        ? "Desactivar quita sus permisos a quienes lo tienen; las asignaciones se conservan."
                        : "Activar devuelve sus permisos a quienes lo tienen asignado."}
                </p>
                <Boton variante={desactivar ? "secundario" : "primario"} onClick={() => setAbierto(true)}>
                    {desactivar ? <PowerOff /> : <Power />}
                    {desactivar ? "Desactivar rol" : "Activar rol"}
                </Boton>
            </TarjetaContenido>
            <Dialogo open={abierto} onOpenChange={setAbierto}>
                <DialogoContenido
                    titulo={desactivar ? `¿Desactivar «${rol.nombre}»?` : `¿Activar «${rol.nombre}»?`}
                    descripcion={
                        desactivar
                            ? `${rol.usuarios === 1 ? "1 usuario pierde" : `${rol.usuarios} usuarios pierden`} los permisos de este rol en su siguiente acción.`
                            : `${rol.usuarios === 1 ? "1 usuario recupera" : `${rol.usuarios} usuarios recuperan`} los permisos de este rol.`
                    }
                >
                    <div className="grid gap-4">
                        <CampoArea etiqueta="Motivo (opcional)" rows={2} maxLength={200} value={motivo} onChange={(e) => setMotivo(e.target.value)} />
                        {error ? (
                            <Aviso
                                tono="error"
                                titulo={conflicto ? "Alguien más cambió el rol" : "No se pudo completar"}
                                accion={
                                    conflicto ? (
                                        <Boton
                                            variante="secundario"
                                            tamano="sm"
                                            onClick={() => {
                                                setAbierto(false);
                                                setError(null);
                                                recargar();
                                            }}
                                        >
                                            <RotateCw />
                                            Recargar
                                        </Boton>
                                    ) : undefined
                                }
                            >
                                {mensajeDeError(error).detalle}
                            </Aviso>
                        ) : null}
                        <DialogoPie>
                            <Boton type="button" variante="secundario" onClick={() => setAbierto(false)}>
                                Cancelar
                            </Boton>
                            <Boton type="button" onClick={() => void confirmar()} disabled={enviando}>
                                {desactivar ? <PowerOff /> : <Power />}
                                {enviando ? "Guardando…" : desactivar ? "Desactivar" : "Activar"}
                            </Boton>
                        </DialogoPie>
                    </div>
                </DialogoContenido>
            </Dialogo>
        </Tarjeta>
    );
}
