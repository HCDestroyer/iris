"use client";

import { avisar, Aviso, Boton, cn, Esqueleto, EstadoVacio, GraficaSerie, GrupoSegmentado, InsigniaEstado, Medidor, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo, Tarjeta } from "@iris/ui";
import { Building2, Check, CircleCheck, Clock, Cpu, Info, Layers, Link2, MemoryStick, MessageCircle, RefreshCw, Server, Sparkles, Thermometer, TriangleAlert, Users, Wrench, type LucideIcon } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import { probarMotor } from "@/lib/servidor-ia/api";
import {
    cantidad,
    cifrasVigentes,
    enServicio,
    lineaEstado,
    MIENTRAS_TANTO,
    NOMBRE_PASO,
    NOMBRE_RANGO,
    queActualizar,
    seriesDe,
    tamano,
    tarjetaGpu,
    TEXTO_RANGO,
    textoError,
    tiempoRespuesta,
    ultimaVez,
} from "@/lib/servidor-ia/presentacion";
import { RANGOS, type Rango, type ResultadoPrueba, type Tablero, type Umbrales } from "@/lib/servidor-ia/tipos";
import { DE_FABRICA } from "@/lib/servidor-ia/umbrales";
import { Comando, LineaEstado, NavServidorIa, RUTA, usePermisosServidorIa, useTablero } from "./comun";

/**
 * `/administracion/servidor-ia`: el tablero del Servidor de IA (contratos/voz.md §4.12). Quien
 * administra la instalación ve todo; el admin de una empresa (`servidor_ia.ver_uso`), el estado
 * general y solo el uso de su empresa. Si no responde, las cifras de «ahora» quedan «Sin datos»:
 * nunca una cifra vieja como si fuera actual.
 */
export function TableroServidorIa({ umbrales = null }: { umbrales?: Umbrales | null }) {
    const { global, administra } = usePermisosServidorIa();
    const [rango, setRango] = useState<Rango>("ahora");
    const { lectura, refrescar, ahora } = useTablero(rango);

    return (
        <>
            <EncabezadoPagina titulo="Servidor de IA" descripcion={global ? "La computadora con tarjeta gráfica donde trabaja el asistente. Sirve a todas tus empresas." : "La computadora de tu proveedor donde trabaja el asistente de tu empresa."} />
            <NavServidorIa />
            {lectura.estado === "cargando" ? (
                <div className="grid gap-3" aria-busy="true">
                    <span className="sr-only" role="status">
                        Leyendo el estado del servidor de IA…
                    </span>
                    <Esqueleto className="h-6 w-2/3" />
                    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                        {[0, 1, 2, 3].map((i) => (
                            <Esqueleto key={i} className="h-28" />
                        ))}
                    </div>
                    <Esqueleto className="h-56" />
                </div>
            ) : lectura.estado === "error" ? (
                // Sin lectura no hay cifras: ni las de antes.
                <ErrorLectura error={lectura.error} reintentar={refrescar} que="el servidor de IA" />
            ) : lectura.datos.estado.estado === null ? (
                <SinServidor global={global} administra={administra} />
            ) : lectura.datos.estado.estado === "sin_enlazar" ? (
                <Tarjeta>
                    <EstadoVacio
                        icono={Link2}
                        titulo="El servidor está enlazado pero falta activarlo"
                        descripcion="Hasta activarlo, Iris no le manda nada y ninguna empresa puede elegirlo. Un enlace sin activar caduca a las 24 horas."
                        accion={
                            administra ? (
                                <Boton asChild>
                                    <Link href={`${RUTA}/enlazar` as Route}>Terminar de enlazar</Link>
                                </Boton>
                            ) : undefined
                        }
                    />
                </Tarjeta>
            ) : (
                <Vista tablero={lectura.datos} rango={rango} alRango={setRango} ahora={ahora} global={global} administra={administra} umbrales={umbrales} refrescar={refrescar} />
            )}
        </>
    );
}

