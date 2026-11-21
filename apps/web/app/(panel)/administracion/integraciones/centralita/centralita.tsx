"use client";

import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoSelector,
    Casilla,
    Dialogo,
    DialogoConfirmar,
    DialogoContenido,
    DialogoPie,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { Pencil, PhoneCall, Plug, Plus, RefreshCw, ShieldCheck, Trash2, Users } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { EnlaceExtension, TarjetaGrupos, TarjetaTelefono } from "@/componentes/telefono/administracion";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { activarCentralita, asignarExtension, confirmarHuella, desactivarCentralita, leerCentralita, leerExtensiones, probarCentralita, quitarExtension } from "@/lib/centralita/api";
import { estadoConexion, huellaLegible, textoPrueba, ultimaSenal } from "@/lib/centralita/presentacion";
import { PERMISO_ASIGNAR_EXTENSION, PERMISO_CONFIGURAR_CENTRALITA, type Centralita, type ExtensionCentralita, type PruebaCentralita } from "@/lib/centralita/tipos";
import { buscarPersonas, type PersonaChat } from "@/lib/chat/api";
import { formatearFecha } from "@/lib/fechas";
import { estadoExtension } from "@/lib/telefono/administracion";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

const RUTA_CONEXION = "/administracion/integraciones/centralita/conexion";

/**
 * `/administracion/integraciones/centralita` (E4a, contratos/voz.md §3.12): la centralita Issabel de
 * la empresa. Estado de la conexión (un punto si todo va bien; crece solo si hay que actuar),
 * «Probar conexión», encender o apagar, y la extensión de cada persona (solo a ellas les sale la
 * ventana). Editar la conexión va en su propia pantalla (más de 3 campos).
 */
export function PaginaCentralita() {
    const { puede } = useSesion();
    const configura = puede([PERMISO_CONFIGURAR_CENTRALITA]);
    const lectura = useLectura((signal) => leerCentralita(signal));
    if (lectura.estado === "cargando") return <Esqueleto className="h-48" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="la centralita" />;
    const c = lectura.datos;
    if (!c) {
        return (
            <>
                <EncabezadoPagina titulo="Centralita" descripcion="Cuando suene la extensión de alguien de tu equipo, Iris le muestra quién llama." />
                <Tarjeta>
                    <EstadoVacio
                        icono={PhoneCall}
                        titulo="Conecta tu Issabel"
                        descripcion="Iris solo escucha: no marca, no graba y no cambia nada en tu centralita. Necesitas un usuario AMI de solo lectura (docs/runbooks/centralita-issabel.md)."
                        accion={
                            configura ? (
                                <Boton asChild>
                                    <Link href={RUTA_CONEXION}>
                                        <Plug />
                                        Conectar centralita
                                    </Link>
                                </Boton>
                            ) : (
                                <p className="text-sm text-muted-foreground">Pide a un administrador que la conecte.</p>
                            )
                        }
                    />
                </Tarjeta>
            </>
        );
    }
    return (
        <>
            <EncabezadoPagina
                titulo={c.nombre}
                descripcion="Issabel · solo lectura"
                acciones={
                    configura ? (
                        <Boton variante="secundario" asChild>
                            <Link href={RUTA_CONEXION}>
                                <Pencil />
                                Editar
                            </Link>
                        </Boton>
                    ) : null
                }
            />
            <div className="grid gap-4">
                <Conexion centralita={c} configura={configura} alCambiar={() => void lectura.refrescar({ silencioso: true })} />
                <TarjetaTelefono centralita={c} configura={configura} alCambiar={() => void lectura.refrescar({ silencioso: true })} />
                {puede([PERMISO_ASIGNAR_EXTENSION]) ? <Extensiones centralita={c} /> : null}
                {configura ? <TarjetaGrupos centralita={c} /> : null}
            </div>
        </>
    );
}

