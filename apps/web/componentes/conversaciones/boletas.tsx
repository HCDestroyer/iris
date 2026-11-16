"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoArea, CampoSelector, Cargando, cn, DialogoCerrar, DialogoContenido, DialogoPie, DialogoProtegido, InsigniaEstado, useCambiosSinGuardar } from "@iris/ui";
import { Banknote, CircleAlert, CircleCheck, CircleX, Clock, Info, RotateCw, Send, TriangleAlert, type LucideIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { BotonVincularArgos } from "@/componentes/integraciones/abrir-argos";
import { ErrorApi } from "@/lib/api/errores";
import { PERMISO_ABRIR_ARGOS, PERMISO_APROBAR_BOLETA, PERMISO_RECHAZAR_BOLETA } from "@/lib/asistente/permisos";
import {
    aprobarBoleta,
    CAUSAS,
    debeReleer,
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
import { useSesion } from "@/lib/sesion";

const ICONO_ESTADO: Record<Boleta["estado"], LucideIcon> = { por_enviar: Send, registrada: Clock, aprobada: CircleCheck, rechazada: CircleX, fallida: TriangleAlert };

type Lista =
    | { fase: "cargando" }
    | { fase: "error"; error: unknown }
    /** `desactualizada`: el último refresco falló; lo leído sigue a la vista y se dice. */
    | { fase: "lista"; boletas: Boleta[]; desactualizada: boolean };

/**
 * Las boletas de la conversación. A diferencia de `useLectura`, un refresco que falla **no** tapa lo
 * que ya se leyó (la tarjeta y el diálogo abierto siguen ahí, marcados como sin actualizar), y
 * aplicar una decisión **aborta** el refresco pendiente: una lectura vieja no pisa el resultado.
 */
function useBoletas(conversacionId: number) {
    const [lista, setLista] = useState<Lista>({ fase: "cargando" });
    const control = useRef<AbortController | null>(null);

    const leer = useCallback(async () => {
        control.current?.abort();
        const c = new AbortController();
        control.current = c;
        try {
            const boletas = await leerBoletas(conversacionId, c.signal);
            if (!c.signal.aborted) setLista({ fase: "lista", boletas, desactualizada: false });
        } catch (error) {
            if (c.signal.aborted) return;
            setLista((l) => (l.fase === "lista" ? { ...l, desactualizada: true } : { fase: "error", error }));
        }
    }, [conversacionId]);

    useEffect(() => {
        void leer();
        return () => control.current?.abort();
    }, [leer]);

    /** La boleta como la devolvió una decisión o una relectura: gana sobre cualquier lectura en curso. */
    const aplicar = useCallback((b: Boleta) => {
        control.current?.abort();
        setLista((l) => (l.fase === "lista" ? { ...l, boletas: l.boletas.map((x) => (x.id === b.id ? b : x)) } : l));
    }, []);

    const reintentar = useCallback(() => {
        setLista((l) => (l.fase === "lista" ? l : { fase: "cargando" }));
        void leer();
    }, [leer]);

    return { lista, refrescar: leer, reintentar, aplicar };
}

/**
 * Boletas de la conversación en la ficha (pedido B3, contrato con Argos 0.7.0 §7.7): cada una con
 * su estado y, mientras está pendiente de revisar, «Aprobar y registrar pago» y «Rechazar» para
 * quien tiene `boletas.aprobar` / `boletas.rechazar`. Argos decide con el usuario de Argos
 * vinculado a quien pulsa; el asistente de IA nunca.
 */
export function BoletasConversacion({ conv }: { conv: ConversacionDetalle }) {
    const { puede } = useSesion();
    const { lista, refrescar, reintentar, aplicar } = useBoletas(conv.id);
    const [dialogo, setDialogo] = useState<{ boleta: Boleta; accion: AccionBoleta } | null>(null);
    /** Boletas que otra persona decidió mientras esta intentaba decidirlas. */
    const [decididasPorOtro, setDecididasPorOtro] = useState<readonly string[]>([]);
    /** Boletas cuyo estado no se pudo confirmar tras un 409: no se deciden hasta volver a leerlas. */
    const [sinConfirmar, setSinConfirmar] = useState<readonly string[]>([]);
    const [releyendo, setReleyendo] = useState<string | null>(null);
    // Un mensaje de sistema (boleta registrada, aprobada, rechazada) sube la versión: se releen.
    const version = useRef(conv.version);
    useEffect(() => {
        if (version.current === conv.version) return;
        version.current = conv.version;
        void refrescar();
    }, [conv.version, refrescar]);

    const confirmada = (b: Boleta) => {
        aplicar(b);
        setSinConfirmar((l) => l.filter((id) => id !== b.id));
    };

    async function releer(id: string) {
        setReleyendo(id);
        try {
            confirmada(await leerBoleta(id));
        } catch {
            // Sigue marcada: «Reintentar» queda a la vista.
        } finally {
            setReleyendo(null);
        }
    }

    const boletas = lista.fase === "lista" ? lista.boletas : [];
    const bloqueada = conv.soloLectura === true;
    let contenido: ReactNode = null;
    if (lista.fase === "error") {
        // Sin acceso a las boletas (o un servidor anterior): la ficha queda como antes.
        const sinAcceso = lista.error instanceof ErrorApi && (lista.error.estado === 403 || lista.error.estado === 404);
        contenido = sinAcceso ? null : (
            <Marco>
                <p role="alert" className="text-[0.8125rem] text-error">
                    No se pudieron leer las boletas.
                </p>
                <div>
                    <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                </div>
            </Marco>
        );
    } else if (lista.fase === "lista" && boletas.length > 0) {
        contenido = (
            <Marco>
                {lista.desactualizada ? (
                    <p role="alert" className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-warning" data-slot="boletas-desactualizadas">
                        <CircleAlert aria-hidden className="size-3.5 shrink-0" />
                        No se pudieron actualizar: pueden haber cambiado.
                        <Boton variante="enlace" tamano="sm" className="h-auto px-0 text-xs" onClick={() => void refrescar()}>
                            Reintentar
                        </Boton>
                    </p>
                ) : null}
                <ul aria-label="Boletas de la conversación" className="grid gap-2">
                    {boletas.map((b) => {
                        const dudosa = sinConfirmar.includes(b.id);
                        return (
                            <TarjetaBoleta
                                key={b.id}
                                boleta={b}
                                porOtro={decididasPorOtro.includes(b.id)}
                                sinConfirmar={dudosa}
                                releyendo={releyendo === b.id}
                                alReleer={() => void releer(b.id)}
                                puedeAprobar={b.puedeDecidir && !bloqueada && puede([PERMISO_APROBAR_BOLETA])}
                                puedeRechazar={b.puedeDecidir && !bloqueada && puede([PERMISO_RECHAZAR_BOLETA])}
                                alDecidir={(accion) => setDialogo({ boleta: b, accion })}
                            />
                        );
                    })}
                </ul>
            </Marco>
        );
    }

    // El diálogo va fuera de las ramas de arriba: un refresco de la lista (o su fallo) no lo desmonta
    // a media decisión. Muestra la boleta como está ahora en la lista (p. ej. tras cambiar el monto).
    const enDialogo = dialogo ? (boletas.find((b) => b.id === dialogo.boleta.id) ?? dialogo.boleta) : null;
    return (
        <>
            {contenido}
            {dialogo && enDialogo ? (
                <DialogoDecidir
                    key={`${dialogo.boleta.id}-${dialogo.accion}`}
                    boleta={enDialogo}
                    accion={dialogo.accion}
                    cliente={conv.titulo}
                    cerrar={(huboError) => {
                        setDialogo(null);
                        // Tras un error no se sabe cómo quedó en Argos: se vuelve a leer.
                        if (huboError) void refrescar();
                    }}
                    alDecidida={(b) => {
                        confirmada(b);
                        setDialogo(null);
                    }}
                    alDecididaPorOtro={(b) => {
                        confirmada(b);
                        setDecididasPorOtro((l) => [...l, b.id]);
                        setDialogo(null);
                    }}
                    alActualizada={confirmada}
                    alSinConfirmar={(id) => {
                        setSinConfirmar((l) => (l.includes(id) ? l : [...l, id]));
                        setDialogo(null);
                    }}
                />
            ) : null}
        </>
    );
}

function Marco({ children }: { children: ReactNode }) {
    return (
        <section aria-label="Boletas" data-slot="boletas" className="grid min-w-0 grid-cols-1 gap-2 border-b border-border px-4 py-3">
            <h3 className="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">Boletas de pago</h3>
            {children}
        </section>
    );
}

type PropsTarjeta = {
    boleta: Boleta;
    porOtro: boolean;
    sinConfirmar: boolean;
    releyendo: boolean;
    alReleer: () => void;
    puedeAprobar: boolean;
    puedeRechazar: boolean;
    alDecidir: (a: AccionBoleta) => void;
};

function TarjetaBoleta({ boleta: b, porOtro, sinConfirmar, releyendo, alReleer, puedeAprobar, puedeRechazar, alDecidir }: PropsTarjeta) {
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
            {sinConfirmar ? (
                // No se muestra un estado que no se pudo confirmar.
                <div role="alert" className="grid gap-1.5" data-slot="boleta-sin-confirmar">
                    <p className="flex items-start gap-1.5 text-xs font-medium text-warning">
                        <CircleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                        Cambió en Argos y no se pudo leer cómo quedó.
                    </p>
                    <div>
                        <Boton variante="secundario" tamano="sm" onClick={alReleer} disabled={releyendo}>
                            {releyendo ? <Cargando texto="Leyendo…" /> : <RotateCw />}
                            {releyendo ? null : "Reintentar"}
                        </Boton>
                    </div>
                </div>
            ) : (
                <div>
                    <InsigniaEstado tono={estado.tono} icono={ICONO_ESTADO[b.estado]} texto={estado.texto} />
                </div>
            )}
            {porOtro ? (
                <p role="status" className="flex items-start gap-1.5 text-xs font-medium text-foreground" data-slot="decidida-por-otro">
                    <Info aria-hidden className="mt-0.5 size-3.5 shrink-0 text-info" />
                    Ya la decidió otra persona.
                </p>
            ) : null}
            {quien && !sinConfirmar ? <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{quien}.</p> : null}
            {b.estado === "rechazada" && b.motivoRechazo && !sinConfirmar ? <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">Motivo: {b.motivoRechazo}</p> : null}
            {b.estado === "fallida" && b.detalle ? <p className="text-xs text-error [overflow-wrap:anywhere]">{b.detalle}</p> : null}
            {b.avisoCliente === "sin_aviso" ? <p className="text-xs font-medium text-warning [overflow-wrap:anywhere]">No se le pudo avisar al cliente por WhatsApp: avísale tú.</p> : null}
            {puedeAprobar || puedeRechazar ? (
                <div className="grid gap-1.5">
                    {puedeAprobar ? (
                        <Boton tamano="sm" onClick={() => alDecidir("aprobar")} disabled={sinConfirmar}>
                            <CircleCheck />
                            Aprobar y registrar pago
                        </Boton>
                    ) : null}
                    {puedeRechazar ? (
                        <Boton variante="contorno" tamano="sm" onClick={() => alDecidir("rechazar")} disabled={sinConfirmar}>
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
    /** La boleta como está ahora (se actualiza si una relectura la cambia). */
    boleta: Boleta;
    accion: AccionBoleta;
    cliente: string;
    cerrar: (huboError: boolean) => void;
    alDecidida: (b: Boleta) => void;
    alDecididaPorOtro: (b: Boleta) => void;
    /** Releída tras un 409 y sigue pendiente (p. ej. cambió el monto). */
    alActualizada: (b: Boleta) => void;
    /** Hubo un 409 y la relectura falló: no se sabe cómo quedó. */
    alSinConfirmar: (id: string) => void;
};

/**
 * Diálogo corto de la decisión. Aprobar: confirma el monto, con una nota opcional. Rechazar: el
 * motivo (se le dice al cliente) y la causa. Los errores dicen qué hacer: vincular el usuario de
 * Argos y reintentar con la misma clave, reintentar para confirmar si Argos no respondió, o por qué
 * no se puede. Todo 409 relee la boleta antes de decir nada.
 */
function DialogoDecidir({ boleta, accion, cliente, cerrar, alDecidida, alDecididaPorOtro, alActualizada, alSinConfirmar }: PropsDialogo) {
    const { puede, yo } = useSesion();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<ErrorBoleta | null>(null);
    const [huboError, setHuboError] = useState(false);
    const aprobar = accion === "aprobar";
    const form = useForm<Valores>({
        resolver: zodResolver(aprobar ? esquemaAprobar.extend({ motivo: esquemaAprobar.shape.nota, causa: esquemaRechazar.shape.causa }) : esquemaRechazar.extend({ nota: esquemaAprobar.shape.nota })),
        defaultValues: { nota: "", motivo: "", causa: "otro" },
    });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const errores = form.formState.errors;
    const monto = montoLegible(boleta.monto);
    const exito = (b: Boleta) => avisar({ titulo: b.estado === "aprobada" ? `Boleta ${b.referencia} aprobada: pago de ${montoLegible(b.monto)} registrado en Argos` : `Boleta ${b.referencia} rechazada` });

    async function enviar(d: Valores) {
        setError(null);
        try {
            const b = aprobar ? await aprobarBoleta(boleta.id, d.nota.trim(), clave.actual()) : await rechazarBoleta(boleta.id, { motivo: d.motivo.trim(), causa: d.causa }, clave.actual());
            clave.renovar();
            // El monto del aviso es el que registró el servidor, no el que se veía aquí.
            exito(b);
            alDecidida(b);
        } catch (e) {
            if (erroresACampos(e, ["nota", "motivo", "causa"], form.setError)) {
                clave.trasError(e);
                return;
            }
            setHuboError(true);
            const api = e instanceof ErrorApi ? e : new ErrorApi({ estado: 0, codigo: "sin_conexion", titulo: "Sin conexión" });
            const explicado = explicarErrorBoleta(api, accion);
            // Tras vincular se reintenta con la misma clave; en lo demás, la regla de siempre.
            if (explicado.tipo !== "vincular") clave.trasError(e);
            if (debeReleer(api)) {
                let actual: Boleta;
                try {
                    actual = await leerBoleta(boleta.id);
                } catch {
                    alSinConfirmar(boleta.id);
                    return;
                }
                if (actual.estado !== "registrada") {
                    // Si quien la decidió es quien está aquí (un intento anterior sí llegó), es un éxito.
                    if (actual.decididaPor?.id === yo.usuario.id) {
                        exito(actual);
                        alDecidida(actual);
                    } else alDecididaPorOtro(actual);
                    return;
                }
                alActualizada(actual);
            }
            setError(explicado);
        }
    }

    const sinSalida = error?.tipo === "explicar" || error?.tipo === "releer";
    const reintenta = error?.tipo === "vincular" || error?.tipo === "reintentar";
    const textoBoton = reintenta ? "Reintentar" : aprobar ? "Aprobar y registrar pago" : "Rechazar boleta";
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar(huboError)}>
            <DialogoContenido titulo={aprobar ? "Aprobar y registrar pago" : "Rechazar boleta"} descripcion={`${cliente} · boleta ${boleta.referencia} · ${monto}`}>
                <form noValidate className="grid gap-4" onSubmit={form.handleSubmit(enviar)}>
                    {aprobar ? (
                        <>
                            <p className="text-sm">
                                Argos registra un pago de{" "}
                                <span className="font-semibold tabular-nums" data-slot="monto-a-aprobar">
                                    {monto}
                                </span>{" "}
                                a tu nombre y le avisa al cliente en la conversación. Si el depósito es por otro monto, recházala.
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
                        <Aviso tono={error.tipo === "vincular" || error.tipo === "confirmar" ? "warning" : "error"} titulo={error.titulo} data-error-boleta={error.tipo}>
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
                                {reintenta ? <RotateCw /> : null}
                                {form.formState.isSubmitting ? (aprobar ? "Aprobando…" : "Rechazando…") : textoBoton}
                            </Boton>
                        )}
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
