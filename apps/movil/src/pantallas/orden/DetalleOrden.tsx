// La orden paso a paso, como la PWA (fase 4): resumen → «Empezar trabajo» → Llegada · Checklist ·
// Materiales y equipos · Fotos, cada paso en su propia vista (se guarda en el teléfono al tocar,
// con avance, «Anterior» / «Siguiente» y «Atrás» de Android) → «Firma del cliente y cierre», el
// paso final y único. Las reglas vienen de `@iris/sync/campo` y de `campo/pasos.ts`.
import type { OrdenVista } from "@iris/sync";
import { accionesDe, AVISOS_SERVIDOR, horaCorta, llego, OPERACIONES, TIPOS_ORDEN, TITULOS_PASO, ventanaCita, type PasoOrden } from "@iris/sync/campo";
import {
    ArrowLeft,
    ArrowRight,
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
    type LucideIcon,
} from "lucide-react-native";
import { useState } from "react";
import { ActivityIndicator, Linking, StyleSheet, View } from "react-native";
import { Aviso, Boton, Fila, Insignia, Pila, Tarjeta, Texto, Titulo, Tocable } from "../../componentes";
import { useCampo, useOrden } from "../../campo/contexto";
import { avanceOrden, botonTrabajo, navegacionPaso, type MarcaPaso } from "../../campo/pasos";
import { useTema } from "../../tema/tema";
import { InsigniaOrden } from "../ListaOrdenes";
import { Checklist } from "./Checklist";
import { FallarOrden, PasoCierre } from "./Cierre";
import { Equipos } from "./Equipos";
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

export function DetalleOrden({ id, paso }: { id: number; paso?: PasoOrden | undefined }) {
    const { navegar, estado } = useCampo();
    const orden = useOrden(id);
    if (!orden && estado.fecha === null && estado.ultimaSincronizacion === null) {
        return <ActivityIndicator accessibilityLabel="Cargando la orden" />;
    }
    if (!orden) {
        return (
            <Pila>
                <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "lista" })} estilo={estilos.volver}>
                    Mis órdenes
                </Boton>
                <Aviso tono="neutral" icono={FileQuestion} titulo="La orden ya no está en el teléfono">
                    Puede que se haya reasignado o que no sea de hoy.
                </Aviso>
            </Pila>
        );
    }
    return paso ? <PasoDeOrden orden={orden} paso={paso} /> : <ResumenOrden orden={orden} />;
}

/** Resumen: cliente, avisos, pausa, avance, notas, «No se pudo» y «Empezar trabajo» (o «Continuar»). */
function ResumenOrden({ orden }: { orden: OrdenVista }) {
    const { navegar } = useCampo();
    const acciones = accionesDe(orden);
    const inicio = orden.cita?.inicio ?? orden.programadaInicio;
    const fin = orden.cita?.fin ?? orden.programadaFin;
    const ventana = ventanaCita(orden.cita);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";
    const trabajable = acciones.iniciarCamino || acciones.llegar || acciones.trabajo;
    const trabajo = botonTrabajo(orden);

    return (
        <Pila espacio={14}>
            <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "lista" })} estilo={estilos.volver}>
                Mis órdenes
            </Boton>
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
            {orden.estado === "pendiente" ? (
                <Aviso tono="neutral" titulo="Esperando programación">
                    Despacho todavía no le dio hora a esta orden.
                </Aviso>
            ) : null}
            {trabajable ? (
                <Boton icono={llego(orden) ? ListChecks : Play} alPresionar={() => navegar(trabajo.vista)}>
                    {trabajo.texto}
                </Boton>
            ) : null}
            <PausaOrden orden={orden} pausar={acciones.pausar} reanudar={acciones.reanudar} />
            <Cliente orden={orden} nombre={nombre} />
            {orden.estado !== "pendiente" ? <ProgresoOrden orden={orden} /> : null}
            <Notas orden={orden} editable={acciones.notas} />
            {acciones.fallar ? <FallarOrden orden={orden} /> : null}
        </Pila>
    );
}

