"use client";

import type { Schema } from "@iris/contratos";
import {
    avisar,
    Aviso,
    Boton,
    DialogoConfirmar,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import {
    ChevronDown,
    CircleCheck,
    CirclePause,
    Inbox,
    Mail,
    MailX,
    Pencil,
    Plug,
    Plus,
    Power,
    PowerOff,
    RotateCw,
    Send,
    ShieldAlert,
    ShieldCheck,
    TriangleAlert,
} from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nombreCategoria, useCategorias, type Categoria } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { NOMBRES_TLS, textoPrueba } from "@/lib/buzones/presentacion";
import type { Buzon } from "@/lib/buzones/esquemas";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { PRIORIDADES } from "@/lib/tickets/presentacion";
import { esConflicto, textoErrorBuzon } from "./acciones-buzon";
import { FormularioBuzon } from "./formulario-buzon";
import { avisoEstado } from "@/lib/avisos";

type Pagina = Schema<"PaginaBuzon">;
type ResultadoPrueba = Schema<"ResultadoPrueba">;

const LIMITE = 50;

function leerPagina(cursor: string | undefined, signal?: AbortSignal): Promise<Pagina> {
    return resolver(
        api().GET("/api/v1/buzones", {
            params: { query: { limit: LIMITE, ...(cursor ? { cursor } : {}) } },
            ...(signal ? { signal } : {}),
        }),
    );
}

export function ListaBuzones() {
    const { puede } = useSesion();
    const primera = useLectura((signal) => leerPagina(undefined, signal));
    const categorias = useCategorias();
    const [extra, setExtra] = useState<{ items: Buzon[]; cursor: string | null } | null>(null);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const [formulario, setFormulario] = useState<{ buzon?: Buzon } | null>(null);
    const siguiente = primera.estado === "listo" ? (extra ? extra.cursor : primera.datos.nextCursor) : null;

    async function cargarMas() {
        if (!siguiente) return;
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const p = await leerPagina(siguiente);
            setExtra((prev) => ({ items: [...(prev?.items ?? []), ...(p.data ?? [])], cursor: p.nextCursor }));
        } catch (e) {
            setErrorMas(e);
        } finally {
            setCargandoMas(false);
        }
    }

    async function refrescar() {
        setExtra(null);
        await primera.refrescar();
    }

    const encabezado = (
        <EncabezadoPagina
            titulo="Buzones de correo"
            descripcion="Los correos que llegan a estos buzones abren tickets o se suman a su conversación, y las respuestas salen por SMTP."
            acciones={
                puede(["buzones.crear"]) ? (
                    <Boton onClick={() => setFormulario({})}>
                        <Plus />
                        Nuevo buzón
                    </Boton>
                ) : null
            }
        />
    );

    let contenido;
    if (primera.estado === "cargando") {
        contenido = (
            <div className="grid gap-3" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando buzones…
                </span>
                <Esqueleto className="h-36" />
                <Esqueleto className="h-36" />
            </div>
        );
    } else if (primera.estado === "error") {
        contenido = <ErrorLectura error={primera.error} que="los buzones" reintentar={primera.reintentar} />;
    } else {
        const items = [...(primera.datos.data ?? []), ...(extra?.items ?? [])];
        contenido =
            items.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio
                        icono={Inbox}
                        titulo="Aún no hay buzones"
                        descripcion={
                            puede(["buzones.crear"])
                                ? "Registra el buzón de soporte para que los correos de los clientes abran tickets."
                                : "Cuando un administrador registre un buzón, aparecerá aquí."
                        }
                    />
                </Tarjeta>
            ) : (
                <ul className="grid gap-3" aria-label="Buzones">
                    {items.map((b) => (
                        <li key={b.id}>
                            <TarjetaBuzon buzon={b} categorias={categorias.estado === "listo" ? categorias.datos : undefined} alEditar={() => setFormulario({ buzon: b })} alCambiar={refrescar} />
                        </li>
                    ))}
                </ul>
            );
    }

    return (
        <>
            {encabezado}
            {contenido}
            {errorMas ? (
                <Aviso
                    className="mt-3"
                    tono="error"
                    titulo="No se pudieron cargar más buzones"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={cargarMas}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {textoErrorBuzon(errorMas).detalle}
                </Aviso>
            ) : siguiente ? (
                <div className="mt-3 flex justify-center">
                    <Boton variante="contorno" tamano="sm" onClick={cargarMas} disabled={cargandoMas}>
                        <ChevronDown />
                        {cargandoMas ? "Cargando…" : "Cargar más"}
                    </Boton>
                </div>
            ) : null}
            {formulario ? (
                <FormularioBuzon
                    key={formulario.buzon?.id ?? "nuevo"}
                    buzon={formulario.buzon}
                    alCerrar={() => setFormulario(null)}
                    alGuardar={() => {
                        setFormulario(null);
                        void refrescar();
                    }}
                />
            ) : null}
        </>
    );
}

