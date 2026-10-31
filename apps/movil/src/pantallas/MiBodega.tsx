// «Mi bodega» (dentro de «Yo»): existencias con modalidad, equipos con serie y movimientos
// recientes de Argos, solo lectura, con «Reintentar» si no se pudo leer. Como la PWA.
import type { MiBodega as DatosBodega } from "@iris/sync";
import { fechaHoraCorta, MODALIDADES_BODEGA } from "@iris/sync/campo";
import { ArrowLeft, PackageOpen } from "lucide-react-native";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { Aviso, Boton, Fila, Pila, Tarjeta, Texto, Titulo } from "../componentes";
import { useCampo } from "../campo/contexto";
import { useCargarAlAbrir, useInventario } from "../campo/inventario";
import { useTema } from "../tema/tema";
import { ErrorLectura } from "./PorRecibir";

export function MiBodega() {
    const { navegar } = useCampo();
    const { colores } = useTema();
    const { bodega, cargarBodega } = useInventario();
    useCargarAlAbrir(cargarBodega);
    const d = bodega.datos;
    return (
        <Pila espacio={14}>
            <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "yo" })} estilo={estilos.volver}>
                Yo
            </Boton>
            <Fila entre>
                <View style={estilos.flex}>
                    <Titulo>Mi bodega</Titulo>
                    <Texto suave>{d ? d.bodega.nombre : "Tu inventario en Argos (solo lectura)."}</Texto>
                </View>
                {bodega.estado === "cargando" ? <ActivityIndicator color={colores.primario} accessibilityLabel="Leyendo" /> : null}
            </Fila>
            {bodega.estado === "error" ? <ErrorLectura error={bodega.error} que="tu bodega" reintentar={() => void cargarBodega()} conDatos={d !== null} /> : null}
            {!d && bodega.estado !== "error" ? <ActivityIndicator color={colores.primario} accessibilityLabel="Leyendo tu bodega" /> : null}
            {d ? <ContenidoBodega bodega={d} /> : null}
            {d && bodega.leidoEn !== null ? <Texto suave>Leído de Argos {fechaHoraCorta(d.leidoEn) ?? ""}</Texto> : null}
        </Pila>
    );
}

const mayuscula = (texto: string) => texto.charAt(0).toUpperCase() + texto.slice(1);

function ContenidoBodega({ bodega }: { bodega: DatosBodega }) {
    const { navegar } = useCampo();
    const existencias = bodega.existencias ?? [];
    const unidades = bodega.unidades ?? [];
    const kardex = bodega.kardex ?? [];
    const porRecibir = bodega.trasladosPendientes ?? [];
    return (
        <>
            {porRecibir.length > 0 ? (
                <Aviso
                    tono="info"
                    icono={PackageOpen}
                    titulo={`Tienes ${porRecibir.length} ${porRecibir.length === 1 ? "traslado" : "traslados"} por recibir`}
                    accion={
                        <Boton variante="secundario" alPresionar={() => navegar({ tipo: "recibir" })}>
                            Ver
                        </Boton>
                    }
                />
            ) : null}
            <Tarjeta titulo="Existencias" testID="existencias">
                {existencias.length === 0 ? (
                    <Texto suave>Tu bodega está vacía.</Texto>
                ) : (
                    existencias.map((e) => (
                        <Fila key={e.productoId} entre>
                            <View style={estilos.flex}>
                                <Texto>{e.descripcion}</Texto>
                                {MODALIDADES_BODEGA[e.modalidad] ? <Texto suave>{MODALIDADES_BODEGA[e.modalidad]}</Texto> : null}
                            </View>
                            <Texto negrita>
                                {e.cantidad.toLocaleString("es-GT")} {e.unidad}
                            </Texto>
                        </Fila>
                    ))
                )}
            </Tarjeta>
            {unidades.length > 0 ? (
                <Tarjeta titulo={`Equipos con serie (${unidades.length})`}>
                    {unidades.map((u) => (
                        <View key={u.id}>
                            <Texto negrita>{u.descripcion}</Texto>
                            <Texto mono suave>
                                {u.serie}
                                {u.mac ? ` · ${u.mac}` : ""}
                                {u.estado ? ` · ${u.estado.replaceAll("_", " ")}` : ""}
                            </Texto>
                        </View>
                    ))}
                </Tarjeta>
            ) : null}
            <Tarjeta titulo="Movimientos recientes">
                {kardex.length === 0 ? (
                    <Texto suave>Sin movimientos recientes.</Texto>
                ) : (
                    kardex.map((m) => (
                        <View key={m.id}>
                            <Fila entre>
                                <Texto negrita estilo={estilos.flex}>
                                    {m.descripcion}
                                </Texto>
                                <Texto negrita>{m.cantidad.toLocaleString("es-GT")}</Texto>
                            </Fila>
                            <Texto suave>
                                {mayuscula(m.tipo.replaceAll("_", " "))}
                                {m.referencia ? ` · ${m.referencia}` : ""}
                                {m.ocurridoEn ? ` · ${fechaHoraCorta(m.ocurridoEn) ?? ""}` : ""}
                            </Texto>
                        </View>
                    ))
                )}
            </Tarjeta>
        </>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    volver: { alignSelf: "flex-start", paddingHorizontal: 0 },
});
