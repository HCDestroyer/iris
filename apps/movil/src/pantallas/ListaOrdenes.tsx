// «Hoy» (diseño «combinada», como la PWA): la siguiente orden en grande (estado, código y hora,
// cliente, tipo y dirección, anillo de avance y «Continuar trabajo» / «Cómo llegar» / «Llamar»), los
// mosaicos «Ruta de hoy» y «Por recibir» (solo con datos leídos) y la lista «Después». El orden lo
// da `@iris/sync` (ruta publicada o hora de la cita). Con error al bajar el paquete se ve
// «Reintentar» y nunca un total inventado.
import type { OrdenVista, RutaCampo } from "@iris/sync";
import { accionPrincipal, anilloAvance, ESTADOS_TEXTO, horaCorta, hoyEnGuatemala, intercalarTareas, mosaicoPorRecibir, mosaicoRuta, nombreCliente, resumenHoy, tareasDelDia, TIPOS_ORDEN, ventanaCita } from "@iris/sync/campo";
import { CalendarX, ChevronRight, Clock, CloudUpload, ListChecks, MapPin, Phone, Play, RefreshCw, Route, TriangleAlert, UserX } from "lucide-react-native";
import { Linking, StyleSheet, Text, View } from "react-native";
import Svg, { Circle } from "react-native-svg";
import { Aviso, Boton, BotonRedondo, Cargando, Insignia, Pila, Texto, Tocable } from "../componentes";
import { useCampo } from "../campo/contexto";
import { ICONOS_ESTADO } from "../campo/iconos";
import { useInventario } from "../campo/inventario";
import { useTema } from "../tema/tema";
import { enlaceComoLlegar } from "./orden/DetalleOrden";
import { TarjetaTarea } from "./Tareas";

export type CargaPaquete = { estado: "inactiva" | "cargando" | "lista" } | { estado: "error"; mensaje: string };

export function InsigniaOrden({ estado }: { estado: OrdenVista["estado"] }) {
    const p = ESTADOS_TEXTO[estado];
    return <Insignia tono={p.tono} icono={ICONOS_ESTADO[estado]} texto={p.texto} testID={`estado-${estado}`} />;
}

