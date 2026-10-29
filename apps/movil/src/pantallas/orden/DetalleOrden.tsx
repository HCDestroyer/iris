// Detalle de una orden con todas las acciones (las mismas reglas que la PWA: `accionesDe`).
import type { OrdenVista } from "@iris/sync";
import { accionesDe, AVISOS_SERVIDOR, horaCorta, OPERACIONES, TIPOS_ORDEN, ventanaCita } from "@iris/sync/campo";
import { ArrowLeft, FileQuestion, MapPin, MapPinCheck, Navigation, Phone, Route } from "lucide-react-native";
import { useState } from "react";
import { Linking, StyleSheet, View } from "react-native";
import { Aviso, Boton, Pila, Tarjeta, Texto, Titulo } from "../../componentes";
import { useCampo, useOrden } from "../../campo/contexto";
import { useTema } from "../../tema/tema";
import { InsigniaOrden } from "../ListaOrdenes";
import { Checklist } from "./Checklist";
import { CerrarOrden, FallarOrden } from "./Cierre";
import { Equipos } from "./Equipos";
import { Firma } from "./Firma";
import { FotosOrden } from "./Fotos";
import { Materiales } from "./Materiales";
import { Notas } from "./Notas";
import { PausaOrden } from "./Pausa";

/** Enlace a la app de mapas con destino en las coordenadas (o la dirección). */
export function enlaceComoLlegar(lat: number | null, lon: number | null, direccion: string): string | null {
    if (lat !== null && lon !== null) {
        return `https://www.google.com/maps/dir/?api=1&destination=${lat},${lon}`;
    }
    const d = direccion.trim();
    return d ? `https://www.google.com/maps/dir/?api=1&destination=${encodeURIComponent(d)}` : null;
}

