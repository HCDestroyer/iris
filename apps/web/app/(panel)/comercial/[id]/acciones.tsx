"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, CampoArea, CampoSelector, Casilla, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie } from "@iris/ui";
import { CircleCheck, CircleX, PhoneCall, RotateCcw, Trophy, UserRoundPlus, type LucideIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { AvisoAccion, useEnvio } from "@/componentes/comercial/comun";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nombreUsuario, type Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { accionesVisibles, type AccionProspecto } from "@/lib/comercial/acciones";
import { ErrorApi } from "@/lib/api/errores";
import { esquemaPerder, esquemaReabrir, type DatosPerder, type DatosReabrir } from "@/lib/comercial/esquemas";
import { requisitosGanar, type Cobertura, type ProspectoDetalle } from "@/lib/comercial/presentacion";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type Accion = AccionProspecto;

type Def = { etiqueta: string; icono: LucideIcon; titulo: string };

const ACCIONES: Record<Accion, Def> = {
    contactar: { etiqueta: "Marcar contactado", icono: PhoneCall, titulo: "Marcar como contactado" },
    asignar: { etiqueta: "Asignar vendedor", icono: UserRoundPlus, titulo: "Asignar vendedor" },
    ganar: { etiqueta: "Ganar", icono: Trophy, titulo: "Ganar el prospecto" },
    perder: { etiqueta: "Perder", icono: CircleX, titulo: "Marcar como perdido" },
    reabrir: { etiqueta: "Reabrir", icono: RotateCcw, titulo: "Reabrir el prospecto" },
};

type Props = { detalle: ProspectoDetalle; usuarios: readonly Usuario[] | null; recargar: () => Promise<void> };

export function BarraAcciones({ detalle, usuarios, recargar }: Props) {
    const { puede } = useSesion();
    const [abierta, setAbierta] = useState<Accion | null>(null);
    const visibles = accionesVisibles(detalle, puede);
    if (visibles.length === 0) return null;
    const cerrar = () => setAbierta(null);
    const hecho = async () => {
        setAbierta(null);
        await recargar();
    };
    return (
        <>
            <div className="flex flex-wrap gap-2" role="group" aria-label="Acciones del prospecto">
                {visibles.map((a) => {
                    const def = ACCIONES[a];
                    const Icono = def.icono;
                    return (
                        <Boton key={a} variante={a === "ganar" ? "primario" : a === "perder" ? "contorno" : "secundario"} tamano="sm" onClick={() => setAbierta(a)}>
                            <Icono />
                            {def.etiqueta}
                        </Boton>
                    );
                })}
            </div>
            <Dialogo open={abierta !== null} onOpenChange={(v) => (v ? undefined : cerrar())}>
                {abierta ? (
                    <DialogoContenido titulo={ACCIONES[abierta].titulo} descripcion={`${detalle.numero} · ${detalle.nombre}`}>
                        {abierta === "contactar" ? <FormMotivo accion="contactar" detalle={detalle} alHecho={hecho} recargar={recargar} /> : null}
                        {abierta === "reabrir" ? <FormMotivo accion="reabrir" detalle={detalle} alHecho={hecho} recargar={recargar} /> : null}
                        {abierta === "perder" ? <FormPerder detalle={detalle} alHecho={hecho} recargar={recargar} /> : null}
                        {abierta === "asignar" ? <FormAsignar detalle={detalle} usuarios={usuarios} alHecho={hecho} recargar={recargar} /> : null}
                        {abierta === "ganar" ? <FormGanar detalle={detalle} alHecho={hecho} recargar={recargar} /> : null}
                    </DialogoContenido>
                ) : null}
            </Dialogo>
        </>
    );
}

type PropsForm = { detalle: ProspectoDetalle; alHecho: () => Promise<void>; recargar: () => Promise<void> };

function Pie({ etiqueta, enviando, deshabilitado, children }: { etiqueta: string; enviando: boolean; deshabilitado?: boolean; children?: ReactNode }) {
    return (
        <DialogoPie>
            <DialogoCerrar asChild>
                <Boton variante="contorno" disabled={enviando}>
                    Volver
                </Boton>
            </DialogoCerrar>
            <Boton type="submit" disabled={enviando || deshabilitado}>
                {children}
                {enviando ? "Enviando…" : etiqueta}
            </Boton>
        </DialogoPie>
    );
}

function FormMotivo({ accion, detalle, alHecho, recargar }: PropsForm & { accion: "contactar" | "reabrir" }) {
    const envio = useEnvio();
    const formulario = useForm<DatosReabrir>({ resolver: zodResolver(esquemaReabrir), defaultValues: { motivo: "" } });
    async function enviar(d: DatosReabrir) {
        const ruta = accion === "contactar" ? "/api/v1/prospectos/{id}/contactar" : "/api/v1/prospectos/{id}/reabrir";
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST(ruta, {
                        params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                        body: { version: detalle.version, ...(d.motivo.trim() ? { motivo: d.motivo.trim() } : {}) },
                    }),
                ),
            { nombres: ["motivo"], setError: formulario.setError },
        );
        if (r) await alHecho();
    }
    return (
        <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4">
            <CampoArea
                etiqueta={accion === "contactar" ? "Nota (opcional)" : "Motivo (opcional)"}
                rows={2}
                error={formulario.formState.errors.motivo?.message}
                {...formulario.register("motivo")}
            />
            <AvisoAccion envio={envio} que={accion === "contactar" ? "marcarlo como contactado" : "reabrir el prospecto"} recargar={recargar} />
            <Pie etiqueta={accion === "contactar" ? "Marcar contactado" : "Reabrir"} enviando={envio.enviando} deshabilitado={envio.conflicto} />
        </form>
    );
}

