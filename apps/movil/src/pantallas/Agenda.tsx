// Agenda «Mañana» y «Semana» (diseño «combinada», como la PWA): tira de 7 días con el día elegido en
// teal y las citas en bloques de color (en curso en tinta, sin formulario en aviso). Se lee con señal
// (`campo/agenda.tsx`); sin leer no hay contadores ni citas inventadas, y sin señal dice que se lee
// con señal, con «Reintentar». Hoy usa las órdenes del teléfono (con lo hecho sin señal).
import type { OrdenCampo } from "@iris/sync";
import {
    citasDelDia,
    citasPorDia,
    hoyEnGuatemala,
    horaCorta,
    mananaEnGuatemala,
    nombreCliente,
    resumenDia,
    textoErrorLectura,
    textoFormularios,
    tiraSemana,
    tituloAgenda,
    TIPOS_ORDEN,
    tonoCita,
    ventanaCita,
    type CitaAgenda,
} from "@iris/sync/campo";
import { CalendarX, RefreshCw, TriangleAlert, WifiOff } from "lucide-react-native";
import { StyleSheet, Text, View } from "react-native";
import { Aviso, Boton, Cargando, Pila, Tocable } from "../componentes";
import { useAgenda } from "../campo/agenda";
import { useCampo } from "../campo/contexto";
import { useCargarAlAbrir } from "../campo/inventario";
import { useTema } from "../tema/tema";

function useOrdenesAgenda(): { ordenes: OrdenCampo[] | null; hoy: string } {
    const { estado } = useCampo();
    const { semana, cargarSemana } = useAgenda();
    useCargarAlAbrir(cargarSemana);
    const hoy = hoyEnGuatemala();
    if (!semana.datos) {
        return { ordenes: null, hoy };
    }
    const locales = new Set(estado.ordenes.map((o) => o.id));
    return { ordenes: [...estado.ordenes, ...semana.datos.ordenes.filter((o) => !locales.has(o.id))], hoy };
}

/** Sin la semana leída: el error, o sin señal que se lee con señal (con «Reintentar»), o «Leyendo». */
function EstadoLectura({ sinDatos }: { sinDatos: boolean }) {
    const { semana, cargarSemana } = useAgenda();
    const { estado } = useCampo();
    const reintentar = (
        <Boton variante="secundario" icono={RefreshCw} alPresionar={() => void cargarSemana()}>
            Reintentar
        </Boton>
    );
    if (semana.estado === "error") {
        const e = textoErrorLectura(semana.error, "tu agenda");
        return <Aviso tono={semana.datos || e.sinSenal ? "warning" : "error"} icono={e.sinSenal ? WifiOff : TriangleAlert} titulo={semana.datos ? `${e.texto} Se muestra lo último que se leyó.` : e.texto} accion={reintentar} testID="error-agenda" />;
    }
    if (!sinDatos) {
        return null;
    }
    if (semana.estado === "inactiva" && estado.conexion === "sin_conexion") {
        return <Aviso tono="warning" icono={WifiOff} titulo="Sin señal: tu agenda de los próximos días se lee con señal." accion={reintentar} testID="error-agenda" />;
    }
    return <Cargando etiqueta="Leyendo tu agenda" />;
}

export function AgendaDia({ dia }: { dia: string | undefined }) {
    const { ordenes, hoy } = useOrdenesAgenda();
    const { campo, colores } = useTema();
    const fecha = dia ?? mananaEnGuatemala();
    const tira = tiraSemana(hoy);
    const citas = ordenes ? citasDelDia(ordenes, fecha) : null;
    return (
        <Pila espacio={16}>
            <View style={estilos.filaTitulo}>
                <Text accessibilityRole="header" style={[estilos.titulo, { color: colores.texto }]}>
                    {tituloAgenda(fecha, hoy)}
                </Text>
                {citas ? (
                    <View style={[estilos.resumen, { backgroundColor: campo.panel, borderColor: colores.borde }]}>
                        <Text testID="resumen-dia" style={[estilos.textoResumen, { color: colores.texto }]}>
                            {resumenDia(citas)}
                        </Text>
                    </View>
                ) : null}
            </View>
            <TiraSemana elegido={fecha} dias={tira} cuentas={ordenes ? citasPorDia(ordenes, tira.map((d) => d.fecha)) : null} />
            <EstadoLectura sinDatos={!citas} />
            {!citas ? null : citas.length === 0 ? (
                <Aviso tono="neutral" icono={CalendarX} titulo="Sin citas ese día">
                    Si despacho te programa algo, aparecerá aquí.
                </Aviso>
            ) : (
                <ListaCitas citas={citas} />
            )}
        </Pila>
    );
}

export function AgendaSemana() {
    const { ordenes, hoy } = useOrdenesAgenda();
    const { colores } = useTema();
    const tira = tiraSemana(hoy);
    return (
        <Pila espacio={16}>
            <Text accessibilityRole="header" style={[estilos.titulo, { color: colores.texto }]}>
                Esta semana
            </Text>
            <TiraSemana elegido={null} dias={tira} cuentas={ordenes ? citasPorDia(ordenes, tira.map((d) => d.fecha)) : null} />
            <EstadoLectura sinDatos={!ordenes} />
            {ordenes
                ? tira.map((d) => {
                      const citas = citasDelDia(ordenes, d.fecha);
                      return (
                          <View key={d.fecha} accessibilityLabel={d.largo} style={estilos.dia}>
                              <View style={estilos.filaTitulo}>
                                  <Text style={[estilos.tituloDia, { color: colores.textoSuave }]}>{tituloAgenda(d.fecha, hoy).toUpperCase()}</Text>
                                  <Text style={[estilos.textoResumen, { color: colores.textoSuave }]}>{resumenDia(citas)}</Text>
                              </View>
                              {citas.length > 0 ? <ListaCitas citas={citas} /> : null}
                          </View>
                      );
                  })
                : null}
        </Pila>
    );
}

