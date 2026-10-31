"use client";

import { avisar, Aviso, Boton, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie, Esqueleto, InsigniaEstado, Tarjeta } from "@iris/ui";
import { CalendarDays, CalendarOff, CalendarPlus, Clock, Pencil, Phone, CircleX } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState, type ReactNode } from "react";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { useCatalogosAgenda } from "@/componentes/despacho/datos";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { CambiarEstado } from "@/componentes/equipo/cambiar-estado";
import { FormularioAusencia } from "@/componentes/equipo/formulario-ausencia";
import { FormularioTecnico, textoZonas } from "@/componentes/equipo/formulario-tecnico";
import { FormularioTurnos } from "@/componentes/equipo/formulario-turnos";
import { InsigniaActivo } from "@/componentes/equipo/insignias";
import { ErrorLectura } from "@/componentes/error-lectura";
import { DIAS_SEMANA, textoMotivo, type Ausencia, type TecnicoDetalle } from "@/lib/agenda/esquemas";
import { api, resolver } from "@/lib/api/cliente";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { mensajeDeError } from "@/lib/api/errores";
import { diaDesdeIso, etiquetaDia, horaDe } from "@/lib/despacho/fechas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type Abierto = "editar" | "turnos" | "ausencia" | null;

/** Ficha de un técnico: datos de contacto, cuadrilla y zonas, turnos semanales y ausencias. */
export function FichaTecnico() {
    const params = useParams<{ id: string }>();
    const id = Number(params.id);
    const { puede } = useSesion();
    const catalogos = useCatalogosAgenda();
    const ficha = useLectura<TecnicoDetalle>((signal) => resolver(api().GET("/api/v1/tecnicos/{id}", { params: { path: { id } }, signal })));
    useNombreEnMigas(ficha.estado === "listo" ? ficha.datos?.nombre : null);
    const [abierto, setAbierto] = useState<Abierto>(null);
    const cuadrillas = catalogos.cuadrillas.estado === "listo" ? catalogos.cuadrillas.datos : undefined;
    const zonas = catalogos.zonas.estado === "listo" ? catalogos.zonas.datos : undefined;

    if (ficha.estado === "cargando") {
        return (
            <>
                <div className="grid gap-3" aria-busy="true" aria-label="Cargando la ficha">
                    <Esqueleto className="h-10 w-64" />
                    <Esqueleto className="h-40" />
                    <Esqueleto className="h-40" />
                </div>
            </>
        );
    }
    if (ficha.estado === "error") {
        return (
            <>
                <ErrorLectura error={ficha.error} que="la ficha del técnico" reintentar={ficha.reintentar} />
            </>
        );
    }
    const t = ficha.datos;
    const cuadrilla = cuadrillas?.find((c) => c.id === t.cuadrillaId);
    const turnos = [...(t.turnos ?? [])].sort((a, b) => a.dia - b.dia || a.inicio.localeCompare(b.inicio));
    const ausencias = [...(t.ausencias ?? [])].filter((a) => a.anuladaEn === null).sort((a, b) => a.inicio.localeCompare(b.inicio));
    const tras = (nueva?: TecnicoDetalle) => {
        setAbierto(null);
        if (nueva) ficha.reemplazar(nueva);
        else void ficha.refrescar();
    };

    return (
        <>
            <EncabezadoPagina
                titulo={t.nombre}
                descripcion={t.correo}
                acciones={
                    <div className="flex flex-wrap items-center gap-2">
                        <InsigniaActivo activo={t.activo} />
                        <Boton asChild variante="secundario" tamano="sm">
                            <Link href={`/agenda?tecnico=${t.usuarioId}`}>
                                <CalendarDays />
                                Ver su agenda
                            </Link>
                        </Boton>
                        <CambiarEstado
                            activo={t.activo}
                            nombre={t.nombre}
                            puede={puede([t.activo ? "agenda.desactivar_tecnico" : "agenda.activar_tecnico"])}
                            consecuencia="Deja de ser asignable en la agenda y el despacho. Sus citas ya programadas no se mueven: reprográmalas si hace falta."
                            ejecutar={(accion, clave) =>
                                resolver(
                                    api().POST(accion === "activar" ? "/api/v1/tecnicos/{id}/activar" : "/api/v1/tecnicos/{id}/desactivar", {
                                        params: { path: { id: t.id }, header: { "Idempotency-Key": clave } },
                                        body: { version: t.version },
                                    }),
                                ).then((nueva) => ficha.reemplazar(nueva))
                            }
                            alHecho={() => undefined}
                        />
                    </div>
                }
            />
            <AvisoCatalogos {...catalogos} />
            <div className="grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
                <Seccion
                    titulo="Datos de la ficha"
                    accion={
                        puede(["agenda.editar_tecnico"]) && cuadrillas ? (
                            <Boton variante="contorno" tamano="sm" onClick={() => setAbierto("editar")}>
                                <Pencil />
                                Editar
                            </Boton>
                        ) : null
                    }
                >
                    <dl className="grid gap-3 text-sm sm:grid-cols-2">
                        <Dato etiqueta="Cuadrilla">
                            {t.cuadrillaId === null ? "Sin cuadrilla" : (cuadrilla?.nombre ?? `Cuadrilla ${t.cuadrillaId}`)}
                            {cuadrilla ? <span className="block text-xs text-muted-foreground">{textoZonas(cuadrilla, zonas)}</span> : null}
                        </Dato>
                        <Dato etiqueta="Teléfono">
                            {t.telefono ? (
                                <a href={`tel:${t.telefono.replace(/[^\d+]/g, "")}`} className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline">
                                    <Phone aria-hidden className="size-3.5" />
                                    {t.telefono}
                                </a>
                            ) : (
                                <span className="text-muted-foreground">Sin teléfono</span>
                            )}
                        </Dato>
                        <Dato etiqueta="Correo">{t.correo}</Dato>
                        <Dato etiqueta="Habilidades">{(t.habilidades ?? []).length > 0 ? (t.habilidades ?? []).join(", ") : <span className="text-muted-foreground">Ninguna</span>}</Dato>
                        <Dato etiqueta="Bodega en Argos">{t.bodegaId ?? <span className="text-muted-foreground">Sin bodega</span>}</Dato>
                    </dl>
                </Seccion>
                <Seccion
                    titulo="Turnos semanales"
                    accion={
                        puede(["agenda.fijar_turnos"]) ? (
                            <Boton variante="contorno" tamano="sm" onClick={() => setAbierto("turnos")}>
                                <Clock />
                                Editar turnos
                            </Boton>
                        ) : null
                    }
                >
                    {turnos.length === 0 ? (
                        <Aviso tono="warning" titulo="Sin turnos">
                            No se le pueden programar órdenes hasta fijar sus turnos.
                        </Aviso>
                    ) : (
                        <ul className="grid gap-1 text-sm" aria-label="Turnos">
                            {turnos.map((tu) => (
                                <li key={`${tu.dia}-${tu.inicio}`} className="flex justify-between gap-3 border-b border-border/60 py-1 last:border-b-0">
                                    <span>{DIAS_SEMANA[tu.dia - 1]?.texto ?? tu.dia}</span>
                                    <span className="tabular-nums">
                                        {tu.inicio}–{tu.fin}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    )}
                </Seccion>
                <Seccion
                    titulo="Ausencias"
                    className="lg:col-span-2"
                    accion={
                        puede(["agenda.registrar_ausencia"]) ? (
                            <Boton variante="contorno" tamano="sm" onClick={() => setAbierto("ausencia")}>
                                <CalendarPlus />
                                Registrar ausencia
                            </Boton>
                        ) : null
                    }
                >
                    {ausencias.length === 0 ? (
                        <p className="text-sm text-muted-foreground">Sin ausencias vigentes ni próximas.</p>
                    ) : (
                        <ul className="grid gap-2" aria-label="Ausencias">
                            {ausencias.map((a) => (
                                <FilaAusencia key={a.id} tecnico={t} ausencia={a} alAnular={() => void ficha.refrescar()} />
                            ))}
                        </ul>
                    )}
                </Seccion>
            </div>
            {abierto === "editar" && cuadrillas ? <FormularioTecnico tecnico={t} cuadrillas={cuadrillas} zonas={zonas} alCerrar={() => setAbierto(null)} alGuardar={tras} /> : null}
            {abierto === "turnos" ? <FormularioTurnos tecnico={t} alCerrar={() => setAbierto(null)} alGuardar={tras} /> : null}
            {abierto === "ausencia" ? <FormularioAusencia tecnico={t} alCerrar={() => setAbierto(null)} alGuardar={() => tras()} /> : null}
        </>
    );
}

function Seccion({ titulo, accion, className, children }: { titulo: string; accion?: ReactNode; className?: string; children: ReactNode }) {
    return (
        <Tarjeta className={`grid content-start gap-3 p-4 [&>*]:min-w-0 ${className ?? ""}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 className="text-base font-semibold tracking-[-0.015em]">{titulo}</h2>
                {accion}
            </div>
            {children}
        </Tarjeta>
    );
}

function Dato({ etiqueta, children }: { etiqueta: string; children: ReactNode }) {
    return (
        <div className="grid min-w-0 gap-0.5">
            <dt className="text-xs font-medium text-muted-foreground">{etiqueta}</dt>
            <dd className="min-w-0 break-words">{children}</dd>
        </div>
    );
}

function textoRango(a: Ausencia): string {
    const d1 = diaDesdeIso(a.inicio) ?? "";
    const d2 = diaDesdeIso(a.fin) ?? "";
    return `${etiquetaDia(d1)} ${horaDe(a.inicio)} → ${etiquetaDia(d2)} ${horaDe(a.fin)}`;
}

function FilaAusencia({ tecnico, ausencia: a, alAnular }: { tecnico: TecnicoDetalle; ausencia: Ausencia; alAnular: () => void }) {
    const { puede } = useSesion();
    const clave = useClaveIdempotencia();
    const [confirmando, setConfirmando] = useState(false);
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);

    async function anular() {
        setEnviando(true);
        setError(null);
        try {
            await resolver(
                api().POST("/api/v1/tecnicos/{id}/ausencias/{ausenciaId}/anular", {
                    params: { path: { id: tecnico.id, ausenciaId: a.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: { version: a.version },
                }),
            );
            clave.renovar();
            setConfirmando(false);
            avisar({ titulo: `Ausencia de ${tecnico.nombre} anulada` });
            alAnular();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    return (
        <li className="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg border border-border p-2.5 text-sm" data-ausencia={a.id}>
            <InsigniaEstado tono="warning" icono={CalendarOff} texto={textoMotivo(a.motivo)} />
            <span className="tabular-nums">{textoRango(a)}</span>
            {a.detalle ? <span className="w-full text-muted-foreground">{a.detalle}</span> : null}
            {puede(["agenda.anular_ausencia"]) ? (
                <Boton variante="contorno" tamano="sm" className="ml-auto" onClick={() => setConfirmando(true)} aria-label={`Anular la ausencia (${textoMotivo(a.motivo)})`}>
                    <CircleX />
                    Anular
                </Boton>
            ) : null}
            {confirmando ? (
                <Dialogo open onOpenChange={(abierto) => !abierto && !enviando && setConfirmando(false)}>
                    <DialogoContenido role="alertdialog" titulo="¿Anular esta ausencia?" descripcion={`${textoMotivo(a.motivo)} · ${textoRango(a)}. Queda en el historial y ${tecnico.nombre} vuelve a estar disponible en ese horario.`}>
                        {error ? (
                            <Aviso tono="error" titulo="No se pudo anular">
                                {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                            </Aviso>
                        ) : null}
                        <DialogoPie>
                            <DialogoCerrar asChild>
                                <Boton variante="contorno" disabled={enviando}>
                                    Volver
                                </Boton>
                            </DialogoCerrar>
                            <Boton onClick={() => void anular()} disabled={enviando}>
                                {enviando ? "Anulando…" : "Anular ausencia"}
                            </Boton>
                        </DialogoPie>
                    </DialogoContenido>
                </Dialogo>
            ) : null}
        </li>
    );
}
