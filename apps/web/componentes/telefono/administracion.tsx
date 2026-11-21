"use client";

import { avisar, Aviso, Boton, Campo, DialogoConfirmar, Dialogo, DialogoContenido, DialogoPie, Esqueleto, InsigniaEstado, Interruptor, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ChevronRight, MonitorSmartphone, Pencil, Plus, RefreshCw, ShieldCheck, UsersRound } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import type { Centralita } from "@/lib/centralita/tipos";
import { formatearFecha } from "@/lib/fechas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { estadoTelefonoCentralita, textoPruebaTelefono } from "@/lib/telefono/administracion";
import { activarTelefono, crearGrupo, desactivarTelefono, editarGrupo, leerGrupos, probarTelefonoCentralita } from "@/lib/telefono/api";
import type { GrupoCentralita, PruebaTelefono } from "@/lib/telefono/tipos";

/*
 * Administración del teléfono de Iris (E4c) en la pantalla de la centralita: cómo llega Iris a la
 * Issabel, si está encendido y qué falta; y los grupos a los que se puede transferir.
 */

export const RUTA_TELEFONO = "/administracion/integraciones/centralita/telefono";
export const rutaExtension = (id: number) => `/administracion/integraciones/centralita/extensiones/${id}`;

export function TarjetaTelefono({ centralita: c, configura, alCambiar }: { centralita: Centralita; configura: boolean; alCambiar: () => void }) {
    const t = c.telefono;
    const e = estadoTelefonoCentralita(t);
    const [prueba, setPrueba] = useState<PruebaTelefono | null>(null);
    const [ocupado, setOcupado] = useState(false);
    const [apagar, setApagar] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const hacer = async (f: () => Promise<unknown>, ok?: string) => {
        setOcupado(true);
        setError(null);
        try {
            await f();
            if (ok) avisar({ titulo: ok });
            alCambiar();
        } catch (err) {
            setError(err);
        } finally {
            setOcupado(false);
        }
    };
    const configurado = !!t?.huellaWss;
    const listoParaEncender = !!t && !t.activo && !!t.probadoEn && !!t.restriccionesConfirmadasEn;
    return (
        <Tarjeta data-slot="telefono-centralita">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center gap-2">
                <TarjetaTitulo className="flex items-center gap-2">
                    <MonitorSmartphone aria-hidden className="size-4 text-muted-foreground" />
                    Teléfono en Iris
                </TarjetaTitulo>
                <span className="ml-auto" data-slot="estado-telefono-centralita" data-estado={t?.activo ? "encendido" : "apagado"}>
                    {e.tono === "success" ? (
                        <span className="flex items-center gap-1.5 text-sm font-medium text-success">
                            <span aria-hidden className="size-2 rounded-full bg-current" />
                            {e.texto}
                        </span>
                    ) : (
                        <InsigniaEstado tono={e.tono} texto={e.texto} />
                    )}
                </span>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4">
                <p className="text-sm text-muted-foreground">
                    Las computadoras hablan solo con Iris. Iris hace de puente hasta la Issabel dentro de la red: la Issabel no se abre a internet y nadie recibe la contraseña de su
                    extensión.
                </p>
                {configurado && t ? (
                    <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-4 gap-y-2 text-sm">
                        <dt className="text-muted-foreground">Iris llega a</dt>
                        <dd className="font-mono text-[0.8125rem] break-all">
                            wss://{c.host}:{t.wssPuerto}
                            {t.wssRuta}
                        </dd>
                        <dt className="text-muted-foreground">Certificado</dt>
                        <dd>
                            <span className="flex items-center gap-1.5 text-success">
                                <ShieldCheck aria-hidden className="size-4" />
                                Huella confirmada
                            </span>
                        </dd>
                        <dt className="text-muted-foreground">Última prueba</dt>
                        <dd>{t.probadoEn ? `${formatearFecha(t.probadoEn)} · todo en orden` : "Sin probar con lo guardado"}</dd>
                        <dt className="text-muted-foreground">Guía de la Issabel</dt>
                        <dd>{t.restriccionesConfirmadasEn ? `Confirmada el ${formatearFecha(t.restriccionesConfirmadasEn)?.slice(0, 10)}` : "Sin confirmar"}</dd>
                        <dt className="text-muted-foreground">Límites</dt>
                        <dd>
                            {t.llamadasPorUsuarioDia} llamadas a la calle por persona al día · {t.llamadasSimultaneas} a la vez
                            {t.prefijoSalida ? ` · prefijo ${t.prefijoSalida}` : ""}
                        </dd>
                    </dl>
                ) : null}
                {e.falta && configura ? (
                    <Aviso tono={e.tono === "warning" ? "warning" : "info"} titulo="Qué falta" data-slot="falta-telefono">
                        {e.falta}
                    </Aviso>
                ) : null}
                {prueba ? <ResultadoPrueba prueba={prueba} /> : null}
                {error ? (
                    <Aviso tono="error" titulo="No se pudo">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
                {configura ? (
                    <div className="flex flex-wrap gap-2">
                        <Boton variante={configurado ? "secundario" : "primario"} asChild>
                            <Link href={RUTA_TELEFONO}>
                                <Pencil />
                                {configurado ? "Configurar" : "Configurar el teléfono"}
                            </Link>
                        </Boton>
                        {configurado ? (
                            <Boton
                                variante="secundario"
                                disabled={ocupado}
                                onClick={() =>
                                    void hacer(async () => {
                                        setPrueba(await probarTelefonoCentralita(c.id));
                                    })
                                }
                            >
                                <RefreshCw />
                                Probar
                            </Boton>
                        ) : null}
                        {t?.activo ? (
                            <Boton variante="fantasma" disabled={ocupado} onClick={() => setApagar(true)}>
                                Apagar
                            </Boton>
                        ) : configurado ? (
                            <Boton disabled={ocupado || !listoParaEncender} onClick={() => void hacer(() => activarTelefono(c.id, c.version), "Teléfono encendido")}>
                                Encender
                            </Boton>
                        ) : null}
                    </div>
                ) : null}
            </TarjetaContenido>
            <DialogoConfirmar
                open={apagar}
                onOpenChange={setApagar}
                titulo="¿Apagar el teléfono de Iris?"
                descripcion="Se cortan las llamadas que estén en curso en Iris y a nadie le vuelve a sonar en el panel. La Issabel sigue funcionando igual."
                textoConfirmar="Apagar"
                peligro
                alConfirmar={async () => {
                    await hacer(() => desactivarTelefono(c.id, c.version), "Teléfono apagado");
                    setApagar(false);
                }}
            />
        </Tarjeta>
    );
}

function ResultadoPrueba({ prueba }: { prueba: PruebaTelefono }) {
    const r = textoPruebaTelefono(prueba);
    return (
        <Aviso tono={r.tono === "success" ? "success" : "error"} titulo={r.titulo} data-slot="resultado-prueba-telefono" data-estado={prueba.estado}>
            {prueba.estado === "ok" ? "Iris se conectó y la centralita contestó." : r.detalle}
        </Aviso>
    );
}

/** Los grupos de timbrado o colas a los que se puede transferir (además de las personas). */
export function TarjetaGrupos({ centralita: c }: { centralita: Centralita }) {
    const lectura = useLectura((signal) => leerGrupos(c.id, signal));
    const [nuevo, setNuevo] = useState(false);
    const [editar, setEditar] = useState<GrupoCentralita | null>(null);
    const lista = lectura.estado === "listo" ? lectura.datos : [];
    const refrescar = () => void lectura.refrescar({ silencioso: true });
    return (
        <Tarjeta data-slot="grupos-centralita">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center gap-2">
                <TarjetaTitulo className="flex items-center gap-2">
                    <UsersRound aria-hidden className="size-4 text-muted-foreground" />
                    Grupos para transferir
                </TarjetaTitulo>
                <Boton className="ml-auto" tamano="sm" variante="secundario" onClick={() => setNuevo(true)}>
                    <Plus />
                    Agregar grupo
                </Boton>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                <p className="text-sm text-muted-foreground">Los grupos de timbrado o colas de la Issabel (PBX › Ring Groups / Queues), de 2 a 6 cifras. Se transfiere a ellos además de a las personas.</p>
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-16" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los grupos" />
                ) : lista.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin grupos todavía.</p>
                ) : (
                    <ul className="grid divide-y divide-border" aria-label="Grupos">
                        {lista.map((g) => (
                            <li key={g.id} className="flex min-w-0 items-center gap-3 py-2.5">
                                <span className="inline-flex h-8 min-w-11 items-center justify-center rounded-lg bg-elevated px-2 text-sm font-bold tabular-nums">{g.numero}</span>
                                <span className="min-w-0 flex-1 truncate font-medium">{g.nombre}</span>
                                {!g.activo ? <InsigniaEstado tono="neutral" texto="Apagado" /> : null}
                                <Boton variante="fantasma" tamano="icono-sm" aria-label={`Editar el grupo ${g.nombre}`} onClick={() => setEditar(g)}>
                                    <Pencil />
                                </Boton>
                            </li>
                        ))}
                    </ul>
                )}
            </TarjetaContenido>
            {nuevo || editar ? (
                <DialogoGrupo
                    centralita={c}
                    grupo={editar}
                    alCerrar={() => {
                        setNuevo(false);
                        setEditar(null);
                    }}
                    alGuardar={() => {
                        setNuevo(false);
                        setEditar(null);
                        refrescar();
                    }}
                />
            ) : null}
        </Tarjeta>
    );
}

