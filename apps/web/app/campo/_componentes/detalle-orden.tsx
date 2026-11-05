"use client";

import type { OrdenVista } from "@iris/sync";
import { estadoPasos, faltantesAntesDeFirmar, llego, PASOS_ORDEN, progresoPasos, puedeReanudar, siguientePaso, TITULOS_PASO, urlDeVista, type PasoOrden } from "@iris/sync/campo";
import { Aviso, Boton, Cargando, EstadoVacio, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo, cn } from "@iris/ui";
import {
    ArrowLeft,
    CircleCheck,
    CircleDashed,
    FileQuestion,
    ListChecks,
    Lock,
    MapPin,
    MapPinCheck,
    Navigation,
    PenLine,
    Phone,
    Play,
    Route,
} from "lucide-react";
import { useEffect, useRef, useState, type MouseEvent } from "react";
import { ListaTelefonos } from "@/componentes/clientes/telefonos";
import { useAhora } from "@/componentes/tickets/insignias";
import { horaCorta, ventanaCita } from "@/lib/campo/fechas";
import { enlaceComoLlegar } from "@/lib/campo/gps";
import { accionesDe } from "@/lib/campo/reglas";
import { AVISOS_SERVIDOR, OPERACIONES, TIPOS_ORDEN } from "@/lib/campo/textos";
import { haceTiempo } from "@/lib/datos/presentacion";
import { enlaceTel, principal, telefonos } from "@/lib/datos/telefono";
import { formatearFecha } from "@/lib/fechas";
import { Checklist } from "./checklist";
import { FallarOrden, PasoCierre } from "./cierre";
import { AbrirChatOrden } from "./chat";
import { useCampo, useOrden } from "./contexto";
import { Equipos } from "./equipos";
import { FotosOrden } from "./fotos";
import { InsigniaOrden } from "./insignias";
import { Materiales } from "./materiales";
import { Notas } from "./notas";
import { PausaOrden } from "./pausa";
import { AVISO_TURNO_INICIADO, useUbicacion } from "./ubicacion";

export function DetalleOrden({ id, paso }: { id: number; paso?: PasoOrden | undefined }) {
    const { navegar, estado } = useCampo();
    const orden = useOrden(id);
    if (!orden && estado.fecha === null && estado.ultimaSincronizacion === null) {
        // Enlace directo con el teléfono vacío: espera el paquete del día.
        return <Cargando visible texto="Cargando la orden…" />;
    }
    if (!orden) {
        return (
            <div className="grid gap-4">
                <Boton variante="fantasma" tamano="lg" className="-ml-2 w-fit" onClick={() => navegar({ tipo: "lista" })}>
                    <ArrowLeft aria-hidden />
                    Mis órdenes
                </Boton>
                <EstadoVacio icono={FileQuestion} titulo="La orden ya no está en el teléfono" descripcion="Puede que se haya reasignado o que no sea de hoy." />
            </div>
        );
    }
    return paso ? <PasoDeOrden orden={orden} paso={paso} /> : <ResumenOrden orden={orden} />;
}

/** Resumen: cliente, avisos, pausa, avance y «Empezar trabajo» (o «Continuar trabajo»). */
function ResumenOrden({ orden }: { orden: OrdenVista }) {
    const { navegar, sesion } = useCampo();
    const acciones = accionesDe(orden);
    const inicio = orden.cita?.inicio ?? orden.programadaInicio;
    const fin = orden.cita?.fin ?? orden.programadaFin;
    const ventana = ventanaCita(orden.cita);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";
    const trabajable = acciones.iniciarCamino || acciones.llegar || acciones.trabajo;
    const empezado = llego(orden);

    return (
        <article aria-labelledby="titulo-orden" className="grid gap-4">
            <header className="flex items-center gap-3">
                <BotonVolver etiqueta="Mis órdenes" alPresionar={() => navegar({ tipo: "lista" })} />
                <div className="grid min-w-0 gap-1">
                    <div className="flex flex-wrap items-center gap-2">
                        <h1 id="titulo-orden" className="font-mono text-xl font-semibold">
                            {orden.numero}
                        </h1>
                        <InsigniaOrden estado={orden.estado} />
                    </div>
                    <p className="text-sm text-muted-foreground">
                    {TIPOS_ORDEN[orden.tipo]}
                    {inicio ? ` · cita ${horaCorta(inicio)}${fin ? `–${horaCorta(fin)}` : ""}` : ""}
                    {ventana ? ` · llegada acordada ${ventana}` : ""}
                    </p>
                </div>
            </header>

            <AvisosOrden orden={orden} />
            {orden.estado === "pendiente" ? (
                <Aviso tono="neutral" titulo="Esperando programación">
                    Despacho todavía no le dio hora a esta orden.
                </Aviso>
            ) : null}
            {trabajable ? (
                <Boton tamano="lg" className="h-[3.25rem] w-full rounded-full text-base" onClick={() => navegar({ tipo: "orden", id: orden.id, paso: empezado ? siguientePaso(orden) : "llegada" })}>
                    {empezado ? <ListChecks aria-hidden /> : <Play aria-hidden />}
                    {empezado ? "Continuar trabajo" : "Empezar trabajo"}
                </Boton>
            ) : null}
            <PausaOrden orden={orden} pausar={acciones.pausar} reanudar={acciones.reanudar} puedeReanudar={puedeReanudar(sesion.permisos)} />
            <Cliente orden={orden} nombre={nombre} />
            {orden.estado !== "pendiente" ? <ProgresoOrden orden={orden} /> : null}
            <Notas orden={orden} editable={acciones.notas} />
            {acciones.fallar ? <FallarOrden orden={orden} /> : null}
        </article>
    );
}