function SinServidor({ global, administra }: { global: boolean; administra: boolean }) {
    return (
        <Tarjeta data-slot="sin-servidor">
            <EstadoVacio
                icono={Server}
                titulo="Sin servidor de IA"
                descripcion={
                    global
                        ? "Es una computadora tuya, con tarjeta gráfica, donde el asistente trabaja sin mandar nada a servicios de fuera. Solo Iris se conecta a ella, por un canal cifrado, y sirve para todas tus empresas."
                        : "Tu instalación de Iris todavía no tiene un servidor de IA propio. El asistente de tu empresa usa el proveedor que elegiste en sus ajustes."
                }
                accion={
                    administra ? (
                        <Boton asChild>
                            <Link href={`${RUTA}/enlazar` as Route}>
                                <Link2 />
                                Enlazar servidor
                            </Link>
                        </Boton>
                    ) : undefined
                }
            />
        </Tarjeta>
    );
}

type PropsVista = { tablero: Tablero; rango: Rango; alRango: (r: Rango) => void; ahora: number; global: boolean; administra: boolean; umbrales: Umbrales | null; refrescar: () => void };

function Vista({ tablero, rango, alRango, ahora, global, administra, umbrales, refrescar }: PropsVista) {
    const e = tablero.estado;
    const vigentes = cifrasVigentes(tablero);
    const fuera = !enServicio(e.estado) && e.estado !== "degradado";
    const [prueba, setPrueba] = useState<{ estado: "libre" } | { estado: "probando" } | { estado: "hecha"; resultado: ResultadoPrueba } | { estado: "error"; error: unknown }>({ estado: "libre" });
    const limites = umbrales ?? { ...DE_FABRICA, version: 0 };

    async function probar() {
        if (e.motorId === null) return;
        setPrueba({ estado: "probando" });
        try {
            const resultado = await probarMotor(e.motorId, nuevaClaveIdempotencia());
            setPrueba({ estado: "hecha", resultado });
            avisar(resultado.estado === "ok" ? { titulo: "El servidor de IA respondió bien" } : { tono: "warning", titulo: "La prueba encontró un problema" });
            refrescar();
        } catch (error) {
            setPrueba({ estado: "error", error });
        }
    }
    const botonProbar = administra ? (
        <Boton variante={fuera ? "primario" : "contorno"} tamano={fuera ? "md" : "sm"} disabled={prueba.estado === "probando"} onClick={() => void probar()}>
            <RefreshCw />
            {prueba.estado === "probando" ? "Probando…" : fuera ? "Probar ahora" : "Probar"}
        </Boton>
    ) : null;

    return (
        <div className="grid min-w-0 gap-4" data-slot="tablero-servidor-ia">
            {fuera ? (
                <Fuera tablero={tablero} ahora={ahora} accion={botonProbar} global={global} />
            ) : (
                <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                    <LineaEstado linea={lineaEstado(e, ahora)} estado={e.estado} />
                    {botonProbar}
                </div>
            )}
            {prueba.estado === "hecha" ? <ResultadoDePrueba resultado={prueba.resultado} /> : null}
            {prueba.estado === "error" ? (
                <Aviso tono="error" titulo="No se pudo hacer la prueba">
                    {mensajeDeError(prueba.error).titulo}
                </Aviso>
            ) : null}
            {!fuera && !e.lecturaOk ? (
                <Aviso tono="warning" titulo="No se pudo leer el servidor en el último intento" data-slot="lectura-fallida">
                    {e.tomadoEn ? `La última lectura buena fue ${ultimaVez(e.tomadoEn, ahora)}. ` : ""}Las cifras de ahora no se muestran hasta que vuelva a responder.
                </Aviso>
            ) : null}

            <GrupoSegmentado etiqueta="Periodo" valor={rango} alCambiar={alRango} opciones={RANGOS.map((r) => ({ valor: r, etiqueta: NOMBRE_RANGO[r] }))} className="w-full sm:w-fit" />

            {global ? <Cifras tablero={tablero} vigentes={vigentes} rango={rango} limites={limites} /> : <EstadoGeneral tablero={tablero} />}
            {global ? null : <CifrasEmpresa tablero={tablero} rango={rango} />}
            {tablero.series ? <Series tablero={tablero} rango={rango} global={global} /> : null}
            {global && vigentes && tablero.servidor ? <Modelos tablero={tablero} administra={administra} /> : null}
            {global ? <Uso tablero={tablero} rango={rango} /> : null}
            <Errores tablero={tablero} rango={rango} ahora={ahora} />
            {global ? null : (
                <p className="flex items-start gap-2 rounded-lg bg-elevated p-3 text-[0.8125rem] text-muted-foreground" data-slot="nota-admin-empresa">
                    <Info aria-hidden className="mt-0.5 size-4 shrink-0" />
                    Como administrador de una empresa ves el estado general y el uso de tu empresa. La tarjeta gráfica, los modelos y las otras empresas los ve quien administra toda la instalación.
                </p>
            )}
        </div>
    );
}

