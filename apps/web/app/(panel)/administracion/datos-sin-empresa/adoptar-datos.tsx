"use client";

import type { Schema } from "@iris/contratos";
import {
    Aviso,
    Boton,
    CampoSelector,
    Dialogo,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    EstadoVacio,
    Tarjeta,
    TarjetaContenido,
    TarjetaDescripcion,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { Building2, CircleCheck, DatabaseZap } from "lucide-react";
import { useRef, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { useSesion } from "@/lib/sesion";

/**
 * Pantalla mínima de adopción (i1.md, «Transición sin conector → con conector»): asigna a una
 * empresa los datos operativos guardados con `empresa_id` null. Solo admin global.
 */
export function AdoptarDatos() {
    const { yo, empresas, refrescarSesion } = useSesion();
    const [empresaId, setEmpresaId] = useState<string>("");
    const [confirmar, setConfirmar] = useState(false);
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const [resultado, setResultado] = useState<Schema<"DatosAdoptados"> | null>(null);
    const clave = useRef(nuevaClaveIdempotencia());
    const elegida = empresas.find((e) => String(e.id) === empresaId);

    async function adoptar() {
        if (!elegida) {
            return;
        }
        setEnviando(true);
        setError(null);
        try {
            const datos = await resolver(
                api().POST("/api/v1/empresas/{id}/adoptar-sin-empresa", {
                    params: { path: { id: elegida.id }, header: { "Idempotency-Key": clave.current } },
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            setResultado(datos);
            setConfirmar(false);
            refrescarSesion();
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    const encabezado = (
        <EncabezadoPagina
            titulo="Datos sin empresa"
            descripcion="Lo guardado antes de conectar Argos no tiene empresa y nadie lo ve. Asígnalo a la empresa a la que pertenece."
        />
    );

    if (resultado) {
        const nombre = empresas.find((e) => e.id === resultado.empresaId)?.nombre ?? `Empresa ${resultado.empresaId}`;
        return (
            <>
                {encabezado}
                <Tarjeta>
                    <TarjetaEncabezado>
                        <TarjetaTitulo>
                            <span className="inline-flex items-center gap-2">
                                <CircleCheck aria-hidden className="size-5 text-success" />
                                Datos asignados a {nombre}
                            </span>
                        </TarjetaTitulo>
                        <TarjetaDescripcion>
                            {resultado.total === 1 ? "Se asignó 1 registro." : `Se asignaron ${resultado.total.toLocaleString("es-GT")} registros.`}
                        </TarjetaDescripcion>
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <dl className="grid gap-1 text-sm tabular-nums">
                            {Object.entries(resultado.filas).map(([tabla, n]) => (
                                <div key={tabla} className="flex justify-between gap-4 border-b py-1.5 last:border-0">
                                    <dt className="font-mono text-muted-foreground">{tabla}</dt>
                                    <dd>{n.toLocaleString("es-GT")}</dd>
                                </div>
                            ))}
                        </dl>
                    </TarjetaContenido>
                </Tarjeta>
            </>
        );
    }

    if (!yo.hayDatosSinEmpresa || empresas.length === 0) {
        return (
            <>
                {encabezado}
                <Tarjeta>
                    <EstadoVacio icono={CircleCheck} titulo="No hay datos sin empresa" descripcion="Todo lo guardado ya pertenece a una empresa." />
                </Tarjeta>
            </>
        );
    }

    return (
        <>
            {encabezado}
            <Tarjeta className="max-w-xl">
                <TarjetaContenido className="grid gap-4 pt-5">
                    <CampoSelector etiqueta="Empresa que recibe los datos" value={empresaId} onChange={(e) => setEmpresaId(e.target.value)}>
                        <option value="">Elige una empresa…</option>
                        {empresas.map((e) => (
                            <option key={e.id} value={e.id}>
                                {e.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                    <Boton className="justify-self-start" disabled={!elegida} onClick={() => setConfirmar(true)}>
                        <DatabaseZap />
                        Asignar datos…
                    </Boton>
                </TarjetaContenido>
            </Tarjeta>
            <Dialogo open={confirmar} onOpenChange={(abierto) => !enviando && setConfirmar(abierto)}>
                <DialogoContenido titulo="¿Asignar los datos sin empresa?" descripcion="No se puede deshacer desde el panel.">
                    <p className="flex items-start gap-2 text-sm">
                        <Building2 aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                        <span>
                            Todos los tickets, mensajes e historial guardados sin empresa pasarán a <strong>{elegida?.nombre}</strong> y los
                            verán solo quienes operan en ella.
                        </span>
                    </p>
                    {error ? (
                        <Aviso tono="error" titulo="No se pudieron asignar">
                            {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={enviando}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <Boton onClick={adoptar} disabled={enviando}>
                            {enviando ? "Asignando…" : `Asignar a ${elegida?.nombre ?? "la empresa"}`}
                        </Boton>
                    </DialogoPie>
                </DialogoContenido>
            </Dialogo>
        </>
    );
}
