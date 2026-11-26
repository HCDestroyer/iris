"use client";

import { avisar, Aviso, Boton, CampoBusqueda, CampoSelector, cn, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie, Esqueleto, EstadoVacio, FilaLista, GrupoSegmentado, Interruptor, ListaFilas, MenuAcciones, Tarjeta } from "@iris/ui";
import { Check, CircleCheck, MessageSquare, Pencil, Plus, RefreshCw, Settings2, Sparkles, SquareArrowOutUpRight, Trash2, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { useAhora } from "@/componentes/tickets/insignias";
import { ErrorApi } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import * as P from "@/lib/plantillas-whatsapp/api";
import {
    CATEGORIAS,
    calidadVisible,
    cuandoCorto,
    estadoVisible,
    IDIOMAS,
    PERMISO_CREAR,
    PERMISO_EDITAR,
    PERMISO_SINCRONIZAR,
    RUTA,
    textoCategoria,
    textoIdioma,
    textoUso,
    type CuentaPWA,
    type PlantillaWA,
    type ResumenSincronizacion,
} from "@/lib/plantillas-whatsapp/tipos";
import { useSesion } from "@/lib/sesion";
import { EstadoPlantilla, ErrorPlantilla } from "./comunes";

type Grupo = "todas" | "activas" | "revision" | "atender";

/** Pantalla a de la maqueta: cuenta, sincronización, filtros y la lista. */
export function ListaPlantillasWA() {
    const params = useSearchParams();
    const cuentas = useLectura((signal) => P.leerCuentas(signal));
    const [elegida, setWaba] = useState<string | null>(params.get("cuenta"));
    // La elegida si sigue entre las cuentas; si no, la primera.
    const waba = cuentas.estado === "listo" ? (cuentas.datos.some((c) => c.wabaId === elegida) ? elegida : (cuentas.datos[0]?.wabaId ?? null)) : null;
    const { puede } = useSesion();

    const acciones = (
        <div className="flex flex-wrap gap-2">
            {puede([PERMISO_EDITAR]) ? <AjustesMarketing /> : null}
            {waba ? (
                <Boton asChild variante="contorno">
                    <Link href={`${RUTA}/sugeridas?cuenta=${encodeURIComponent(waba)}`}>
                        <Sparkles />
                        Sugeridas
                    </Link>
                </Boton>
            ) : null}
            {waba && puede([PERMISO_CREAR]) ? (
                <Boton asChild>
                    <Link href={`${RUTA}/nueva?cuenta=${encodeURIComponent(waba)}`}>
                        <Plus />
                        Nueva plantilla
                    </Link>
                </Boton>
            ) : null}
        </div>
    );

    return (
        <>
            <EncabezadoPagina titulo="Plantillas de WhatsApp" descripcion="Los mensajes que Meta aprueba para escribirle primero a un cliente: avisos, recordatorios y visitas." acciones={acciones} />
            {cuentas.estado === "cargando" ? (
                <div className="grid gap-3" aria-busy="true">
                    <Esqueleto className="h-20" />
                    <Esqueleto className="h-64" />
                </div>
            ) : cuentas.estado === "error" ? (
                <ErrorLectura error={cuentas.error} reintentar={cuentas.reintentar} que="las cuentas de WhatsApp" />
            ) : cuentas.datos.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio
                        icono={MessageSquare}
                        titulo="Aún no hay una cuenta de WhatsApp"
                        descripcion="Las plantillas viven en la cuenta de WhatsApp de tus números. Conecta un número con su cuenta (WABA ID) y vuelve aquí."
                    />
                    <div className="flex justify-center pb-4">
                        <Boton asChild variante="contorno">
                            <Link href="/administracion/numeros">Ir a Números de WhatsApp</Link>
                        </Boton>
                    </div>
                </Tarjeta>
            ) : waba ? (
                <Cuenta key={waba} cuentas={cuentas.datos} waba={waba} alCambiarCuenta={setWaba} alSincronizar={() => void cuentas.refrescar({ silencioso: true })} puedeSincronizar={puede([PERMISO_SINCRONIZAR])} />
            ) : null}
        </>
    );
}

