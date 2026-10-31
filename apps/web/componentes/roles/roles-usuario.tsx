"use client";

import type { Schema } from "@iris/contratos";
import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoSelector, GrupoSegmentado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Plus, ShieldCheck, X } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { cuerpoAsignarRol, esquemaAsignarRol, faltaEnAsignacion, type DatosAsignarRol } from "@/lib/accesos/esquemas";
import { textoAlcance, type Rol } from "@/lib/accesos/presentacion";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type Usuario = Schema<"Usuario">;
type Asignacion = Schema<"AsignacionRol">;
type Alcance = DatosAsignarRol["alcance"];

const VACIO: DatosAsignarRol = { rol: "", alcance: "global", zonaId: "", cuadrillaId: "" };

/**
 * Roles del usuario con su alcance (zona o cuadrilla) y las acciones explícitas para asignar
 * (`POST /usuarios/{id}/asignar-rol`) y quitar (`/quitar-rol`). «Asignar» queda deshabilitado
 * mientras falte el rol o la zona/cuadrilla del alcance, y dice qué falta.
 */
export function RolesUsuario({ usuario, alCambiar }: { usuario: Usuario; alCambiar: (u: Usuario) => void }) {
    const { puede } = useSesion();
    const puedeAsignar = puede(["usuarios.asignar_rol"]);
    const puedeQuitar = puede(["usuarios.quitar_rol"]);
    const roles = useLectura<Rol[]>(async (signal) =>
        puedeAsignar || puede(["roles.ver"]) ? ((await resolver(api().GET("/api/v1/roles", { params: { query: { limit: 200 } }, signal }))).data ?? []) : [],
    );
    const conAgenda = puede(["agenda.ver"]);
    const zonas = useLectura<Schema<"Zona">[]>(async (signal) => (conAgenda ? ((await resolver(api().GET("/api/v1/zonas", { signal }))).data ?? []) : []));
    const cuadrillas = useLectura<Schema<"Cuadrilla">[]>(async (signal) => (conAgenda ? ((await resolver(api().GET("/api/v1/cuadrillas", { signal }))).data ?? []) : []));
    const nombres = useMemo(
        () => ({
            zonas: new Map(zonas.estado === "listo" ? zonas.datos.map((z) => [z.id, z.nombre] as const) : []),
            cuadrillas: new Map(cuadrillas.estado === "listo" ? cuadrillas.datos.map((c) => [c.id, c.nombre] as const) : []),
        }),
        [zonas, cuadrillas],
    );
    const nombreRol = (codigo: string) => (roles.estado === "listo" ? (roles.datos.find((r) => r.codigo === codigo)?.nombre ?? codigo) : codigo);
    const [error, setError] = useState<unknown>(null);
    const [quitando, setQuitando] = useState<string | null>(null);
    const claveQuitar = useRef(nuevaClaveIdempotencia());
    const asignaciones = usuario.roles ?? [];

    async function quitar(a: Asignacion) {
        const id = claveAsignacion(a);
        setQuitando(id);
        setError(null);
        try {
            const u = await resolver(
                api().POST("/api/v1/usuarios/{id}/quitar-rol", {
                    params: { path: { id: usuario.id }, header: { "Idempotency-Key": claveQuitar.current } },
                    body: { rol: a.rol, ...(a.zonaId ? { zonaId: a.zonaId } : {}), ...(a.cuadrillaId ? { cuadrillaId: a.cuadrillaId } : {}) },
                }),
            );
            claveQuitar.current = nuevaClaveIdempotencia();
            avisar({ titulo: `Rol quitado a ${u.nombre}` });
            alCambiar(u);
        } catch (e) {
            claveQuitar.current = claveTrasError(e, claveQuitar.current);
            setError(e);
        } finally {
            setQuitando(null);
        }
    }

    return (
        <Tarjeta data-seccion="roles">
            <TarjetaEncabezado>
                <TarjetaTitulo>Roles</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4">
                {asignaciones.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin roles: no puede hacer nada en Iris.</p>
                ) : (
                    <ul className="grid gap-2" aria-label="Roles asignados">
                        {asignaciones.map((a) => (
                            <li key={claveAsignacion(a)} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2" data-asignacion={claveAsignacion(a)}>
                                <span className="inline-flex min-w-0 items-center gap-2 text-sm">
                                    <ShieldCheck aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                                    <span className="font-medium">{nombreRol(a.rol)}</span>
                                    <span className="text-muted-foreground">· {textoAlcance(a, nombres)}</span>
                                </span>
                                {puedeQuitar ? (
                                    <Boton variante="fantasma" tamano="sm" onClick={() => void quitar(a)} disabled={quitando !== null} aria-label={`Quitar ${nombreRol(a.rol)} (${textoAlcance(a, nombres)})`}>
                                        <X />
                                        {quitando === claveAsignacion(a) ? "Quitando…" : "Quitar"}
                                    </Boton>
                                ) : null}
                            </li>
                        ))}
                    </ul>
                )}
                {error ? (
                    <Aviso tono="error" titulo="No se pudo quitar el rol">
                        {mensajeDeError(error).detalle}
                    </Aviso>
                ) : null}
                {puedeAsignar && usuario.activo ? (
                    <FormularioAsignar
                        usuarioId={usuario.id}
                        roles={roles.estado === "listo" ? roles.datos.filter((r) => r.activo) : null}
                        errorRoles={roles.estado === "error"}
                        zonas={zonas.estado === "listo" ? zonas.datos : null}
                        cuadrillas={cuadrillas.estado === "listo" ? cuadrillas.datos : null}
                        conAgenda={conAgenda}
                        alAsignar={alCambiar}
                    />
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function claveAsignacion(a: Asignacion): string {
    return `${a.rol}:${a.zonaId ?? 0}:${a.cuadrillaId ?? 0}`;
}

function FormularioAsignar({
    usuarioId,
    roles,
    errorRoles,
    zonas,
    cuadrillas,
    conAgenda,
    alAsignar,
}: {
    usuarioId: number;
    roles: Rol[] | null;
    errorRoles: boolean;
    zonas: Schema<"Zona">[] | null;
    cuadrillas: Schema<"Cuadrilla">[] | null;
    conAgenda: boolean;
    alAsignar: (u: Usuario) => void;
}) {
    const formulario = useForm<DatosAsignarRol>({ resolver: zodResolver(esquemaAsignarRol), defaultValues: VACIO });
    const { errors, isSubmitting } = formulario.formState;
    const valores = useWatch({ control: formulario.control });
    const falta = faltaEnAsignacion(valores);
    const [error, setError] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());

    async function enviar(d: DatosAsignarRol) {
        setError(null);
        try {
            const u = await resolver(
                api().POST("/api/v1/usuarios/{id}/asignar-rol", {
                    params: { path: { id: usuarioId }, header: { "Idempotency-Key": clave.current } },
                    body: cuerpoAsignarRol(d),
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            formulario.reset(VACIO);
            avisar({ titulo: `Rol asignado a ${u.nombre}` });
            alAsignar(u);
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        }
    }

    const opcionesAlcance: { valor: Alcance; etiqueta: string }[] = [
        { valor: "global", etiqueta: "Todas las zonas" },
        ...(conAgenda ? [{ valor: "zona" as const, etiqueta: "Una zona" }, { valor: "cuadrilla" as const, etiqueta: "Una cuadrilla" }] : []),
    ];
    return (
        <form className="grid gap-3 border-t border-border pt-4" noValidate aria-label="Asignar rol" onSubmit={(e) => void formulario.handleSubmit(enviar)(e)}>
            <CampoSelector
                etiqueta="Rol"
                {...formulario.register("rol")}
                disabled={!roles}
                error={errors.rol?.message}
                ayuda={errorRoles ? "No se pudo leer la lista de roles." : "Solo los roles activos."}
            >
                <option value="">Elige un rol…</option>
                {(roles ?? []).map((r) => (
                    <option key={r.id} value={r.codigo}>
                        {r.nombre}
                    </option>
                ))}
            </CampoSelector>
            <GrupoSegmentado<Alcance>
                etiqueta="Alcance"
                opciones={opcionesAlcance}
                valor={valores.alcance ?? "global"}
                alCambiar={(v) => formulario.setValue("alcance", v, { shouldValidate: formulario.formState.isSubmitted })}
            />
            {valores.alcance === "zona" ? (
                <CampoSelector etiqueta="Zona" {...formulario.register("zonaId")} disabled={!zonas} error={errors.zonaId?.message}>
                    <option value="">Elige la zona…</option>
                    {(zonas ?? []).map((z) => (
                        <option key={z.id} value={String(z.id)}>
                            {z.nombre}
                        </option>
                    ))}
                </CampoSelector>
            ) : null}
            {valores.alcance === "cuadrilla" ? (
                <CampoSelector etiqueta="Cuadrilla" {...formulario.register("cuadrillaId")} disabled={!cuadrillas} error={errors.cuadrillaId?.message}>
                    <option value="">Elige la cuadrilla…</option>
                    {(cuadrillas ?? []).map((c) => (
                        <option key={c.id} value={String(c.id)}>
                            {c.nombre}
                        </option>
                    ))}
                </CampoSelector>
            ) : null}
            {error ? (
                <Aviso tono="error" titulo="No se pudo asignar el rol">
                    {mensajeDeError(error).detalle}
                </Aviso>
            ) : null}
            <div className="flex flex-wrap items-center gap-3">
                <Boton type="submit" disabled={falta !== null || isSubmitting}>
                    <Plus />
                    {isSubmitting ? "Asignando…" : "Asignar rol"}
                </Boton>
                {falta ? (
                    <p className="text-sm text-muted-foreground" role="status">
                        {falta}
                    </p>
                ) : null}
            </div>
        </form>
    );
}
