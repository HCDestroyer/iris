"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoArea, CampoSelector, cn, DialogoCerrar, DialogoContenido, DialogoPie, DialogoProtegido, InsigniaEstado, useCambiosSinGuardar } from "@iris/ui";
import { Banknote, CircleCheck, CircleX, Clock, Info, RotateCw, Send, TriangleAlert, type LucideIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { BotonVincularArgos } from "@/componentes/integraciones/abrir-argos";
import { ErrorApi } from "@/lib/api/errores";
import { PERMISO_ABRIR_ARGOS, PERMISO_APROBAR_BOLETA, PERMISO_RECHAZAR_BOLETA } from "@/lib/asistente/permisos";
import {
    aprobarBoleta,
    CAUSAS,
    esquemaAprobar,
    esquemaRechazar,
    estadoBoleta,
    explicarErrorBoleta,
    leerBoleta,
    leerBoletas,
    montoLegible,
    quienDecidio,
    rechazarBoleta,
    type AccionBoleta,
    type Boleta,
    type CausaRechazo,
    type ErrorBoleta,
} from "@/lib/conversaciones/boletas";
import type { ConversacionDetalle } from "@/lib/conversaciones/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

const ICONO_ESTADO: Record<Boleta["estado"], LucideIcon> = { por_enviar: Send, registrada: Clock, aprobada: CircleCheck, rechazada: CircleX, fallida: TriangleAlert };

/**
 * Boletas de la conversación en la ficha (pedido B3, contrato con Argos 0.7.0 §7.7): cada una con
 * su estado y, mientras está pendiente de revisar, «Aprobar y registrar pago» y «Rechazar» para
 * quien tiene `boletas.aprobar` / `boletas.rechazar`. Argos decide con el usuario de Argos
 * vinculado a quien pulsa; el asistente de IA nunca.
 */
export function BoletasConversacion({ conv }: { conv: ConversacionDetalle }) {
    const { puede } = useSesion();
    const lectura = useLectura((signal) => leerBoletas(conv.id, signal));
    const [dialogo, setDialogo] = useState<{ boleta: Boleta; accion: AccionBoleta } | null>(null);
    /** Boletas que otra persona decidió mientras esta intentaba decidirlas. */
    const [decididasPorOtro, setDecididasPorOtro] = useState<readonly string[]>([]);
    // Un mensaje de sistema (boleta registrada, aprobada, rechazada) sube la versión: se releen.
    const refrescar = lectura.refrescar;
    const version = useRef(conv.version);
    useEffect(() => {
        if (version.current === conv.version) return;
        version.current = conv.version;
        void refrescar();
    }, [conv.version, refrescar]);

    if (lectura.estado === "cargando") return null;
    if (lectura.estado === "error") {
        // Sin acceso a las boletas (o un servidor anterior): la ficha queda como antes.
        if (lectura.error instanceof ErrorApi && (lectura.error.estado === 403 || lectura.error.estado === 404)) return null;
        return (
            <Marco>
                <p role="alert" className="text-[0.8125rem] text-error">
                    No se pudieron leer las boletas.
                </p>
                <div>
                    <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                </div>
            </Marco>
        );
    }
    const boletas = lectura.datos;
    if (boletas.length === 0) return null;
    const reemplazar = (b: Boleta) => lectura.reemplazar(boletas.map((x) => (x.id === b.id ? b : x)));
    const bloqueada = conv.soloLectura === true;
    return (
        <Marco>
            <ul aria-label="Boletas de la conversación" className="grid gap-2">
                {boletas.map((b) => (
                    <TarjetaBoleta
                        key={b.id}
                        boleta={b}
                        porOtro={decididasPorOtro.includes(b.id)}
                        puedeAprobar={b.puedeDecidir && !bloqueada && puede([PERMISO_APROBAR_BOLETA])}
                        puedeRechazar={b.puedeDecidir && !bloqueada && puede([PERMISO_RECHAZAR_BOLETA])}
                        alDecidir={(accion) => setDialogo({ boleta: b, accion })}
                    />
                ))}
            </ul>
            {dialogo ? (
                <DialogoDecidir
                    key={`${dialogo.boleta.id}-${dialogo.accion}`}
                    boleta={dialogo.boleta}
                    accion={dialogo.accion}
                    cliente={conv.titulo}
                    cerrar={() => setDialogo(null)}
                    alDecidida={(b) => {
                        reemplazar(b);
                        setDialogo(null);
                    }}
                    alDecididaPorOtro={(b) => {
                        reemplazar(b);
                        setDecididasPorOtro((l) => [...l, b.id]);
                        setDialogo(null);
                    }}
                />
            ) : null}
        </Marco>
    );
}

function Marco({ children }: { children: React.ReactNode }) {
    return (
        <section aria-label="Boletas" data-slot="boletas" className="grid min-w-0 grid-cols-1 gap-2 border-b border-border px-4 py-3">
            <h3 className="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">Boletas de pago</h3>
            {children}
        </section>
    );
}

function TarjetaBoleta({ boleta: b, porOtro, puedeAprobar, puedeRechazar, alDecidir }: { boleta: Boleta; porOtro: boolean; puedeAprobar: boolean; puedeRechazar: boolean; alDecidir: (a: AccionBoleta) => void }) {
    const estado = estadoBoleta(b);
    const quien = quienDecidio(b);
    return (
        <li data-boleta={b.id} data-estado={b.estado} className="grid min-w-0 gap-2 rounded-lg border border-border bg-background p-3">
            <div className="flex min-w-0 items-start gap-2.5">
                <Banknote aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <div className="grid min-w-0 flex-1 gap-0.5">
                    <span className="text-[0.9375rem] font-semibold tabular-nums">{montoLegible(b.monto)}</span>
                    <span className="truncate text-xs text-muted-foreground">Boleta {b.referencia}</span>
                </div>
            </div>
            <div>
                <InsigniaEstado tono={estado.tono} icono={ICONO_ESTADO[b.estado]} texto={estado.texto} />
            </div>
            {porOtro ? (
                <p role="status" className="flex items-start gap-1.5 text-xs font-medium text-foreground" data-slot="decidida-por-otro">
                    <Info aria-hidden className="mt-0.5 size-3.5 shrink-0 text-info" />
                    Ya la decidió otra persona.
                </p>
            ) : null}
            {quien ? <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{quien}.</p> : null}
            {b.estado === "rechazada" && b.motivoRechazo ? <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">Motivo: {b.motivoRechazo}</p> : null}
            {b.estado === "fallida" && b.detalle ? <p className="text-xs text-error [overflow-wrap:anywhere]">{b.detalle}</p> : null}
            {b.avisoCliente === "sin_aviso" ? <p className="text-xs font-medium text-warning [overflow-wrap:anywhere]">No se le pudo avisar al cliente por WhatsApp: avísale tú.</p> : null}
            {puedeAprobar || puedeRechazar ? (
                <div className="grid gap-1.5">
                    {puedeAprobar ? (
                        <Boton tamano="sm" onClick={() => alDecidir("aprobar")}>
                            <CircleCheck />
                            Aprobar y registrar pago
                        </Boton>
                    ) : null}
                    {puedeRechazar ? (
                        <Boton variante="contorno" tamano="sm" onClick={() => alDecidir("rechazar")}>
                            <CircleX />
                            Rechazar
                        </Boton>
                    ) : null}
                </div>
            ) : null}
        </li>
    );
}

type Valores = { nota: string; motivo: string; causa: CausaRechazo };

type PropsDialogo = {
    boleta: Boleta;
    accion: AccionBoleta;
    cliente: string;
    cerrar: () => void;
    alDecidida: (b: Boleta) => void;
    alDecididaPorOtro: (b: Boleta) => void;
};

/**
 * Diálogo corto de la decisión. Aprobar: confirma el monto, con una nota opcional. Rechazar: el
 * motivo (se le dice al cliente) y la causa. Los errores dicen qué hacer: vincular el usuario de
 * Argos y reintentar con la misma clave, reintentar si Argos no respondió, o por qué no se puede.
 */
function DialogoDecidir({ boleta, accion, cliente, cerrar, alDecidida, alDecididaPorOtro }: PropsDialogo) {
    const { puede } = useSesion();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<ErrorBoleta | null>(null);
    const aprobar = accion === "aprobar";
    const form = useForm<Valores>({
        resolver: zodResolver(aprobar ? esquemaAprobar.extend({ motivo: esquemaAprobar.shape.nota, causa: esquemaRechazar.shape.causa }) : esquemaRechazar.extend({ nota: esquemaAprobar.shape.nota })),
        defaultValues: { nota: "", motivo: "", causa: "otro" },
    });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const errores = form.formState.errors;
    const monto = montoLegible(boleta.monto);

    async function enviar(d: Valores) {
        setError(null);
        try {
            const b = aprobar ? await aprobarBoleta(boleta.id, d.nota.trim(), clave.actual()) : await rechazarBoleta(boleta.id, { motivo: d.motivo.trim(), causa: d.causa }, clave.actual());
            clave.renovar();
            avisar({ titulo: aprobar ? `Boleta ${b.referencia} aprobada: pago de ${monto} registrado en Argos` : `Boleta ${b.referencia} rechazada` });
            alDecidida(b);
        } catch (e) {
            if (erroresACampos(e, ["nota", "motivo", "causa"], form.setError)) {
                clave.trasError(e);
                return;
            }
            const api = e instanceof ErrorApi ? e : new ErrorApi({ estado: 0, codigo: "sin_conexion", titulo: "Sin conexión" });
            const explicado = explicarErrorBoleta(api, accion);
            // Tras vincular se reintenta con la misma clave; en lo demás, la regla de siempre.
            if (explicado.tipo !== "vincular") clave.trasError(e);
            if (explicado.tipo === "releer") {
                try {
                    alDecididaPorOtro(await leerBoleta(boleta.id));
                    return;
                } catch {
                    // No se pudo releer: se explica aquí y la ficha la relee al cerrar.
                }
            }
            setError(explicado);
        }
    }

    const sinSalida = error?.tipo === "explicar" || error?.tipo === "releer";
    const textoBoton = error?.tipo === "vincular" || error?.tipo === "reintentar" ? "Reintentar" : aprobar ? "Aprobar y registrar pago" : "Rechazar boleta";
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo={aprobar ? "Aprobar y registrar pago" : "Rechazar boleta"} descripcion={`${cliente} · boleta ${boleta.referencia} · ${monto}`}>
                <form noValidate className="grid gap-4" onSubmit={form.handleSubmit(enviar)}>
                    {aprobar ? (
                        <>
                            <p className="text-sm">
                                Argos registra un pago de <span className="font-semibold tabular-nums">{monto}</span> a tu nombre y le avisa al cliente en la conversación. Si el depósito es por otro monto, recházala.
                            </p>
                            <CampoArea etiqueta="Nota para caja (opcional)" rows={2} maxLength={400} ayuda="Queda en Argos. El cliente no la ve." error={errores.nota?.message} {...form.register("nota")} />
                        </>
                    ) : (
                        <>
                            <CampoArea etiqueta="Motivo" rows={3} maxLength={500} ayuda="Se le dice al cliente." error={errores.motivo?.message} {...form.register("motivo")} />
                            <CampoSelector etiqueta="Causa" error={errores.causa?.message} {...form.register("causa")}>
                                {CAUSAS.map((c) => (
                                    <option key={c.valor} value={c.valor}>
                                        {c.texto}
                                    </option>
                                ))}
                            </CampoSelector>
                        </>
                    )}
                    {error ? (
                        <Aviso tono={error.tipo === "vincular" ? "warning" : "error"} titulo={error.titulo} data-error-boleta={error.tipo}>
                            <div className="grid gap-2">
                                <span>{error.texto}</span>
                                {error.tipo === "vincular" ? (
                                    puede([PERMISO_ABRIR_ARGOS]) ? (
                                        <BotonVincularArgos />
                                    ) : (
                                        <span className="font-medium">Pide a un administrador de Iris el permiso para abrir Argos desde aquí.</span>
                                    )
                                ) : null}
                            </div>
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno">{sinSalida ? "Cerrar" : "Cancelar"}</Boton>
                        </DialogoCerrar>
                        {sinSalida ? null : (
                            <Boton type="submit" variante={aprobar || error ? "primario" : "peligro"} disabled={form.formState.isSubmitting} className={cn(form.formState.isSubmitting && "opacity-80")}>
                                {error?.tipo === "vincular" || error?.tipo === "reintentar" ? <RotateCw /> : null}
                                {form.formState.isSubmitting ? (aprobar ? "Aprobando…" : "Rechazando…") : textoBoton}
                            </Boton>
                        )}
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
