"use client";

import { Aviso, avisar, Boton, cn, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta, Cargando, Tabla, TablaCelda, TablaCuerpo, TablaFila } from "@iris/ui";
import { AlertTriangle, CheckCircle2, Circle, CircleCheck, CirclePower, Info, Plug, RefreshCw, ShieldAlert } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import {
    corteEnCurso,
    detalleEtapa,
    huellaLegible,
    NOMBRE_RECURSO,
    porRevisar,
    TEXTO_ESTADO,
    TEXTO_SINCRONIZACION,
    textoBotonSincronizar,
    textoInforme,
    TONO_SINCRONIZACION,
    type EstadoConexionArgos,
    type EtapaSincronizacion,
    type SincronizacionArgos,
} from "@/lib/integraciones/estado-argos";
import type { Conector } from "@/lib/integraciones/presentacion";
import { useLectura } from "@/lib/lectura";
import { textoFechaHora } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";

/** Mientras la sincronización corre, la pantalla se relee sola (de fondo, sin parpadeo). */
const RELECTURA_MS = 5000;

/**
 * Administración → Integraciones → Argos (contrato 0.8.0 §2.5 d): lo que Iris sabe de su conexión
 * con Argos. Teléfono primero: una columna de tarjetas; en escritorio, dos. Nunca tokens ni secretos.
 */
export function EstadoArgos() {
    const lectura = useLectura(async (signal) => resolver(api().GET("/api/v1/integraciones/argosisp/estado", { signal })));
    const enCurso = lectura.estado === "listo" && lectura.datos.sincronizacion?.estado === "en_curso";
    const { refrescar } = lectura;
    useEffect(() => {
        if (!enCurso) return;
        const t = window.setInterval(() => void refrescar({ silencioso: true }), RELECTURA_MS);
        return () => window.clearInterval(t);
    }, [enCurso, refrescar]);

    return (
        <>
            <EncabezadoPagina titulo="Conexión con Argos" descripcion="Lo que Iris sabe de su conexión con Argos. Argos muestra lo suyo en su panel." />
            {lectura.estado === "cargando" ? (
                <div className="grid gap-3">
                    <Esqueleto className="h-48" />
                    <Esqueleto className="h-32" />
                </div>
            ) : lectura.estado === "error" ? (
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el estado de la conexión" />
            ) : !lectura.datos.conector ? (
                <Tarjeta>
                    <EstadoVacio icono={Plug} titulo="Iris no está conectado con Argos" descripcion="Conéctalo desde Integraciones con «Conectar con Argos». Mientras tanto, Iris trabaja con sus propios datos." />
                </Tarjeta>
            ) : (
                <Contenido datos={lectura.datos} conector={lectura.datos.conector} alCambiar={(s) => lectura.reemplazar({ ...lectura.datos, sincronizacion: s })} />
            )}
        </>
    );
}

function Contenido({ datos: d, conector: c, alCambiar }: { datos: EstadoConexionArgos; conector: Conector; alCambiar: (s: SincronizacionArgos) => void }) {
    return (
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)] lg:items-start">
            <TarjetaSincronizacion sinc={d.sincronizacion} conectorId={c.id} activo={c.estado === "activo"} alCambiar={alCambiar} />
            <div className="grid gap-3">
                <TarjetaConexion datos={d} conector={c} />
                <TarjetaDatos datos={d} />
            </div>
            <TarjetaFallos datos={d} />
            <TarjetaEmpresas datos={d} />
            <TarjetaCertificado datos={d} />
        </div>
    );
}

function Seccion({ titulo, estado, children, className, testid }: { titulo: string; estado?: ReactNode; children: ReactNode; className?: string; testid: string }) {
    return (
        <Tarjeta className={cn("grid min-w-0 content-start gap-3 p-4", className)} data-testid={testid}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 className="text-base font-semibold">{titulo}</h2>
                {estado}
            </div>
            {children}
        </Tarjeta>
    );
}