export function ListaOrdenes({ carga, bajarPaquete }: { carga: CargaPaquete; bajarPaquete: () => Promise<void> }) {
    const { estado } = useCampo();
    const { colores } = useTema();
    const conRuta = (estado.ruta?.ordenIds?.length ?? 0) > 0;
    const { siguiente, despues, terminadas } = resumenHoy(estado.ordenes);
    // Las tareas propias de hoy van intercaladas por hora en «Después» (borde punteado).
    const tareas = tareasDelDia(estado.tareas, hoyEnGuatemala());
    // Las órdenes quedan en su orden (paradas o cita); cada tarea entra antes de la primera cuya hora
    // (llegada estimada con ruta, inicio de la cita sin ruta) sea posterior.
    const entradas = intercalarTareas(
        despues,
        (o) => (conRuta ? paradaDe(estado.ruta, o.id)?.llegadaEstimada : null) ?? o.cita?.inicio ?? o.programadaInicio,
        tareas,
    );
    const posicion = (o: OrdenVista) => (conRuta ? (estado.ruta?.ordenIds?.indexOf(o.id) ?? -1) + 1 || null : null);

    return (
        <Pila espacio={16}>
            {/* El saludo es el encabezado visible; esto lo dice a los lectores de pantalla. */}
            <Text accessibilityRole="header" style={estilos.oculto}>
                Mis órdenes de hoy
            </Text>
            {carga.estado === "cargando" ? <Cargando etiqueta="Bajando órdenes" /> : null}

            {conRuta && estado.ruta?.desactualizada ? (
                <Aviso tono="warning" icono={Route} titulo="Tu ruta cambió">
                    Una orden cambió de técnico o de hora después de publicarla. Despacho publicará otra; mientras tanto se muestra la anterior.
                </Aviso>
            ) : null}
            {estado.caducado ? (
                <Aviso tono="warning" titulo="Pasaron 7 días sin sincronizar" accion={<Boton variante="secundario" alPresionar={() => void bajarPaquete()}>Bajar órdenes</Boton>}>
                    Se borraron las órdenes viejas del teléfono; lo que hiciste sigue guardado.
                </Aviso>
            ) : null}
            {carga.estado === "error" ? (
                <Aviso
                    tono={estado.fecha ? "warning" : "error"}
                    titulo={carga.mensaje}
                    testID="error-paquete"
                    accion={
                        <Boton variante="secundario" icono={RefreshCw} alPresionar={() => void bajarPaquete()}>
                            Reintentar
                        </Boton>
                    }
                />
            ) : null}

            {estado.ordenes.length === 0 && tareas.length === 0 ? (
                carga.estado === "cargando" || carga.estado === "inactiva" ? (
                    <Texto suave>Cargando tus órdenes…</Texto>
                ) : (
                    <Aviso tono="neutral" icono={CalendarX} titulo={estado.fecha === null ? "Sin órdenes en el teléfono" : "No tienes órdenes para hoy"}>
                        {estado.fecha === null ? "Conéctate para bajar tus órdenes del día." : "Si despacho te asigna una, aparecerá aquí."}
                    </Aviso>
                )
            ) : (
                <View accessibilityLabel="Órdenes del día" style={estilos.lista}>
                    {siguiente ? <SiguienteOrden orden={siguiente} posicion={posicion(siguiente)} parada={paradaDe(estado.ruta, siguiente.id)} /> : null}
                    <Mosaicos />
                    {entradas.length > 0 ? (
                        <View accessibilityLabel="Después" style={estilos.seccion}>
                            <Text accessibilityRole="header" style={[estilos.tituloSeccion, { color: colores.textoSuave }]}>
                                DESPUÉS
                            </Text>
                            {entradas.map((e) =>
                                e.tipo === "orden" ? (
                                    <FilaOrden key={e.clave} orden={e.orden} posicion={posicion(e.orden)} parada={paradaDe(estado.ruta, e.orden.id)} />
                                ) : (
                                    <TarjetaTarea key={e.clave} tarea={e.tarea} />
                                ),
                            )}
                        </View>
                    ) : null}
                </View>
            )}
            {terminadas.length > 0 ? (
                <View accessibilityLabel="Órdenes terminadas" style={estilos.seccion}>
                    <Text accessibilityRole="header" style={[estilos.tituloSeccion, { color: colores.textoSuave }]}>
                        TERMINADAS
                    </Text>
                    {terminadas.map((o) => (
                        <FilaOrden key={o.id} orden={o} posicion={null} parada={null} />
                    ))}
                </View>
            ) : null}
        </Pila>
    );
}

type Parada = NonNullable<RutaCampo["paradas"]>[number];

function paradaDe(ruta: RutaCampo | null, ordenId: number): Parada | null {
    return ruta?.paradas?.find((p) => p.ordenId === ordenId) ?? null;
}

/** Llegada estimada (ruta), ventana acordada y lo que falta por enviar o fue rechazado. */
function Detalles({ orden, parada }: { orden: OrdenVista; parada: Parada | null }) {
    const { colores } = useTema();
    const ventana = ventanaCita(orden.cita);
    const sinEnviar = orden.pendientes + orden.subidasPendientes;
    return (
        <>
            {parada ? (
                <View style={estilos.fila}>
                    {parada.fueraDeVentana ? <TriangleAlert size={14} color={colores.advertencia} /> : <Route size={14} color={colores.textoSuave} />}
                    <Texto suave>
                        Llegada estimada {horaCorta(parada.llegadaEstimada)}
                        {parada.fueraDeVentana ? " · fuera de la ventana acordada" : ""}
                    </Texto>
                </View>
            ) : null}
            {ventana ? (
                <View style={estilos.fila}>
                    <Clock size={14} color={colores.textoSuave} />
                    <Texto suave>Llegada acordada {ventana}</Texto>
                </View>
            ) : null}
            {orden.reasignada || orden.rechazos > 0 || sinEnviar > 0 ? (
                <View style={estilos.fila}>
                    {orden.reasignada ? <Insignia tono="warning" icono={UserX} texto="Reasignada" /> : null}
                    {orden.rechazos > 0 ? <Insignia tono="error" icono={TriangleAlert} texto={`${orden.rechazos} rechazada${orden.rechazos === 1 ? "" : "s"}`} /> : null}
                    {sinEnviar > 0 ? <Insignia tono="warning" icono={CloudUpload} texto={`${sinEnviar} sin enviar`} /> : null}
                </View>
            ) : null}
        </>
    );
}