/** Un paso de trabajo: su contenido, el avance y «Anterior» / «Siguiente». Todo se guarda solo. */
function PasoDeOrden({ orden, paso }: { orden: OrdenVista; paso: PasoOrden }) {
    const { navegar } = useCampo();
    const acciones = accionesDe(orden);
    const nav = navegacionPaso(orden, paso);
    const estadoPaso = avanceOrden(orden).pasos.find((p) => p.paso === paso);
    const sinLlegar = !acciones.trabajo && !llego(orden);
    const irALlegada = sinLlegar ? (
        <Aviso
            tono="neutral"
            titulo="Marca tu llegada para llenar este paso"
            accion={
                <Boton variante="secundario" alPresionar={() => navegar({ tipo: "orden", id: orden.id, paso: "llegada" })}>
                    Ir a la llegada
                </Boton>
            }
        />
    ) : null;

    return (
        <Pila espacio={14}>
            <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "orden", id: orden.id })} estilo={estilos.volver}>
                {`Resumen de ${orden.numero}`}
            </Boton>
            <View style={estilos.encabezado}>
                <Texto suave>
                    <Texto mono suave>
                        {orden.numero}
                    </Texto>
                    {` · ${nav.posicion}`}
                </Texto>
                <InsigniaOrden estado={orden.estado} />
            </View>
            <View style={estilos.encabezado}>
                <Titulo>{TITULOS_PASO[paso]}</Titulo>
                {estadoPaso && paso !== "cierre" ? (
                    <Insignia
                        tono={estadoPaso.marca === "falta" ? "warning" : "success"}
                        icono={estadoPaso.marca === "falta" ? CircleDashed : CircleCheck}
                        texto={estadoPaso.detalle}
                        testID="estado-paso"
                    />
                ) : null}
            </View>
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
                            {orden.enSitioEn ? `Llegada registrada a las ${horaCorta(orden.enSitioEn) ?? ""}.` : "Llegada registrada."}
                        </Aviso>
                    ) : null}
                    {acciones.iniciarCamino || acciones.llegar ? <AccionesEstado orden={orden} camino={acciones.iniciarCamino} llegar={acciones.llegar} /> : null}
                    <PausaOrden orden={orden} pausar={acciones.pausar} reanudar={acciones.reanudar} />
                </>
            ) : null}
            {paso === "checklist" ? (
                <>
                    {irALlegada}
                    {acciones.trabajo || acciones.agregarFormulario || (orden.checklist ?? []).some((i) => i.valor !== null) ? (
                        <Checklist orden={orden} editable={acciones.trabajo} agregar={acciones.agregarFormulario} />
                    ) : null}
                </>
            ) : null}
            {paso === "materiales" ? (
                <>
                    {irALlegada}
                    {acciones.trabajo || (orden.materiales ?? []).length > 0 ? <Materiales orden={orden} editable={acciones.trabajo} /> : null}
                    <Equipos orden={orden} instalar={acciones.instalar} retirar={acciones.retirar} />
                    <Comodato orden={orden} />
                </>
            ) : null}
            {paso === "fotos" ? acciones.notas ? <FotosOrden orden={orden} /> : <Texto suave>Esta orden ya no admite fotos.</Texto> : null}
            {paso === "cierre" ? <PasoCierre orden={orden} /> : null}

            {paso !== "cierre" ? (
                <View accessibilityLabel="Pasos" style={estilos.pie}>
                    <View style={estilos.flex}>
                        <Boton variante="secundario" icono={ArrowLeft} alPresionar={() => navegar(nav.anterior.vista)}>
                            {nav.anterior.texto}
                        </Boton>
                    </View>
                    {nav.siguiente ? (
                        <View style={estilos.flex}>
                            <Boton icono={ArrowRight} alPresionar={() => nav.siguiente && navegar(nav.siguiente.vista)}>
                                {nav.siguiente.texto}
                            </Boton>
                        </View>
                    ) : null}
                </View>
            ) : null}
        </Pila>
    );
}

const ICONO_MARCA: Record<MarcaPaso, LucideIcon> = {
    listo: CircleCheck,
    opcional: CircleCheck,
    hecho: CircleCheck,
    falta: CircleDashed,
    bloqueado: Lock,
    para_firmar: PenLine,
};

/**
 * Avance de la orden: barra con los pasos listos y cada paso como botón (se puede volver a
 * cualquiera). «Firma del cliente y cierre» queda con candado hasta que no falte nada.
 */
function ProgresoOrden({ orden, actual, compacto = false }: { orden: OrdenVista; actual?: PasoOrden; compacto?: boolean }) {
    const { navegar } = useCampo();
    const { colores, radio } = useTema();
    const avance = avanceOrden(orden);
    const contenido = (
        <>
            <Fila entre>
                <Texto negrita>{compacto ? "Avance" : "Trabajo en la orden"}</Texto>
                <Texto suave testID="avance-orden">
                    {avance.texto}
                </Texto>
            </Fila>
            <View
                accessibilityRole="progressbar"
                accessibilityLabel="Avance de la orden"
                accessibilityValue={{ min: 0, max: avance.total, now: avance.listos, text: avance.texto }}
                style={[estilos.barra, { backgroundColor: colores.fondoElevado }]}
            >
                <View style={[estilos.relleno, { backgroundColor: colores.primario, width: `${Math.round((avance.listos / avance.total) * 100)}%` }]} />
            </View>
            <View style={compacto ? estilos.pasosCompactos : estilos.pasos}>
                {avance.pasos.map((p) => {
                    const Icono = ICONO_MARCA[p.marca];
                    const esActual = actual === p.paso;
                    const color = esActual ? colores.primario : p.marca === "listo" || p.marca === "hecho" ? colores.tono.success : colores.textoSuave;
                    return (
                        <Tocable
                            key={p.paso}
                            etiqueta={p.etiqueta}
                            seleccionado={esActual}
                            alPresionar={() => navegar({ tipo: "orden", id: orden.id, paso: p.paso })}
                            estilo={[compacto ? estilos.pasoCompacto : estilos.paso, { borderRadius: radio }, esActual ? { backgroundColor: colores.fondoTono.info } : null]}
                        >
                            <Icono size={20} color={color} />
                            {compacto ? (
                                <Texto suave>{String(p.numero)}</Texto>
                            ) : (
                                <>
                                    <Texto estilo={estilos.flex}>{p.titulo}</Texto>
                                    <Texto suave>{p.detalle}</Texto>
                                </>
                            )}
                        </Tocable>
                    );
                })}
            </View>
        </>
    );
    return compacto ? <View style={estilos.progreso}>{contenido}</View> : <Tarjeta>{contenido}</Tarjeta>;
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
    pie: { flexDirection: "row", gap: 8, paddingTop: 8 },
    progreso: { gap: 8 },
    barra: { height: 8, borderRadius: 999, overflow: "hidden" },
    relleno: { height: "100%", borderRadius: 999 },
    pasos: { gap: 4 },
    pasosCompactos: { flexDirection: "row", gap: 4 },
    paso: { minHeight: 44, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 8 },
    pasoCompacto: { flex: 1, minHeight: 48, alignItems: "center", justifyContent: "center", gap: 2 },
});