function Conexion({ centralita: c, configura, alCambiar }: { centralita: Centralita; configura: boolean; alCambiar: () => void }) {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = window.setInterval(() => setAhora(Date.now()), 30_000);
        return () => window.clearInterval(t);
    }, []);
    const e = estadoConexion(c, ahora);
    const [prueba, setPrueba] = useState<PruebaCentralita | null>(null);
    const [ocupado, setOcupado] = useState(false);
    const [apagar, setApagar] = useState(false);
    const [huella, setHuella] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const hacer = async (f: () => Promise<unknown>, ok: string) => {
        setOcupado(true);
        setError(null);
        try {
            await f();
            avisar({ titulo: ok });
            alCambiar();
        } catch (err) {
            setError(err);
        } finally {
            setOcupado(false);
        }
    };
    const senal = ultimaSenal(c, ahora);
    return (
        <Tarjeta data-slot="conexion-centralita">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center gap-2">
                <TarjetaTitulo className="flex items-center gap-2">
                    <Plug aria-hidden className="size-4 text-muted-foreground" />
                    Conexión
                </TarjetaTitulo>
                <span className="ml-auto" data-slot="estado-centralita" data-estado={c.estado}>
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
                {e.detalle && (e.tono === "warning" || e.tono === "error") ? (
                    <Aviso tono={e.tono} titulo={e.texto} data-slot="aviso-centralita-estado">
                        {e.detalle}
                        {configura && c.errorCodigo === "huella_cambiada" && c.huellaVista ? (
                            <div className="mt-2">
                                <Boton tamano="sm" variante="secundario" onClick={() => setHuella(true)}>
                                    Comparar y confirmar
                                </Boton>
                            </div>
                        ) : null}
                    </Aviso>
                ) : null}
                <dl className="grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-4 gap-y-2 text-sm">
                    {senal ? (
                        <>
                            <dt className="text-muted-foreground">Última señal</dt>
                            <dd>{senal}</dd>
                        </>
                    ) : null}
                    <dt className="text-muted-foreground">Dirección</dt>
                    <dd className="font-mono text-[0.8125rem] break-all">
                        {c.host} · puerto {c.puerto} (cifrado)
                    </dd>
                    <dt className="text-muted-foreground">Usuario AMI</dt>
                    <dd className="font-mono text-[0.8125rem] break-all">{c.usuario}</dd>
                    <dt className="text-muted-foreground">Contraseña</dt>
                    <dd>{c.secretoGuardadoEn ? `Guardada el ${formatearFecha(c.secretoGuardadoEn)?.slice(0, 10)}` : "Sin guardar"}</dd>
                    <dt className="text-muted-foreground">Permisos</dt>
                    <dd>
                        {c.probadaEn ? (
                            <span className="flex items-center gap-1.5 text-success">
                                <ShieldCheck aria-hidden className="size-4" />
                                Solo lectura, comprobado
                            </span>
                        ) : (
                            <span className="text-muted-foreground">Sin probar</span>
                        )}
                    </dd>
                </dl>
                {prueba ? <ResultadoPrueba prueba={prueba} /> : null}
                {error ? (
                    <Aviso tono="error" titulo="No se pudo">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
                {configura ? (
                    <div className="flex flex-wrap gap-2">
                        <Boton
                            variante="secundario"
                            disabled={ocupado}
                            onClick={async () => {
                                setOcupado(true);
                                setError(null);
                                try {
                                    setPrueba(await probarCentralita(c.id));
                                    alCambiar();
                                } catch (err) {
                                    setError(err);
                                } finally {
                                    setOcupado(false);
                                }
                            }}
                        >
                            <RefreshCw />
                            Probar conexión
                        </Boton>
                        {c.activa ? (
                            <Boton variante="fantasma" disabled={ocupado} onClick={() => setApagar(true)}>
                                Apagar
                            </Boton>
                        ) : (
                            <Boton disabled={ocupado || !c.probadaEn} onClick={() => void hacer(() => activarCentralita(c.id, c.version), "Centralita encendida")}>
                                Encender
                            </Boton>
                        )}
                    </div>
                ) : null}
                {configura && !c.activa && !c.probadaEn ? <p className="text-sm text-muted-foreground">Prueba la conexión para poder encenderla.</p> : null}
            </TarjetaContenido>
            <DialogoConfirmar
                open={apagar}
                onOpenChange={setApagar}
                titulo="¿Apagar la centralita en Iris?"
                descripcion="Iris deja de escuchar: no salen ventanas ni se guardan llamadas. Issabel sigue funcionando igual."
                textoConfirmar="Apagar"
                peligro
                alConfirmar={async () => {
                    await hacer(() => desactivarCentralita(c.id, c.version), "Centralita apagada");
                    setApagar(false);
                }}
            />
            {huella && c.huellaVista ? <ConfirmarHuella centralita={c} alCerrar={() => setHuella(false)} alConfirmar={alCambiar} /> : null}
        </Tarjeta>
    );
}

export function ResultadoPrueba({ prueba }: { prueba: PruebaCentralita }) {
    const t = textoPrueba(prueba);
    return (
        <Aviso tono={t.tono === "success" ? "success" : "error"} titulo={t.titulo} data-slot="resultado-prueba" data-estado={prueba.estado}>
            {t.detalle}
        </Aviso>
    );
}

/** El certificado cambió: el admin compara la huella nueva con la de Issabel (1 casilla). */
function ConfirmarHuella({ centralita: c, alCerrar, alConfirmar }: { centralita: Centralita; alCerrar: () => void; alConfirmar: () => void }) {
    const [es, setEs] = useState(false);
    const [error, setError] = useState<unknown>(null);
    return (
        <Dialogo open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo="Comparar el certificado" descripcion="Pide a quien administra Issabel la huella de su certificado y compárala.">
                <dl className="grid gap-1 text-sm">
                    <dt className="text-muted-foreground">Antes</dt>
                    <dd className="font-mono text-xs break-all">{huellaLegible(c.huellaTls)}</dd>
                    <dt className="mt-2 text-muted-foreground">Ahora</dt>
                    <dd className="font-mono text-xs break-all" data-slot="huella-nueva">
                        {huellaLegible(c.huellaVista)}
                    </dd>
                </dl>
                <Casilla etiqueta="Es la nueva huella de mi Issabel" checked={es} onChange={(e) => setEs(e.target.checked)} />
                {error ? (
                    <Aviso tono="error" titulo="No se pudo confirmar">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <Boton variante="fantasma" onClick={alCerrar}>
                        Cancelar
                    </Boton>
                    <Boton
                        disabled={!es}
                        onClick={async () => {
                            try {
                                await confirmarHuella(c.id, c.version, c.huellaVista ?? "");
                                avisar({ titulo: "Certificado confirmado: Iris vuelve a conectarse" });
                                alConfirmar();
                                alCerrar();
                            } catch (e) {
                                setError(e);
                            }
                        }}
                    >
                        Confirmar y conectar
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}

function Extensiones({ centralita: c }: { centralita: Centralita }) {
    const lectura = useLectura((signal) => leerExtensiones(c.id, signal));
    const [asignar, setAsignar] = useState(false);
    const [quitar, setQuitar] = useState<ExtensionCentralita | null>(null);
    const lista = lectura.estado === "listo" ? (lectura.datos ?? []) : [];
    return (
        <Tarjeta data-slot="extensiones-centralita">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center gap-2">
                <TarjetaTitulo className="flex items-center gap-2">
                    <Users aria-hidden className="size-4 text-muted-foreground" />
                    Extensiones
                </TarjetaTitulo>
                <Boton className="ml-auto" tamano="sm" variante="secundario" onClick={() => setAsignar(true)}>
                    <Plus />
                    Asignar extensión
                </Boton>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                <p className="text-sm text-muted-foreground">Solo a estas personas les sale la ventana. Las demás extensiones de la centralita Iris ni las mira. «En Iris»: contestan y llaman desde el panel.</p>
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-24" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las extensiones" />
                ) : lista.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Nadie tiene extensión todavía.</p>
                ) : (
                    <ul className="grid divide-y divide-border" aria-label="Extensiones">
                        {lista.map((x) => {
                            const e = estadoExtension(x);
                            return (
                                <li key={x.id} className="flex min-w-0 items-center gap-3 py-2.5" data-slot="extension" data-tipo={x.tipo}>
                                    <span className="inline-flex h-8 min-w-11 items-center justify-center rounded-lg bg-elevated px-2 text-sm font-bold tabular-nums">{x.extension}</span>
                                    <span className="grid min-w-0 flex-1">
                                        <span className="truncate font-medium">{x.usuarioNombre}</span>
                                        <span className="truncate text-xs text-muted-foreground">{e.donde}</span>
                                        <span className={e.tono === "success" ? "text-xs text-success" : e.tono === "error" ? "text-xs text-error" : e.tono === "warning" ? "text-xs text-warning" : "text-xs text-muted-foreground"}>{e.texto}</span>
                                    </span>
                                    <Boton variante="fantasma" tamano="icono-sm" aria-label={`Quitar la extensión ${x.extension} de ${x.usuarioNombre}`} onClick={() => setQuitar(x)}>
                                        <Trash2 />
                                    </Boton>
                                    <EnlaceExtension id={x.id} nombre={x.usuarioNombre} />
                                </li>
                            );
                        })}
                    </ul>
                )}
            </TarjetaContenido>
            {asignar ? (
                <AsignarExtension
                    centralita={c}
                    alCerrar={() => setAsignar(false)}
                    alAsignar={() => {
                        setAsignar(false);
                        void lectura.refrescar({ silencioso: true });
                    }}
                />
            ) : null}
            <DialogoConfirmar
                open={quitar !== null}
                onOpenChange={(a) => !a && setQuitar(null)}
                titulo={quitar ? `¿Quitar la extensión ${quitar.extension} de ${quitar.usuarioNombre}?` : ""}
                descripcion="Ya no le saldrá la ventana. Sus llamadas anteriores se quedan."
                textoConfirmar="Quitar"
                peligro
                alConfirmar={async () => {
                    if (!quitar) return;
                    try {
                        await quitarExtension(c.id, quitar.id);
                        avisar({ titulo: `Extensión ${quitar.extension} quitada` });
                        setQuitar(null);
                        void lectura.refrescar({ silencioso: true });
                    } catch (e) {
                        avisar({ tono: "error", titulo: mensajeDeError(e).titulo });
                    }
                }}
            />
        </Tarjeta>
    );
}

/** Persona y extensión: 2 campos, en un diálogo. */
function AsignarExtension({ centralita: c, alCerrar, alAsignar }: { centralita: Centralita; alCerrar: () => void; alAsignar: () => void }) {
    const personas = useLectura((signal) => buscarPersonas("", signal));
    const [usuario, setUsuario] = useState("");
    const [extension, setExtension] = useState("");
    const [error, setError] = useState<unknown>(null);
    const [enviando, setEnviando] = useState(false);
    const clave = useClaveIdempotencia();
    const extValida = /^[0-9]{2,8}$/.test(extension);
    const lista: PersonaChat[] = personas.estado === "listo" ? personas.datos : [];
    return (
        <Dialogo open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo="Asignar extensión">
                <div className="grid gap-3">
                    <CampoSelector etiqueta="Persona" value={usuario} onChange={(e) => setUsuario(e.target.value)} disabled={personas.estado !== "listo"}>
                        <option value="">{personas.estado === "cargando" ? "Cargando…" : "Elegir…"}</option>
                        {lista.map((p) => (
                            <option key={p.id} value={String(p.id)}>
                                {p.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                    <Campo
                        etiqueta="Extensión"
                        inputMode="numeric"
                        value={extension}
                        onChange={(e) => setExtension(e.target.value.replace(/\D/g, "").slice(0, 8))}
                        ayuda="De 2 a 8 dígitos, como aparece en Issabel."
                        error={extension !== "" && !extValida ? "De 2 a 8 dígitos" : undefined}
                    />
                    {error ? (
                        <Aviso tono="error" titulo={error instanceof ErrorApi && error.estado === 409 ? "Ya está ocupada" : "No se pudo asignar"}>
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                </div>
                <DialogoPie>
                    <Boton variante="fantasma" onClick={alCerrar}>
                        Cancelar
                    </Boton>
                    <Boton
                        disabled={!usuario || !extValida || enviando}
                        onClick={async () => {
                            setEnviando(true);
                            setError(null);
                            try {
                                const x = await asignarExtension(c.id, Number(usuario), extension, clave.actual());
                                clave.renovar();
                                avisar({ titulo: `Extensión ${x.extension} asignada a ${x.usuarioNombre}` });
                                alAsignar();
                            } catch (e) {
                                clave.trasError(e);
                                setError(e);
                            } finally {
                                setEnviando(false);
                            }
                        }}
                    >
                        Asignar
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