/**
 * Un paso de trabajo: su contenido y el avance. «Anterior», «Guardar» y «Siguiente» van en la isla
 * de pasos (`IslaPasos`). Con la orden en pausa, todo de solo lectura con el aviso «Orden en pausa»
 * y «Reanudar» arriba.
 */
function PasoDeOrden({ orden, paso }: { orden: OrdenVista; paso: PasoOrden }) {
    const { navegar, sesion } = useCampo();
    const acciones = accionesDe(orden);
    const indice = PASOS_ORDEN.indexOf(paso);
    const estado = estadoPasos(orden).find((p) => p.paso === paso);
    const sinLlegar = !acciones.trabajo && !llego(orden);
    const irALlegada = sinLlegar ? (
        <Aviso
            tono="neutral"
            titulo="Marca tu llegada para llenar este paso"
            accion={
                <Boton variante="secundario" tamano="lg" onClick={() => navegar({ tipo: "orden", id: orden.id, paso: "llegada" })}>
                    Ir a la llegada
                </Boton>
            }
        />
    ) : null;

    return (
        <article aria-labelledby="titulo-paso" className="grid gap-4" data-paso={paso}>
            <header className="flex items-center gap-3">
                <BotonVolver etiqueta={`Resumen de ${orden.numero}`} alPresionar={() => navegar({ tipo: "orden", id: orden.id })} />
                <div className="grid min-w-0 gap-0.5">
                    <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                        <span>
                            <span className="font-mono">{orden.numero}</span> · {paso === "cierre" ? "Paso final" : `Paso ${indice + 1} de ${PASOS_ORDEN.length}`}
                        </span>
                        <InsigniaOrden estado={orden.estado} />
                    </p>
                    <div className="flex flex-wrap items-center gap-2">
                        <h1 id="titulo-paso" className="text-[1.375rem] leading-tight font-bold tracking-[-0.02em]">
                            {TITULOS_PASO[paso]}
                        </h1>
                        {estado && paso !== "cierre" ? (
                            estado.completo ? (
                                <InsigniaEstado tono="success" icono={CircleCheck} texto={estado.opcional ? "Opcional" : "Listo"} />
                            ) : (
                                <InsigniaEstado tono="warning" icono={CircleDashed} texto={`Falta${estado.faltan.length === 1 ? "" : "n"} ${estado.faltan.length}`} />
                            )
                        ) : null}
                    </div>
                </div>
            </header>
            {acciones.enPausa ? <PausaOrden orden={orden} pausar={false} reanudar puedeReanudar={puedeReanudar(sesion.permisos)} enPasos /> : null}
            <ProgresoOrden orden={orden} actual={paso} compacto />
            <AvisosOrden orden={orden} />

            {paso === "llegada" ? (
                <>
                    {orden.estado === "pendiente" ? (
                        <Aviso tono="neutral" titulo="Esperando programación">
                            Despacho todavía no le dio hora a esta orden.
                        </Aviso>
                    ) : null}
                    {llego(orden) ? (
                        <Aviso tono="success" icono={MapPinCheck} titulo="Llegaste al sitio">
                            {orden.enSitioEn ? `Llegada registrada a las ${horaCorta(orden.enSitioEn)}.` : "Llegada registrada."}
                        </Aviso>
                    ) : null}
                    {acciones.iniciarCamino || acciones.llegar ? <AccionesEstado orden={orden} camino={acciones.iniciarCamino} llegar={acciones.llegar} /> : null}
                    <PausaOrden orden={orden} pausar={acciones.pausar} reanudar={false} />
                </>
            ) : null}
            {paso === "checklist" ? (
                <>
                    {irALlegada}
                    {acciones.trabajo || acciones.enPausa || acciones.agregarFormulario || (orden.checklist ?? []).some((i) => i.valor !== null) ? (
                        <Checklist orden={orden} editable={acciones.trabajo} agregar={acciones.agregarFormulario} />
                    ) : null}
                </>
            ) : null}
            {paso === "materiales" ? (
                <>
                    {irALlegada}
                    {acciones.trabajo || acciones.enPausa || (orden.materiales ?? []).length > 0 ? <Materiales orden={orden} editable={acciones.trabajo} /> : null}
                    <Equipos orden={orden} instalar={acciones.instalar} retirar={acciones.retirar} />
                    <Comodato orden={orden} />
                </>
            ) : null}
            {paso === "fotos" ? acciones.notas ? <FotosOrden orden={orden} editable={!acciones.enPausa} /> : <p className="text-sm text-muted-foreground">Esta orden ya no admite fotos.</p> : null}
            {paso === "cierre" ? <PasoCierre orden={orden} /> : null}

        </article>
    );
}

