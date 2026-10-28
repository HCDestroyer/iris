"use client";

import type { OrdenVista } from "@iris/sync";
import { Aviso, Boton, EstadoVacio, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowLeft, FileQuestion, MapPin, MapPinCheck, Navigation, Phone, Route } from "lucide-react";
import { useState } from "react";
import { horaCorta, ventanaCita } from "@/lib/campo/fechas";
import { enlaceComoLlegar } from "@/lib/campo/gps";
import { accionesDe } from "@/lib/campo/reglas";
import { TIPOS_ORDEN } from "@/lib/campo/textos";
import { Checklist } from "./checklist";
import { CerrarOrden, FallarOrden } from "./cierre";
import { useCampo, useOrden } from "./contexto";
import { Equipos } from "./equipos";
import { Firma } from "./firma";
import { FotosOrden } from "./fotos";
import { InsigniaOrden } from "./insignias";
import { Materiales } from "./materiales";
import { Notas } from "./notas";
import { AVISO_TURNO_INICIADO, useUbicacion } from "./ubicacion";

export function DetalleOrden({ id }: { id: number }) {
    const { navegar } = useCampo();
    const orden = useOrden(id);
    const volver = (
        <Boton variante="fantasma" className="-ml-2 w-fit" onClick={() => navegar({ tipo: "lista" })}>
            <ArrowLeft aria-hidden />
            Mis órdenes
        </Boton>
    );
    if (!orden) {
        return (
            <div className="grid gap-4">
                {volver}
                <EstadoVacio icono={FileQuestion} titulo="La orden ya no está en el teléfono" descripcion="Puede que se haya reasignado o que no sea de hoy." />
            </div>
        );
    }
    const acciones = accionesDe(orden);
    const inicio = orden.cita?.inicio ?? orden.programadaInicio;
    const fin = orden.cita?.fin ?? orden.programadaFin;
    const ventana = ventanaCita(orden.cita);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";

    return (
        <article aria-labelledby="titulo-orden" className="grid gap-4">
            {volver}
            <header className="grid gap-2">
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
            </header>

            <AvisosOrden orden={orden} />
            <Cliente orden={orden} nombre={nombre} />
            {acciones.iniciarCamino || acciones.llegar ? <AccionesEstado orden={orden} camino={acciones.iniciarCamino} llegar={acciones.llegar} /> : null}
            {orden.estado === "pendiente" ? (
                <Aviso tono="neutral" titulo="Esperando programación">
                    Despacho todavía no le dio hora a esta orden.
                </Aviso>
            ) : null}

            {acciones.trabajo || (orden.checklist ?? []).some((i) => i.valor !== null) ? <Checklist orden={orden} editable={acciones.trabajo} /> : null}
            {acciones.trabajo || (orden.materiales ?? []).length > 0 ? <Materiales orden={orden} editable={acciones.trabajo} /> : null}
            <Equipos orden={orden} instalar={acciones.instalar} retirar={acciones.retirar} />
            {acciones.trabajo || orden.firma ? <Firma orden={orden} editable={acciones.trabajo} /> : null}
            {acciones.notas ? <FotosOrden orden={orden} /> : null}
            <Comodato orden={orden} />
            <Notas orden={orden} editable={acciones.notas} />

            {acciones.cerrar || acciones.fallar ? (
                <div className="glass-material sticky bottom-0 z-20 -mx-4 flex gap-2 border-t border-border px-4 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
                    {acciones.fallar ? <FallarOrden orden={orden} /> : null}
                    {acciones.cerrar ? <CerrarOrden orden={orden} /> : null}
                </div>
            ) : null}
        </article>
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
                            <Boton variante="secundario" onClick={() => void ejecutar(() => motor.descartarOrden(orden.id), "Se descartó lo pendiente")}>
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
                        <Boton variante="secundario" onClick={() => navegar({ tipo: "pendientes" })}>
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
            {orden.esperandoAdjuntos ? (
                <Aviso tono="info" titulo="El cierre espera a que suban las fotos o la firma">
                    Se envía solo cuando terminen de subir.
                </Aviso>
            ) : null}
        </>
    );
}

function Cliente({ orden, nombre }: { orden: OrdenVista; nombre: string }) {
    const telefono = orden.cliente?.telefono || orden.contacto.telefono;
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
                        Servicio {orden.servicio.codigo}
                        {orden.servicio.plan ? ` · ${orden.servicio.plan}` : ""}
                        {orden.servicio.estado ? ` · ${orden.servicio.estado.replaceAll("_", " ")}` : ""}
                    </p>
                ) : null}
                {orden.descripcion ? <p className="whitespace-pre-wrap">{orden.descripcion}</p> : null}
                <div className="grid grid-cols-2 gap-2">
                    {telefono ? (
                        <Boton asChild variante="secundario" tamano="lg">
                            <a href={`tel:${telefono.replace(/[^\d+]/g, "")}`}>
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
                {telefono ? <p className="text-muted-foreground">Teléfono: {telefono}</p> : null}
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