/** La siguiente orden en grande: tarjeta de 28 px con el anillo de avance y sus tres botones. */
function SiguienteOrden({ orden, posicion, parada }: { orden: OrdenVista; posicion: number | null; parada: Parada | null }) {
    const { navegar } = useCampo();
    const { campo, colores, fuenteMono } = useTema();
    const nombre = nombreCliente(orden);
    const hora = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const anillo = anilloAvance(orden);
    const principal = accionPrincipal(orden);
    const telefono = orden.contacto.telefono || orden.cliente?.telefono;
    const comoLlegar = enlaceComoLlegar(orden.lat ?? orden.cliente?.lat ?? null, orden.lon ?? orden.cliente?.lon ?? null, orden.direccion);
    return (
        <View accessibilityLabel="Siguiente orden" testID="siguiente-orden" style={[estilos.heroe, { backgroundColor: campo.heroe, borderColor: campo.heroeBorde, shadowColor: campo.sombra }]}>
            <Tocable etiqueta={`${orden.numero}, ${nombre}`} alPresionar={() => navegar({ tipo: "orden", id: orden.id })} estilo={estilos.cuerpoHeroe}>
                <View style={estilos.filaEntre}>
                    <View style={estilos.fila}>
                        {posicion !== null ? (
                            <View accessibilityLabel={`Parada ${posicion}`} style={[estilos.parada, { backgroundColor: colores.primario }]}>
                                <Texto estilo={{ color: colores.sobrePrimario }} negrita>
                                    {posicion}
                                </Texto>
                            </View>
                        ) : null}
                        <InsigniaOrden estado={orden.estado} />
                    </View>
                    <Text style={[estilos.codigo, { color: colores.textoSuave, fontFamily: fuenteMono }]}>
                        {orden.numero}
                        {hora ? ` · ${hora}` : ""}
                    </Text>
                </View>
                <View style={estilos.fila16}>
                    <View style={[estilos.flex, estilos.textos]}>
                        <Text style={[estilos.nombreHeroe, { color: colores.texto }]}>{nombre}</Text>
                        <Texto suave>{TIPOS_ORDEN[orden.tipo]}</Texto>
                        {orden.direccion ? (
                            <View style={estilos.fila}>
                                <MapPin size={14} color={colores.textoSuave} />
                                <Texto suave estilo={estilos.encoger}>
                                    {orden.direccion}
                                </Texto>
                            </View>
                        ) : null}
                        <Detalles orden={orden} parada={parada} />
                    </View>
                    {orden.estado !== "pendiente" ? <AnilloAvance fraccion={anillo.fraccion} texto={anillo.texto} etiqueta={anillo.etiqueta} /> : null}
                </View>
            </Tocable>
            <View style={estilos.botonesHeroe}>
                <View style={estilos.flex}>
                    <Boton
                        icono={principal.texto === "Empezar trabajo" ? Play : principal.texto === "Continuar trabajo" ? ListChecks : ChevronRight}
                        alPresionar={() => navegar(principal.vista)}
                        estilo={estilos.botonPrincipal}
                        testID="accion-principal"
                    >
                        {principal.texto}
                    </Boton>
                </View>
                {comoLlegar ? (
                    <BotonRedondo icono={Route} etiqueta="Cómo llegar" tamano={52} fondo={campo.panel2} tinta={colores.texto} borde={colores.borde} alPresionar={() => void Linking.openURL(comoLlegar)} />
                ) : null}
                {telefono ? (
                    <BotonRedondo
                        icono={Phone}
                        etiqueta="Llamar al cliente"
                        tamano={52}
                        fondo={campo.panel2}
                        tinta={colores.texto}
                        borde={colores.borde}
                        alPresionar={() => void Linking.openURL(`tel:${telefono.replace(/[^\d+]/g, "")}`)}
                    />
                ) : null}
            </View>
        </View>
    );
}