/** Caído, incompatible o desactivado: qué pasa, desde cuándo, qué hace el asistente mientras tanto y qué revisar. */
function Fuera({ tablero, ahora, accion, global }: { tablero: Tablero; ahora: number; accion: ReactNode; global: boolean }) {
    const e = tablero.estado;
    const linea = lineaEstado(e, ahora);
    const incompatible = e.estado === "incompatible";
    const actualizar = incompatible ? queActualizar({ version: e.version ?? "", motivoIncompatible: e.motivoIncompatible, versionDisponible: null }) : null;
    const empresas = e.empresasQueLoUsan ?? [];
    return (
        <>
            <Aviso tono={e.estado === "desactivado" ? "warning" : "error"} titulo={linea.titulo} accion={accion} data-slot="servidor-fuera" data-estado={e.estado ?? ""}>
                {incompatible && actualizar ? (
                    <span className="grid gap-2">
                        <span>
                            <strong className="font-semibold">{actualizar.titulo}.</strong> {actualizar.detalle}
                        </span>
                        {actualizar.comando && global ? <Comando texto={actualizar.comando} /> : null}
                    </span>
                ) : e.estado === "desactivado" ? (
                    "Alguien lo desactivó desde Iris: el asistente no lo usa hasta que se vuelva a enlazar."
                ) : (
                    <>
                        {e.tomadoEn ? `Visto por última vez ${ultimaVez(e.tomadoEn, ahora)}. ` : "Todavía no ha respondido ninguna vez. "}
                        Iris lo vuelve a intentar sola cada 10 segundos y, cuando responda, vuelve a usarlo sin que hagas nada.
                    </>
                )}
            </Aviso>
            <Tarjeta className="grid gap-2.5 p-4 sm:p-5" data-slot="mientras-tanto">
                <h2 className="flex items-center gap-2 text-[0.9375rem] font-semibold">
                    <Sparkles aria-hidden className="size-4 text-muted-foreground" />
                    Qué pasa con el asistente mientras tanto
                </h2>
                <ul className="grid gap-1.5 text-sm">
                    {MIENTRAS_TANTO.map((texto, i) => {
                        const Icono = [RefreshCw, Users, Check][i] ?? Check;
                        return (
                            <li key={texto} className="flex items-start gap-2">
                                <Icono aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                                {texto}
                            </li>
                        );
                    })}
                </ul>
                {global && empresas.length > 0 ? (
                    <p className="flex items-start gap-2 rounded-lg bg-elevated p-2.5 text-[0.8125rem] text-muted-foreground">
                        <Building2 aria-hidden className="mt-0.5 size-4 shrink-0" />
                        {empresas.length === 1 ? `Lo usa 1 empresa: ${empresas[0]}.` : `Lo usan ${empresas.length} empresas: ${empresas.join(", ")}.`}
                    </p>
                ) : null}
            </Tarjeta>
            {global && e.estado === "no_disponible" ? (
                <Tarjeta className="grid gap-2.5 p-4 sm:p-5" data-slot="que-revisar">
                    <h2 className="flex items-center gap-2 text-[0.9375rem] font-semibold">
                        <Wrench aria-hidden className="size-4 text-muted-foreground" />
                        Qué revisar en el servidor
                    </h2>
                    <ul className="grid gap-1.5 text-sm">
                        <li className="flex items-start gap-2">
                            <Check aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                            Que esté encendido y con red.
                        </li>
                        <li className="flex items-start gap-2">
                            <Check aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                            Que Iris Voz esté corriendo. Este comando lo dice:
                        </li>
                    </ul>
                    <Comando texto="sudo iris-voz estado" />
                    <p className="text-[0.8125rem] text-muted-foreground">Si lo acabas de reiniciar, tarda cerca de un minuto en volver.</p>
                </Tarjeta>
            ) : null}
        </>
    );
}

