"use client";

import { Autocompletar, Aviso, Boton, Cargando, InsigniaEstado, ListaOpciones } from "@iris/ui";
import { ArrowLeft, Building2, History, MapPin, Phone, PlugZap, RotateCw, Search, UserRound, Wifi, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { escribirLocal, leerLocalJson } from "@/lib/almacen";
import { errorDeCliente } from "@/lib/clientes/errores";
import {
    agregarReciente,
    claveRecientes,
    esIP,
    estadoServicio,
    nombresEmpresas,
    MIN_BUSQUEDA,
    ordenarServicios,
    soloMinimo,
    sonRecientes,
    type ClienteReciente,
    type ClienteResumen,
    type SeleccionCliente,
    type ServicioCliente,
} from "@/lib/clientes/selector";
import { useDiferido } from "@/lib/diferido";
import { useSesion } from "@/lib/sesion";

export type { ClienteResumen, SeleccionCliente, ServicioCliente } from "@/lib/clientes/selector";

type PropsSelector = {
    valor: SeleccionCliente | null;
    alCambiar: (valor: SeleccionCliente | null) => void;
    /** El conector con Argos está apagado (503 `conector_apagado`): la pantalla pide el contacto a mano. */
    alConectorApagado?: (apagado: boolean) => void;
    etiqueta?: string;
    ayuda?: ReactNode;
    /** Mensaje junto al campo (falta elegirlo o error del servidor). */
    error?: string | undefined;
};

/**
 * Selector de cliente único del panel, sobre `Autocompletar` (HeroUI `ComboBox`): busca en Argos
 * mientras se escribe por nombre, código, teléfono, NIT/CUI o dirección
 * (`GET /clientes?q=&servicios=true`) o por IP (`resolver-ip` + ficha); cada opción muestra
 * dirección, teléfono, empresa y servicios con su estado. Con el campo vacío ofrece los recientes.
 * Al elegir, confirma contra la ficha y, si el cliente tiene varios servicios, pide cuál
 * (`ListaOpciones`, HeroUI `ListBox`). Los errores van bajo el campo con «Reintentar».
 */
export function SelectorCliente({ valor, alCambiar, alConectorApagado, etiqueta = "Cliente", ayuda, error }: PropsSelector) {
    const [cambiando, setCambiando] = useState(false);
    if (valor && !cambiando) {
        return (
            <div className="grid gap-1.5" data-testid="selector-cliente">
                <span className="text-sm font-medium">{etiqueta}</span>
                <ClienteElegido seleccion={valor} alCambiar={() => setCambiando(true)} alQuitar={() => alCambiar(null)} />
                {error ? (
                    <p className="text-sm text-error" role="alert">
                        {error}
                    </p>
                ) : null}
            </div>
        );
    }
    return (
        <div className="grid gap-1.5" data-testid="selector-cliente">
            <Busqueda
                etiqueta={etiqueta}
                ayuda={ayuda}
                error={error}
                enfocar={cambiando}
                alElegir={(s) => {
                    alCambiar(s);
                    setCambiando(false);
                }}
                {...(valor ? { alCancelar: () => setCambiando(false) } : {})}
                {...(alConectorApagado ? { alConectorApagado } : {})}
            />
        </div>
    );
}

function ClienteElegido({ seleccion, alCambiar, alQuitar }: { seleccion: SeleccionCliente; alCambiar: () => void; alQuitar: () => void }) {
    const { cliente, servicio } = seleccion;
    return (
        <div className="grid gap-2 rounded-md border bg-elevated p-3" data-testid="cliente-elegido">
            <div className="flex items-start gap-3">
                <UserRound aria-hidden className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                <span className="grid min-w-0 flex-1">
                    <span className="font-medium break-words">{cliente.nombre}</span>
                    <span className="text-sm break-words text-muted-foreground">
                        <span className="font-mono">{cliente.codigo}</span> · {cliente.direccion}
                    </span>
                </span>
                <Boton variante="fantasma" tamano="icono-sm" aria-label="Quitar cliente" title="Quitar cliente" onClick={alQuitar}>
                    <X />
                </Boton>
            </div>
            {servicio ? (
                <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="servicio-elegido">
                    <Wifi aria-hidden className="size-4 text-muted-foreground" />
                    <span>
                        Servicio <span className="font-mono">{servicio.codigo}</span> · {servicio.planNombre || "Sin plan"}
                    </span>
                    <InsigniaEstado {...estadoServicio(servicio.estado)} />
                </div>
            ) : null}
            <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={alCambiar}>
                <Search />
                Cambiar cliente
            </Boton>
        </div>
    );
}

type Estado =
    | { q: string; tipo: "listo"; datos: ClienteResumen[]; stale: boolean; servicioIP?: number | null }
    | { q: string; tipo: "error"; error: unknown };

/** Lee los clientes del texto (o de la IP). Devuelve resultados con sus servicios. */
async function buscarClientes(q: string, signal: AbortSignal): Promise<Extract<Estado, { tipo: "listo" }>> {
    if (esIP(q)) {
        try {
            const r = await resolver(api().GET("/api/v1/clientes/resolver-ip", { params: { query: { ip: q.trim() } }, signal }));
            if (r.clienteId === null) return { q, tipo: "listo", datos: [], stale: r.stale };
            const ficha = await resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: r.clienteId } }, signal }));
            return { q, tipo: "listo", datos: [{ ...ficha.cliente, servicios: ficha.servicios ?? [] }], stale: r.stale || ficha.stale, servicioIP: r.servicioId };
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 404) return { q, tipo: "listo", datos: [], stale: false };
            throw e;
        }
    }
    const r = await resolver(api().GET("/api/v1/clientes", { params: { query: { q: q.slice(0, 100), limit: 8, servicios: true } }, signal }));
    return { q, tipo: "listo", datos: r.data ?? [], stale: r.stale };
}