function FormPerder({ detalle, alHecho, recargar }: PropsForm) {
    const envio = useEnvio();
    const formulario = useForm<DatosPerder>({ resolver: zodResolver(esquemaPerder), defaultValues: { motivo: "" } });
    async function enviar(d: DatosPerder) {
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/prospectos/{id}/perder", {
                        params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                        body: { version: detalle.version, motivo: d.motivo },
                    }),
                ),
            { nombres: ["motivo"], setError: formulario.setError },
        );
        if (r) await alHecho();
    }
    return (
        <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4">
            <CampoArea
                etiqueta="Motivo"
                rows={3}
                ayuda="Precio, sin cobertura, eligió a la competencia… Se puede reabrir después."
                error={formulario.formState.errors.motivo?.message}
                {...formulario.register("motivo")}
            />
            <AvisoAccion envio={envio} que="marcarlo como perdido" recargar={recargar} />
            <Pie etiqueta="Marcar perdido" enviando={envio.enviando} deshabilitado={envio.conflicto} />
        </form>
    );
}

const esquemaAsignar = z.object({ vendedorId: z.string().regex(/^[1-9]\d*$/, "Elige a quién asignarlo") });

function FormAsignar({ detalle, usuarios, alHecho, recargar }: PropsForm & { usuarios: readonly Usuario[] | null }) {
    const { yo } = useSesion();
    const envio = useEnvio();
    const formulario = useForm<z.infer<typeof esquemaAsignar>>({
        resolver: zodResolver(esquemaAsignar),
        defaultValues: { vendedorId: detalle.vendedorId === null ? "" : String(detalle.vendedorId) },
    });
    const otros = (usuarios ?? []).filter((u) => u.id !== yo.usuario.id && u.tipo === "persona" && u.activo);
    const actual = nombreUsuario(usuarios, detalle.vendedorId, { id: yo.usuario.id, nombre: yo.usuario.nombre });
    async function enviar(d: z.infer<typeof esquemaAsignar>) {
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/prospectos/{id}/asignar", {
                        params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                        body: { version: detalle.version, vendedorId: Number(d.vendedorId) },
                    }),
                ),
            { nombres: ["vendedorId"], setError: formulario.setError },
        );
        if (r) await alHecho();
    }
    return (
        <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4">
            <p className="text-sm text-muted-foreground">Vendedor actual: {actual ?? "sin vendedor"}.</p>
            <CampoSelector
                etiqueta="Vendedor"
                ayuda={usuarios === null ? "Sin permiso para ver usuarios: puedes asignártelo a ti." : "La API valida que opere en la empresa y vea la zona."}
                error={formulario.formState.errors.vendedorId?.message}
                {...formulario.register("vendedorId")}
            >
                <option value="">Elige…</option>
                <option value={String(yo.usuario.id)}>{yo.usuario.nombre} (tú)</option>
                {otros.map((u) => (
                    <option key={u.id} value={String(u.id)}>
                        {u.nombre}
                    </option>
                ))}
            </CampoSelector>
            <AvisoAccion envio={envio} que="asignar el vendedor" recargar={recargar} />
            <Pie etiqueta="Asignar" enviando={envio.enviando} deshabilitado={envio.conflicto} />
        </form>
    );
}

