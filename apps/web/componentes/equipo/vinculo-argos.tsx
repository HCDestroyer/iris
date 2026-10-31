"use client";

import type { Schema } from "@iris/contratos";
import {
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    CampoArea,
    CampoSelector,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Esqueleto,
    useCambiosSinGuardar,
} from "@iris/ui";
import { Link2, Link2Off, RotateCw, Unplug } from "lucide-react";
import { useState } from "react";
import { Seccion } from "@/componentes/equipo/seccion-ficha";
import type { TecnicoDetalle } from "@/lib/agenda/esquemas";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";
import { conInventarioPorTecnico, opcionTecnicoArgos, SIN_INVENTARIO_POR_TECNICO, textoErrorVinculo } from "@/lib/agenda/argos";

type TecnicoArgos = Schema<"TecnicoDeArgos">;

/**
 * Vínculo de la ficha con su usuario de Argos (inventario por técnico, Argos ≥ 1.6.0): con él,
 * los consumos y comodatos salen a nombre del técnico. «Vincular con Argos»
 * (`agenda.vincular_argos`) elige de `GET /integraciones/argosisp/tecnicos`; «Desvincular»
 * (`agenda.desvincular_argos`). Sin la capacidad `inventario_por_tecnico` lo dice y no ofrece vincular.
 */
export function VinculoArgos({ tecnico: t, alCambiar }: { tecnico: TecnicoDetalle; alCambiar: (t: TecnicoDetalle) => void }) {
    const { puede } = useSesion();
    const puedeVincular = puede(["agenda.vincular_argos"]);
    const puedeDesvincular = puede(["agenda.desvincular_argos"]);
    const verConectores = puede(["integraciones.ver"]);
    const [vinculando, setVinculando] = useState(false);
    const [desvinculando, setDesvinculando] = useState(false);
    // Con permiso de ver integraciones se sabe de antemano si Argos tiene la capacidad.
    const conector = useLectura<boolean | null>(async (signal) => {
        if (!verConectores) return null;
        const pagina = await resolver(api().GET("/api/v1/integraciones/conectores", { params: { query: { limit: 50 } }, signal }));
        return conInventarioPorTecnico(pagina.data ?? []);
    });
    const sinCapacidad = conector.estado === "listo" && conector.datos === false;

    return (
        <Seccion
            titulo="Argos"
            accion={
                t.argos ? (
                    puedeDesvincular ? (
                        <Boton variante="contorno" tamano="sm" onClick={() => setDesvinculando(true)}>
                            <Link2Off />
                            Desvincular
                        </Boton>
                    ) : null
                ) : puedeVincular && !sinCapacidad ? (
                    <Boton variante="secundario" tamano="sm" onClick={() => setVinculando(true)}>
                        <Link2 />
                        Vincular con Argos
                    </Boton>
                ) : null
            }
        >
            <div className="grid gap-2 text-sm" data-testid="vinculo-argos">
                {t.argos ? (
                    <p>
                        Vinculado con <span className="font-medium">{t.argos.nombre}</span> ({t.argos.correo}) · usuario {t.argos.idExterno} de Argos
                        <span className="block text-xs text-muted-foreground">Desde {formatearFecha(t.argos.vinculadoEn)}. Los consumos y comodatos salen a su nombre.</span>
                    </p>
                ) : (
                    <p className="text-muted-foreground">Sin vincular: Argos descuenta de la bodega de la ficha{t.bodegaId !== null ? ` (${t.bodegaId})` : ""}.</p>
                )}
                {sinCapacidad ? (
                    <Aviso tono="warning" titulo={SIN_INVENTARIO_POR_TECNICO} icono={Unplug}>
                        Actualiza Argos a 1.6.0 o más para vincular técnicos; mientras tanto Iris usa la bodega de la ficha.
                    </Aviso>
                ) : null}
            </div>
            <DialogoProtegido open={vinculando} onOpenChange={setVinculando}>
                {vinculando ? (
                    <Vincular
                        tecnico={t}
                        alVincular={(nueva) => {
                            setVinculando(false);
                            alCambiar(nueva);
                        }}
                    />
                ) : null}
            </DialogoProtegido>
            <DialogoProtegido open={desvinculando} onOpenChange={setDesvinculando}>
                {desvinculando ? (
                    <Desvincular
                        tecnico={t}
                        alDesvincular={(nueva) => {
                            setDesvinculando(false);
                            alCambiar(nueva);
                        }}
                    />
                ) : null}
            </DialogoProtegido>
        </Seccion>
    );
}