/** Botón redondo de 48 px para volver (al resumen o a «Mis órdenes»). */
function BotonVolver({ etiqueta, alPresionar }: { etiqueta: string; alPresionar: () => void }) {
    return (
        <Boton
            variante="secundario"
            aria-label={etiqueta}
            title={etiqueta}
            className="size-12 shrink-0 rounded-full bg-[var(--campo-panel)] p-0 md:size-12 [&_svg]:size-5"
            onClick={alPresionar}
        >
            <ArrowLeft aria-hidden />
        </Boton>
    );
}

/** Texto corto de cada paso en los chips (el nombre accesible lleva el título completo). */
const CORTO_PASO: Record<PasoOrden, string> = {
    llegada: "Llegada",
    checklist: "Checklist",
    materiales: "Materiales",
    fotos: "Fotos",
    cierre: "Firma",
};

/**
 * Avance de la orden: los pasos listos (barra por pasos) y cada paso como enlace (se puede volver a
 * cualquiera). «Firma del cliente y cierre» queda con candado hasta que no falte nada. En el resumen
 * es una tarjeta con la lista; dentro de un paso, chips (listo en marca suave, el actual en tinta).
 */
function ProgresoOrden({ orden, actual, compacto = false }: { orden: OrdenVista; actual?: PasoOrden; compacto?: boolean }) {
    const { navegar } = useCampo();
    const pasos = estadoPasos(orden);
    const { listos, total } = progresoPasos(orden);
    const listaParaFirmar = faltantesAntesDeFirmar(orden).length === 0;
    const texto = `${listos} de ${total} pasos listos`;
    const actualRef = useRef<HTMLAnchorElement | null>(null);
    useEffect(() => {
        // El chip del paso actual siempre a la vista (la tira se desplaza de lado).
        actualRef.current?.scrollIntoView?.({ block: "nearest", inline: "center" });
    }, [actual]);
    const ir = (p: PasoOrden) => (e: MouseEvent<HTMLAnchorElement>) => {
        e.preventDefault();
        navegar({ tipo: "orden", id: orden.id, paso: p });
    };
    const barra = (
        <div
            role="progressbar"
            aria-label="Avance de la orden"
            aria-valuemin={0}
            aria-valuemax={total}
            aria-valuenow={listos}
            aria-valuetext={texto}
            className="grid gap-1.5"
            style={{ gridTemplateColumns: `repeat(${total}, minmax(0, 1fr))` }}
        >
            {Array.from({ length: total }, (_, i) => (
                <span key={i} className={cn("h-1.5 rounded-full", i < listos ? "bg-primary" : "bg-[var(--campo-anillo)]")} />
            ))}
        </div>
    );
    const estadoDe = (p: (typeof pasos)[number]) => {
        const bloqueado = p.paso === "cierre" && !listaParaFirmar && orden.estado !== "cerrada";
        const hecho = p.paso === "cierre" ? orden.estado === "cerrada" : p.completo;
        const Icono = p.paso === "cierre" ? (orden.estado === "cerrada" ? CircleCheck : bloqueado ? Lock : PenLine) : p.completo ? CircleCheck : CircleDashed;
        const estadoTexto =
            p.paso === "cierre" ? (orden.estado === "cerrada" ? "hecho" : bloqueado ? "bloqueado" : "listo para firmar") : p.completo ? (p.opcional ? "opcional" : "listo") : `faltan ${p.faltan.length}`;
        return { bloqueado, hecho, Icono, estadoTexto };
    };

    if (compacto) {
        return (
            <nav aria-label="Pasos de la orden" className="grid gap-2.5">
                {barra}
                <ol className="campo-sin-barra -mx-[1.125rem] flex gap-1.5 overflow-x-auto px-[1.125rem] pb-0.5">
                    {pasos.map((p, i) => {
                        const { hecho, Icono, estadoTexto } = estadoDe(p);
                        const esActual = actual === p.paso;
                        return (
                            <li key={p.paso} className="shrink-0">
                                <a
                                    ref={esActual ? actualRef : undefined}
                                    href={urlDeVista({ tipo: "orden", id: orden.id, paso: p.paso })}
                                    onClick={ir(p.paso)}
                                    aria-current={esActual ? "step" : undefined}
                                    aria-label={`${i + 1}. ${p.titulo}: ${estadoTexto}`}
                                    data-paso={p.paso}
                                    className={cn(
                                        "inline-flex h-11 items-center gap-1.5 rounded-full px-3 text-[0.8125rem] whitespace-nowrap focus-visible:outline-2 focus-visible:outline-offset-2",
                                        esActual
                                            ? "bg-[var(--campo-tinta)] font-semibold text-[var(--campo-sobre-tinta)]"
                                            : hecho
                                              ? "bg-[var(--campo-marca-suave)] font-semibold text-[var(--campo-marca-suave-texto)]"
                                              : "border border-border bg-[var(--campo-panel)] font-medium text-muted-foreground",
                                    )}
                                >
                                    {esActual ? null : <Icono aria-hidden className="size-4 shrink-0" />}
                                    <span aria-hidden>{esActual ? `${i + 1} · ${CORTO_PASO[p.paso]}` : CORTO_PASO[p.paso]}</span>
                                </a>
                            </li>
                        );
                    })}
                </ol>
            </nav>
        );
    }

    return (
        <nav aria-label="Pasos de la orden" className="surface-card grid gap-2 rounded-card p-4">
            <div className="flex items-center justify-between gap-2 text-sm">
                <span className="font-semibold">Trabajo en la orden</span>
                <span className="text-muted-foreground tabular-nums">{texto}</span>
            </div>
            {barra}
            <ol className="grid gap-1">
                {pasos.map((p, i) => {
                    const { bloqueado, Icono, estadoTexto } = estadoDe(p);
                    const tono = p.completo && p.paso !== "cierre" ? "text-success" : p.paso === "cierre" && orden.estado === "cerrada" ? "text-success" : "text-muted-foreground";
                    return (
                        <li key={p.paso}>
                            <a
                                href={urlDeVista({ tipo: "orden", id: orden.id, paso: p.paso })}
                                onClick={ir(p.paso)}
                                aria-current={actual === p.paso ? "step" : undefined}
                                aria-label={`${i + 1}. ${p.titulo}: ${estadoTexto}`}
                                data-paso={p.paso}
                                className="flex min-h-11 items-center gap-2 rounded-xl px-2 text-sm transition-colors hover:bg-[var(--campo-panel-2)] focus-visible:outline-2 focus-visible:outline-offset-2"
                            >
                                <Icono aria-hidden className={cn("size-5 shrink-0", tono)} />
                                <span className="flex-1">{p.titulo}</span>
                                <span className="text-xs text-muted-foreground">
                                    {p.paso === "cierre"
                                        ? orden.estado === "cerrada"
                                            ? "Hecho"
                                            : bloqueado
                                              ? "Al terminar lo demás"
                                              : "Listo para firmar"
                                        : p.completo
                                          ? p.opcional
                                              ? "Opcional"
                                              : "Listo"
                                          : `Falta${p.faltan.length === 1 ? "" : "n"} ${p.faltan.length}`}
                                </span>
                            </a>
                        </li>
                    );
                })}
            </ol>
        </nav>
    );
}

