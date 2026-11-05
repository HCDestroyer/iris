"use client";

import type { OrdenCampo } from "@iris/sync";
import {
    citasDelDia,
    citasPorDia,
    mananaEnGuatemala,
    nombreCliente,
    resumenDia,
    textoFormularios,
    tiraSemana,
    tituloAgenda,
    tonoCita,
    urlDeVista,
    ventanaCita,
    type CitaAgenda,
} from "@iris/sync/campo";
import { Aviso, Boton, Cargando, EstadoVacio, cn } from "@iris/ui";
import { CalendarX, RefreshCw, WifiOff } from "lucide-react";
import type { MouseEvent } from "react";
import { horaCorta, hoyEnGuatemala } from "@/lib/campo/fechas";
import { TIPOS_ORDEN } from "@/lib/campo/textos";
import { useAgenda } from "./agenda-datos";
import { useCampo } from "./contexto";
import { ErrorLectura, useCargarAlAbrir } from "./inventario";

/** Órdenes para la agenda: hoy, las del teléfono (con lo hecho sin señal); los demás días, lo leído. */
function useOrdenesAgenda(): { ordenes: OrdenCampo[] | null; hoy: string } {
    const { estado } = useCampo();
    const { semana } = useAgenda();
    const hoy = hoyEnGuatemala();
    if (!semana.datos) {
        return { ordenes: null, hoy };
    }
    const locales = new Set(estado.ordenes.map((o) => o.id));
    return { ordenes: [...estado.ordenes, ...semana.datos.ordenes.filter((o) => !locales.has(o.id))], hoy };
}

function useLecturaSemana() {
    const { semana, cargarSemana } = useAgenda();
    useCargarAlAbrir(semana, cargarSemana);
    return { semana, cargarSemana };
}

/**
 * Sin la semana leída: el error con «Reintentar» o, sin señal y sin haberla leído nunca, que se
 * lee con señal (también con «Reintentar»); si no, que se está leyendo.
 */
function EstadoLectura({ sinDatos }: { sinDatos: boolean }) {
    const { semana, cargarSemana } = useAgenda();
    const { estado } = useCampo();
    if (semana.estado === "error") {
        return <ErrorLectura error={semana.error} que="tu agenda" reintentar={() => void cargarSemana()} conDatos={semana.datos !== null} />;
    }
    if (!sinDatos) {
        return null;
    }
    if (semana.estado === "inactiva" && estado.conexion === "sin_conexion") {
        return (
            <Aviso
                tono="warning"
                icono={WifiOff}
                titulo="Sin señal: tu agenda de los próximos días se lee con señal."
                accion={
                    <Boton variante="secundario" tamano="lg" onClick={() => void cargarSemana()}>
                        <RefreshCw aria-hidden />
                        Reintentar
                    </Boton>
                }
            />
        );
    }
    return <Cargando visible texto="Leyendo tu agenda…" />;
}

/**
 * Agenda de un día (por defecto, mañana) con la tira de la semana: el día elegido en teal y las
 * citas en bloques de color. Se lee con señal; sin leer no hay contadores ni citas inventadas.
 */
export function AgendaDia({ dia }: { dia: string | undefined }) {
    useLecturaSemana();
    const { ordenes, hoy } = useOrdenesAgenda();
    const fecha = dia ?? mananaEnGuatemala();
    const tira = tiraSemana(hoy);
    const citas = ordenes ? citasDelDia(ordenes, fecha) : null;
    return (
        <section aria-labelledby="titulo-agenda" className="grid gap-4" data-slot="agenda-dia">
            <div className="flex items-center justify-between gap-3">
                <h2 id="titulo-agenda" className="text-[1.625rem] font-bold tracking-[-0.02em]">
                    {tituloAgenda(fecha, hoy)}
                </h2>
                {citas ? (
                    <span className="inline-flex h-[1.875rem] shrink-0 items-center rounded-full border border-border bg-[var(--campo-panel)] px-3 text-[0.8125rem] font-medium" data-testid="resumen-dia">
                        {resumenDia(citas)}
                    </span>
                ) : null}
            </div>
            <TiraSemana elegido={fecha} dias={tira} cuentas={ordenes ? citasPorDia(ordenes, tira.map((d) => d.fecha)) : null} />
            <EstadoLectura sinDatos={!citas} />
            {!citas ? null : citas.length === 0 ? (
                <EstadoVacio icono={CalendarX} titulo="Sin citas ese día" descripcion="Si despacho te programa algo, aparecerá aquí." />
            ) : (
                <ListaCitas citas={citas} />
            )}
        </section>
    );
}