/** Anillo «2/4»: avance de los pasos de trabajo (el nombre accesible lo dice completo). */
function AnilloAvance({ fraccion, texto, etiqueta }: { fraccion: number; texto: string; etiqueta: string }) {
    const { campo, colores } = useTema();
    const radio = 29;
    const largo = 2 * Math.PI * radio;
    return (
        <View accessible accessibilityRole="image" accessibilityLabel={etiqueta} testID="anillo-avance" style={estilos.anillo}>
            <Svg width={66} height={66} viewBox="0 0 66 66">
                <Circle cx={33} cy={33} r={radio} stroke={campo.anillo} strokeWidth={7} fill="none" />
                <Circle
                    cx={33}
                    cy={33}
                    r={radio}
                    stroke={colores.primario}
                    strokeWidth={7}
                    fill="none"
                    strokeDasharray={`${largo * fraccion} ${largo}`}
                    strokeLinecap="butt"
                    transform="rotate(-90 33 33)"
                />
            </Svg>
            <Text style={[estilos.textoAnillo, { color: colores.texto }]}>{texto}</Text>
        </View>
    );
}

/** «Ruta de hoy» y «Por recibir»: solo con datos leídos (nunca un total inventado). */
function Mosaicos() {
    const { estado, navegar } = useCampo();
    const { traslados } = useInventario();
    const { campo, colores } = useTema();
    const ruta = mosaicoRuta(estado);
    const recibir = mosaicoPorRecibir(traslados.datos, estado.recepciones);
    if (!ruta) {
        return null;
    }
    const aviso = recibir !== null && recibir.cantidad > 0;
    const tintaRecibir = aviso ? colores.advertencia : colores.texto;
    return (
        <View accessibilityLabel="Resumen del día" style={estilos.mosaicos}>
            <View testID="mosaico-ruta" style={[estilos.mosaico, { backgroundColor: campo.panel, borderColor: colores.borde }]}>
                <Texto suave>Ruta de hoy</Texto>
                <Text style={[estilos.valorMosaico, { color: colores.texto }]}>{ruta.valor}</Text>
                <Text style={[estilos.detalleMosaico, { color: colores.textoSuave }]}>{ruta.detalle}</Text>
            </View>
            <Tocable
                etiqueta={recibir ? `Por recibir: ${recibir.valor}, ${recibir.detalle}` : "Por recibir: sin leer"}
                testID="mosaico-recibir"
                alPresionar={() => navegar({ tipo: "recibir" })}
                estilo={[estilos.mosaico, { backgroundColor: aviso ? campo.avisoFondo : campo.panel, borderColor: aviso ? campo.avisoBorde : colores.borde }]}
            >
                <Text style={[estilos.etiquetaMosaico, { color: tintaRecibir }]}>Por recibir</Text>
                <Text style={[estilos.valorMosaico, { color: recibir ? tintaRecibir : colores.textoSuave }]}>{recibir ? recibir.valor : "—"}</Text>
                <Text style={[estilos.detalleMosaico, { color: recibir ? tintaRecibir : colores.textoSuave }]}>
                    {recibir ? recibir.detalle : traslados.estado === "cargando" ? "Leyendo…" : "Sin leer: toca para ver"}
                </Text>
            </Tocable>
        </View>
    );
}