/** Nombre, número y si está activo: 3 campos, en un diálogo. */
function DialogoGrupo({ centralita: c, grupo, alCerrar, alGuardar }: { centralita: Centralita; grupo: GrupoCentralita | null; alCerrar: () => void; alGuardar: () => void }) {
    const [nombre, setNombre] = useState(grupo?.nombre ?? "");
    const [numero, setNumero] = useState(grupo?.numero ?? "");
    const [activo, setActivo] = useState(grupo?.activo ?? true);
    const [error, setError] = useState<unknown>(null);
    const [enviando, setEnviando] = useState(false);
    const clave = useClaveIdempotencia();
    const numeroValido = /^[0-9]{2,6}$/.test(numero);
    const nombreValido = nombre.trim().length >= 1 && nombre.trim().length <= 60;
    return (
        <Dialogo open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo={grupo ? `Editar ${grupo.nombre}` : "Agregar grupo"}>
                <div className="grid gap-3">
                    <Campo etiqueta="Nombre" value={nombre} maxLength={60} onChange={(e) => setNombre(e.target.value)} placeholder="Técnicos de guardia" />
                    <Campo
                        etiqueta="Número"
                        inputMode="numeric"
                        value={numero}
                        onChange={(e) => setNumero(e.target.value.replace(/\D/g, "").slice(0, 6))}
                        ayuda="El del grupo de timbrado o la cola en Issabel (2 a 6 cifras)."
                        error={numero !== "" && !numeroValido ? "De 2 a 6 cifras" : undefined}
                    />
                    {grupo ? <Interruptor etiqueta="Activo" checked={activo} onChange={(e) => setActivo(e.target.checked)} /> : null}
                    {error ? (
                        <Aviso tono="error" titulo={error instanceof ErrorApi && error.codigo === "grupo_existente" ? "Ya hay un grupo con ese número" : "No se pudo guardar"}>
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                </div>
                <DialogoPie>
                    <Boton variante="fantasma" onClick={alCerrar}>
                        Cancelar
                    </Boton>
                    <Boton
                        disabled={!numeroValido || !nombreValido || enviando}
                        onClick={async () => {
                            setEnviando(true);
                            setError(null);
                            try {
                                if (grupo) await editarGrupo(c.id, grupo.id, { version: grupo.version, nombre: nombre.trim(), numero, activo });
                                else await crearGrupo(c.id, { nombre: nombre.trim(), numero }, clave.actual());
                                clave.renovar();
                                avisar({ titulo: grupo ? "Grupo guardado" : `Grupo ${numero} agregado` });
                                alGuardar();
                            } catch (e) {
                                clave.trasError(e);
                                setError(e);
                            } finally {
                                setEnviando(false);
                            }
                        }}
                    >
                        Guardar
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}

/** El enlace a la configuración de la extensión (dónde contesta, credencial). */
export function EnlaceExtension({ id, nombre }: { id: number; nombre: string }) {
    return (
        <Boton variante="fantasma" tamano="icono-sm" asChild>
            <Link href={rutaExtension(id)} aria-label={`Configurar la extensión de ${nombre}`}>
                <ChevronRight />
            </Link>
        </Boton>
    );
}