/**
 * Ganar: lo que falta (contrato, cotización, factibilidad), confirmar la obra si la hay y, con
 * conector y sin router sugerido por la factibilidad, elegir el router del servicio (422
 * `router_requerido`).
 */
function FormGanar({ detalle, alHecho, recargar }: PropsForm) {
    const envio = useEnvio();
    const obra = detalle.acciones.confirmaObra;
    const sugerido = detalle.factibilidad?.routerId ?? null;
    // Sin router sugerido: la cobertura dice si hay conector y qué routers hay.
    const cobertura = useLectura<Cobertura | null>(async (signal) => (sugerido === null ? resolver(api().GET("/api/v1/comercial/cobertura", { signal })) : null));
    const routers = cobertura.estado === "listo" && cobertura.datos?.disponible ? (cobertura.datos.routers ?? []) : [];
    const pideRouter = sugerido === null && routers.length > 0;
    const esquema = z
        .object({ confirmarObra: z.boolean(), routerId: z.string() })
        .superRefine((d, ctx) => {
            if (obra && !d.confirmarObra) ctx.addIssue({ code: "custom", path: ["confirmarObra"], message: "Confirma la obra para ganar" });
            if (pideRouter && d.routerId === "") ctx.addIssue({ code: "custom", path: ["routerId"], message: "Elige el router desde donde se atenderá" });
        });
    const formulario = useForm<{ confirmarObra: boolean; routerId: string }>({ resolver: zodResolver(esquema), defaultValues: { confirmarObra: false, routerId: "" } });
    const requisitos = requisitosGanar(detalle);
    const faltaRouter = envio.error instanceof ErrorApi && envio.error.codigo === "router_requerido";
    async function enviar(d: { confirmarObra: boolean; routerId: string }) {
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/prospectos/{id}/ganar", {
                        params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                        body: {
                            version: detalle.version,
                            ...(obra ? { confirmarObra: d.confirmarObra } : {}),
                            ...(d.routerId ? { routerId: Number(d.routerId) } : {}),
                        },
                    }),
                ),
            { nombres: ["confirmarObra", "routerId"], setError: formulario.setError },
        );
        if (r) await alHecho();
    }
    return (
        <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4">
            <ul className="grid gap-2 text-sm" aria-label="Requisitos para ganar">
                {requisitos.map((r) => (
                    <li key={r.clave} className="flex items-start gap-2">
                        {r.cumplido ? <CircleCheck aria-hidden className="mt-0.5 size-4 shrink-0 text-success" /> : <CircleX aria-hidden className="mt-0.5 size-4 shrink-0 text-error" />}
                        <span>
                            {r.texto}
                            {r.detalle ? <span className="text-muted-foreground"> · {r.detalle}</span> : null}
                        </span>
                    </li>
                ))}
            </ul>
            {obra ? (
                <Aviso tono="warning" titulo="La factibilidad requiere obra">
                    La instalación necesita extender la red. Confírmalo solo si el cliente lo aceptó.
                </Aviso>
            ) : null}
            {obra ? (
                <Casilla etiqueta="Confirmo que la instalación requiere obra y el cliente lo aceptó" error={formulario.formState.errors.confirmarObra?.message} {...formulario.register("confirmarObra")} />
            ) : null}
            {sugerido === null && cobertura.estado === "error" ? (
                <ErrorLectura error={cobertura.error} que="los routers de la cobertura" reintentar={cobertura.reintentar} />
            ) : null}
            {pideRouter || faltaRouter ? (
                <CampoSelector
                    etiqueta="Router del servicio"
                    ayuda="La factibilidad no sugirió router: elige desde dónde se atenderá en Argos."
                    error={formulario.formState.errors.routerId?.message}
                    {...formulario.register("routerId")}
                >
                    <option value="">Elige…</option>
                    {routers.map((r) => (
                        <option key={r.id} value={String(r.id)}>
                            {r.nombre}
                        </option>
                    ))}
                </CampoSelector>
            ) : null}
            <p className="text-sm text-muted-foreground">
                Al ganar se da de alta al cliente y el servicio en Argos (o se crea la orden de instalación si Iris trabaja sin conector).
            </p>
            {faltaRouter ? null : <AvisoAccion envio={envio} que="ganar el prospecto" recargar={recargar} />}
            <Pie etiqueta="Ganar" enviando={envio.enviando} deshabilitado={envio.conflicto || (sugerido === null && cobertura.estado === "cargando")}>
                <Trophy />
            </Pie>
        </form>
    );
}