export function DetalleOrden({ id }: { id: number }) {
    const { navegar } = useCampo();
    const orden = useOrden(id);
    const volver = (
        <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "lista" })} estilo={estilos.volver}>
            Mis órdenes
        </Boton>
    );
    if (!orden) {
        return (
            <Pila>
                {volver}
                <Aviso tono="neutral" icono={FileQuestion} titulo="La orden ya no está en el teléfono">
                    Puede que se haya reasignado o que no sea de hoy.
                </Aviso>
            </Pila>
        );
    }
    const acciones = accionesDe(orden);
    const inicio = orden.cita?.inicio ?? orden.programadaInicio;
    const fin = orden.cita?.fin ?? orden.programadaFin;
    const ventana = ventanaCita(orden.cita);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";

    return (
        <Pila espacio={14}>
            {volver}
            <View style={estilos.encabezado}>
                <Titulo>{orden.numero}</Titulo>
                <InsigniaOrden estado={orden.estado} />
            </View>
            <Texto suave>
                {TIPOS_ORDEN[orden.tipo]}
                {inicio ? ` · cita ${horaCorta(inicio) ?? ""}${fin ? `–${horaCorta(fin) ?? ""}` : ""}` : ""}
                {ventana ? ` · llegada acordada ${ventana}` : ""}
            </Texto>

            <AvisosOrden orden={orden} />
            <Cliente orden={orden} nombre={nombre} />
            {acciones.iniciarCamino || acciones.llegar ? <AccionesEstado orden={orden} camino={acciones.iniciarCamino} llegar={acciones.llegar} /> : null}
            <PausaOrden orden={orden} pausar={acciones.pausar} reanudar={acciones.reanudar} />
            {orden.estado === "pendiente" ? (
                <Aviso tono="neutral" titulo="Esperando programación">
                    Despacho todavía no le dio hora a esta orden.
                </Aviso>
            ) : null}
            {acciones.trabajo || acciones.agregarFormulario || (orden.checklist ?? []).some((i) => i.valor !== null) ? (
                <Checklist orden={orden} editable={acciones.trabajo} agregar={acciones.agregarFormulario} />
            ) : null}
            {acciones.trabajo || (orden.materiales ?? []).length > 0 ? <Materiales orden={orden} editable={acciones.trabajo} /> : null}
            <Equipos orden={orden} instalar={acciones.instalar} retirar={acciones.retirar} />
            {acciones.trabajo || orden.firma ? <Firma orden={orden} editable={acciones.trabajo} /> : null}
            {acciones.notas ? <FotosOrden orden={orden} /> : null}
            <Comodato orden={orden} />
            <Notas orden={orden} editable={acciones.notas} />
            {acciones.cerrar || acciones.fallar ? (
                <View style={estilos.cierre}>
                    {acciones.fallar ? (
                        <View style={estilos.flex}>
                            <FallarOrden orden={orden} />
                        </View>
                    ) : null}
                    {acciones.cerrar ? (
                        <View style={estilos.flex}>
                            <CerrarOrden orden={orden} />
                        </View>
                    ) : null}
                </View>
            ) : null}
        </Pila>
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
                            <Boton variante="secundario" alPresionar={() => void ejecutar(() => motor.descartarOrden(orden.id), "Se descartó lo pendiente")}>
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
                    testID="aviso-rechazos"
                    accion={
                        <Boton variante="secundario" alPresionar={() => navegar({ tipo: "pendientes" })}>
                            Revisar
                        </Boton>
                    }
                >
                    {orden.bloqueada ? "Lo que hiciste después espera a que decidas qué hacer con el rechazo." : undefined}
                </Aviso>
            ) : null}
            {cierreRetenido ? (
                <Aviso tono="info" titulo="El cierre se enviará cuando terminen de subir las fotos y la firma">
                    {`Quedan ${orden.subidasPendientes} ${orden.subidasPendientes === 1 ? "archivo" : "archivos"} por subir.`}
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
                            <Boton variante="secundario" alPresionar={() => void motor.descartarAviso(a.id)}>
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
    const { colores } = useTema();
    const telefono = orden.cliente?.telefono || orden.contacto.telefono;
    const lat = orden.lat ?? orden.cliente?.lat ?? null;
    const lon = orden.lon ?? orden.cliente?.lon ?? null;
    const comoLlegar = enlaceComoLlegar(lat, lon, orden.direccion);
    return (
        <Tarjeta titulo={nombre}>
            {orden.direccion ? (
                <View style={estilos.fila}>
                    <MapPin size={16} color={colores.textoSuave} />
                    <Texto>{orden.direccion}</Texto>
                </View>
            ) : null}
            {orden.servicio ? (
                <Texto suave>
                    Servicio {orden.servicio.codigo}
                    {orden.servicio.plan ? ` · ${orden.servicio.plan}` : ""}
                    {orden.servicio.estado ? ` · ${orden.servicio.estado.replaceAll("_", " ")}` : ""}
                </Texto>
            ) : null}
            {orden.descripcion ? <Texto>{orden.descripcion}</Texto> : null}
            <View style={estilos.fila}>
                {telefono ? (
                    <View style={estilos.flex}>
                        <Boton variante="secundario" icono={Phone} alPresionar={() => void Linking.openURL(`tel:${telefono.replace(/[^\d+]/g, "")}`)}>
                            Llamar
                        </Boton>
                    </View>
                ) : null}
                {comoLlegar ? (
                    <View style={estilos.flex}>
                        <Boton variante="secundario" icono={Route} alPresionar={() => void Linking.openURL(comoLlegar)}>
                            Cómo llegar
                        </Boton>
                    </View>
                ) : null}
            </View>
            {telefono ? <Texto suave>Teléfono: {telefono}</Texto> : null}
        </Tarjeta>
    );
}

function AccionesEstado({ orden, camino, llegar }: { orden: OrdenVista; camino: boolean; llegar: boolean }) {
    const { motor, ejecutar, servicios, rastreo } = useCampo();
    const [ocupado, setOcupado] = useState<null | "camino" | "llegar">(null);
    const correr = async (cual: "camino" | "llegar") => {
        setOcupado(cual);
        // Con GPS si el permiso ya está dado; sin él, la acción se registra igual en la cola.
        const p = await servicios.posicion();
        const datos = p ? { lat: p.lat, lon: p.lon, precision: p.precision } : {};
        const texto = cual === "camino" ? (p ? "En camino, con ubicación" : "En camino, sin ubicación") : p ? "Llegada registrada con ubicación" : "Llegada registrada sin ubicación";
        if (await ejecutar(() => motor.registrar(cual === "camino" ? "orden.iniciar_camino" : "orden.llegar", orden.id, datos), texto)) {
            // Acción del propio técnico: levanta su «Dejar de compartir ubicación».
            void rastreo?.accionPropia();
        }
        setOcupado(null);
    };
    return (
        <View style={estilos.fila}>
            {camino ? (
                <View style={estilos.flex}>
                    <Boton variante={llegar ? "secundario" : "primario"} icono={Navigation} cargando={ocupado === "camino"} deshabilitado={ocupado !== null} alPresionar={() => void correr("camino")}>
                        {ocupado === "camino" ? "Ubicando…" : "En camino"}
                    </Boton>
                </View>
            ) : null}
            {llegar ? (
                <View style={estilos.flex}>
                    <Boton icono={MapPinCheck} cargando={ocupado === "llegar"} deshabilitado={ocupado !== null} alPresionar={() => void correr("llegar")}>
                        {ocupado === "llegar" ? "Ubicando…" : "Llegué"}
                    </Boton>
                </View>
            ) : null}
        </View>
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
        <Tarjeta titulo="Equipos en comodato">
            {!orden.comodato.disponible ? (
                <Texto suave>No disponible: {MOTIVOS_COMODATO[orden.comodato.motivo ?? ""] ?? "sin datos de Argos."}</Texto>
            ) : equipos.length === 0 ? (
                <Texto suave>El cliente no tiene equipos en comodato.</Texto>
            ) : (
                equipos.map((e) => (
                    <Texto key={e.unidadId}>
                        {e.modelo} · <Texto mono>{e.serie}</Texto>
                        {e.mac ? ` · ${e.mac}` : ""}
                    </Texto>
                ))
            )}
        </Tarjeta>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    volver: { alignSelf: "flex-start", paddingHorizontal: 0 },
    encabezado: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    fila: { flexDirection: "row", alignItems: "center", gap: 8 },
    cierre: { flexDirection: "row", gap: 8, paddingVertical: 8 },
});