/** Los 7 días desde hoy, agrupados por día. */
export function AgendaSemana() {
    useLecturaSemana();
    const { ordenes, hoy } = useOrdenesAgenda();
    const tira = tiraSemana(hoy);
    return (
        <section aria-labelledby="titulo-semana" className="grid gap-4" data-slot="agenda-semana">
            <h2 id="titulo-semana" className="text-[1.625rem] font-bold tracking-[-0.02em]">
                Esta semana
            </h2>
            <TiraSemana elegido={null} dias={tira} cuentas={ordenes ? citasPorDia(ordenes, tira.map((d) => d.fecha)) : null} />
            <EstadoLectura sinDatos={!ordenes} />
            {!ordenes
                ? null
                : tira.map((d) => {
                    const citas = citasDelDia(ordenes, d.fecha);
                    return (
                        <section key={d.fecha} aria-label={d.largo} className="grid gap-2">
                            <h3 className="flex items-baseline justify-between gap-2 text-[0.8125rem] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
                                <span>{tituloAgenda(d.fecha, hoy)}</span>
                                <span className="font-medium tracking-normal normal-case">{resumenDia(citas)}</span>
                            </h3>
                            {citas.length > 0 ? <ListaCitas citas={citas} /> : null}
                        </section>
                    );
                })}
        </section>
    );
}

/** Tira de 7 días: el elegido en teal; un punto bajo el número si ese día tiene citas. */
function TiraSemana({ elegido, dias, cuentas }: { elegido: string | null; dias: ReturnType<typeof tiraSemana>; cuentas: Record<string, number> | null }) {
    const { navegar } = useCampo();
    return (
        <nav aria-label="Semana" className="grid grid-cols-7 gap-1.5">
            {dias.map((d) => {
                const activo = d.fecha === elegido;
                const n = cuentas?.[d.fecha];
                const vista = { tipo: "agenda" as const, dia: d.fecha };
                const ir = (e: MouseEvent<HTMLAnchorElement>) => {
                    e.preventDefault();
                    navegar(vista);
                };
                return (
                    <a
                        key={d.fecha}
                        href={urlDeVista(vista)}
                        onClick={ir}
                        aria-current={activo ? "date" : undefined}
                        aria-label={`${d.largo}${n === undefined ? "" : `: ${n === 0 ? "sin citas" : `${n} ${n === 1 ? "cita" : "citas"}`}`}`}
                        className={cn(
                            "grid h-14 place-content-center justify-items-center gap-0.5 rounded-[0.875rem] text-[0.6875rem] focus-visible:outline-2 focus-visible:outline-offset-2",
                            activo ? "bg-primary font-semibold text-primary-foreground" : "bg-[var(--campo-panel)] font-medium text-muted-foreground",
                        )}
                    >
                        <span aria-hidden>{d.corto}</span>
                        <b aria-hidden className={cn("text-base", activo ? "font-bold" : "font-semibold text-foreground")}>
                            {d.numero}
                        </b>
                        <span aria-hidden className={cn("size-1 rounded-full", n ? (activo ? "bg-primary-foreground" : "bg-primary") : "bg-transparent")} />
                    </a>
                );
            })}
        </nav>
    );
}

const BLOQUE: Record<ReturnType<typeof tonoCita>, string> = {
    marca: "bg-[var(--campo-tinta)] text-[var(--campo-sobre-tinta)] border-transparent",
    aviso: "bg-[var(--campo-aviso-fondo)] border-[var(--campo-aviso-borde)]",
    neutral: "bg-[var(--campo-panel)] border-border",
};

/** Citas con la hora a la izquierda y el bloque de color (en curso, sin formulario o normal). */
function ListaCitas({ citas }: { citas: CitaAgenda[] }) {
    const { navegar, estado } = useCampo();
    const enTelefono = new Set(estado.ordenes.map((o) => o.id));
    return (
        <ol className="grid grid-cols-[3.25rem_1fr] gap-x-2.5 gap-y-2.5">
            {citas.map(({ orden, inicio }) => {
                const tono = tonoCita(orden);
                const ventana = ventanaCita(orden.cita);
                const zona = orden.cliente?.direccion || orden.direccion;
                const contenido = (
                    <>
                        <span className="text-[0.9375rem] font-semibold">
                            {TIPOS_ORDEN[orden.tipo]} · {nombreCliente(orden)}
                        </span>
                        <span className={cn("text-[0.8125rem]", tono === "marca" ? "opacity-80" : tono === "aviso" ? "text-warning" : "text-muted-foreground")}>
                            {[zona, ventana ? `ventana ${ventana}` : null, textoFormularios(orden)].filter(Boolean).join(" · ")}
                        </span>
                    </>
                );
                const clases = cn("grid gap-1 rounded-[1.375rem] border p-3.5", BLOQUE[tono]);
                return (
                    <li key={orden.id} className="contents">
                        <span className="pt-3.5 text-[0.8125rem] font-semibold tabular-nums">{horaCorta(inicio)}</span>
                        {enTelefono.has(orden.id) ? (
                            // Hoy: la orden está en el teléfono y se abre (funciona sin señal).
                            <a
                                href={urlDeVista({ tipo: "orden", id: orden.id })}
                                onClick={(e) => {
                                    e.preventDefault();
                                    navegar({ tipo: "orden", id: orden.id });
                                }}
                                aria-label={`${orden.numero}, ${nombreCliente(orden)}`}
                                className={cn(clases, "focus-visible:outline-2 focus-visible:outline-offset-2")}
                            >
                                {contenido}
                            </a>
                        ) : (
                            <div className={clases} aria-label={`${orden.numero}, ${nombreCliente(orden)}`} role="group">
                                {contenido}
                            </div>
                        )}
                    </li>
                );
            })}
        </ol>
    );
}
