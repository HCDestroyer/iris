// La orden paso a paso, como la PWA (fase 4): resumen → «Empezar trabajo» → Llegada · Checklist ·
// Materiales y equipos · Fotos, cada paso en su propia vista (se guarda en el teléfono al tocar,
// con avance, «Anterior» / «Siguiente» y «Atrás» de Android) → «Firma del cliente y cierre», el
// paso final y único. Las reglas vienen de `@iris/sync/campo` y de `campo/pasos.ts`.
import type { OrdenVista } from "@iris/sync";
import { accionesDe, AVISOS_SERVIDOR, horaCorta, llego, OPERACIONES, puedeReanudar, TITULOS_PASO, type PasoOrden } from "@iris/sync/campo";
import {
    ArrowLeft,
    CircleCheck,
    CircleDashed,
    FileQuestion,
    Lock,
    MapPin,
    MapPinCheck,
    Navigation,
    PenLine,
    Phone,
    Route,
    type LucideIcon,
} from "lucide-react-native";
import { useState } from "react";
import { Linking, ScrollView, StyleSheet, Text, View } from "react-native";
import { Aviso, Boton, BotonRedondo, Cargando, Fila, Insignia, Pila, Tarjeta, Texto, Titulo, Tocable } from "../../componentes";
import { useCampo, useOrden } from "../../campo/contexto";
import { avanceOrden, navegacionPaso, type MarcaPaso } from "../../campo/pasos";
import { useTema } from "../../tema/tema";
import { AbrirChatOrden } from "../Chat";
import { InsigniaOrden } from "../ListaOrdenes";
import { Checklist } from "./Checklist";
import { PasoCierre } from "./Cierre";
import { Equipos } from "./Equipos";
import { FotosOrden } from "./Fotos";
import { Materiales } from "./Materiales";
import { AccionesPrincipales, EncabezadoOrden } from "./AccionesOrden";
import { Notas } from "./Notas";
import { PausaOrden, TiempoPausado } from "./Pausa";

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
        return <Cargando etiqueta="Cargando la orden" />;
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

/**
 * Resumen (decisión del PM): ← | número y «● Estado · Tipo · ventana» | «⋯»; la acción principal en
 * «tinta» con [Pausar | No se pudo] debajo (o el aviso de pausa y «Reanudar»), el cliente con
 * «Cómo llegar» y «Llamar», el avance y las notas.
 */
function ResumenOrden({ orden }: { orden: OrdenVista }) {
    const { navegar } = useCampo();
    const acciones = accionesDe(orden);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";
    const [agregandoNota, setAgregandoNota] = useState(false);

    return (
        <Pila espacio={14}>
            <EncabezadoOrden orden={orden} alVolver={() => navegar({ tipo: "lista" })} alAgregarNota={acciones.notas ? () => setAgregandoNota(true) : null} />
            <AvisosOrden orden={orden} />
            {orden.estado === "pendiente" ? (
                <Aviso tono="neutral" titulo="Esperando programación">
                    Despacho todavía no le dio hora a esta orden.
                </Aviso>
            ) : null}
            <AccionesPrincipales orden={orden} />
            {acciones.enPausa ? null : <TiempoPausado orden={orden} />}
            <Cliente orden={orden} nombre={nombre} />
            {orden.estado !== "pendiente" ? <ProgresoOrden orden={orden} /> : null}
            <Notas orden={orden} editable={acciones.notas} agregando={agregandoNota} alAgregar={setAgregandoNota} />
        </Pila>
    );
}

