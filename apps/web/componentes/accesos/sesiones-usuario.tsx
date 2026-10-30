"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, CampoArea, Dialogo, DialogoContenido, DialogoPie, Esqueleto, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { LogOut, MonitorCheck } from "lucide-react";
import { useRef, useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { Canal } from "./insignias";

type SesionActiva = Schema<"SesionActiva">;
type Cierre = { tipo: "una"; sesion: SesionActiva } | { tipo: "todas" };

/** Sesiones abiertas del usuario y las acciones explícitas para cerrar una o todas. */
export function SesionesUsuario({ usuarioId, propio }: { usuarioId: number; propio: boolean }) {
    const { puede } = useSesion();
    const lectura = useLectura<{ sesiones: SesionActiva[]; leidaEn: number }>(async (signal) => ({
        sesiones: (await resolver(api().GET("/api/v1/usuarios/{id}/sesiones", { params: { path: { id: usuarioId } }, signal }))).data ?? [],
        leidaEn: Date.now(),
    }));
    const [cierre, setCierre] = useState<Cierre | null>(null);
    const puedeCerrar = puede(["usuarios.cerrar_sesiones"]);

    let contenido;
    if (lectura.estado === "cargando") {
        contenido = <Esqueleto className="h-20" />;
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} que="las sesiones" reintentar={lectura.reintentar} />;
    } else if (lectura.datos.sesiones.length === 0) {
        contenido = <p className="text-sm text-muted-foreground">No tiene sesiones abiertas.</p>;
    } else {
        const { sesiones, leidaEn: ahora } = lectura.datos;
        const cerrables = sesiones.filter((s) => !s.actual);
        contenido = (
            <>
                <ul className="grid gap-2" aria-label="Sesiones abiertas">
                    {sesiones.map((s) => (
                        <li key={s.id} data-sesion={s.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2">
                            <div className="grid min-w-0 gap-0.5">
                                <span className="flex flex-wrap items-center gap-2">
                                    <Canal canal={s.canal} />
                                    {s.actual ? <InsigniaEstado tono="info" icono={MonitorCheck} texto="Esta sesión" /> : null}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                    {s.dispositivo || "Dispositivo desconocido"} · IP {s.ip || "—"}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                    Abierta {formatearFecha(s.creadaEn)} · último uso {haceCuanto(s.ultimoUso, ahora)} · vence {formatearFecha(s.expiraEn)}
                                </span>
                            </div>
                            {puedeCerrar && !s.actual ? (
                                <Boton variante="secundario" tamano="sm" onClick={() => setCierre({ tipo: "una", sesion: s })}>
                                    <LogOut />
                                    Cerrar
                                </Boton>
                            ) : null}
                        </li>
                    ))}
                </ul>
                {puedeCerrar && cerrables.length > 1 ? (
                    <Boton variante="secundario" className="justify-self-start" onClick={() => setCierre({ tipo: "todas" })}>
                        <LogOut />
                        {propio ? "Cerrar las demás sesiones" : "Cerrar todas las sesiones"}
                    </Boton>
                ) : null}
            </>
        );
    }
    return (
        <Tarjeta data-seccion="sesiones">
            <TarjetaEncabezado>
                <TarjetaTitulo>Sesiones activas</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">{contenido}</TarjetaContenido>
            {cierre ? (
                <DialogoCerrar
                    usuarioId={usuarioId}
                    cierre={cierre}
                    propio={propio}
                    alCerrar={() => {
                        setCierre(null);
                        void lectura.refrescar();
                    }}
                    alCancelar={() => setCierre(null)}
                />
            ) : null}
        </Tarjeta>
    );
}

function DialogoCerrar({ usuarioId, cierre, propio, alCerrar, alCancelar }: { usuarioId: number; cierre: Cierre; propio: boolean; alCerrar: () => void; alCancelar: () => void }) {
    const [motivo, setMotivo] = useState("");
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());

    async function confirmar() {
        setEnviando(true);
        setError(null);
        const cuerpo = motivo.trim() ? { motivo: motivo.trim() } : {};
        const cabecera = { "Idempotency-Key": clave.current };
        try {
            if (cierre.tipo === "una") {
                await resolver(
                    api().POST("/api/v1/usuarios/{id}/sesiones/{sesionId}/cerrar", { params: { path: { id: usuarioId, sesionId: cierre.sesion.id }, header: cabecera }, body: cuerpo }),
                );
            } else {
                await resolver(api().POST("/api/v1/usuarios/{id}/cerrar-sesiones", { params: { path: { id: usuarioId }, header: cabecera }, body: cuerpo }));
            }
            alCerrar();
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    return (
        <Dialogo open onOpenChange={(a) => (a ? undefined : alCancelar())}>
            <DialogoContenido
                titulo={cierre.tipo === "una" ? "¿Cerrar esta sesión?" : propio ? "¿Cerrar tus demás sesiones?" : "¿Cerrar todas sus sesiones?"}
                descripcion={
                    cierre.tipo === "una"
                        ? "Quien la use tendrá que volver a entrar. Los cambios sin enviar de la app de campo se conservan en el teléfono."
                        : "Tendrá que volver a entrar en cada dispositivo. Los cambios sin enviar de la app de campo se conservan en el teléfono."
                }
            >
                <div className="grid gap-4">
                    <CampoArea etiqueta="Motivo (opcional)" rows={2} maxLength={200} value={motivo} onChange={(e) => setMotivo(e.target.value)} />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo cerrar">
                            {mensajeDeError(error).detalle}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <Boton type="button" variante="secundario" onClick={alCancelar}>
                            Cancelar
                        </Boton>
                        <Boton type="button" onClick={() => void confirmar()} disabled={enviando}>
                            <LogOut />
                            {enviando ? "Cerrando…" : cierre.tipo === "una" ? "Cerrar sesión" : "Cerrar sesiones"}
                        </Boton>
                    </DialogoPie>
                </div>
            </DialogoContenido>
        </Dialogo>
    );
}