function Busqueda({
    etiqueta,
    ayuda,
    error,
    enfocar,
    alElegir,
    alCancelar,
    alConectorApagado,
}: {
    etiqueta: string;
    ayuda: ReactNode;
    error: string | undefined;
    enfocar: boolean;
    alElegir: (s: SeleccionCliente) => void;
    alCancelar?: () => void;
    alConectorApagado?: (apagado: boolean) => void;
}) {
    const { yo } = useSesion();
    const clave = claveRecientes(yo.usuario.id, yo.empresaActiva?.id);
    // Solo en el navegador: los recientes se leen al montar la búsqueda (componente de cliente).
    const [recientes] = useState<ClienteReciente[]>(() => soloMinimo(leerLocalJson(clave, sonRecientes, [])));
    const [texto, setTexto] = useState("");
    const q = useDiferido(texto.trim(), 300);
    const [estado, setEstado] = useState<Estado | null>(null);
    const [intento, setIntento] = useState(0);
    const [pendiente, setPendiente] = useState<{ elegido: ClienteReciente; servicioIP: number | null | undefined } | null>(null);
    const apagadoRef = useRef(alConectorApagado);
    useEffect(() => {
        apagadoRef.current = alConectorApagado;
    }, [alConectorApagado]);

    useEffect(() => {
        if (q.length < MIN_BUSQUEDA) return undefined;
        const control = new AbortController();
        buscarClientes(q, control.signal).then(
            (r) => {
                if (control.signal.aborted) return;
                setEstado(r);
                apagadoRef.current?.(false);
            },
            (falla: unknown) => {
                if (control.signal.aborted) return;
                setEstado({ q, tipo: "error", error: falla });
                apagadoRef.current?.(falla instanceof ErrorApi && falla.codigo === "conector_apagado");
            },
        );
        return () => control.abort();
    }, [q, intento]);

    const terminar = useCallback(
        (cliente: ClienteResumen, servicio: ServicioCliente | null) => {
            // En el navegador solo id, código y nombre.
            escribirLocal(clave, JSON.stringify(agregarReciente(recientes, cliente)));
            alElegir({ cliente, servicio });
        },
        [alElegir, clave, recientes],
    );

    if (pendiente) {
        return <ConfirmarCliente elegido={pendiente.elegido} servicioIP={pendiente.servicioIP} alVolver={() => setPendiente(null)} alTerminar={terminar} />;
    }

    const corta = q.length < MIN_BUSQUEDA;
    const vigente = estado && estado.q === q && !corta ? estado : null;
    const escribiendo = texto.trim() !== q;
    const buscando = (!corta && !vigente) || (escribiendo && texto.trim().length >= MIN_BUSQUEDA);
    const lista: (ClienteResumen | ClienteReciente)[] = corta ? recientes : vigente?.tipo === "listo" ? vigente.datos : [];
    const servicioIP = vigente?.tipo === "listo" ? vigente.servicioIP : undefined;

    // Elegir siempre confirma contra la ficha: cliente y servicio salen de ahí.
    const elegir = (idCliente: string | null) => {
        const c = lista.find((x) => String(x.id) === idCliente);
        if (c) setPendiente({ elegido: { id: c.id, codigo: c.codigo, nombre: c.nombre }, servicioIP: corta ? undefined : servicioIP });
    };

    const sinResultados = corta ? (
        `Escribe al menos ${MIN_BUSQUEDA} caracteres: nombre, código, teléfono, NIT/CUI, dirección o la IP del servicio.`
    ) : vigente?.tipo === "error" ? (
        vigente.error instanceof ErrorApi && vigente.error.codigo === "conector_apagado" ? (
            "Conector con Argos apagado: captura los datos de contacto a mano."
        ) : (
            "No se pudo buscar: el aviso con «Reintentar» está bajo el campo."
        )
    ) : (
        <span className="grid gap-0.5">
            <span className="font-medium text-foreground">{`Ningún cliente coincide con «${q}»`}</span>
            <span>{esIP(q) ? "Ningún servicio de Argos tiene esa IP en esta empresa." : "Revisa la ortografía o prueba con el código, el teléfono o el NIT."}</span>
        </span>
    );

    return (
        <>
            <Autocompletar
                etiqueta={etiqueta}
                data-testid="buscar-cliente"
                conLupa
                placeholder="Nombre, código, teléfono, NIT, dirección o IP"
                maxLength={100}
                autoFocus={enfocar}
                texto={texto}
                alBuscar={setTexto}
                valor={null}
                alCambiar={elegir}
                cargando={buscando}
                textoCargando="Buscando clientes…"
                sinResultados={sinResultados}
                etiquetaLista={corta ? "Clientes recientes" : "Clientes encontrados"}
                cabeceraLista={
                    corta && recientes.length > 0 ? (
                        <>
                            <History aria-hidden className="size-3.5" />
                            Recientes
                        </>
                    ) : undefined
                }
                opciones={lista.map((c) => ({ valor: String(c.id), etiqueta: c.nombre, contenido: <OpcionCliente cliente={c} /> }))}
                ayuda={ayuda}
                error={error}
            />
            {vigente?.tipo === "error" ? <ErrorBusqueda error={vigente.error} reintentar={() => setIntento((n) => n + 1)} /> : null}
            {vigente?.tipo === "listo" && vigente.stale ? (
                <InsigniaEstado tono="warning" icono={History} texto="Desactualizado: Argos no respondió, resultados de la copia local" className="justify-self-start whitespace-normal" />
            ) : null}
            {alCancelar ? (
                <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={alCancelar}>
                    <ArrowLeft />
                    Dejar el cliente elegido
                </Boton>
            ) : null}
        </>
    );
}