/**
 * Un paso de trabajo: su contenido y el avance. «Anterior» y «Siguiente» (o «Guardar y seguir») van en la isla
 * de pasos (`IslaPasos`, fuera del desplazamiento). Con la orden en pausa, todo de solo lectura con
 * el aviso «Orden en pausa» y «Reanudar» arriba.
 */
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
            <View style={estilos.encabezadoPaso}>
                <BotonVolver etiqueta={`Resumen de ${orden.numero}`} alPresionar={() => navegar({ tipo: "orden", id: orden.id })} />
                <View style={[estilos.flex, estilos.titulosPaso]}>
                    <View style={estilos.encabezado}>
                        <Texto suave>
                            <Texto mono suave>
                                {orden.numero}
                            </Texto>
                            {` · ${nav.posicion}`}
                        </Texto>
                        <InsigniaOrden estado={orden.estado} />
                    </View>
                    <Titulo>{TITULOS_PASO[paso]}</Titulo>
                </View>
            </View>
            <View style={estilos.encabezado}>
                {estadoPaso && paso !== "cierre" ? (
                    <Insignia
                        tono={estadoPaso.marca === "falta" ? "warning" : "success"}
                        icono={estadoPaso.marca === "falta" ? CircleDashed : CircleCheck}
                        texto={estadoPaso.detalle}
                        testID="estado-paso"
                    />
                ) : null}
            </View>
            {acciones.enPausa ? <AvisoEnPausa orden={orden} /> : null}
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
            {paso === "fotos" ? acciones.notas ? <FotosOrden orden={orden} editable={!acciones.enPausa} /> : <Texto suave>Esta orden ya no admite fotos.</Texto> : null}
            {paso === "cierre" ? <PasoCierre orden={orden} /> : null}

        </Pila>
    );
}

/** Aviso arriba de cada paso con la orden en pausa: nada se registra hasta reanudar. */
function AvisoEnPausa({ orden }: { orden: OrdenVista }) {
    const { sesion } = useCampo();
    return <PausaOrden orden={orden} pausar={false} reanudar puedeReanudar={puedeReanudar(sesion.permisos)} enPasos />;
}

const ICONO_MARCA: Record<MarcaPaso, LucideIcon> = {
    listo: CircleCheck,
    opcional: CircleCheck,
    hecho: CircleCheck,
    falta: CircleDashed,
    bloqueado: Lock,
    para_firmar: PenLine,
};

/** Botón redondo de 48 px para volver (al resumen o a «Mis órdenes»). */
function BotonVolver({ etiqueta, alPresionar }: { etiqueta: string; alPresionar: () => void }) {
    const { campo, colores } = useTema();
    return <BotonRedondo icono={ArrowLeft} etiqueta={etiqueta} fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={alPresionar} />;
}

/** Texto corto de cada paso en los chips (el nombre accesible lleva el título completo). */
const CORTO_PASO: Record<PasoOrden, string> = { llegada: "Llegada", checklist: "Checklist", materiales: "Materiales", fotos: "Fotos", cierre: "Firma" };

/**
 * Avance de la orden: los pasos listos (barra por pasos) y cada paso como botón (se puede volver a
 * cualquiera). «Firma del cliente y cierre» queda con candado hasta que no falte nada. En el resumen
 * es una tarjeta con la lista; dentro de un paso, chips (listo en marca suave, el actual en tinta).
 */