function ResultadoDePrueba({ resultado }: { resultado: ResultadoPrueba }) {
    return (
        <Tarjeta className="grid gap-2 p-4" data-slot="resultado-prueba" data-estado={resultado.estado}>
            <h2 className="text-[0.9375rem] font-semibold">{resultado.estado === "ok" ? "La prueba salió bien" : "La prueba encontró un problema"}</h2>
            <ul className="grid gap-1.5 text-sm">
                {resultado.pasos.map((p) => (
                    <li key={p.codigo} className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5" data-paso={p.codigo} data-estado={p.estado}>
                        <InsigniaEstado tono={p.estado === "ok" ? "success" : p.estado === "error" ? "error" : "neutral"} texto={p.estado === "ok" ? "Bien" : p.estado === "error" ? "Falló" : "No aplica"} />
                        <span className="font-medium">{NOMBRE_PASO[p.codigo] ?? p.codigo}</span>
                        {p.ms !== null && p.estado === "ok" ? <span className="text-muted-foreground tabular-nums">{tiempoRespuesta(p.ms)}</span> : null}
                        {p.detalle && p.estado === "error" ? <span className="basis-full text-[0.8125rem] text-muted-foreground">{p.detalle}</span> : null}
                    </li>
                ))}
            </ul>
        </Tarjeta>
    );
}

/** Una cifra con su nombre, su medidor y una línea de detalle. Sin dato: una raya y «Sin datos», nunca un cero. */
function Cifra({ icono: Icono, nombre, valor, unidad, detalle, medidor, slot }: { icono: LucideIcon; nombre: string; valor: string | null; unidad?: string; detalle?: string; medidor?: { parte: number; tono?: "normal" | "aviso" | "error"; texto: string }; slot: string }) {
    return (
        <Tarjeta className="grid content-start gap-2 p-3.5 sm:p-4" data-slot={slot} data-vacio={valor === null || undefined}>
            <p className="flex items-start gap-1.5 text-[0.8125rem] text-muted-foreground">
                <Icono aria-hidden className="mt-0.5 size-4 shrink-0" />
                {nombre}
            </p>
            {valor === null ? (
                <>
                    <span aria-hidden className="my-2 block h-0.5 w-8 rounded-full bg-muted-foreground/40" />
                    <p className="text-[0.8125rem] text-muted-foreground">Sin datos</p>
                </>
            ) : (
                <>
                    <p className="text-2xl leading-none font-semibold tabular-nums">
                        {valor}
                        {unidad ? <span className="ml-1 text-sm font-normal text-muted-foreground">{unidad}</span> : null}
                    </p>
                    {medidor ? <Medidor parte={medidor.parte} etiqueta={nombre} texto={medidor.texto} tono={medidor.tono ?? "normal"} /> : null}
                    {detalle ? <p className={cn("text-[0.8125rem]", medidor?.tono === "error" ? "font-medium text-error" : medidor?.tono === "aviso" ? "font-medium text-warning" : "text-muted-foreground")}>{detalle}</p> : null}
                </>
            )}
        </Tarjeta>
    );
}