function Vincular({ tecnico: t, alVincular }: { tecnico: TecnicoDetalle; alVincular: (t: TecnicoDetalle) => void }) {
    const clave = useClaveIdempotencia();
    const [elegido, setElegido] = useState("");
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    useCambiosSinGuardar(elegido !== "", enviando);
    const lista = usePaginado<TecnicoArgos>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/integraciones/argosisp/tecnicos", {
                params: { query: { limit: 50, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera, items } = lista;

    async function vincular() {
        setEnviando(true);
        setError(null);
        try {
            const nueva = await resolver(
                api().POST("/api/v1/tecnicos/{id}/vincular-argos", {
                    params: { path: { id: t.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: { version: t.version, idExterno: elegido },
                }),
            );
            clave.renovar();
            avisar({ titulo: `${t.nombre} quedó vinculado con Argos` });
            alVincular(nueva);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    const sinCapacidad = primera.estado === "error" && primera.error instanceof ErrorApi && primera.error.codigo === "argos_sin_inventario_tecnico";
    return (
        <DialogoContenido titulo="Vincular con Argos" descripcion={`${t.nombre} · elige su usuario de Argos (con bodega en esta empresa)`}>
            <form
                noValidate
                className="grid gap-4"
                onSubmit={(e) => {
                    e.preventDefault();
                    void vincular();
                }}
            >
                {primera.estado === "cargando" ? (
                    <Esqueleto className="h-10" />
                ) : sinCapacidad ? (
                    <Aviso tono="warning" titulo={SIN_INVENTARIO_POR_TECNICO} icono={Unplug}>
                        Actualiza Argos a 1.6.0 o más para vincular técnicos; mientras tanto Iris usa la bodega de la ficha.
                    </Aviso>
                ) : primera.estado === "error" ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudieron leer los técnicos de Argos"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={primera.reintentar}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        {mensajeDeError(primera.error).titulo}
                    </Aviso>
                ) : (
                    <CampoSelector
                        etiqueta="Usuario de Argos"
                        value={elegido}
                        onChange={(e) => setElegido(e.target.value)}
                        ayuda={items.length === 0 ? "Argos no tiene usuarios con bodega técnico en las empresas que ves." : "Solo se pueden elegir los que tienen bodega en la empresa activa y no están vinculados a otra ficha."}
                    >
                        <option value="">Elige el usuario…</option>
                        {items.map((a) => {
                            const o = opcionTecnicoArgos(a, t.id);
                            return (
                                <option key={a.idExterno} value={a.idExterno} disabled={!o.elegible}>
                                    {o.texto}
                                </option>
                            );
                        })}
                    </CampoSelector>
                )}
                {lista.siguiente ? (
                    <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={() => void lista.cargarMas()} disabled={lista.cargandoMas}>
                        {lista.cargandoMas ? "Cargando…" : "Cargar más"}
                    </Boton>
                ) : null}
                {error ? (
                    <Aviso tono="error" titulo="No se pudo vincular">
                        {textoErrorVinculo(error) ?? [mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={enviando}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <BotonEnviar enviando={enviando} textoEnviando="Vinculando…" faltan={elegido ? [] : ["el usuario de Argos"]} disabled={sinCapacidad}>
                        Vincular
                    </BotonEnviar>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}

function Desvincular({ tecnico: t, alDesvincular }: { tecnico: TecnicoDetalle; alDesvincular: (t: TecnicoDetalle) => void }) {
    const clave = useClaveIdempotencia();
    const [motivo, setMotivo] = useState("");
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    useCambiosSinGuardar(motivo.trim() !== "", enviando);

    async function desvincular() {
        setEnviando(true);
        setError(null);
        try {
            const nueva = await resolver(
                api().POST("/api/v1/tecnicos/{id}/desvincular-argos", {
                    params: { path: { id: t.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: { version: t.version, ...(motivo.trim() ? { motivo: motivo.trim() } : {}) },
                }),
            );
            clave.renovar();
            avisar({ titulo: `${t.nombre} quedó sin vínculo con Argos` });
            alDesvincular(nueva);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    return (
        <DialogoContenido titulo="¿Desvincular de Argos?" descripcion={`${t.nombre} · lo que use desde ahora sale de la bodega de la ficha.`}>
            <div className="grid gap-4">
                <CampoArea etiqueta="Motivo (opcional)" rows={2} maxLength={500} value={motivo} onChange={(e) => setMotivo(e.target.value)} />
                {error ? (
                    <Aviso tono="error" titulo="No se pudo desvincular">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={enviando}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <Boton variante="peligro" onClick={() => void desvincular()} disabled={enviando}>
                        <Link2Off />
                        {enviando ? "Desvinculando…" : "Desvincular"}
                    </Boton>
                </DialogoPie>
            </div>
        </DialogoContenido>
    );
}