function Cuenta({ cuentas, waba, alCambiarCuenta, alSincronizar, puedeSincronizar }: { cuentas: CuentaPWA[]; waba: string; alCambiarCuenta: (w: string) => void; alSincronizar: () => void; puedeSincronizar: boolean }) {
    const cuenta = cuentas.find((c) => c.wabaId === waba) ?? cuentas[0];
    const clave = useClaveIdempotencia();
    const ahora = useAhora(60_000);
    const [sincronizando, setSincronizando] = useState(false);
    const [resumen, setResumen] = useState<ResumenSincronizacion | null>(null);
    const [errorSync, setErrorSync] = useState<unknown>(null);
    const [sigue, setSigue] = useState(false);
    // Sube tras sincronizar: la lista se vuelve a leer desde el principio.
    const [lectura, setLectura] = useState(0);
    const [grupo, setGrupo] = useState<Grupo>("todas");
    const [buscar, setBuscar] = useState("");
    const [buscarDiferido, setBuscarDiferido] = useState("");
    const [categoria, setCategoria] = useState("");
    const [idioma, setIdioma] = useState("");

    useEffect(() => {
        const t = setTimeout(() => setBuscarDiferido(buscar.trim()), 250);
        return () => clearTimeout(t);
    }, [buscar]);
    const filtros = useMemo(
        (): P.FiltrosLista => ({ wabaId: waba, ...(grupo !== "todas" ? { grupo } : {}), ...(categoria ? { categoria: categoria as PlantillaWA["categoria"] } : {}), ...(idioma ? { idioma } : {}), ...(buscarDiferido ? { buscar: buscarDiferido } : {}) }),
        [waba, grupo, categoria, idioma, buscarDiferido],
    );

    // El botón encola la relectura (202) y se vuelve a leer la cuenta hasta ver el resultado.
    const montado = useRef<AbortController | null>(null);
    useEffect(() => {
        const c = new AbortController();
        montado.current = c;
        return () => c.abort();
    }, []);
    async function sincronizar() {
        const senal = montado.current?.signal;
        setSincronizando(true);
        setErrorSync(null);
        setResumen(null);
        setSigue(false);
        try {
            const pedida = await P.sincronizar(waba, clave.actual());
            clave.renovar();
            const r = await P.esperarSincronizacion(waba, { sincronizadaEn: pedida.sincronizadaEn, errorEn: cuenta?.error?.en ?? null }, senal ? { signal: senal } : {});
            alSincronizar();
            if (r.fin === "lista") {
                setResumen(r.cuenta.resumen);
                avisar({ titulo: "Listo: Iris quedó igual que Meta" });
                setLectura((n) => n + 1);
            } else if (r.fin === "error" && r.cuenta.error) {
                setErrorSync(new ErrorApi({ estado: 0, codigo: r.cuenta.error.codigo, titulo: "No se pudo hablar con Meta" }));
            } else {
                setSigue(true);
            }
        } catch (e) {
            if (senal?.aborted) return;
            clave.trasError(e);
            setErrorSync(e);
        } finally {
            if (!senal?.aborted) setSincronizando(false);
        }
    }

    const errorCuenta = cuenta?.error ? new ErrorApi({ estado: 0, codigo: cuenta.error.codigo, titulo: "No se pudo hablar con Meta" }) : null;
    const numeros = cuenta?.numeros ?? [];
    const sinFiltros = grupo === "todas" && !buscarDiferido && !categoria && !idioma;

    return (
        <div className="grid min-w-0 gap-4">
            <Tarjeta className="flex flex-wrap items-center gap-3 p-3.5" data-slot="cuenta">
                <div className="flex min-w-0 flex-1 basis-56 items-center gap-2.5">
                    <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                        <MessageSquare aria-hidden className="size-4" />
                    </span>
                    <div className="grid min-w-0 gap-0.5">
                        {cuentas.length > 1 ? (
                            <CampoSelector etiqueta="Cuenta de WhatsApp" tamano="sm" value={waba} onChange={(e) => alCambiarCuenta(e.target.value)}>
                                {cuentas.map((c) => (
                                    <option key={c.wabaId} value={c.wabaId}>
                                        {c.nombres[0] ?? c.wabaId} · {c.wabaId}
                                    </option>
                                ))}
                            </CampoSelector>
                        ) : (
                            <span className="truncate font-semibold">Cuenta de WhatsApp {cuenta?.wabaId}</span>
                        )}
                        <span className="text-sm break-words text-muted-foreground">
                            Sirven para {numeros.length === 1 ? "su número" : "sus números"}: {numeros.map((n, i) => `${n}${cuenta?.nombres[i] ? ` (${cuenta.nombres[i]})` : ""}`).join(", ")}
                        </span>
                    </div>
                </div>
                <div className="flex flex-wrap items-center gap-2.5 max-sm:w-full">
                    {cuenta?.sincronizadaEn ? (
                        <span className="flex items-center gap-1 text-sm text-muted-foreground" data-slot="sincronizada">
                            <Check aria-hidden className="size-3.5 text-success" />
                            Sincronizado {haceCuanto(cuenta.sincronizadaEn, ahora)}
                        </span>
                    ) : (
                        <span className="text-sm text-muted-foreground">Aún sin sincronizar</span>
                    )}
                    {puedeSincronizar ? (
                        <Boton variante="contorno" onClick={() => void sincronizar()} disabled={sincronizando} className="max-sm:flex-1">
                            <RefreshCw className={cn(sincronizando && "animate-spin")} />
                            {sincronizando ? "Sincronizando…" : "Sincronizar con Meta"}
                        </Boton>
                    ) : null}
                </div>
            </Tarjeta>

            {errorSync ? <ErrorPlantilla error={errorSync} reintentar={() => void sincronizar()} /> : errorCuenta && !resumen ? <ErrorPlantilla error={errorCuenta} {...(puedeSincronizar ? { reintentar: () => void sincronizar() } : {})} /> : null}
            {resumen ? <ResultadoSincronizacion resumen={resumen} /> : null}
            {sigue ? (
                <Aviso tono="info" titulo="Meta está tardando en contestar" data-slot="sincronizacion-en-curso">
                    <p>La sincronización sigue en curso. La lista se pone al día sola; vuelve en un momento.</p>
                </Aviso>
            ) : null}

            <Tarjeta className="min-w-0 overflow-hidden p-0">
                <div className="flex flex-wrap items-end gap-2 p-3" role="group" aria-label="Filtros">
                    <CampoBusqueda etiqueta="Buscar por nombre" etiquetaVisible={false} placeholder="Buscar por nombre" valor={buscar} alCambiar={setBuscar} maxLength={100} className="w-full sm:w-80" />
                    <CampoSelector etiqueta="Categoría" tamano="sm" className="w-40" value={categoria} onChange={(e) => setCategoria(e.target.value)}>
                        <option value="">Todas</option>
                        {CATEGORIAS.map((c) => (
                            <option key={c.valor} value={c.valor}>
                                {c.texto}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoSelector etiqueta="Idioma" tamano="sm" className="w-40" value={idioma} onChange={(e) => setIdioma(e.target.value)}>
                        <option value="">Todos</option>
                        {IDIOMAS.map((c) => (
                            <option key={c.valor} value={c.valor}>
                                {c.texto}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
            </Tarjeta>
            {/* Otra búsqueda (u otra lectura tras sincronizar): la lista empieza de nuevo. */}
            <ListaFiltrada key={`${JSON.stringify(filtros)}·${lectura}`} filtros={filtros} grupo={grupo} alCambiarGrupo={setGrupo} sinFiltros={sinFiltros} ahora={ahora} />
        </div>
    );
}

function ListaFiltrada({ filtros, grupo, alCambiarGrupo, sinFiltros, ahora }: { filtros: P.FiltrosLista; grupo: Grupo; alCambiarGrupo: (g: Grupo) => void; sinFiltros: boolean; ahora: number }) {
    const lista = useLectura((signal) => P.leerLista(filtros, signal));
    const [mas, setMas] = useState<PlantillaWA[]>([]);
    // El cursor de «Cargar más» (undefined: el de la primera página).
    const [cursorMas, setCursorMas] = useState<string | null | undefined>(undefined);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const cursor = cursorMas !== undefined ? cursorMas : lista.estado === "listo" ? (lista.datos.nextCursor ?? null) : null;

    async function cargarMas() {
        if (!cursor) return;
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const r = await P.leerLista({ ...filtros, cursor });
            setMas((m) => [...m, ...r.data]);
            setCursorMas(r.nextCursor ?? null);
        } catch (e) {
            setErrorMas(e);
        } finally {
            setCargandoMas(false);
        }
    }

    const router = useRouter();
    const filas = lista.estado === "listo" ? [...lista.datos.data, ...mas] : [];
    const conteos = lista.estado === "listo" ? lista.datos.conteos : null;
    const grupos = (
        <GrupoSegmentado<Grupo>
            etiqueta="Estado"
            className="max-w-full overflow-x-auto"
            valor={grupo}
            alCambiar={alCambiarGrupo}
            opciones={[
                { valor: "todas", etiqueta: "Todas", ...(conteos ? { contador: conteos.todas } : {}) },
                { valor: "activas", etiqueta: "Activas", ...(conteos ? { contador: conteos.activas } : {}) },
                { valor: "revision", etiqueta: "En revisión", ...(conteos ? { contador: conteos.revision } : {}) },
                { valor: "atender", etiqueta: "Atender", ...(conteos ? { contador: conteos.atender } : {}) },
            ]}
        />
    );
    const vacio =
        lista.estado === "cargando" ? (
            <div className="grid gap-2" aria-busy="true">
                <Esqueleto className="h-12" />
                <Esqueleto className="h-12" />
                <Esqueleto className="h-12" />
            </div>
        ) : lista.estado === "error" ? (
            <ErrorLectura error={lista.error} reintentar={lista.reintentar} que="las plantillas" />
        ) : (
            <EstadoVacio
                icono={MessageSquare}
                titulo={sinFiltros ? "Esta cuenta aún no tiene plantillas" : "Ninguna plantilla con estos filtros"}
                descripcion={sinFiltros ? "Empieza con las sugeridas para tu ISP: ya vienen conectadas con los datos de Iris y Argos." : "Cambia los filtros para ver otras."}
            />
        );
    return (
        <ListaFilas
            etiqueta="Plantillas"
            cabecera={grupos}
            vacio={vacio}
            {...(filas.length > 0 ? { resumen: `Mostrando ${filas.length}${conteos ? ` de ${grupo === "todas" ? conteos.todas : conteos[grupo]}` : ""}` } : {})}
            {...(cursor ? { alCargarMas: () => void cargarMas() } : {})}
            cargandoMas={cargandoMas}
            {...(errorMas ? { errorMas: <ErrorPlantilla error={errorMas} reintentar={() => void cargarMas()} /> } : {})}
        >
            {filas.map((p) => {
                const calidad = calidadVisible(p);
                const extra = p.estado === "DELETED" && p.borradaPor === "meta" ? " · ya no está en Meta" : p.origen === "meta" ? " · creada en Meta" : "";
                return (
                    <FilaLista
                        key={p.id}
                        data-plantilla={p.nombre}
                        icono={MessageSquare}
                        tonoIcono={p.atender ? "peligro" : "marca"}
                        titulo={p.etiqueta}
                        detalle={
                            <>
                                <span className="font-mono">
                                    {p.nombre}
                                    {extra}
                                </span>
                                {` · ${textoCategoria(p.categoria)} · ${textoIdioma(p.idioma)}`}
                                {calidad ? ` · Calidad ${calidad.texto.toLowerCase()}` : ""}
                                {` · ${p.usos.length ? p.usos.map(textoUso).join(", ") : "Sin usar en Iris"}`}
                            </>
                        }
                        href={`${RUTA}/${p.id}`}
                        className={cn(p.atender && "bg-[color-mix(in_srgb,var(--color-error)_4%,transparent)]")}
                        extremo={
                            <>
                                <EstadoPlantilla plantilla={p} />
                                <span className="text-xs whitespace-nowrap text-muted-foreground">{cuandoCorto(p.estadoEn, ahora)}</span>
                            </>
                        }
                        extremoTelefono="debajo"
                        menu={
                            <MenuAcciones
                                etiqueta={`Acciones de ${p.etiqueta}`}
                                acciones={[{ id: "abrir", texto: "Abrir plantilla", icono: SquareArrowOutUpRight, alElegir: () => router.push(`${RUTA}/${p.id}`) }]}
                            />
                        }
                    />
                );
            })}
        </ListaFilas>
    );
}

function ResultadoSincronizacion({ resumen }: { resumen: ResumenSincronizacion }) {
    const cambios: { icono: LucideIcon; tono: string; texto: React.ReactNode }[] = [];
    if (resumen.nuevas.length) cambios.push({ icono: Plus, tono: "text-success", texto: <><strong>{resumen.nuevas.length} {resumen.nuevas.length === 1 ? "nueva" : "nuevas"}</strong> que alguien creó en Meta: {resumen.nuevas.map((x) => x.nombre).join(", ")}</> });
    if (resumen.cambiadas.length) cambios.push({ icono: Pencil, tono: "text-info", texto: <><strong>{resumen.cambiadas.length} {resumen.cambiadas.length === 1 ? "cambió" : "cambiaron"}</strong>: {resumen.cambiadas.map((x) => `${x.etiqueta} (${estadoVisible({ estado: x.estado as PlantillaWA["estado"], pausadaHasta: null, borradaPor: null }).texto.toLowerCase()})`).join(", ")}</> });
    for (const b of resumen.borradas)
        cambios.push({ icono: Trash2, tono: "text-error", texto: <><strong>Ya no está en Meta</strong>: {b.nombre}.{b.codigos.length || b.usos.length ? " Se usaba: elige otra." : ""}</> });
    return (
        <Aviso tono="success" icono={CircleCheck} titulo="Listo: Iris quedó igual que Meta" data-slot="resultado-sincronizacion">
            <p>
                Revisamos {resumen.total} {resumen.total === 1 ? "plantilla" : "plantillas"} de la cuenta. {cambios.length ? "Esto cambió desde la última vez:" : "No cambió nada."}
            </p>
            {cambios.length ? (
                <ul className="mt-1.5 grid gap-1 text-sm">
                    {cambios.map((c, i) => (
                        <li key={i} className="flex gap-1.5">
                            <c.icono aria-hidden className={cn("mt-0.5 size-3.5 shrink-0", c.tono)} />
                            <span className="min-w-0 break-words">{c.texto}</span>
                        </li>
                    ))}
                </ul>
            ) : null}
        </Aviso>
    );
}

/** Ajustes de la empresa (un interruptor: diálogo corto). Marketing apagado de fábrica. */
function AjustesMarketing() {
    const router = useRouter();
    const [abierto, setAbierto] = useState(false);
    const ajustes = useLectura((signal) => P.leerAjustes(signal));
    const clave = useClaveIdempotencia();
    const [marketing, setMarketing] = useState<boolean | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [guardando, setGuardando] = useState(false);
    const actual = ajustes.estado === "listo" ? ajustes.datos : null;
    async function guardar() {
        if (!actual) return;
        setGuardando(true);
        setError(null);
        try {
            const r = await P.guardarAjustes({ marketingPermitido: marketing ?? actual.marketingPermitido, version: actual.version }, clave.actual());
            clave.renovar();
            ajustes.reemplazar(r);
            setAbierto(false);
            avisar({ titulo: r.marketingPermitido ? "Plantillas de marketing activadas" : "Plantillas de marketing apagadas" });
            router.refresh();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setGuardando(false);
        }
    }
    return (
        <>
            <Boton variante="contorno" onClick={() => (setMarketing(null), setError(null), setAbierto(true))}>
                <Settings2 />
                Ajustes
            </Boton>
            {abierto ? (
                <Dialogo open onOpenChange={(a) => !a && !guardando && setAbierto(false)}>
                    <DialogoContenido titulo="Ajustes de plantillas" descripcion="Se aplican a todas las cuentas de tu empresa.">
                        {ajustes.estado === "cargando" ? <Esqueleto className="h-12" /> : null}
                        {ajustes.estado === "error" ? <ErrorLectura error={ajustes.error} reintentar={ajustes.reintentar} que="los ajustes" /> : null}
                        {actual ? (
                            <Interruptor
                                etiqueta="Permitir plantillas de marketing"
                                ayuda="Promociones, planes nuevos o encuestas. Meta cobra más por cada mensaje de marketing."
                                checked={marketing ?? actual.marketingPermitido}
                                onChange={(e) => setMarketing(e.target.checked)}
                            />
                        ) : null}
                        {error ? <ErrorPlantilla error={error} /> : null}
                        <DialogoPie>
                            <DialogoCerrar asChild>
                                <Boton variante="contorno" disabled={guardando}>
                                    Cancelar
                                </Boton>
                            </DialogoCerrar>
                            <Boton onClick={() => void guardar()} disabled={guardando || !actual}>
                                {guardando ? "Guardando…" : "Guardar"}
                            </Boton>
                        </DialogoPie>
                    </DialogoContenido>
                </Dialogo>
            ) : null}
        </>
    );
}