function Cifras({ tablero, vigentes, rango, limites }: { tablero: Tablero; vigentes: boolean; rango: Rango; limites: Umbrales }) {
    const s = tablero.servidor;
    if (!vigentes || !s) {
        // No hay lectura de ahora: las cuatro cifras, vacías. Nada de la última muestra.
        return (
            <section aria-label="Cifras de ahora" className="grid gap-2" data-slot="cifras" data-vigentes="no">
                <h2 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">Cifras de ahora</h2>
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                    <Cifra icono={Cpu} nombre="Uso de la tarjeta gráfica" valor={null} slot="cifra-uso" />
                    <Cifra icono={MemoryStick} nombre="Memoria de video" valor={null} slot="cifra-vram" />
                    <Cifra icono={Thermometer} nombre="Temperatura" valor={null} slot="cifra-temperatura" />
                    <Cifra icono={MessageCircle} nombre="Peticiones en curso" valor={null} slot="cifra-peticiones" />
                </div>
            </section>
        );
    }
    const gpus = Array.isArray(s.gpus) ? s.gpus : [];
    const peticiones = (
        <Cifra
            icono={MessageCircle}
            nombre="Peticiones en curso"
            valor={String(s.llm.enCurso)}
            unidad={`de ${s.llm.maxSimultaneas} a la vez`}
            detalle={s.rechazadas === 0 ? `Ninguna rechazada por saturación ${TEXTO_RANGO[rango]}` : `${cantidad(s.rechazadas)} ${s.rechazadas === 1 ? "rechazada" : "rechazadas"} por saturación ${TEXTO_RANGO[rango]}`}
            medidor={{ parte: s.llm.maxSimultaneas > 0 ? s.llm.enCurso / s.llm.maxSimultaneas : 0, tono: s.rechazadas > 0 ? "aviso" : "normal", texto: `${s.llm.enCurso} de ${s.llm.maxSimultaneas}` }}
            slot="cifra-peticiones"
        />
    );
    if (gpus.length === 0) {
        return (
            <section aria-label="Cifras de ahora" className="grid gap-3" data-slot="cifras" data-vigentes="si">
                <Aviso tono="warning" titulo="No hay tarjeta gráfica detectada" data-slot="sin-tarjeta-grafica">
                    El servidor responde, pero no reporta ninguna tarjeta gráfica: sin ella los modelos trabajan con el procesador y contestan mucho más lento. Revisa que la tarjeta esté instalada y con sus controladores.
                </Aviso>
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">{peticiones}</div>
            </section>
        );
    }
    return (
        <section aria-label="Cifras de ahora" className="grid gap-3" data-slot="cifras" data-vigentes="si">
            {gpus.map((g, i) => {
                const t = tarjetaGpu(g, limites);
                return (
                    <div key={g.indice} className="grid gap-2" data-gpu={g.indice}>
                        {gpus.length > 1 ? (
                            <h2 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                                Tarjeta {i + 1} · {t.nombre}
                            </h2>
                        ) : null}
                        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                            <Cifra icono={Cpu} nombre="Uso de la tarjeta gráfica" valor={t.uso ? t.uso.valor.replace(" %", "") : null} unidad="%" {...(gpus.length === 1 ? { detalle: t.nombre } : {})} {...(t.uso ? { medidor: { parte: t.uso.parte, texto: t.uso.valor } } : {})} slot="cifra-uso" />
                            <Cifra icono={MemoryStick} nombre="Memoria de video" valor={t.memoria?.valor ?? null} {...(t.memoria ? { unidad: t.memoria.de, detalle: t.memoria.detalle, medidor: { parte: t.memoria.parte, tono: t.memoria.tono, texto: `${t.memoria.valor} ${t.memoria.de}` } } : {})} slot="cifra-vram" />
                            <Cifra icono={Thermometer} nombre="Temperatura" valor={t.temperatura?.valor ?? null} unidad="°C" {...(t.temperatura ? { detalle: t.temperatura.detalle, medidor: { parte: t.temperatura.parte, tono: t.temperatura.tono, texto: `${t.temperatura.valor} °C` } } : {})} slot="cifra-temperatura" />
                            {i === 0 ? peticiones : null}
                        </div>
                    </div>
                );
            })}
        </section>
    );
}

