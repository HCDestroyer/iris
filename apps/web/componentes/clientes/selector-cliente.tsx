"use client";

import { Aviso, Boton, Cargando, cn, Dialogo, DialogoContenido, Entrada, Etiqueta, InsigniaEstado } from "@iris/ui";
import { ArrowLeft, Building2, History, MapPin, Phone, PlugZap, RotateCw, Search, SearchX, UserRound, Wifi, X } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { escribirLocal, leerLocalJson } from "@/lib/almacen";
import {
    agregarReciente,
    claveRecientes,
    esIP,
    estadoServicio,
    moverIndice,
    nombresEmpresas,
    ordenarServicios,
    pasoTrasElegir,
    sonRecientes,
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
 * Selector de cliente único del panel: un campo que abre la búsqueda en Argos (en el teléfono, a
 * pantalla completa). Busca mientras se escribe por nombre, código, teléfono, NIT/CUI o dirección
 * (`GET /clientes?q=&servicios=true`) o por IP (`resolver-ip` + ficha); muestra servicios con su
 * estado y la empresa; recuerda los recientes; teclado completo. Si el cliente tiene varios
 * servicios, pide cuál.
 */
export function SelectorCliente({ valor, alCambiar, alConectorApagado, etiqueta = "Cliente", ayuda, error }: PropsSelector) {
    const id = useId();
    const [abierto, setAbierto] = useState(false);
    const idAyuda = `${id}-ayuda`;
    const idError = `${id}-error`;
    const describe = [ayuda ? idAyuda : null, error ? idError : null].filter(Boolean).join(" ") || undefined;

    return (
        <div className="grid gap-1.5" data-testid="selector-cliente">
            <Etiqueta htmlFor={id}>{etiqueta}</Etiqueta>
            {valor ? (
                <ClienteElegido seleccion={valor} alCambiar={() => setAbierto(true)} alQuitar={() => alCambiar(null)} idBoton={id} describe={describe} />
            ) : (
                <Boton
                    id={id}
                    variante="contorno"
                    className={cn("h-auto min-h-10 w-full justify-start py-2 text-left font-normal whitespace-normal text-muted-foreground", error && "border-error")}
                    aria-haspopup="dialog"
                    aria-invalid={error ? true : undefined}
                    aria-describedby={describe}
                    onClick={() => setAbierto(true)}
                >
                    <Search />
                    Buscar por nombre, código, teléfono, NIT, dirección o IP
                </Boton>
            )}
            {ayuda ? (
                <p id={idAyuda} className="text-sm text-muted-foreground">
                    {ayuda}
                </p>
            ) : null}
            {error ? (
                <p id={idError} className="text-sm text-error" role="alert">
                    {error}
                </p>
            ) : null}
            <Dialogo open={abierto} onOpenChange={setAbierto}>
                {abierto ? (
                    <DialogoContenido
                        titulo="Elegir cliente"
                        descripcion="Busca en Argos y elige el cliente; si tiene varios servicios, elige el servicio."
                        className="top-0 h-dvh max-h-dvh rounded-none sm:h-auto sm:max-h-[85dvh] sm:w-[min(40rem,calc(100vw-2rem))] sm:rounded-[var(--hx-radius-card)]"
                        data-testid="dialogo-cliente"
                        onOpenAutoFocus={(e) => {
                            // El foco va directo a la búsqueda (no a la X de cerrar).
                            const caja = e.target instanceof HTMLElement ? e.target.querySelector<HTMLInputElement>('[role="combobox"]') : null;
                            if (caja) {
                                e.preventDefault();
                                caja.focus();
                            }
                        }}
                    >
                        <Busqueda
                            alElegir={(s) => {
                                alCambiar(s);
                                setAbierto(false);
                            }}
                            {...(alConectorApagado ? { alConectorApagado } : {})}
                        />
                    </DialogoContenido>
                ) : null}
            </Dialogo>
        </div>
    );
}

function ClienteElegido({ seleccion, alCambiar, alQuitar, idBoton, describe }: { seleccion: SeleccionCliente; alCambiar: () => void; alQuitar: () => void; idBoton: string; describe: string | undefined }) {
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
            <Boton id={idBoton} variante="secundario" tamano="sm" className="justify-self-start" aria-describedby={describe} onClick={alCambiar}>
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

function Busqueda({ alElegir, alConectorApagado }: { alElegir: (s: SeleccionCliente) => void; alConectorApagado?: (apagado: boolean) => void }) {
    const { yo } = useSesion();
    const id = useId();
    const clave = claveRecientes(yo.usuario.id, yo.empresaActiva?.id);
    // Solo en el navegador: la búsqueda se monta al abrir el diálogo.
    const [recientes] = useState<ClienteResumen[]>(() => leerLocalJson(clave, sonRecientes, []));
    const [texto, setTexto] = useState("");
    const q = useDiferido(texto.trim(), 300);
    const [estado, setEstado] = useState<Estado | null>(null);
    const [intento, setIntento] = useState(0);
    const [activo, setActivo] = useState(-1);
    const [pendiente, setPendiente] = useState<{ cliente: ClienteResumen; servicioIP?: number | null } | null>(null);
    const apagadoRef = useRef(alConectorApagado);
    useEffect(() => {
        apagadoRef.current = alConectorApagado;
    }, [alConectorApagado]);

    useEffect(() => {
        if (q.length < 2) return undefined;
        const control = new AbortController();
        buscarClientes(q, control.signal).then(
            (r) => {
                if (control.signal.aborted) return;
                setEstado(r);
                setActivo(r.datos.length > 0 ? 0 : -1);
                apagadoRef.current?.(false);
            },
            (error: unknown) => {
                if (control.signal.aborted) return;
                setEstado({ q, tipo: "error", error });
                apagadoRef.current?.(error instanceof ErrorApi && error.codigo === "conector_apagado");
            },
        );
        return () => control.abort();
    }, [q, intento]);

    const terminar = useCallback(
        (cliente: ClienteResumen, servicio: ServicioCliente | null) => {
            const guardado = agregarReciente(recientes, { ...cliente, servicios: null });
            escribirLocal(clave, JSON.stringify(guardado));
            alElegir({ cliente, servicio });
        },
        [alElegir, clave, recientes],
    );

    const elegir = (cliente: ClienteResumen, servicioIP?: number | null) => {
        const servicios = cliente.servicios;
        const deIP = servicioIP ? servicios?.find((s) => s.id === servicioIP) : undefined;
        if (deIP) {
            terminar(cliente, deIP);
            return;
        }
        const paso = pasoTrasElegir(servicios);
        if (paso === "listo") {
            terminar(cliente, servicios?.[0] ?? null);
            return;
        }
        setPendiente({ cliente, ...(servicioIP !== undefined ? { servicioIP } : {}) });
    };

    if (pendiente) {
        return <ElegirServicio cliente={pendiente.cliente} alVolver={() => setPendiente(null)} alElegir={(s) => terminar(pendiente.cliente, s)} />;
    }

    const vigente = estado && estado.q === q && q.length >= 2 ? estado : null;
    const buscando = q.length >= 2 && !vigente;
    const escribiendo = texto.trim() !== q;
    const lista: ClienteResumen[] = q.length < 2 ? recientes : vigente?.tipo === "listo" ? vigente.datos : [];
    const servicioIP = vigente?.tipo === "listo" ? vigente.servicioIP : undefined;
    const idLista = `${id}-lista`;
    const idOpcion = (i: number) => `${id}-op-${i}`;

    const teclado = (e: KeyboardEvent<HTMLInputElement>) => {
        const siguiente = moverIndice(activo, lista.length, e.key);
        if (siguiente !== null) {
            e.preventDefault();
            setActivo(siguiente);
            document.getElementById(idOpcion(siguiente))?.scrollIntoView({ block: "nearest" });
            return;
        }
        if (e.key === "Enter") {
            e.preventDefault();
            const c = lista[activo];
            if (c) elegir(c, q.length < 2 ? undefined : servicioIP);
        }
    };

    return (
        <div className="grid min-h-0 flex-1 content-start gap-3">
            <div className="relative">
                <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                <Entrada
                    type="search"
                    className="pl-9"
                    role="combobox"
                    aria-label="Buscar cliente"
                    aria-expanded={lista.length > 0}
                    aria-controls={idLista}
                    aria-autocomplete="list"
                    aria-activedescendant={activo >= 0 && lista[activo] ? idOpcion(activo) : undefined}
                    placeholder="Nombre, código, teléfono, NIT, dirección o IP"
                    maxLength={100}
                    autoComplete="off"
                    value={texto}
                    onChange={(e) => {
                        setTexto(e.target.value);
                        setActivo(-1);
                    }}
                    onKeyDown={teclado}
                />
            </div>
            <p className="text-xs text-muted-foreground" aria-hidden>
                ↑ ↓ para moverte · Enter para elegir · Esc para cerrar
            </p>
            {q.length < 2 ? (
                recientes.length === 0 ? (
                    <Vacio icono={Search} titulo="Escribe al menos 2 caracteres" texto="Busca por nombre, código, teléfono, NIT/CUI, dirección o la IP del servicio." />
                ) : (
                    <span className="flex items-center gap-1.5 text-sm font-medium text-muted-foreground">
                        <History aria-hidden className="size-4" />
                        Recientes
                    </span>
                )
            ) : null}
            {buscando || (escribiendo && q.length >= 2) ? <Cargando texto="Buscando clientes…" visible /> : null}
            {vigente?.tipo === "error" ? <ErrorBusqueda error={vigente.error} reintentar={() => setIntento((n) => n + 1)} /> : null}
            {vigente?.tipo === "listo" && vigente.stale ? (
                <InsigniaEstado tono="warning" icono={History} texto="Desactualizado: Argos no respondió, resultados de la copia local" className="justify-self-start whitespace-normal" />
            ) : null}
            {vigente?.tipo === "listo" && vigente.datos.length === 0 ? (
                <Vacio
                    icono={SearchX}
                    titulo={`Ningún cliente coincide con «${q}»`}
                    texto={esIP(q) ? "Ningún servicio de Argos tiene esa IP en esta empresa." : "Revisa la ortografía o prueba con el código, el teléfono o el NIT."}
                />
            ) : null}
            {lista.length > 0 ? (
                <ul id={idLista} role="listbox" aria-label={q.length < 2 ? "Clientes recientes" : "Clientes encontrados"} className="grid gap-2">
                    {lista.map((c, i) => (
                        <OpcionCliente
                            key={c.id}
                            id={idOpcion(i)}
                            cliente={c}
                            activa={i === activo}
                            alEnfocar={() => setActivo(i)}
                            alElegir={() => elegir(c, q.length < 2 ? undefined : servicioIP)}
                        />
                    ))}
                </ul>
            ) : null}
        </div>
    );
}

function OpcionCliente({ id, cliente, activa, alEnfocar, alElegir }: { id: string; cliente: ClienteResumen; activa: boolean; alEnfocar: () => void; alElegir: () => void }) {
    const { empresas } = useSesion();
    const empresa = nombresEmpresas(cliente, empresas);
    const servicios = cliente.servicios ? ordenarServicios(cliente.servicios) : null;
    return (
        <li
            id={id}
            role="option"
            aria-selected={activa}
            data-cliente={cliente.codigo}
            onMouseMove={alEnfocar}
            onClick={alElegir}
            className={cn(
                "grid cursor-pointer gap-1.5 rounded-md border p-3 text-sm transition-colors",
                activa ? "border-primary bg-primary/10" : "bg-background hover:border-primary/60",
            )}
        >
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
            {servicios === null ? null : servicios.length === 0 ? (
                <span className="text-xs text-muted-foreground">Sin servicios en esta empresa</span>
            ) : (
                <span className="flex flex-wrap gap-1.5" aria-label="Servicios">
                    {servicios.slice(0, 3).map((s) => (
                        <InsigniaEstado key={s.id} {...estadoServicio(s.estado)} texto={`${s.planNombre || s.codigo} · ${estadoServicio(s.estado).texto}`} />
                    ))}
                    {servicios.length > 3 ? <span className="text-xs text-muted-foreground">+{servicios.length - 3} más</span> : null}
                </span>
            )}
        </li>
    );
}

/** Paso 2: el cliente tiene varios servicios (o hay que leerlos de la ficha). */
function ElegirServicio({ cliente, alVolver, alElegir }: { cliente: ClienteResumen; alVolver: () => void; alElegir: (s: ServicioCliente | null) => void }) {
    const id = useId();
    const [lectura, setLectura] = useState<{ tipo: "cargando" } | { tipo: "listo"; servicios: ServicioCliente[] } | { tipo: "error"; error: unknown }>(() =>
        cliente.servicios ? { tipo: "listo", servicios: ordenarServicios(cliente.servicios) } : { tipo: "cargando" },
    );
    const [intento, setIntento] = useState(0);
    const [activo, setActivo] = useState(0);
    const conocidos = cliente.servicios;

    const lista = useRef<HTMLUListElement>(null);
    const hayLista = lectura.tipo === "listo";
    useEffect(() => {
        // El foco entra en la lista para elegir con el teclado.
        if (hayLista) lista.current?.focus();
    }, [hayLista]);

    useEffect(() => {
        if (conocidos) return undefined;
        const control = new AbortController();
        resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: cliente.id } }, signal: control.signal })).then(
            (f) => {
                if (control.signal.aborted) return;
                const servicios = ordenarServicios(f.servicios ?? []);
                if (servicios.length <= 1) {
                    alElegir(servicios[0] ?? null);
                    return;
                }
                setLectura({ tipo: "listo", servicios });
            },
            (error: unknown) => !control.signal.aborted && setLectura({ tipo: "error", error }),
        );
        return () => control.abort();
        // alElegir cambia en cada render del padre; leer de nuevo solo con el cliente o «Reintentar».
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [cliente.id, conocidos, intento]);

    const servicios = lectura.tipo === "listo" ? lectura.servicios : [];
    const idOpcion = (i: number) => `${id}-srv-${i}`;

    return (
        <div className="grid content-start gap-3" data-testid="elegir-servicio">
            <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={alVolver}>
                <ArrowLeft />
                Volver a los resultados
            </Boton>
            <div className="grid gap-0.5">
                <span className="font-medium">{cliente.nombre}</span>
                <span className="text-sm text-muted-foreground">Tiene varios servicios: elige a cuál se refiere.</span>
            </div>
            {lectura.tipo === "cargando" ? <Cargando texto="Leyendo los servicios…" visible /> : null}
            {lectura.tipo === "error" ? <ErrorBusqueda
                    error={lectura.error}
                    reintentar={() => {
                        setLectura({ tipo: "cargando" });
                        setIntento((n) => n + 1);
                    }}
                    que="los servicios del cliente"
                /> : null}
            {servicios.length > 0 ? (
                <ul
                    role="listbox"
                    aria-label="Servicios del cliente"
                    tabIndex={0}
                    aria-activedescendant={idOpcion(activo)}
                    ref={lista}
                    onKeyDown={(e) => {
                        const siguiente = moverIndice(activo, servicios.length, e.key);
                        if (siguiente !== null) {
                            e.preventDefault();
                            setActivo(siguiente);
                        } else if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            const s = servicios[activo];
                            if (s) alElegir(s);
                        }
                    }}
                    className="grid gap-2 rounded-md focus-visible:outline-2"
                >
                    {servicios.map((s, i) => (
                        <li
                            key={s.id}
                            id={idOpcion(i)}
                            role="option"
                            aria-selected={i === activo}
                            data-servicio={s.codigo}
                            onMouseMove={() => setActivo(i)}
                            onClick={() => alElegir(s)}
                            className={cn(
                                "grid cursor-pointer gap-1 rounded-md border p-3 text-sm",
                                i === activo ? "border-primary bg-primary/10" : "bg-background hover:border-primary/60",
                            )}
                        >
                            <span className="flex flex-wrap items-center justify-between gap-2">
                                <span className="font-medium">{s.planNombre || "Sin plan"}</span>
                                <InsigniaEstado {...estadoServicio(s.estado)} />
                            </span>
                            <span className="text-xs break-words text-muted-foreground">
                                <span className="font-mono">{s.codigo}</span>
                                {s.ip ? ` · IP ${s.ip}` : null}
                                {s.direccion ? ` · ${s.direccion}` : null}
                                {s.zonaNombre ? ` · ${s.zonaNombre}` : null}
                            </span>
                        </li>
                    ))}
                </ul>
            ) : null}
            {lectura.tipo !== "cargando" ? (
                <Boton variante="contorno" className="justify-self-start" onClick={() => alElegir(null)}>
                    Continuar sin servicio específico
                </Boton>
            ) : null}
        </div>
    );
}

function Vacio({ icono: Icono, titulo, texto }: { icono: typeof Search; titulo: string; texto: string }) {
    return (
        <div className="grid justify-items-center gap-1 rounded-md border border-dashed p-6 text-center" role="status">
            <Icono aria-hidden className="size-6 text-muted-foreground" />
            <p className="font-medium">{titulo}</p>
            <p className="text-sm text-muted-foreground">{texto}</p>
        </div>
    );
}

function ErrorBusqueda({ error, reintentar, que = "los clientes" }: { error: unknown; reintentar: () => void; que?: string }) {
    if (error instanceof ErrorApi && error.codigo === "conector_apagado") {
        return (
            <Aviso tono="neutral" titulo="Conector con Argos apagado" icono={PlugZap}>
                No se puede buscar en Argos. Cierra esta búsqueda y captura los datos de contacto a mano.
            </Aviso>
        );
    }
    const m = mensajeDeError(error);
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
