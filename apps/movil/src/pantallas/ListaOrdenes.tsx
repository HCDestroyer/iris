// Mis órdenes del día en orden de ruta publicada (o por hora de cita), como la PWA: el orden lo da
// `@iris/sync`. Con error al bajar el paquete se ve «Reintentar» y nunca un total inventado.
import type { OrdenVista, RutaCampo } from "@iris/sync";
import { diaLargo, ESTADOS_TEXTO, horaCorta, hoyEnGuatemala, TIPOS_ORDEN, ventanaCita } from "@iris/sync/campo";
import { CalendarX, ChevronRight, Clock, CloudUpload, MapPin, RefreshCw, Route, TriangleAlert, UserX } from "lucide-react-native";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { Aviso, Boton, Insignia, Pila, Texto, Titulo, Tocable } from "../componentes";
import { useCampo } from "../campo/contexto";
import { ICONOS_ESTADO } from "../campo/iconos";
import { useTema } from "../tema/tema";

export type CargaPaquete = { estado: "inactiva" | "cargando" | "lista" } | { estado: "error"; mensaje: string };

const TERMINALES = new Set(["cerrada", "fallida", "cancelada"]);

export function InsigniaOrden({ estado }: { estado: OrdenVista["estado"] }) {
    const p = ESTADOS_TEXTO[estado];
    return <Insignia tono={p.tono} icono={ICONOS_ESTADO[estado]} texto={p.texto} testID={`estado-${estado}`} />;
}

export function ListaOrdenes({ carga, bajarPaquete }: { carga: CargaPaquete; bajarPaquete: () => Promise<void> }) {
    const { estado } = useCampo();
    const { colores } = useTema();
    const fecha = estado.fecha ?? hoyEnGuatemala();
    const conRuta = (estado.ruta?.ordenIds?.length ?? 0) > 0;
    const activas = estado.ordenes.filter((o) => !TERMINALES.has(o.estado) && !o.reasignada);
    const terminadas = estado.ordenes.filter((o) => !activas.includes(o));

    return (
        <Pila espacio={14}>
            <View style={estilos.encabezado}>
                <View style={estilos.flex}>
                    <Titulo>Mis órdenes de hoy</Titulo>
                    <Texto suave>
                        {diaLargo(fecha)} · {conRuta ? "en orden de ruta" : "por hora de cita"}
                    </Texto>
                </View>
                {carga.estado === "cargando" ? <ActivityIndicator color={colores.primario} accessibilityLabel="Bajando órdenes" /> : null}
            </View>

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

            {estado.ordenes.length === 0 ? (
                carga.estado === "cargando" || carga.estado === "inactiva" ? (
                    <Texto suave>Cargando tus órdenes…</Texto>
                ) : (
                    <Aviso
                        tono="neutral"
                        icono={CalendarX}
                        titulo={estado.fecha === null ? "Sin órdenes en el teléfono" : "No tienes órdenes para hoy"}
                    >
                        {estado.fecha === null ? "Conéctate para bajar tus órdenes del día." : "Si despacho te asigna una, aparecerá aquí."}
                    </Aviso>
                )
            ) : (
                <View accessibilityLabel="Órdenes del día" style={estilos.lista}>
                    {activas.map((o, i) => (
                        <TarjetaOrden key={o.id} orden={o} posicion={conRuta ? i + 1 : null} parada={paradaDe(estado.ruta, o.id)} />
                    ))}
                </View>
            )}
            {terminadas.length > 0 ? (
                <>
                    <Texto suave negrita>
                        Terminadas
                    </Texto>
                    <View accessibilityLabel="Órdenes terminadas" style={estilos.lista}>
                        {terminadas.map((o) => (
                            <TarjetaOrden key={o.id} orden={o} posicion={null} parada={null} />
                        ))}
                    </View>
                </>
            ) : null}
        </Pila>
    );
}

type Parada = NonNullable<RutaCampo["paradas"]>[number];

function paradaDe(ruta: RutaCampo | null, ordenId: number): Parada | null {
    return ruta?.paradas?.find((p) => p.ordenId === ordenId) ?? null;
}

function TarjetaOrden({ orden, posicion, parada }: { orden: OrdenVista; posicion: number | null; parada: Parada | null }) {
    const { navegar } = useCampo();
    const { colores, radioTarjeta } = useTema();
    const hora = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const ventana = ventanaCita(orden.cita);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";
    const sinEnviar = orden.pendientes + orden.subidasPendientes;
    return (
        <Tocable
            etiqueta={`${orden.numero}, ${nombre}`}
            alPresionar={() => navegar({ tipo: "orden", id: orden.id })}
            estilo={[estilos.tarjeta, { backgroundColor: colores.tarjeta, borderColor: colores.borde, borderRadius: radioTarjeta }]}
        >
            <View style={estilos.columnaHora}>
                {posicion !== null ? (
                    <View accessibilityLabel={`Parada ${posicion}`} style={[estilos.parada, { backgroundColor: colores.primario }]}>
                        <Texto estilo={{ color: colores.sobrePrimario }} negrita>
                            {posicion}
                        </Texto>
                    </View>
                ) : null}
                <Texto negrita>{hora ?? "—"}</Texto>
            </View>
            <View style={[estilos.flex, estilos.cuerpo]}>
                <View style={estilos.fila}>
                    <Texto mono negrita>
                        {orden.numero}
                    </Texto>
                    <InsigniaOrden estado={orden.estado} />
                </View>
                <Texto negrita>{nombre}</Texto>
                <Texto suave>{TIPOS_ORDEN[orden.tipo]}</Texto>
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
                {orden.direccion ? (
                    <View style={estilos.fila}>
                        <MapPin size={14} color={colores.textoSuave} />
                        <Texto suave>{orden.direccion}</Texto>
                    </View>
                ) : null}
                <View style={estilos.fila}>
                    {orden.reasignada ? <Insignia tono="warning" icono={UserX} texto="Reasignada" /> : null}
                    {orden.rechazos > 0 ? <Insignia tono="error" icono={TriangleAlert} texto={`${orden.rechazos} rechazada${orden.rechazos === 1 ? "" : "s"}`} /> : null}
                    {sinEnviar > 0 ? <Insignia tono="warning" icono={CloudUpload} texto={`${sinEnviar} sin enviar`} /> : null}
                </View>
            </View>
            <ChevronRight size={20} color={colores.textoSuave} />
        </Tocable>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    encabezado: { flexDirection: "row", alignItems: "flex-end", gap: 12 },
    lista: { gap: 12 },
    tarjeta: { flexDirection: "row", alignItems: "center", gap: 12, padding: 14, borderWidth: 1 },
    columnaHora: { width: 52, alignItems: "center", gap: 4, alignSelf: "flex-start" },
    parada: { width: 28, height: 28, borderRadius: 14, alignItems: "center", justifyContent: "center" },
    cuerpo: { gap: 4 },
    fila: { flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 6 },
});