/** Lo que ve el admin de una empresa: si está listo y qué modelos hay. Nada de la tarjeta gráfica ni de la ocupación. */
function EstadoGeneral({ tablero }: { tablero: Tablero }) {
    const e = tablero.estado;
    return (
        <Tarjeta className="grid gap-2 p-4 sm:p-5" data-slot="estado-general">
            <h2 className="flex items-center gap-2 text-[0.9375rem] font-semibold">
                <Layers aria-hidden className="size-4 text-muted-foreground" />
                Modelos disponibles
            </h2>
            {e.modelosDisponibles.length > 0 ? (
                <ul className="flex flex-wrap gap-1.5">
                    {e.modelosDisponibles.map((m) => (
                        <li key={m} className="rounded-md bg-elevated px-2 py-0.5 font-mono text-[0.8125rem]">
                            {m}
                        </li>
                    ))}
                </ul>
            ) : (
                <p className="text-sm text-muted-foreground">Ahora no hay ningún modelo disponible.</p>
            )}
            <p className="text-[0.8125rem] text-muted-foreground">
                El modelo de tu empresa se elige en{" "}
                <Link href="/administracion/asistente/conexion" className="text-primary underline-offset-2 hover:underline">
                    los ajustes del asistente
                </Link>
                .
            </p>
        </Tarjeta>
    );
}

/** El uso de la empresa activa en el periodo. Sin lectura: «Sin datos», nunca ceros. */
function CifrasEmpresa({ tablero, rango }: { tablero: Tablero; rango: Rango }) {
    const u = tablero.uso?.empresas[0] ?? null;
    // Leído y sin fila: la empresa no lo usó. Sin leer: no se sabe.
    const cero = tablero.uso !== null && u === null;
    const numero = (v: number | undefined) => (v !== undefined ? cantidad(v) : cero ? "0" : null);
    return (
        <section aria-label="Uso de tu empresa" className="grid gap-2" data-slot="uso-empresa">
            <h2 className="text-sm text-muted-foreground">Uso de tu empresa {TEXTO_RANGO[rango]}</h2>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                <Cifra icono={MessageCircle} nombre="Peticiones" valor={numero(u?.peticiones)} detalle="de su asistente" slot="cifra-peticiones" />
                <Cifra icono={Layers} nombre="Tokens" valor={numero(u ? u.tokensEntrada + u.tokensSalida : undefined)} detalle="texto leído y escrito" slot="cifra-tokens" />
                <Cifra icono={Clock} nombre="Tiempo medio de respuesta" valor={u && u.latenciaMediaMs !== null ? tiempoRespuesta(u.latenciaMediaMs) : cero || u ? "—" : null} detalle="por petición" slot="cifra-tiempo" />
                <Cifra icono={TriangleAlert} nombre="Errores" valor={numero(u?.errores)} detalle={u && u.errores > 0 ? "ver motivos abajo" : "en este periodo"} slot="cifra-errores" />
            </div>
        </section>
    );
}

function Series({ tablero, rango, global }: { tablero: Tablero; rango: Rango; global: boolean }) {
    const s = seriesDe(tablero.series ?? [], rango);
    const porcentaje = (v: number) => `${Math.round(v)} %`;
    const segundos = (v: number) => `${v >= 10 ? Math.round(v) : v.toFixed(1)} s`;
    return (
        <div className="grid min-w-0 gap-4 lg:grid-cols-2" data-slot="series">
            {global && s.hayGpu ? (
                <GraficaSerie
                    className="lg:col-span-2"
                    titulo="Uso y memoria de video"
                    descripcion="En porcentaje del total. La parte gris es el tiempo sin respuesta."
                    series={[
                        { nombre: "Uso", color: 1, valores: s.uso },
                        { nombre: "Memoria de video", color: 2, valores: s.vram },
                    ]}
                    instantes={s.instantes}
                    marcas={s.marcas}
                    formato={porcentaje}
                    maximo={100}
                />
            ) : null}
            <GraficaSerie titulo="Peticiones por minuto" {...(global ? {} : { descripcion: "De tu empresa" })} series={[{ nombre: "Peticiones", color: 1, valores: s.peticiones }]} instantes={s.instantes} marcas={s.marcas} formato={(v) => String(Math.round(v))} area />
            <GraficaSerie
                titulo="Tiempo de respuesta"
                descripcion="En segundos. Más bajo es mejor."
                series={[
                    { nombre: "Media", color: 1, valores: s.media },
                    { nombre: "El 95 % tarda menos de", color: 2, valores: s.p95 },
                ]}
                instantes={s.instantes}
                marcas={s.marcas}
                formato={segundos}
            />
        </div>
    );
}

