"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import {
    Aviso,
    Boton,
    Campo,
    CampoSelector,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Esqueleto,
    Tarjeta,
    TarjetaContenido,
    TarjetaDescripcion,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { Link2, Link2Off, Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { textoFechaHora } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";

type Vinculo = Schema<"VinculoSSO">;
type Conector = Schema<"Conector">;

const SISTEMAS: Record<string, string> = { argosisp: "Argos", horus: "Horus Flow" };
const nombreSistema = (s: string) => SISTEMAS[s] ?? s;

/** Mensaje de los errores de crear un vínculo (409/422 de la API). */
export function mensajeVinculo(e: unknown): string {
    const codigo = e instanceof ErrorApi ? e.codigo : undefined;
    switch (codigo) {
        case "vinculo_existente":
            return "Este usuario ya tiene una cuenta de Argos vinculada: quítala primero.";
        case "cuenta_externa_vinculada":
            return "Esa cuenta de Argos ya está vinculada con otro usuario de Iris.";
        case "conector_invalido":
            return "El conector no es el de Argos activo. Recarga y elige otro.";
        case "usuario_no_vinculable":
            return "Este usuario no se puede vincular (inactivo, de integración o sin contraseña).";
        default:
            return mensajeDeError(e).detalle ?? mensajeDeError(e).titulo;
    }
}

/**
 * «Cuentas vinculadas» (autologin desde Argos): lista, «Quitar» y, para un administrador
 * (`usuarios.editar`) en el detalle de otro usuario, «Vincular cuenta de Argos». En una sesión
 * abierta desde Argos, quitar o vincular pide la contraseña (lo resuelve el cliente HTTP).
 */
export function VinculosSSO({ usuarioId, propio }: { usuarioId: number; propio: boolean }) {
    const { puede } = useSesion();
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/usuarios/{id}/vinculos-sso", { params: { path: { id: usuarioId } }, signal }))).data ?? []);
    const [quitar, setQuitar] = useState<Vinculo | null>(null);
    const puedeQuitar = propio || puede(["usuarios.editar"]);
    const puedeVincular = !propio && puede(["usuarios.editar"]);

    return (
        <Tarjeta data-testid="vinculos-sso">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-start justify-between gap-3">
                <div className="grid gap-1">
                    <TarjetaTitulo>Cuentas vinculadas</TarjetaTitulo>
                    <TarjetaDescripcion>
                        {propio ? "Cuentas de otros sistemas con que entras a Iris sin contraseña («Ver en Iris» desde Argos)." : "Cuentas de otros sistemas con que este usuario entra a Iris («Ver en Iris» desde Argos)."}
                    </TarjetaDescripcion>
                </div>
                {puedeVincular && lectura.estado === "listo" ? <DialogoVincular usuarioId={usuarioId} alVincular={() => void lectura.refrescar()} /> : null}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-14" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las cuentas vinculadas" />
                ) : lectura.datos.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin cuentas vinculadas.</p>
                ) : (
                    <ul className="grid gap-2" aria-label="Cuentas vinculadas">
                        {lectura.datos.map((v) => (
                            <li key={v.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3" data-vinculo={v.id}>
                                <div className="grid min-w-0 gap-0.5">
                                    <span className="inline-flex items-center gap-1.5 text-sm font-medium">
                                        <Link2 aria-hidden className="size-4 text-muted-foreground" />
                                        {nombreSistema(v.sistema)} · <span className="font-mono break-all">{v.usuarioExterno}</span>
                                    </span>
                                    <span className="text-xs text-muted-foreground tabular-nums">Vinculada el {textoFechaHora(v.creadoEn)}</span>
                                </div>
                                {puedeQuitar ? (
                                    <Boton variante="secundario" tamano="sm" onClick={() => setQuitar(v)} aria-label={`Quitar el vínculo con ${nombreSistema(v.sistema)} ${v.usuarioExterno}`}>
                                        <Link2Off />
                                        Quitar
                                    </Boton>
                                ) : null}
                            </li>
                        ))}
                    </ul>
                )}
            </TarjetaContenido>
            {quitar ? (
                <DialogoQuitar
                    usuarioId={usuarioId}
                    vinculo={quitar}
                    propio={propio}
                    alCerrar={() => setQuitar(null)}
                    alQuitar={() => {
                        setQuitar(null);
                        void lectura.refrescar();
                    }}
                />
            ) : null}
        </Tarjeta>
    );
}

const esquemaQuitar = z.object({ motivo: z.string().trim().max(500, "Como mucho 500 caracteres") });

