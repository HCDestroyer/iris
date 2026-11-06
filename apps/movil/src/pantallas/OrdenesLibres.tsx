// «Órdenes libres» (botón «+»): las de tu zona sin técnico, más cercanas primero. Tomar una requiere
// señal (no va por la cola): confirmación, aviso emergente y, si otro la tomó primero (409
// `orden_ya_tomada`), el aviso con su nombre y la orden fuera de la lista. Como la PWA (`libres.tsx`).
import { ErrorTransporte, type OrdenLibre } from "@iris/sync";
import {
    avisoYaTomada,
    cuandoLibre,
    distanciaTexto,
    subtituloLibres,
    TIPOS_ORDEN,
} from "@iris/sync/campo";
import { Hand, Info, MapPin, RefreshCw, WifiOff } from "lucide-react-native";
import { useEffect, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Aviso, Boton, Cargando, Dialogo, Texto } from "../componentes";
import { useCampo } from "../campo/contexto";
import { useLibres } from "../campo/libres";
import { useTema } from "../tema/tema";
import { BotonTinta, EncabezadoPantalla } from "./Tareas";

function nombreDe(o: OrdenLibre): string {
    return o.clienteNombre ?? o.contacto.nombre ?? o.direccion ?? o.numero;
}

export function OrdenesLibres() {
    const { estado, navegar, notificar, motor } = useCampo();
    const { campo, colores } = useTema();
    const { api, libres, cargarLibres, quitadas, quitar } = useLibres();
    const [confirmar, setConfirmar] = useState<OrdenLibre | null>(null);
    const [ocupado, setOcupado] = useState(false);
    const [yaTomada, setYaTomada] = useState<string | null>(null);
    const sinSenal = estado.conexion === "sin_conexion";
    useEffect(() => {
        if (!sinSenal) {
            void cargarLibres();
        }
    }, [sinSenal, cargarLibres]);

    const tomar = async (o: OrdenLibre) => {
        setOcupado(true);
        try {
            await api.tomar(o.id);
            quitar(o.id);
            notificar({
                tono: "success",
                texto: `${o.numero} quedó a tu nombre. Despacho recibe el aviso.`,
            });
            motor.reintentarAhora();
        } catch (e) {
            if (e instanceof ErrorTransporte && e.problema?.status === 409) {
                quitar(o.id);
                setYaTomada(
                    avisoYaTomada(o.numero, e.problema.tomadaPor?.nombre),
                );
            } else {
                notificar({
                    tono: "error",
                    texto:
                        e instanceof ErrorTransporte && e.tipo === "red"
                            ? "Sin señal: tomar una orden necesita conexión."
                            : "No se pudo tomar la orden. Inténtalo de nuevo.",
                });
            }
        } finally {
            setOcupado(false);
            setConfirmar(null);
        }
    };

    const datos = libres.datos;
    const lista = (datos?.data ?? []).filter((o) => !quitadas.has(o.id));
    return (
        <View testID="ordenes-libres" style={estilos.pila}>
            <EncabezadoPantalla
                titulo="Órdenes libres"
                alAtras={() => navegar({ tipo: "lista" })}
            />
            {yaTomada ? (
                <View
                    testID="ya-tomada"
                    accessibilityRole="alert"
                    style={[
                        estilos.aviso,
                        {
                            backgroundColor: campo.avisoFondo,
                            borderColor: campo.avisoBorde,
                        },
                    ]}
                >
                    <Info size={20} color={colores.tono.warning} />
                    <Text
                        style={[
                            estilos.textoAviso,
                            { color: colores.tono.warning },
                        ]}
                    >
                        {yaTomada}
                    </Text>
                </View>
            ) : null}
            {datos && lista.length > 0 ? (
                <Texto suave>
                    {subtituloLibres(datos.zonas ?? [], datos.orden)}
                </Texto>
            ) : null}
            {sinSenal && !datos ? (
                <Aviso tono="warning" icono={WifiOff} titulo="Sin señal">
                    Tomar una orden libre necesita conexión.
                </Aviso>
            ) : libres.estado === "error" && !datos ? (
                <Aviso
                    tono="error"
                    titulo="No se pudieron leer las órdenes libres"
                    accion={
                        <Boton
                            variante="secundario"
                            icono={RefreshCw}
                            alPresionar={() => void cargarLibres()}
                        >
                            Reintentar
                        </Boton>
                    }
                >
                    Revisa la señal.
                </Aviso>
            ) : !datos ? (
                <Cargando etiqueta="Buscando órdenes libres" />
            ) : lista.length === 0 ? (
                <Texto suave>
                    Ninguna orden de tu zona está sin técnico ahora.
                </Texto>
            ) : (
                lista.map((o) => {
                    const distancia = distanciaTexto(o.distanciaM);
                    return (
                        <View
                            key={o.id}
                            testID={`libre-${o.id}`}
                            style={[
                                estilos.tarjeta,
                                {
                                    backgroundColor: campo.panel,
                                    borderColor: colores.borde,
                                },
                            ]}
                        >
                            <View style={estilos.filaTitulo}>
                                <View style={estilos.flex}>
                                    <Text
                                        style={[
                                            estilos.nombre,
                                            { color: colores.texto },
                                        ]}
                                    >
                                        {nombreDe(o)}
                                    </Text>
                                    <Text style={{ color: colores.textoSuave }}>
                                        {o.numero} · {TIPOS_ORDEN[o.tipo]}
                                    </Text>
                                    {distancia ? (
                                        <View style={estilos.distancia}>
                                            <MapPin
                                                size={15}
                                                color={colores.textoSuave}
                                            />
                                            <Text
                                                style={{
                                                    color: colores.textoSuave,
                                                }}
                                            >
                                                {distancia}
                                            </Text>
                                        </View>
                                    ) : null}
                                </View>
                                <Text style={{ color: colores.textoSuave }}>
                                    {cuandoLibre(o)}
                                </Text>
                            </View>
                            <Boton
                                variante="secundario"
                                icono={Hand}
                                deshabilitado={sinSenal}
                                alPresionar={() => setConfirmar(o)}
                                estilo={estilos.pildora}
                                testID={`tomar-${o.id}`}
                            >
                                Tomarla
                            </Boton>
                        </View>
                    );
                })
            )}
            <Dialogo
                abierto={confirmar !== null}
                alCerrar={() => setConfirmar(null)}
                titulo={confirmar ? `¿Tomar ${confirmar.numero}?` : ""}
                descripcion={
                    confirmar
                        ? `${nombreDe(confirmar)} · ${TIPOS_ORDEN[confirmar.tipo]} · ${cuandoLibre(confirmar).toLowerCase()}. Queda a tu nombre y despacho recibe el aviso.`
                        : ""
                }
                testID="confirmar-tomar"
            >
                <BotonTinta
                    alPresionar={() => confirmar && void tomar(confirmar)}
                    deshabilitado={ocupado}
                    testID="confirmar-tomarla"
                >
                    Tomarla
                </BotonTinta>
                <Boton
                    variante="secundario"
                    alPresionar={() => setConfirmar(null)}
                    estilo={estilos.pildora}
                >
                    Cancelar
                </Boton>
            </Dialogo>
        </View>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1, gap: 2 },
    pila: { gap: 14 },
    aviso: {
        flexDirection: "row",
        gap: 10,
        padding: 14,
        borderRadius: 22,
        borderWidth: 1,
    },
    textoAviso: { flex: 1, fontSize: 15, fontWeight: "600" },
    tarjeta: { gap: 12, padding: 16, borderRadius: 22, borderWidth: 1 },
    filaTitulo: { flexDirection: "row", gap: 12, alignItems: "flex-start" },
    nombre: { fontSize: 17, fontWeight: "700" },
    distancia: { flexDirection: "row", alignItems: "center", gap: 6 },
    pildora: { borderRadius: 999, minHeight: 52 },
});