function Modelos({ tablero, administra }: { tablero: Tablero; administra: boolean }) {
    const modelos = tablero.servidor?.modelos ?? [];
    return (
        <Tarjeta className="grid min-w-0 gap-3 p-4 sm:p-5" data-slot="modelos">
            <div className="flex items-center justify-between gap-2">
                <h2 className="flex items-center gap-2 text-[0.9375rem] font-semibold">
                    <Layers aria-hidden className="size-4 text-muted-foreground" />
                    Modelos
                </h2>
                <span className="rounded-md bg-elevated px-2 py-0.5 text-xs font-medium text-muted-foreground">ahora mismo</span>
            </div>
            {modelos.length === 0 ? (
                <p className="text-sm text-muted-foreground">El servidor no tiene ningún modelo instalado.</p>
            ) : (
                <Tabla>
                    <TablaEncabezado>
                        <TablaFila>
                            <TablaTitulo>Nombre</TablaTitulo>
                            <TablaTitulo className="text-right">Tamaño</TablaTitulo>
                            <TablaTitulo>En memoria</TablaTitulo>
                            <TablaTitulo className="text-right">Ocupa</TablaTitulo>
                        </TablaFila>
                    </TablaEncabezado>
                    <TablaCuerpo>
                        {modelos.map((m) => (
                            <TablaFila key={m.id} data-modelo={m.id}>
                                <TablaCelda className="font-mono text-[0.8125rem]">{m.id}</TablaCelda>
                                <TablaCelda className="text-right whitespace-nowrap">{m.tamanoMb === null ? "—" : tamano(m.tamanoMb)}</TablaCelda>
                                <TablaCelda>
                                    {m.cargado ? (
                                        <span className="inline-flex items-center gap-1 font-medium text-success">
                                            <Check aria-hidden className="size-3.5" />
                                            Sí
                                        </span>
                                    ) : (
                                        "No"
                                    )}
                                </TablaCelda>
                                <TablaCelda className="text-right whitespace-nowrap">{m.cargado && m.vramMb !== null ? tamano(m.vramMb) : "—"}</TablaCelda>
                            </TablaFila>
                        ))}
                    </TablaCuerpo>
                </Tabla>
            )}
            <p className="text-[0.8125rem] text-muted-foreground">
                Un modelo cargado responde de inmediato; uno sin cargar tarda unos segundos la primera vez. Los modelos se descargan en el servidor
                {administra ? (
                    <>
                        {" "}
                        (ver{" "}
                        <Link href={`${RUTA}/mantenimiento` as Route} className="text-primary underline-offset-2 hover:underline">
                            Mantenimiento
                        </Link>
                        ).
                    </>
                ) : (
                    "."
                )}
            </p>
        </Tarjeta>
    );
}