function AvisosOrden({ orden }: { orden: OrdenVista }) {
    const { motor, navegar, ejecutar, estado } = useCampo();
    const cierreRetenido = orden.subidasPendientes > 0 && estado.cola.some((op) => op.ordenId === orden.id && op.tipo === "orden.cerrar");
    return (
        <>
            {orden.reasignada ? (
                <Aviso
                    tono="warning"
                    titulo="Esta orden se asignó a otro técnico"
                    accion={
                        orden.pendientes + orden.rechazos > 0 ? (
                            <Boton tamano="lg" variante="secundario" onClick={() => void ejecutar(() => motor.descartarOrden(orden.id), "Se descartó lo pendiente")}>
                                Descartar lo pendiente
                            </Boton>
                        ) : undefined
                    }
                >
                    Lo que ya se envió queda registrado; lo demás ya no se puede enviar.
                </Aviso>
            ) : null}
            {orden.estado === "cancelada" ? (
                <Aviso tono="neutral" titulo="Despacho canceló esta orden">
                    Solo puedes agregar notas y fotos como evidencia.
                </Aviso>
            ) : null}
            {orden.rechazos > 0 ? (
                <Aviso
                    tono="error"
                    titulo={`${orden.rechazos} ${orden.rechazos === 1 ? "acción rechazada" : "acciones rechazadas"} por el servidor`}
                    accion={
                        <Boton tamano="lg" variante="secundario" onClick={() => navegar({ tipo: "pendientes" })}>
                            Revisar
                        </Boton>
                    }
                >
                    {orden.bloqueada ? "Lo que hiciste después espera a que decidas qué hacer con el rechazo." : null}
                </Aviso>
            ) : null}
            {cierreRetenido ? (
                <Aviso tono="info" titulo="El cierre se enviará cuando terminen de subir las fotos y la firma">
                    Quedan {orden.subidasPendientes} {orden.subidasPendientes === 1 ? "archivo" : "archivos"} por subir.
                </Aviso>
            ) : null}
            {estado.avisos
                .filter((a) => a.ordenId === orden.id)
                .map((a) => (
                    <Aviso
                        key={a.id}
                        tono="info"
                        titulo={`${OPERACIONES[a.tipo]}: el servidor lo guardó con un ajuste`}
                        accion={
                            <Boton tamano="lg" variante="secundario" onClick={() => void motor.descartarAviso(a.id)}>
                                Entendido
                            </Boton>
                        }
                    >
                        {AVISOS_SERVIDOR[a.aviso] ?? a.aviso}
                    </Aviso>
                ))}
            {orden.esperandoAdjuntos ? (
                <Aviso tono="info" titulo="El cierre espera a que suban las fotos o la firma">
                    Se envía solo cuando terminen de subir.
                </Aviso>
            ) : null}
        </>
    );
}