/** Fila de «Después»: hora en un bloque, cliente, tipo y la insignia de estado. */
function FilaOrden({ orden, posicion, parada }: { orden: OrdenVista; posicion: number | null; parada: Parada | null }) {
    const { navegar } = useCampo();
    const { campo, colores, fuenteMono } = useTema();
    const hora = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const nombre = nombreCliente(orden);
    return (
        <Tocable
            etiqueta={`${orden.numero}, ${nombre}`}
            alPresionar={() => navegar({ tipo: "orden", id: orden.id })}
            estilo={[estilos.filaOrden, { backgroundColor: campo.panel, borderColor: colores.borde }]}
        >
            <View style={[estilos.bloqueHora, { backgroundColor: campo.panel2 }]}>
                <Text style={[estilos.hora, { color: colores.texto }]}>{hora ?? "—"}</Text>
                {posicion !== null ? (
                    <View accessibilityLabel={`Parada ${posicion}`} style={[estilos.paradaChica, { backgroundColor: colores.primario }]}>
                        <Text style={[estilos.textoParada, { color: colores.sobrePrimario }]}>{posicion}</Text>
                    </View>
                ) : null}
            </View>
            <View style={[estilos.flex, estilos.textos]}>
                <Texto negrita>{nombre}</Texto>
                <Texto suave>
                    <Text style={{ fontFamily: fuenteMono }}>{orden.numero}</Text> · {TIPOS_ORDEN[orden.tipo]}
                </Texto>
                <Detalles orden={orden} parada={parada} />
            </View>
            <InsigniaOrden estado={orden.estado} />
        </Tocable>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    encoger: { flexShrink: 1 },
    oculto: { position: "absolute", width: 1, height: 1, opacity: 0 },
    lista: { gap: 16 },
    seccion: { gap: 8 },
    tituloSeccion: { fontSize: 13, fontWeight: "600", letterSpacing: 0.8 },
    fila: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 6 },
    fila16: { flexDirection: "row", alignItems: "center", gap: 16 },
    filaEntre: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
    textos: { gap: 4 },
    heroe: { borderRadius: 28, borderWidth: 1, padding: 18, gap: 14, shadowOpacity: 0.07, shadowRadius: 20, shadowOffset: { width: 0, height: 10 }, elevation: 3 },
    cuerpoHeroe: { gap: 14, borderRadius: 20 },
    codigo: { fontSize: 12, fontWeight: "500" },
    nombreHeroe: { fontSize: 22, fontWeight: "700", letterSpacing: -0.22 },
    parada: { width: 28, height: 28, borderRadius: 14, alignItems: "center", justifyContent: "center" },
    botonesHeroe: { flexDirection: "row", alignItems: "center", gap: 10 },
    botonPrincipal: { minHeight: 52, borderRadius: 999 },
    anillo: { width: 66, height: 66, alignItems: "center", justifyContent: "center" },
    textoAnillo: { position: "absolute", fontSize: 15, fontWeight: "700" },
    mosaicos: { flexDirection: "row", gap: 10 },
    mosaico: { flex: 1, gap: 6, padding: 14, borderRadius: 22, borderWidth: 1, alignItems: "flex-start", justifyContent: "flex-start" },
    etiquetaMosaico: { fontSize: 13 },
    valorMosaico: { fontSize: 24, fontWeight: "700", letterSpacing: -0.48 },
    detalleMosaico: { fontSize: 12, fontWeight: "500" },
    filaOrden: { flexDirection: "row", alignItems: "center", gap: 12, padding: 12, borderRadius: 22, borderWidth: 1 },
    bloqueHora: { width: 52, height: 52, borderRadius: 16, alignItems: "center", justifyContent: "center" },
    hora: { fontSize: 15, fontWeight: "700" },
    paradaChica: { position: "absolute", top: -6, left: -6, width: 20, height: 20, borderRadius: 10, alignItems: "center", justifyContent: "center" },
    textoParada: { fontSize: 11, fontWeight: "600" },
});