function TiraSemana({ elegido, dias, cuentas }: { elegido: string | null; dias: ReturnType<typeof tiraSemana>; cuentas: Record<string, number> | null }) {
    const { navegar } = useCampo();
    const { campo, colores } = useTema();
    return (
        <View accessibilityLabel="Semana" style={estilos.tira}>
            {dias.map((d) => {
                const activo = d.fecha === elegido;
                const n = cuentas?.[d.fecha];
                return (
                    <Tocable
                        key={d.fecha}
                        etiqueta={`${d.largo}${n === undefined ? "" : `: ${n === 0 ? "sin citas" : `${n} ${n === 1 ? "cita" : "citas"}`}`}`}
                        seleccionado={activo}
                        testID={`dia-${d.fecha}`}
                        alPresionar={() => navegar({ tipo: "agenda", dia: d.fecha })}
                        estilo={[estilos.diaTira, { backgroundColor: activo ? colores.primario : campo.panel }]}
                    >
                        <Text style={[estilos.corto, { color: activo ? colores.sobrePrimario : colores.textoSuave }]}>{d.corto}</Text>
                        <Text style={[estilos.numero, { color: activo ? colores.sobrePrimario : colores.texto }]}>{d.numero}</Text>
                        <View style={[estilos.puntito, { backgroundColor: n ? (activo ? colores.sobrePrimario : colores.primario) : "transparent" }]} />
                    </Tocable>
                );
            })}
        </View>
    );
}

function ListaCitas({ citas }: { citas: CitaAgenda[] }) {
    const { navegar, estado } = useCampo();
    const { campo, colores } = useTema();
    const enTelefono = new Set(estado.ordenes.map((o) => o.id));
    return (
        <View style={estilos.citas}>
            {citas.map(({ orden, inicio }) => {
                const tono = tonoCita(orden);
                const ventana = ventanaCita(orden.cita);
                const zona = orden.cliente?.direccion || orden.direccion;
                const fondo = tono === "marca" ? campo.tinta : tono === "aviso" ? campo.avisoFondo : campo.panel;
                const borde = tono === "marca" ? campo.tinta : tono === "aviso" ? campo.avisoBorde : colores.borde;
                const tinta = tono === "marca" ? campo.sobreTinta : colores.texto;
                const suave = tono === "marca" ? campo.sobreTinta : tono === "aviso" ? colores.advertencia : colores.textoSuave;
                const contenido = (
                    <>
                        <Text style={[estilos.tituloCita, { color: tinta }]}>
                            {TIPOS_ORDEN[orden.tipo]} · {nombreCliente(orden)}
                        </Text>
                        <Text style={[estilos.detalleCita, { color: suave }]}>{[zona, ventana ? `ventana ${ventana}` : null, textoFormularios(orden)].filter(Boolean).join(" · ")}</Text>
                    </>
                );
                const estiloBloque = [estilos.bloque, { backgroundColor: fondo, borderColor: borde }];
                return (
                    <View key={orden.id} testID={`cita-${orden.id}`} style={estilos.filaCita}>
                        <Text style={[estilos.horaCita, { color: colores.texto }]}>{horaCorta(inicio)}</Text>
                        {enTelefono.has(orden.id) ? (
                            <Tocable etiqueta={`${orden.numero}, ${nombreCliente(orden)}`} alPresionar={() => navegar({ tipo: "orden", id: orden.id })} estilo={[estilos.flex, ...estiloBloque]}>
                                {contenido}
                            </Tocable>
                        ) : (
                            <View accessible accessibilityLabel={`${orden.numero}, ${nombreCliente(orden)}`} style={[estilos.flex, ...estiloBloque]}>
                                {contenido}
                            </View>
                        )}
                    </View>
                );
            })}
        </View>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    filaTitulo: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 12 },
    titulo: { fontSize: 26, fontWeight: "700", letterSpacing: -0.52, flexShrink: 1 },
    resumen: { height: 30, paddingHorizontal: 12, borderRadius: 15, borderWidth: 1, justifyContent: "center" },
    textoResumen: { fontSize: 13, fontWeight: "500" },
    tira: { flexDirection: "row", gap: 6 },
    diaTira: { flex: 1, height: 58, minHeight: 58, borderRadius: 14, alignItems: "center", justifyContent: "center", gap: 2 },
    corto: { fontSize: 11, fontWeight: "600" },
    numero: { fontSize: 16, fontWeight: "700" },
    puntito: { width: 4, height: 4, borderRadius: 2 },
    dia: { gap: 8 },
    tituloDia: { fontSize: 13, fontWeight: "600", letterSpacing: 0.8 },
    citas: { gap: 10 },
    filaCita: { flexDirection: "row", gap: 10 },
    horaCita: { width: 48, paddingTop: 14, fontSize: 13, fontWeight: "600" },
    bloque: { gap: 4, padding: 14, borderRadius: 22, borderWidth: 1 },
    tituloCita: { fontSize: 15, fontWeight: "600" },
    detalleCita: { fontSize: 13 },
});