function ProgresoOrden({ orden, actual, compacto = false }: { orden: OrdenVista; actual?: PasoOrden; compacto?: boolean }) {
    const { navegar } = useCampo();
    const { campo, colores } = useTema();
    const avance = avanceOrden(orden);
    const barra = (
        <View
            accessibilityRole="progressbar"
            accessibilityLabel="Avance de la orden"
            accessibilityValue={{ min: 0, max: avance.total, now: avance.listos, text: avance.texto }}
            style={estilos.barra}
        >
            {Array.from({ length: avance.total }, (_, i) => (
                <View key={i} style={[estilos.segmento, { backgroundColor: i < avance.listos ? colores.primario : campo.anillo }]} />
            ))}
        </View>
    );
    if (compacto) {
        return (
            <View style={estilos.progreso}>
                <Fila entre>
                    <Texto negrita>Avance</Texto>
                    <Texto suave testID="avance-orden">
                        {avance.texto}
                    </Texto>
                </Fila>
                {barra}
                <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={estilos.chips}>
                    {avance.pasos.map((p) => {
                        const Icono = ICONO_MARCA[p.marca];
                        const esActual = actual === p.paso;
                        const hecho = p.marca === "listo" || p.marca === "hecho" || p.marca === "opcional";
                        const fondo = esActual ? campo.tinta : hecho ? campo.marcaSuave : campo.panel;
                        const tinta = esActual ? campo.sobreTinta : hecho ? campo.marcaSuaveTexto : colores.textoSuave;
                        return (
                            <Tocable
                                key={p.paso}
                                etiqueta={p.etiqueta}
                                seleccionado={esActual}
                                testID={`chip-paso-${p.paso}`}
                                alPresionar={() => navegar({ tipo: "orden", id: orden.id, paso: p.paso })}
                                estilo={[estilos.chipPaso, { backgroundColor: fondo }, esActual || hecho ? null : { borderWidth: 1, borderColor: colores.borde }]}
                            >
                                {esActual ? null : <Icono size={16} color={tinta} />}
                                <Text style={[estilos.textoChip, { color: tinta, fontWeight: esActual || hecho ? "600" : "500" }]}>
                                    {esActual ? `${p.numero} · ${CORTO_PASO[p.paso]}` : CORTO_PASO[p.paso]}
                                </Text>
                            </Tocable>
                        );
                    })}
                </ScrollView>
            </View>
        );
    }
    return (
        <Tarjeta>
            <Fila entre>
                <Texto negrita>Trabajo en la orden</Texto>
                <Texto suave testID="avance-orden">
                    {avance.texto}
                </Texto>
            </Fila>
            {barra}
            <View style={estilos.pasos}>
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
                            estilo={[estilos.paso, { borderRadius: 14 }]}
                        >
                            <Icono size={20} color={color} />
                            <Texto estilo={estilos.flex}>{p.titulo}</Texto>
                            <Texto suave>{p.detalle}</Texto>
                        </Tocable>
                    );
                })}
            </View>
        </Tarjeta>
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
    const { campo, colores } = useTema();
    const telefono = orden.cliente?.telefono || orden.contacto.telefono;
    const lat = orden.lat ?? orden.cliente?.lat ?? null;
    const lon = orden.lon ?? orden.cliente?.lon ?? null;
    const comoLlegar = enlaceComoLlegar(lat, lon, orden.direccion);
    return (
        <Tarjeta testID="cliente-orden">
            <View style={estilos.cabezaCliente}>
                <View style={[estilos.flex, estilos.datosCliente]}>
                    <Text accessibilityRole="header" style={[estilos.nombreCliente, { color: colores.texto }]}>
                        {nombre}
                    </Text>
                    {orden.direccion ? (
                        <View style={estilos.fila}>
                            <MapPin size={16} color={colores.textoSuave} />
                            <Texto suave>{orden.direccion}</Texto>
                        </View>
                    ) : null}
                    {orden.servicio ? (
                        <Texto suave>
                            {orden.servicio.codigo || `Servicio #${orden.servicio.id}`}
                            {orden.servicio.plan ? ` · ${orden.servicio.plan}` : ""}
                            {orden.servicio.estado ? ` · ${orden.servicio.estado.replaceAll("_", " ")}` : ""}
                        </Texto>
                    ) : null}
                </View>
                <View style={estilos.botonesCliente}>
                    {comoLlegar ? <BotonRedondo icono={Route} etiqueta="Cómo llegar" fondo={campo.panel2} tinta={colores.texto} alPresionar={() => void Linking.openURL(comoLlegar)} testID="como-llegar" /> : null}
                    {telefono ? (
                        <BotonRedondo icono={Phone} etiqueta="Llamar" fondo={campo.panel2} tinta={colores.texto} alPresionar={() => void Linking.openURL(`tel:${telefono.replace(/[^\d+]/g, "")}`)} testID="llamar" />
                    ) : null}
                </View>
            </View>
            {orden.descripcion ? <Texto>{orden.descripcion}</Texto> : null}
            <AbrirChatOrden ordenId={orden.id} />
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
    cabezaCliente: { flexDirection: "row", alignItems: "flex-start", gap: 12 },
    datosCliente: { gap: 4 },
    nombreCliente: { fontSize: 18, fontWeight: "700" },
    botonesCliente: { flexDirection: "row", gap: 8 },
    flex: { flex: 1 },
    volver: { alignSelf: "flex-start", paddingHorizontal: 0 },
    encabezado: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 8 },
    encabezadoPaso: { flexDirection: "row", alignItems: "center", gap: 12 },
    titulosPaso: { gap: 2 },
    fila: { flexDirection: "row", alignItems: "center", gap: 8 },
    pildora: { minHeight: 54, borderRadius: 999 },
    progreso: { gap: 10 },
    barra: { flexDirection: "row", gap: 6 },
    segmento: { flex: 1, height: 6, borderRadius: 999 },
    chips: { gap: 6 },
    chipPaso: { minHeight: 44, height: 44, flexDirection: "row", alignItems: "center", gap: 6, paddingHorizontal: 12, borderRadius: 22 },
    textoChip: { fontSize: 13 },
    pasos: { gap: 4 },
    paso: { minHeight: 44, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 8 },
});