function OpcionCliente({ cliente: opcion }: { cliente: ClienteResumen | ClienteReciente }) {
    const { empresas } = useSesion();
    // Un reciente solo trae id, código y nombre (lo demás no se guarda en el navegador).
    const cliente: Partial<ClienteResumen> & ClienteReciente = opcion;
    const empresa = "direccion" in opcion ? nombresEmpresas(opcion, empresas) : null;
    const servicios = cliente.servicios ? ordenarServicios(cliente.servicios) : null;
    return (
        <span className="grid min-w-0 flex-1 gap-1 py-1 text-sm" data-cliente={cliente.codigo}>
            <span className="flex flex-wrap items-baseline justify-between gap-x-3">
                <span className="font-medium break-words">{cliente.nombre}</span>
                <span className="font-mono text-xs text-muted-foreground">{cliente.codigo}</span>
            </span>
            <span className="grid gap-0.5 text-muted-foreground">
                {cliente.direccion ? (
                    <span className="flex items-start gap-1.5">
                        <MapPin aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                        <span className="min-w-0 break-words">{cliente.direccion}</span>
                    </span>
                ) : null}
                <span className="flex flex-wrap gap-x-4 gap-y-0.5">
                    {cliente.telefono ? (
                        <span className="inline-flex items-center gap-1.5">
                            <Phone aria-hidden className="size-3.5 shrink-0" />
                            {cliente.telefono}
                        </span>
                    ) : null}
                    {empresa ? (
                        <span className="inline-flex items-center gap-1.5">
                            <Building2 aria-hidden className="size-3.5 shrink-0" />
                            {empresa}
                        </span>
                    ) : null}
                </span>
            </span>
            {"direccion" in opcion ? null : <span className="text-xs text-muted-foreground">Reciente: al elegirlo se lee su ficha</span>}
            {servicios === null ? null : servicios.length === 0 ? (
                <span className="text-xs text-muted-foreground">Sin servicios en esta empresa</span>
            ) : (
                <span className="flex flex-wrap gap-1.5">
                    {servicios.slice(0, 3).map((s) => (
                        <InsigniaEstado key={s.id} {...estadoServicio(s.estado)} texto={`${s.planNombre || s.codigo} · ${estadoServicio(s.estado).texto}`} />
                    ))}
                    {servicios.length > 3 ? <span className="text-xs text-muted-foreground">+{servicios.length - 3} más</span> : null}
                </span>
            )}
        </span>
    );
}

/**
 * Paso 2: confirmar el cliente contra su ficha (`GET /clientes/{id}`, que guarda la instantánea).
 * El cliente y el servicio que se entregan salen de la ficha, nunca de la búsqueda en vivo ni de
 * los recientes. Con un solo servicio (o el de la IP buscada) termina solo; con varios, pregunta.
 */
