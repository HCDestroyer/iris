"use client";

import { Aviso, Boton, Cargando, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { ArrowLeft, CircleCheck, CirclePower, Power, RotateCcw, Unplug } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { quitarFragmentoDeLaUrl } from "@/lib/fragmento";
import {
    activoQueReemplaza,
    CLAVE_PANEL_ARGOS,
    leerVuelta,
    mensajeVuelta,
    panelDe,
    textoAlcance,
    type AutorizacionCompletada,
    type Conector,
    type Vuelta,
} from "@/lib/integraciones/presentacion";
import { useSesion } from "@/lib/sesion";
import { iniciarConexionArgos } from "../../conectar-argos";

type Paso =
    | { paso: "sin_datos" }
    | { paso: "completando" }
    | { paso: "error"; codigo: string | undefined; estado: number; reenviable: boolean }
    | { paso: "resumen"; resumen: AutorizacionCompletada };

const RUTA_INTEGRACIONES = "/administracion/integraciones";

function panelRecordado(): string | null {
    try {
        return window.sessionStorage.getItem(CLAVE_PANEL_ARGOS);
    } catch {
        return null;
    }
}

/**
 * Vuelta de «Conectar con Argos» (contrato 0.4.0 §1.4): lee `#codigo=…&api=…&state=…` (o
 * `#error=…&state=…`), lo borra de la URL y llama a «completar» una sola vez. Muestra el resumen
 * (empresas, alcance, panel y API) y ofrece «Activar».
 */
export function VueltaArgos() {
    const [inicio] = useState(() => leerVuelta(window.location.hash));
    const vuelta = useRef<Vuelta | null>(inicio);
    const clave = useRef(nuevaClaveIdempotencia());
    const enviado = useRef(false);
    const [paso, setPaso] = useState<Paso>(inicio ? { paso: "completando" } : { paso: "sin_datos" });

    const completar = useCallback(async () => {
        const v = vuelta.current;
        if (!v) return;
        setPaso({ paso: "completando" });
        try {
            const resumen = await resolver(
                api().POST("/api/v1/integraciones/argosisp/autorizaciones/completar", { body: v, params: { header: { "Idempotency-Key": clave.current } } }),
            );
            vuelta.current = null;
            setPaso({ paso: "resumen", resumen });
        } catch (e) {
            // Sin respuesta de Iris: se puede repetir con la misma clave. Lo demás gastó el state.
            clave.current = claveTrasError(e, clave.current);
            const estado = e instanceof ErrorApi ? e.estado : 0;
            if (estado !== 0) vuelta.current = null;
            setPaso({ paso: "error", codigo: e instanceof ErrorApi ? e.codigo : undefined, estado, reenviable: estado === 0 });
        }
    }, []);

    useEffect(() => {
        quitarFragmentoDeLaUrl();
        if (vuelta.current && !enviado.current) {
            enviado.current = true;
            void completar();
        }
    }, [completar]);

    return (
        <>
            <Boton variante="fantasma" tamano="sm" asChild className="mb-3 -ml-2">
                <Link href={RUTA_INTEGRACIONES}>
                    <ArrowLeft />
                    Integraciones
                </Link>
            </Boton>
            <EncabezadoPagina titulo="Conectar con Argos" descripcion="Vuelta desde el panel de Argos." />
            {paso.paso === "completando" ? (
                <Tarjeta className="grid place-items-center py-10">
                    <Cargando texto="Completando la conexión con Argos…" visible />
                </Tarjeta>
            ) : paso.paso === "sin_datos" ? (
                <Tarjeta>
                    <EstadoVacio
                        icono={Unplug}
                        titulo="No hay nada que completar"
                        descripcion="Esta página recibe la respuesta de Argos después de pulsar «Conectar con Argos»."
                        accion={
                            <Boton asChild>
                                <Link href={RUTA_INTEGRACIONES}>Ir a Integraciones</Link>
                            </Boton>
                        }
                    />
                </Tarjeta>
            ) : paso.paso === "error" ? (
                <ErrorVuelta codigo={paso.codigo} estado={paso.estado} reintentarEnvio={paso.reenviable ? () => void completar() : null} />
            ) : (
                <Resumen resumen={paso.resumen} />
            )}
        </>
    );
}

function ErrorVuelta({ codigo, estado, reintentarEnvio }: { codigo: string | undefined; estado: number; reintentarEnvio: (() => void) | null }) {
    const m = mensajeVuelta(codigo, estado);
    const [error, setError] = useState<unknown>(null);
    const [reiniciando, setReiniciando] = useState(false);

    async function reintentar() {
        if (reintentarEnvio) {
            reintentarEnvio();
            return;
        }
        // La autorización ya se gastó: se empieza otra con el mismo panel (o se vuelve a Integraciones).
        const panel = panelRecordado();
        if (!panel) {
            window.location.assign(RUTA_INTEGRACIONES);
            return;
        }
        setReiniciando(true);
        setError(null);
        try {
            await iniciarConexionArgos(panel, nuevaClaveIdempotencia());
        } catch (e) {
            setError(e);
            setReiniciando(false);
        }
    }

    return (
        <div className="grid gap-3">
            <Aviso
                tono={codigo === "rechazado" ? "warning" : "error"}
                titulo={m.titulo}
                data-testid="error-vuelta"
                accion={
                    <Boton tamano="sm" variante="secundario" onClick={() => void reintentar()} disabled={reiniciando}>
                        <RotateCcw />
                        {reiniciando ? "Abriendo Argos…" : "Reintentar"}
                    </Boton>
                }
            >
                {m.texto}
            </Aviso>
            {error ? (
                <Aviso tono="error" titulo="No se pudo volver a abrir Argos">
                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
        </div>
    );
}

function Resumen({ resumen }: { resumen: AutorizacionCompletada }) {
    const { puede } = useSesion();
    const [conector, setConector] = useState<Conector>(resumen.conector);
    const [reemplaza, setReemplaza] = useState<Conector | null | "desconocido">(null);
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [activando, setActivando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const empresas = resumen.empresas ?? [];
    const panel = panelDe(conector);
    const activo = conector.estado === "activo";

    useEffect(() => {
        const control = new AbortController();
        resolver(api().GET("/api/v1/integraciones/conectores", { params: { query: { limit: 50 } }, signal: control.signal }))
            .then((p) => setReemplaza(activoQueReemplaza(resumen.conector, p.data ?? []) ?? null))
            .catch(() => {
                if (!control.signal.aborted) setReemplaza("desconocido");
            });
        return () => control.abort();
    }, [resumen.conector]);

    async function activar() {
        setActivando(true);
        setError(null);
        try {
            const c = await resolver(
                api().POST("/api/v1/integraciones/conectores/{id}/activar", {
                    params: { path: { id: conector.id }, header: { "Idempotency-Key": clave } },
                    body: { version: conector.version },
                }),
            );
            setConector(c);
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        } finally {
            setActivando(false);
        }
    }

    return (
        <div className="grid gap-4">
            {activo ? (
                <Aviso tono="success" titulo="Argos quedó conectado y activo" data-testid="argos-activo">
                    Iris ya lee de Argos y recibe sus avisos.{" "}
                    <Link href={RUTA_INTEGRACIONES} className="underline underline-offset-2">
                        Ver integraciones
                    </Link>
                </Aviso>
            ) : (
                <Aviso tono="info" titulo="Argos aprobó la conexión">
                    El conector está apagado hasta que lo actives: revisa lo que verá Iris.
                </Aviso>
            )}
            <Tarjeta className="grid gap-4 p-4" data-testid="resumen-conexion">
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <h2 className="font-medium">Conector de Argos</h2>
                    {activo ? <InsigniaEstado tono="success" icono={CircleCheck} texto="Activo" /> : <InsigniaEstado tono="neutral" icono={CirclePower} texto="Apagado" />}
                </div>
                <dl className="grid gap-x-3 gap-y-1.5 text-sm sm:grid-cols-[auto_minmax(0,1fr)]">
                    <dt className="text-muted-foreground">Panel de Argos</dt>
                    <dd className="break-all" data-testid="resumen-panel">
                        {panel ?? <span className="text-muted-foreground">Sin informar</span>}
                    </dd>
                    <dt className="text-muted-foreground">API de Argos</dt>
                    <dd className="font-mono break-all" data-testid="resumen-api">
                        {conector.apiUrl}
                    </dd>
                    <dt className="text-muted-foreground">Alcance</dt>
                    <dd data-testid="resumen-alcance">{textoAlcance(conector.alcanceGlobal)}</dd>
                </dl>
                <div className="grid gap-1.5">
                    <h3 className="text-sm font-medium">Empresas que verá Iris</h3>
                    {empresas.length === 0 ? (
                        <p className="text-sm text-muted-foreground">Argos no informó empresas: Iris no verá clientes hasta que el usuario técnico tenga alguna.</p>
                    ) : (
                        <ul className="grid gap-1 text-sm" aria-label="Empresas que verá Iris">
                            {empresas.map((e) => (
                                <li key={e.id} className="flex flex-wrap justify-between gap-x-3 rounded-md border px-3 py-2">
                                    <span className="min-w-0 break-words">{e.nombre ?? `Empresa ${e.id}`}</span>
                                    <span className="text-muted-foreground tabular-nums">{e.nit ? `NIT ${e.nit}` : "Sin NIT"}</span>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
                {!activo && reemplaza !== null ? (
                    <Aviso tono="warning" titulo="Reemplazará al conector activo de Argos" data-testid="aviso-reemplazo">
                        {reemplaza === "desconocido"
                            ? "No se pudo comprobar si hay otro conector de Argos activo; si lo hay, quedará apagado al activar este."
                            : `El conector activo (${panelDe(reemplaza) ?? reemplaza.apiUrl}) quedará apagado y su token se revocará en el mismo paso.`}
                    </Aviso>
                ) : null}
                {error ? (
                    <Aviso tono="error" titulo="No se pudo activar">
                        {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
                {activo ? null : puede(["integraciones.activar"]) ? (
                    <div className="flex flex-wrap gap-2">
                        <Boton onClick={() => void activar()} disabled={activando}>
                            <Power />
                            {activando ? "Activando…" : error ? "Reintentar" : "Activar"}
                        </Boton>
                        <Boton variante="secundario" asChild>
                            <Link href={RUTA_INTEGRACIONES}>Más tarde</Link>
                        </Boton>
                    </div>
                ) : (
                    <p className="text-sm text-muted-foreground">Pide a alguien con permiso de activar integraciones que lo active en Integraciones.</p>
                )}
            </Tarjeta>
        </div>
    );
}