function DialogoQuitar({ usuarioId, vinculo, propio, alCerrar, alQuitar }: { usuarioId: number; vinculo: Vinculo; propio: boolean; alCerrar: () => void; alQuitar: () => void }) {
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [error, setError] = useState<unknown>(null);
    const form = useForm<z.infer<typeof esquemaQuitar>>({ resolver: zodResolver(esquemaQuitar), defaultValues: { motivo: "" } });
    const { isSubmitting, errors } = form.formState;

    async function enviar(d: z.infer<typeof esquemaQuitar>) {
        setError(null);
        try {
            await resolver(
                api().POST("/api/v1/usuarios/{id}/vinculos-sso/{vinculoId}/borrar", {
                    params: { path: { id: usuarioId, vinculoId: vinculo.id }, header: { "Idempotency-Key": clave } },
                    body: d.motivo ? { motivo: d.motivo } : {},
                }),
            );
            alQuitar();
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        }
    }

    return (
        <DialogoProtegido sucio={form.formState.isDirty} open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido
                titulo="Quitar el vínculo"
                descripcion={`${propio ? "Ya no podrás" : "Ya no podrá"} entrar desde ${nombreSistema(vinculo.sistema)} con la cuenta ${vinculo.usuarioExterno} sin contraseña.`}
            >
                <form className="grid gap-4" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                    {propio ? null : <Campo etiqueta="Motivo (opcional)" maxLength={500} error={errors.motivo?.message} {...form.register("motivo")} />}
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo quitar el vínculo">
                            {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton type="button" variante="secundario">
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <Boton type="submit" variante="peligro" disabled={isSubmitting}>
                            <Link2Off />
                            {isSubmitting ? "Quitando…" : "Quitar vínculo"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const esquemaVincular = z.object({
    conectorId: z.string().min(1, "Elige el conector de Argos"),
    idExterno: z.string().trim().min(1, "Escribe el id de la cuenta en Argos").max(200, "Como mucho 200 caracteres"),
});

/** Vincular a mano (caso `vinculo_requiere_admin`): conector activo de Argos e id de la cuenta en Argos. */
function DialogoVincular({ usuarioId, alVincular }: { usuarioId: number; alVincular: () => void }) {
    const { puede } = useSesion();
    const [abierto, setAbierto] = useState(false);
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [error, setError] = useState<unknown>(null);
    const verConectores = puede(["integraciones.ver"]);
    const conectores = useLectura<Conector[] | null>(async (signal) =>
        verConectores ? ((await resolver(api().GET("/api/v1/integraciones/conectores", { params: { query: { limit: 50 } }, signal }))).data ?? []) : null,
    );
    const argos = conectores.estado === "listo" ? (conectores.datos ?? []).filter((c) => c.sistema === "argosisp" && c.estado === "activo") : [];
    const form = useForm<z.infer<typeof esquemaVincular>>({ resolver: zodResolver(esquemaVincular), defaultValues: { conectorId: "", idExterno: "" } });
    const { isSubmitting, errors } = form.formState;

    async function enviar(d: z.infer<typeof esquemaVincular>) {
        setError(null);
        try {
            await resolver(
                api().POST("/api/v1/usuarios/{id}/vinculos-sso", {
                    params: { path: { id: usuarioId }, header: { "Idempotency-Key": clave } },
                    body: { conector_id: Number(d.conectorId), id_externo: d.idExterno },
                }),
            );
            setClave(nuevaClaveIdempotencia());
            form.reset({ conectorId: "", idExterno: "" });
            setAbierto(false);
            alVincular();
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        }
    }

    return (
        <>
            <Boton variante="secundario" tamano="sm" onClick={() => setAbierto(true)}>
                <Plus />
                Vincular cuenta de Argos
            </Boton>
            <DialogoProtegido sucio={form.formState.isDirty} open={abierto} onOpenChange={setAbierto}>
                <DialogoContenido titulo="Vincular cuenta de Argos" descripcion="Para cuando el usuario no puede vincularla él mismo. Comprueba antes que la cuenta de Argos sea suya.">
                    <form className="grid gap-4" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                        {!verConectores ? (
                            <Aviso tono="warning" titulo="Falta un permiso">
                                Para elegir el conector de Argos necesitas ver las integraciones.
                            </Aviso>
                        ) : conectores.estado === "error" ? (
                            <ErrorLectura error={conectores.error} reintentar={conectores.reintentar} que="los conectores" />
                        ) : conectores.estado === "listo" && argos.length === 0 ? (
                            <Aviso tono="warning" titulo="No hay un conector de Argos activo">
                                Conecta y activa Argos en Administración › Integraciones.
                            </Aviso>
                        ) : (
                            <CampoSelector etiqueta="Conector de Argos" error={errors.conectorId?.message} disabled={conectores.estado !== "listo"} {...form.register("conectorId")}>
                                <option value="">Elige…</option>
                                {argos.map((c) => (
                                    <option key={c.id} value={c.id}>
                                        Argos · {c.baseUrl}
                                    </option>
                                ))}
                            </CampoSelector>
                        )}
                        <Campo etiqueta="Id de la cuenta en Argos" autoComplete="off" maxLength={200} error={errors.idExterno?.message} {...form.register("idExterno")} />
                        {error ? (
                            <Aviso tono="error" titulo="No se pudo vincular">
                                {mensajeVinculo(error)}
                            </Aviso>
                        ) : null}
                        <DialogoPie>
                            <Boton type="button" variante="secundario" onClick={() => setAbierto(false)}>
                                Cancelar
                            </Boton>
                            <Boton type="submit" disabled={isSubmitting || argos.length === 0}>
                                <Link2 />
                                {isSubmitting ? "Vinculando…" : "Vincular"}
                            </Boton>
                        </DialogoPie>
                    </form>
                </DialogoContenido>
            </DialogoProtegido>
        </>
    );
}