function ConfirmarCliente({
    elegido,
    servicioIP,
    alVolver,
    alTerminar,
}: {
    elegido: ClienteReciente;
    servicioIP: number | null | undefined;
    alVolver: () => void;
    alTerminar: (cliente: ClienteResumen, servicio: ServicioCliente | null) => void;
}) {
    const [lectura, setLectura] = useState<{ tipo: "cargando" } | { tipo: "listo"; cliente: ClienteResumen; servicios: ServicioCliente[] } | { tipo: "error"; error: unknown }>({ tipo: "cargando" });
    const [intento, setIntento] = useState(0);
    const terminarRef = useRef(alTerminar);
    useEffect(() => {
        terminarRef.current = alTerminar;
    }, [alTerminar]);

    useEffect(() => {
        const control = new AbortController();
        resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: elegido.id } }, signal: control.signal })).then(
            (f) => {
                if (control.signal.aborted) return;
                const servicios = ordenarServicios(f.servicios ?? []);
                const deIP = servicioIP ? servicios.find((x) => x.id === servicioIP) : undefined;
                if (deIP || servicios.length <= 1) {
                    terminarRef.current(f.cliente, deIP ?? servicios[0] ?? null);
                    return;
                }
                setLectura({ tipo: "listo", cliente: f.cliente, servicios });
            },
            (falla: unknown) => !control.signal.aborted && setLectura({ tipo: "error", error: falla }),
        );
        return () => control.abort();
    }, [elegido.id, servicioIP, intento]);

    const elegir = (s: ServicioCliente | null) => {
        if (lectura.tipo === "listo") alTerminar(lectura.cliente, s);
    };

    return (
        <div className="grid content-start gap-3 rounded-md border bg-elevated p-3" data-testid="elegir-servicio">
            <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={alVolver}>
                <ArrowLeft />
                Volver a buscar
            </Boton>
            <div className="grid gap-0.5">
                <span className="font-medium">{lectura.tipo === "listo" ? lectura.cliente.nombre : elegido.nombre}</span>
                {lectura.tipo === "listo" ? <span className="text-sm text-muted-foreground">Tiene varios servicios: elige a cuál se refiere.</span> : null}
            </div>
            {lectura.tipo === "cargando" ? <Cargando texto="Leyendo la ficha del cliente…" visible /> : null}
            {lectura.tipo === "error" ? (
                <ErrorBusqueda
                    error={lectura.error}
                    reintentar={() => {
                        setLectura({ tipo: "cargando" });
                        setIntento((n) => n + 1);
                    }}
                    que="la ficha del cliente"
                />
            ) : null}
            {lectura.tipo === "listo" ? (
                <>
                    <ListaOpciones
                        etiqueta="Servicios del cliente"
                        autoFocus
                        alElegir={(id) => {
                            const x = lectura.servicios.find((s) => String(s.id) === id);
                            if (x) elegir(x);
                        }}
                        opciones={lectura.servicios.map((x) => ({
                            valor: String(x.id),
                            texto: `${x.planNombre || "Sin plan"} ${x.codigo}`,
                            contenido: (
                                <span className="grid gap-1" data-servicio={x.codigo}>
                                    <span className="flex flex-wrap items-center justify-between gap-2">
                                        <span className="font-medium">{x.planNombre || "Sin plan"}</span>
                                        <InsigniaEstado {...estadoServicio(x.estado)} />
                                    </span>
                                    <span className="text-xs break-words text-muted-foreground">
                                        <span className="font-mono">{x.codigo}</span>
                                        {x.ip ? ` · IP ${x.ip}` : null}
                                        {x.direccion ? ` · ${x.direccion}` : null}
                                        {x.zonaNombre ? ` · ${x.zonaNombre}` : null}
                                    </span>
                                </span>
                            ),
                        }))}
                    />
                    <Boton variante="contorno" className="justify-self-start" onClick={() => elegir(null)}>
                        Continuar sin servicio específico
                    </Boton>
                </>
            ) : null}
        </div>
    );
}

function ErrorBusqueda({ error, reintentar, que = "los clientes" }: { error: unknown; reintentar: () => void; que?: string }) {
    if (error instanceof ErrorApi && error.codigo === "conector_apagado") {
        return (
            <Aviso tono="neutral" titulo="Conector con Argos apagado" icono={PlugZap}>
                No se puede buscar en Argos. Captura los datos de contacto a mano.
            </Aviso>
        );
    }
    const deCliente = errorDeCliente(error);
    const m = deCliente ? { titulo: deCliente.titulo, detalle: deCliente.detalle } : mensajeDeError(error);
    return (
        <Aviso
            tono="error"
            titulo={`No se pudieron leer ${que}`}
            accion={
                <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {[m.titulo, m.detalle].filter(Boolean).join(". ")}
        </Aviso>
    );
}