function Cliente({ orden, nombre }: { orden: OrdenVista; nombre: string }) {
    // Mismo formato que el panel (+502 5512 3344): los de Argos ya vienen normalizados; el
    // contacto capturado en la orden se normaliza igual. Primero el contacto en sitio de la orden.
    const deArgos = orden.cliente ? (orden.cliente.telefonos ?? telefonos("telefono", orden.cliente.telefono)) : [];
    const lista = [...telefonos("telefono", orden.contacto.telefono), ...deArgos].filter((t, i, todos) => todos.findIndex((x) => (x.e164 ?? x.original) === (t.e164 ?? t.original)) === i);
    const paraLlamar = orden.contacto.telefono ? (lista[0] ?? null) : principal(lista);
    const llamar = paraLlamar ? enlaceTel(paraLlamar) : null;
    const ahora = useAhora(60_000);
    const lat = orden.lat ?? orden.cliente?.lat ?? null;
    const lon = orden.lon ?? orden.cliente?.lon ?? null;
    const comoLlegar = enlaceComoLlegar(lat, lon, orden.direccion);
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>{nombre}</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3 text-sm">
                {orden.direccion ? (
                    <p className="flex items-start gap-2">
                        <MapPin aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                        {orden.direccion}
                    </p>
                ) : null}
                {orden.servicio ? (
                    <p className="text-muted-foreground">
                        Servicio {orden.servicio.codigo || `#${orden.servicio.id}`}
                        {orden.servicio.plan ? ` · ${orden.servicio.plan}` : ""}
                        {orden.servicio.estado ? ` · ${orden.servicio.estado.replaceAll("_", " ")}` : ""}
                    </p>
                ) : null}
                {orden.descripcion ? <p className="whitespace-pre-wrap">{orden.descripcion}</p> : null}
                <div className="grid grid-cols-2 gap-2">
                    {llamar ? (
                        <Boton asChild variante="secundario" tamano="lg">
                            <a href={llamar}>
                                <Phone aria-hidden />
                                Llamar
                            </a>
                        </Boton>
                    ) : null}
                    {comoLlegar ? (
                        <Boton asChild variante="secundario" tamano="lg">
                            <a href={comoLlegar} target="_blank" rel="noopener noreferrer">
                                <Route aria-hidden />
                                Cómo llegar
                            </a>
                        </Boton>
                    ) : null}
                </div>
                <AbrirChatOrden ordenId={orden.id} />
                {lista.length > 0 ? <ListaTelefonos telefonos={lista} /> : null}
                {orden.cliente ? (
                    <p className="text-xs text-muted-foreground" title={formatearFecha(orden.cliente.sincronizadoEn)}>
                        Datos de Argos sincronizados {haceTiempo(orden.cliente.sincronizadoEn, ahora)}
                    </p>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function AccionesEstado({ orden, camino, llegar }: { orden: OrdenVista; camino: boolean; llegar: boolean }) {
    const { motor, ejecutar } = useCampo();
    const ubicacion = useUbicacion();
    const [ocupado, setOcupado] = useState<null | "camino" | "llegar">(null);
    const correr = async (cual: "camino" | "llegar") => {
        setOcupado(cual);
        // Con GPS si el técnico lo permite (explicado antes de que el teléfono pregunte); sin él,
        // la acción se registra igual en la cola sin conexión.
        const p = await ubicacion.posicionParaAccion();
        const datos = p ? { lat: p.lat, lon: p.lon, precision: p.precision } : {};
        // Salir hacia el cliente o llegar es estar en turno: empieza la ubicación en vivo, con aviso
        // (solo si el técnico aceptó la explicación y dio el permiso).
        const empiezaTurno = ubicacion.turnoEmpezariaSolo();
        const texto =
            cual === "camino" ? (p ? "En camino, con ubicación" : "En camino, sin ubicación") : p ? "Llegada registrada con ubicación" : "Llegada registrada sin ubicación";
        const guardada = await ejecutar(
            () => motor.registrar(cual === "camino" ? "orden.iniciar_camino" : "orden.llegar", orden.id, datos),
            empiezaTurno ? `${texto}. ${AVISO_TURNO_INICIADO}` : texto,
        );
        if (guardada) {
            // Fuera de turno lo empieza (si puede); en turno, lo mantiene hasta la última orden.
            ubicacion.iniciarTurnoSolo();
        }
        setOcupado(null);
    };
    return (
        <div className="grid grid-cols-2 gap-2">
            {camino ? (
                <Boton tamano="lg" variante={llegar ? "secundario" : "primario"} disabled={ocupado !== null} onClick={() => void correr("camino")}>
                    <Navigation aria-hidden />
                    {ocupado === "camino" ? "Ubicando…" : "En camino"}
                </Boton>
            ) : null}
            {llegar ? (
                <Boton tamano="lg" className={camino ? undefined : "col-span-2"} disabled={ocupado !== null} onClick={() => void correr("llegar")}>
                    <MapPinCheck aria-hidden />
                    {ocupado === "llegar" ? "Ubicando…" : "Llegué"}
                </Boton>
            ) : null}
        </div>
    );
}

const MOTIVOS_COMODATO: Record<string, string> = {
    sin_conector: "Iris no está conectado a Argos.",
    argos_no_disponible: "Argos no respondió al bajar el paquete.",
    sin_servicio: "La orden no tiene servicio asociado.",
};

function Comodato({ orden }: { orden: OrdenVista }) {
    const equipos = orden.comodato.equipos ?? [];
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Equipos en comodato</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="text-sm">
                {!orden.comodato.disponible ? (
                    <p className="text-muted-foreground">No disponible: {MOTIVOS_COMODATO[orden.comodato.motivo ?? ""] ?? "sin datos de Argos."}</p>
                ) : equipos.length === 0 ? (
                    <p className="text-muted-foreground">El cliente no tiene equipos en comodato.</p>
                ) : (
                    <ul className="grid gap-1">
                        {equipos.map((e) => (
                            <li key={e.unidadId} className="flex flex-wrap gap-x-2">
                                <span className="font-medium">{e.modelo}</span>
                                <span className="font-mono">{e.serie}</span>
                                {e.mac ? <span className="font-mono text-muted-foreground">{e.mac}</span> : null}
                            </li>
                        ))}
                    </ul>
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}