function IconoEtapa({ estado }: { estado: EtapaSincronizacion["estado"] }) {
    const clase = "mt-0.5 size-4 shrink-0";
    switch (estado) {
        case "completa":
            return <CheckCircle2 aria-hidden className={cn(clase, "text-success")} />;
        case "en_curso":
            return <Cargando decorativo className={cn(clase, "text-info")} />;
        case "con_errores":
            return <AlertTriangle aria-hidden className={cn(clase, "text-warning")} />;
        default:
            return <Circle aria-hidden className={cn(clase, "text-muted-foreground")} />;
    }
}

function TarjetaSincronizacion({
    sinc,
    conectorId,
    activo,
    alCambiar,
}: {
    sinc: SincronizacionArgos | undefined;
    conectorId: number;
    activo: boolean;
    alCambiar: (s: SincronizacionArgos) => void;
}) {
    const { puede } = useSesion();
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const etapas = sinc?.etapas ?? [];
    const corte = sinc ? corteEnCurso(sinc) : undefined;

    async function sincronizar() {
        setEnviando(true);
        setError(null);
        try {
            const s = await resolver(api().POST("/api/v1/integraciones/conectores/{id}/sincronizar", { params: { path: { id: conectorId }, header: { "Idempotency-Key": clave } } }));
            setClave(nuevaClaveIdempotencia());
            avisar({ titulo: sinc?.estado === "completa" || !sinc ? "Sincronización lanzada" : "Sincronización reanudada", descripcion: "Solo lee de Argos; el avance aparece aquí." });
            alCambiar(s);
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    const puedeSincronizar = activo && puede(["integraciones.sincronizar"]) && sinc?.estado !== "en_curso";
    return (
        <Seccion
            titulo="Primera sincronización"
            testid="sincronizacion"
            estado={sinc ? <InsigniaEstado tono={TONO_SINCRONIZACION[sinc.estado]} texto={TEXTO_SINCRONIZACION[sinc.estado]} /> : <InsigniaEstado tono="neutral" texto="Sin empezar" />}
        >
            {sinc ? (
                <>
                    <p className="text-xs text-muted-foreground">
                        {sinc.origen === "activacion" ? "Empezó al activar el conector" : "La lanzó un administrador"} · {textoFechaHora(sinc.iniciadaEn)} ·{" "}
                        {sinc.excluirRetirados ? "solo clientes con servicio no retirado" : "todos los clientes (este Argos no filtra retirados)"}
                    </p>
                    <ul className="grid" aria-label="Recursos">
                        {etapas.map((e, i) => (
                            <li key={e.recurso} className="grid grid-cols-[1rem_minmax(0,1fr)_auto] gap-2 border-t py-2 first:border-t-0" data-recurso={e.recurso}>
                                <IconoEtapa estado={e.estado} />
                                <div className="min-w-0">
                                    <div className="text-sm">{NOMBRE_RECURSO[e.recurso]}</div>
                                    <div className="text-xs text-muted-foreground tabular-nums">{detalleEtapa(e, etapas[i - 1])}</div>
                                    {e.error && e.estado === "con_errores" ? <div className="text-xs text-warning">{e.error}</div> : null}
                                </div>
                                <span className={cn("text-xs whitespace-nowrap", e.estado === "pendiente" && "text-muted-foreground")}>{TEXTO_ESTADO[e.estado]}</span>
                            </li>
                        ))}
                    </ul>
                    <p className="text-xs text-muted-foreground" data-testid="informe">
                        {textoInforme(sinc, textoFechaHora)}
                    </p>
                    {corte?.error ? (
                        <Aviso tono="warning" titulo="Argos no respondió" data-testid="corte">
                            Iris sigue solo desde donde quedó. Mientras tanto todo funciona con lo que ya tiene.
                        </Aviso>
                    ) : null}
                </>
            ) : (
                <p className="text-sm text-muted-foreground">
                    {activo ? "Todavía no empieza. Empieza sola al activar el conector." : "Empieza sola cuando actives el conector en Integraciones."}
                </p>
            )}
            {error ? (
                <Aviso tono="error" titulo="No se pudo sincronizar">
                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            {puedeSincronizar ? (
                <div className="flex flex-wrap items-center gap-2">
                    <Boton tamano="sm" variante={sinc?.estado === "con_errores" ? "primario" : "secundario"} onClick={() => void sincronizar()} disabled={enviando}>
                        <RefreshCw />
                        {enviando ? "Enviando…" : textoBotonSincronizar(sinc)}
                    </Boton>
                    <span className="text-xs text-muted-foreground">Solo lee de Argos; no cambia nada allá.</span>
                </div>
            ) : null}
        </Seccion>
    );
}

function TarjetaConexion({ datos: d, conector: c }: { datos: EstadoConexionArgos; conector: Conector }) {
    const apagadas = d.funcionesApagadas ?? [];
    return (
        <Seccion
            titulo="Conexión"
            testid="conexion"
            estado={c.estado === "activo" ? <InsigniaEstado tono="success" icono={CircleCheck} texto="Activo" /> : <InsigniaEstado tono="neutral" icono={CirclePower} texto="Apagado" />}
        >
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm">
                {c.estado === "activo" && c.activadoEn ? (
                    <>
                        <dt className="text-muted-foreground">Desde</dt>
                        <dd>
                            {textoFechaHora(c.activadoEn)}
                            {d.activacion?.actorNombre ? ` · ${d.activacion.actorNombre}` : ""}
                        </dd>
                    </>
                ) : null}
                <dt className="text-muted-foreground">Panel</dt>
                <dd className="break-all">{c.panelUrl ?? "Sin informar"}</dd>
                <dt className="text-muted-foreground">API</dt>
                <dd className="font-mono text-xs break-all">{c.apiUrl}</dd>
                <dt className="text-muted-foreground">Iris</dt>
                <dd>contrato {d.contratoIris}</dd>
                <dt className="text-muted-foreground">Argos</dt>
                <dd data-testid="version-argos">{d.versionArgos || d.contratoArgos ? [d.versionArgos, d.contratoArgos ? `contrato ${d.contratoArgos}` : null].filter(Boolean).join(" · ") : "Sin informar todavía"}</dd>
            </dl>
            {apagadas.length > 0 ? (
                <Aviso tono="info" icono={Info} titulo="Con este Argos queda apagado" data-testid="funciones-apagadas">
                    <ul className="list-disc pl-4">
                        {apagadas.map((f) => (
                            <li key={f.codigo}>
                                {f.texto} (llega con {f.desde})
                            </li>
                        ))}
                    </ul>
                </Aviso>
            ) : null}
        </Seccion>
    );
}

/** Filas de dato y cifra (sin realce al pasar el puntero): una línea entre filas, como una lista. */
const FILA_DATO = "border-t border-b-0 first:border-t-0 hover:bg-transparent";

function TarjetaDatos({ datos: d }: { datos: EstadoConexionArgos }) {
    const [ahora] = useState(() => Date.now());
    return (
        <Seccion titulo="Datos de Argos en Iris" testid="instantaneas">
            <Tabla>
                <TablaCuerpo>
                    {(d.instantaneas ?? []).map((i) => (
                        <TablaFila key={i.recurso} className={FILA_DATO}>
                            <TablaCelda className="px-0 py-1.5">{NOMBRE_RECURSO[i.recurso]}</TablaCelda>
                            <TablaCelda className="px-0 py-1.5 text-right">{i.cuantos.toLocaleString("es-GT")}</TablaCelda>
                            <TablaCelda className="py-1.5 pr-0 pl-3 text-right text-xs text-muted-foreground">{i.ultimoSincronizadoEn ? haceCuanto(i.ultimoSincronizadoEn, ahora) : "—"}</TablaCelda>
                        </TablaFila>
                    ))}
                </TablaCuerpo>
            </Tabla>
        </Seccion>
    );
}

function TarjetaFallos({ datos: d }: { datos: EstadoConexionArgos }) {
    const f = d.fallos;
    const n = porRevisar(f);
    const pendientes = (f.pendientes ?? []).reduce((t, p) => t + p.cuantos, 0);
    return (
        <Seccion
            titulo="Qué falló"
            testid="fallos"
            estado={n > 0 ? <InsigniaEstado tono="warning" texto={`${n} por revisar`} /> : <InsigniaEstado tono="success" texto="Nada" />}
        >
            <Tabla>
                <TablaCuerpo>
                    <TablaFila className={FILA_DATO}>
                        <TablaCelda className="px-0 py-1.5">Avisos de Argos sin aplicar</TablaCelda>
                        <TablaCelda className="px-0 py-1.5 text-right">{f.webhooksConError}</TablaCelda>
                    </TablaFila>
                    <TablaFila className={FILA_DATO}>
                        <TablaCelda className="px-0 py-1.5">Avisos con firma inválida (desde el último arranque)</TablaCelda>
                        <TablaCelda className="px-0 py-1.5 text-right">{f.webhooksRechazados}</TablaCelda>
                    </TablaFila>
                    <TablaFila className={FILA_DATO}>
                        <TablaCelda className="px-0 py-1.5">Acciones hacia Argos esperando</TablaCelda>
                        <TablaCelda className="px-0 py-1.5 text-right">{pendientes}</TablaCelda>
                    </TablaFila>
                </TablaCuerpo>
            </Tabla>
        </Seccion>
    );
}

function TarjetaEmpresas({ datos: d }: { datos: EstadoConexionArgos }) {
    const empresas = d.empresas ?? [];
    return (
        <Seccion titulo="Empresas que ve Iris" testid="empresas">
            {empresas.length > 0 ? (
                <ul className="flex flex-wrap gap-1.5">
                    {empresas.map((e) => (
                        <li key={e.id} className="rounded-full border bg-elevated px-2.5 py-0.5 text-xs">
                            {e.nombre || `Empresa ${e.id}`}
                        </li>
                    ))}
                </ul>
            ) : (
                <p className="text-sm text-muted-foreground">Argos no informó las empresas.</p>
            )}
            {d.alcanceGlobal === true ? <p className="text-xs text-muted-foreground">También las que se creen después en Argos.</p> : null}
            {d.alcanceGlobal === false ? <p className="text-xs text-muted-foreground">Solo estas: una empresa nueva en Argos no se ve hasta reconectar.</p> : null}
        </Seccion>
    );
}

function TarjetaCertificado({ datos: d }: { datos: EstadoConexionArgos }) {
    const c = d.certificado;
    return (
        <Seccion
            titulo="Certificado de Argos"
            testid="certificado"
            className="lg:col-span-2"
            estado={c?.cambiado ? <InsigniaEstado tono="warning" icono={ShieldAlert} texto="Cambió" /> : c ? <InsigniaEstado tono="neutral" texto="Solo informa" /> : null}
        >
            {c ? (
                <>
                    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm">
                        <dt className="text-muted-foreground">Emisor</dt>
                        <dd className="break-words">{c.emisor}</dd>
                        <dt className="text-muted-foreground">Sujeto</dt>
                        <dd className="break-words">{c.sujeto}</dd>
                        <dt className="text-muted-foreground">Huella</dt>
                        <dd className="font-mono text-xs break-all" data-testid="huella">
                            {huellaLegible(c.huellaSha256)} (SHA-256)
                        </dd>
                        {c.venceEn ? (
                            <>
                                <dt className="text-muted-foreground">Vence</dt>
                                <dd>{textoFechaHora(c.venceEn)}</dd>
                            </>
                        ) : null}
                    </dl>
                    {c.cambiado ? (
                        <Aviso tono="warning" titulo={`El certificado cambió el ${textoFechaHora(c.vistoEn)}`} data-testid="certificado-cambio">
                            Lo normal es que sea la renovación automática (cada 60 a 90 días). Iris no detuvo nada. Si no esperabas un cambio, revisa el servidor de Argos.
                            {c.anterior ? <span className="mt-1 block font-mono text-xs break-all">Antes: {huellaLegible(c.anterior.huellaSha256)}</span> : null}
                        </Aviso>
                    ) : null}
                </>
            ) : (
                <p className="text-sm text-muted-foreground">Todavía no se ha visto: aparece en la próxima llamada de Iris a Argos por HTTPS.</p>
            )}
        </Seccion>
    );
}

/** Enlace a esta pantalla desde la fila del conector en Integraciones. */
export const RUTA_ESTADO_ARGOS = "/administracion/integraciones/argos" as Route;

export function EnlaceEstadoArgos() {
    return (
        <Boton asChild variante="secundario" tamano="sm">
            <Link href={RUTA_ESTADO_ARGOS} data-testid="ver-estado-argos">
                Ver estado y sincronización
            </Link>
        </Boton>
    );
}