function Uso({ tablero, rango }: { tablero: Tablero; rango: Rango }) {
    const uso = tablero.uso;
    const global = true;
    const marco = { titulo: "Uso por empresa", rango, global };
    if (!uso) {
        return (
            <MarcoUso {...marco}>
                <p className="text-sm text-muted-foreground">No se pudo leer el uso de este periodo.</p>
            </MarcoUso>
        );
    }
    if (uso.empresas.length === 0) {
        return (
            <MarcoUso {...marco}>
                <p className="text-sm text-muted-foreground">{global ? "Ninguna empresa lo usó en este periodo." : "Tu empresa no lo usó en este periodo."}</p>
            </MarcoUso>
        );
    }
    return (
        <MarcoUso {...marco}>
            <Tabla>
                <TablaEncabezado>
                    <TablaFila>
                        {global ? <TablaTitulo>Empresa</TablaTitulo> : null}
                        <TablaTitulo className="text-right">Peticiones</TablaTitulo>
                        <TablaTitulo className="text-right">Tokens</TablaTitulo>
                        <TablaTitulo className="text-right">Tiempo medio</TablaTitulo>
                        <TablaTitulo className="text-right">Errores</TablaTitulo>
                        <TablaTitulo className="text-right">Rechazadas</TablaTitulo>
                    </TablaFila>
                </TablaEncabezado>
                <TablaCuerpo>
                    {uso.empresas.map((u) => (
                        <TablaFila key={u.empresaId} data-empresa={u.empresaId}>
                            {global ? <TablaCelda>{u.nombre}</TablaCelda> : null}
                            <TablaCelda className="text-right">{cantidad(u.peticiones)}</TablaCelda>
                            <TablaCelda className="text-right">{cantidad(u.tokensEntrada + u.tokensSalida)}</TablaCelda>
                            <TablaCelda className="text-right whitespace-nowrap">{u.latenciaMediaMs === null ? "—" : tiempoRespuesta(u.latenciaMediaMs)}</TablaCelda>
                            <TablaCelda className={cn("text-right", u.errores > 0 && "font-semibold text-error")}>{cantidad(u.errores)}</TablaCelda>
                            <TablaCelda className={cn("text-right", u.rechazadas > 0 && "font-semibold text-warning")}>{cantidad(u.rechazadas)}</TablaCelda>
                        </TablaFila>
                    ))}
                </TablaCuerpo>
            </Tabla>
        </MarcoUso>
    );
}

function MarcoUso({ titulo, rango, global, children }: { titulo: string; rango: Rango; global: boolean; children: ReactNode }) {
    return (
        <Tarjeta className="grid min-w-0 gap-3 p-4 sm:p-5" data-slot="uso">
            <div className="grid gap-0.5">
                <h2 className="flex items-center gap-2 text-[0.9375rem] font-semibold">
                    <Building2 aria-hidden className="size-4 text-muted-foreground" />
                    {titulo}
                </h2>
                <p className="text-[0.8125rem] text-muted-foreground">
                    {TEXTO_RANGO[rango].replace(/^en /, "En ")}. {global ? "Solo lo ve quien ve todas las empresas." : "Lo de otras empresas no se muestra."}
                </p>
            </div>
            {children}
        </Tarjeta>
    );
}

function Errores({ tablero, rango, ahora }: { tablero: Tablero; rango: Rango; ahora: number }) {
    const uso = tablero.uso;
    return (
        <Tarjeta className="grid min-w-0 gap-3 p-4 sm:p-5" data-slot="errores-recientes">
            <h2 className="flex items-center gap-2 text-[0.9375rem] font-semibold">
                <TriangleAlert aria-hidden className="size-4 text-muted-foreground" />
                Errores recientes
            </h2>
            {!uso ? (
                <p className="text-sm text-muted-foreground">No se pudieron leer los errores de este periodo.</p>
            ) : uso.errores.length === 0 ? (
                <p className="flex items-center gap-2 rounded-lg bg-success/10 p-2.5 text-sm">
                    <CircleCheck aria-hidden className="size-4 shrink-0 text-success" />
                    Sin errores {TEXTO_RANGO[rango]}.
                </p>
            ) : (
                <ul className="grid gap-2 text-sm">
                    {uso.errores.map((x) => {
                        const t = textoError(x.motivo, x.cuenta);
                        return (
                            <li key={x.motivo} className="flex min-w-0 flex-wrap items-baseline gap-x-2" data-error={x.motivo}>
                                <span className="font-medium">{t.titulo}</span>
                                <span className="text-muted-foreground tabular-nums">
                                    {t.veces} · la última, {haceCuanto(x.ultimoEn, ahora)}
                                </span>
                            </li>
                        );
                    })}
                </ul>
            )}
        </Tarjeta>
    );
}