function Dato({ etiqueta, children }: { etiqueta: string; children: ReactNode }) {
    return (
        <div className="grid min-w-0 gap-0.5">
            <dt className="text-xs font-medium text-muted-foreground">{etiqueta}</dt>
            <dd className="min-w-0 text-sm break-words">{children}</dd>
        </div>
    );
}

function TarjetaBuzon({
    buzon: b,
    categorias,
    alEditar,
    alCambiar,
}: {
    buzon: Buzon;
    categorias: readonly Categoria[] | undefined;
    alEditar: () => void;
    alCambiar: () => Promise<void>;
}) {
    const { puede } = useSesion();
    const clave = useRef(nuevaClaveIdempotencia());
    const claveProbar = useRef(nuevaClaveIdempotencia());
    const [cambiando, setCambiando] = useState(false);
    const [confirmar, setConfirmar] = useState(false);
    const [probando, setProbando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const [prueba, setPrueba] = useState<ResultadoPrueba | null>(null);
    const [recargando, setRecargando] = useState(false);
    const accion = b.activo ? "desactivar" : "activar";
    const puedeCambiar = puede([b.activo ? "buzones.desactivar" : "buzones.activar"]);
    const puedeEditar = puede(["buzones.editar"]);

    async function cambiarEstado() {
        setCambiando(true);
        setError(null);
        try {
            await resolver(
                api().POST(b.activo ? "/api/v1/buzones/{id}/desactivar" : "/api/v1/buzones/{id}/activar", {
                    params: { path: { id: b.id }, header: { "Idempotency-Key": clave.current } },
                    body: { version: b.version },
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            avisar({ titulo: avisoEstado(b.nombre, !b.activo) });
            await alCambiar();
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        } finally {
            setCambiando(false);
        }
    }

    async function probar() {
        setProbando(true);
        setError(null);
        setPrueba(null);
        try {
            const r = await resolver(
                api().POST("/api/v1/buzones/{id}/probar", { params: { path: { id: b.id }, header: { "Idempotency-Key": claveProbar.current } } }),
            );
            setPrueba(r);
        } catch (e) {
            setError(e);
        } finally {
            // Cada prueba es un intento nuevo (la API guarda la respuesta por clave).
            claveProbar.current = nuevaClaveIdempotencia();
            setProbando(false);
        }
    }

    async function recargar() {
        setRecargando(true);
        try {
            await alCambiar();
            setError(null);
        } finally {
            setRecargando(false);
        }
    }

    const prioridad = PRIORIDADES[b.prioridad];
    return (
        <Tarjeta data-buzon={b.correo}>
            <TarjetaEncabezado className="flex flex-row flex-wrap items-start justify-between gap-3">
                <div className="grid min-w-0 gap-1">
                    <TarjetaTitulo className="break-words">{b.nombre}</TarjetaTitulo>
                    <span className="inline-flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground">
                        <Mail aria-hidden className="size-4 shrink-0" />
                        <span className="truncate font-mono">{b.correo}</span>
                    </span>
                </div>
                <div className="flex flex-wrap gap-1.5">
                    {b.activo ? (
                        <InsigniaEstado tono="success" icono={CircleCheck} texto="Activo" />
                    ) : (
                        <InsigniaEstado tono="neutral" icono={CirclePause} texto="Inactivo" />
                    )}
                    {b.authservId ? (
                        <InsigniaEstado tono="success" icono={ShieldCheck} texto="Verifica remitentes" />
                    ) : (
                        <InsigniaEstado
                            tono="warning"
                            icono={ShieldAlert}
                            texto="Sin verificación de remitente"
                            title="Sin authserv-id ningún remitente queda verificado: las respuestas por correo entran como notas internas"
                        />
                    )}
                </div>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4">
                <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    <Dato etiqueta="Entrada (IMAP)">
                        <span className="font-mono">
                            {b.imap.host}:{b.imap.puerto}
                        </span>{" "}
                        · {NOMBRES_TLS[b.imap.tls]}
                        <span className="block text-xs text-muted-foreground">
                            {b.imap.usuario} · carpeta {b.imap.carpeta}
                            {b.imap.carpetaProcesados ? ` → ${b.imap.carpetaProcesados}` : ""}
                        </span>
                    </Dato>
                    <Dato etiqueta="Salida (SMTP)">
                        {b.smtp ? (
                            <>
                                <span className="font-mono">
                                    {b.smtp.host}:{b.smtp.puerto}
                                </span>{" "}
                                · {NOMBRES_TLS[b.smtp.tls]}
                                <span className="block text-xs text-muted-foreground">{b.smtp.usuario ?? "Sin autenticación"}</span>
                            </>
                        ) : (
                            <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                                <MailX aria-hidden className="size-4" />
                                Sin salida: las respuestas no se envían por correo
                            </span>
                        )}
                    </Dato>
                    <Dato etiqueta="Tickets nuevos">
                        Prioridad {prioridad.texto.toLowerCase()}
                        {b.categoriaId !== null ? <span className="block text-xs text-muted-foreground">{nombreCategoria(categorias, b.categoriaId)}</span> : null}
                    </Dato>
                    <Dato etiqueta="Última lectura">
                        <span className="tabular-nums">{formatearFecha(b.ultimaLecturaEn) ?? "Nunca"}</span>
                    </Dato>
                </dl>
                {b.ultimoError ? (
                    <Aviso tono="error" titulo="Último error de lectura" icono={TriangleAlert} role="status">
                        {b.ultimoError}
                    </Aviso>
                ) : null}
                {prueba ? <ResultadoDePrueba prueba={prueba} /> : null}
                {error ? (
                    esConflicto(error) ? (
                        <Aviso
                            tono="warning"
                            titulo="Alguien más cambió este buzón"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                                    <RotateCw />
                                    {recargando ? "Recargando…" : "Recargar"}
                                </Boton>
                            }
                        >
                            Recarga para ver el cambio y vuelve a intentarlo.
                        </Aviso>
                    ) : (
                        <Aviso tono="error" titulo={textoErrorBuzon(error).titulo}>
                            {textoErrorBuzon(error).detalle}
                        </Aviso>
                    )
                ) : null}
                {puedeEditar || puedeCambiar ? (
                    <div className="flex flex-wrap gap-2" role="group" aria-label={`Acciones de ${b.nombre}`}>
                        {puedeEditar ? (
                            <Boton variante="secundario" tamano="sm" onClick={alEditar}>
                                <Pencil />
                                Editar
                            </Boton>
                        ) : null}
                        {puedeEditar ? (
                            <Boton variante="secundario" tamano="sm" onClick={probar} disabled={probando}>
                                <Plug />
                                {probando ? "Probando… (hasta 20 s)" : "Probar conexión"}
                            </Boton>
                        ) : null}
                        {puedeCambiar ? (
                            <Boton variante={b.activo ? "peligro" : "contorno"} tamano="sm" onClick={b.activo ? () => setConfirmar(true) : cambiarEstado} disabled={cambiando}>
                                {b.activo ? <PowerOff /> : <Power />}
                                {cambiando ? (accion === "activar" ? "Activando…" : "Desactivando…") : accion === "activar" ? "Activar" : "Desactivar"}
                            </Boton>
                        ) : null}
                    </div>
                ) : null}
            </TarjetaContenido>
            <DialogoConfirmar
                open={confirmar}
                onOpenChange={setConfirmar}
                peligro
                titulo={`¿Desactivar ${b.correo}?`}
                descripcion="Iris deja de leer este buzón: los correos nuevos no crearán tickets hasta que lo actives otra vez."
                textoConfirmar="Desactivar"
                alConfirmar={async () => {
                    setConfirmar(false);
                    await cambiarEstado();
                }}
            />
        </Tarjeta>
    );
}

function ResultadoDePrueba({ prueba }: { prueba: ResultadoPrueba }) {
    const filas = [
        { nombre: "IMAP (entrada)", icono: Inbox, p: prueba.imap },
        ...(prueba.smtp ? [{ nombre: "SMTP (salida)", icono: Send, p: prueba.smtp }] : []),
    ];
    const todo = filas.every((f) => f.p.ok);
    return (
        <Aviso tono={todo ? "success" : "error"} titulo={todo ? "Conexión correcta" : "La prueba encontró problemas"} data-testid="resultado-prueba">
            <ul className="grid gap-1.5">
                {filas.map((f) => (
                    <li key={f.nombre} className="flex items-start gap-2" data-servidor={f.nombre} data-ok={f.p.ok}>
                        <InsigniaEstado tono={f.p.ok ? "success" : "error"} texto={f.p.ok ? "Bien" : "Falló"} className="mt-0.5" />
                        <span>
                            <strong className="font-medium">{f.nombre}:</strong> {textoPrueba(f.p)}
                        </span>
                    </li>
                ))}
            </ul>
            {prueba.smtp ? null : <p className="mt-1.5 text-muted-foreground">Este buzón no tiene SMTP: no se probó la salida.</p>}
        </Aviso>
    );
}
