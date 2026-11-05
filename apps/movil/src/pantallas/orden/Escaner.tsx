// Escáner de serie o MAC por campo (expo-camera, lector nativo: QR, Code 128/39/93, EAN, UPC, Data
// Matrix…). Abre sabiendo qué escanea, oscurece la cámara salvo el recuadro (solo cuentan los
// códigos de adentro), junta los distintos durante una ventana corta y muestra la hoja con lo
// detectado: nunca acepta en silencio. Sin permiso se explica y se escribe a mano.
import { candidatoUnico, preguntaConfirmacion, textoRepetido, textosEscaneo, type Candidato, type ObjetivoEscaneo, type UsoCodigo } from "@iris/sync/campo";
import { CameraView, useCameraPermissions, type BarcodeType } from "expo-camera";
import { Barcode, CircleCheck, CircleSlash, Flashlight, FlashlightOff, Info, Keyboard, Package, QrCode, ScanLine, TriangleAlert, X, type LucideIcon } from "lucide-react-native";
import { useState } from "react";
import { Modal, ScrollView, StyleSheet, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Aviso, Boton, BotonIcono, Insignia, Texto, Tocable } from "../../componentes";
import { useEscaneo } from "../../campo/escaner";
import { useTema } from "../../tema/tema";
import { OSCURO } from "../../tema/tokens";

export const TIPOS_CODIGO: BarcodeType[] = ["qr", "code128", "code39", "code93", "datamatrix", "ean13", "ean8", "upc_a", "upc_e", "itf14", "codabar", "pdf417", "aztec"];

export interface PropsEscaner {
    /** Campo que se escanea; `null` = cerrado. */
    objetivo: ObjetivoEscaneo | null;
    /** Equipos ya registrados en esta orden (aviso de serie o MAC repetida). */
    existentes: readonly { serie?: string | null; mac?: string | null }[];
    alElegir: (uso: UsoCodigo) => void;
    alEscribirAMano: () => void;
    alCerrar: () => void;
}

export function Escaner({ objetivo, ...resto }: PropsEscaner) {
    return (
        <Modal visible={objetivo !== null} animationType="slide" onRequestClose={resto.alCerrar}>
            {objetivo ? <Visor key={objetivo} objetivo={objetivo} {...resto} /> : null}
        </Modal>
    );
}

/** El visor va siempre sobre la cámara: colores del tema oscuro de Horus (texto claro, teal 400). */
const VISOR = OSCURO;

function Visor({ objetivo, existentes, alElegir, alEscribirAMano, alCerrar }: Omit<PropsEscaner, "objetivo"> & { objetivo: ObjetivoEscaneo }) {
    const [permiso, pedirPermiso] = useCameraPermissions();
    const [linterna, setLinterna] = useState(false);
    const { estado, recuadro, setVista, leer, seguir } = useEscaneo(objetivo, existentes);
    const textos = textosEscaneo(objetivo);
    const conCamara = permiso?.granted === true;
    return (
        <View style={[estilos.pantalla, { backgroundColor: VISOR.fondo }]} testID="escaner">
            {conCamara ? (
                <View style={StyleSheet.absoluteFill} onLayout={(e) => setVista({ ancho: e.nativeEvent.layout.width, alto: e.nativeEvent.layout.height })} testID="visor">
                    <CameraView
                        style={StyleSheet.absoluteFill}
                        facing="back"
                        enableTorch={linterna}
                        barcodeScannerSettings={{ barcodeTypes: TIPOS_CODIGO }}
                        // Con la hoja abierta no se lee (la cámara sigue encendida).
                        onBarcodeScanned={estado.candidatos ? undefined : leer}
                    />
                    {recuadro ? (
                        <>
                            {/* Lo de afuera del recuadro se oscurece; las esquinas van en teal. */}
                            <View style={[estilos.velo, { top: 0, left: 0, right: 0, height: recuadro.y }]} />
                            <View style={[estilos.velo, { top: recuadro.y + recuadro.alto, left: 0, right: 0, bottom: 0 }]} />
                            <View style={[estilos.velo, { top: recuadro.y, left: 0, width: recuadro.x, height: recuadro.alto }]} />
                            <View style={[estilos.velo, { top: recuadro.y, left: recuadro.x + recuadro.ancho, right: 0, height: recuadro.alto }]} />
                            <View pointerEvents="none" testID="recuadro" style={[estilos.recuadro, { top: recuadro.y, left: recuadro.x, width: recuadro.ancho, height: recuadro.alto }]}>
                                {ESQUINAS.map((e) => (
                                    <View key={e.clave} style={[estilos.esquina, e.estilo, { borderColor: VISOR.primario }]} />
                                ))}
                            </View>
                            <Text accessibilityLiveRegion="polite" style={[estilos.detectados, { top: recuadro.y + recuadro.alto + 12, color: VISOR.texto }]}>
                                Detectados: {estado.detectados}
                            </Text>
                        </>
                    ) : null}
                </View>
            ) : null}
            <SafeAreaView edges={["top"]} style={[estilos.barra, { backgroundColor: VISOR.vidrio }]}>
                <View style={estilos.flex}>
                    <Text accessibilityRole="header" style={[estilos.encabezado, { color: VISOR.texto }]}>
                        Escaneando: <Text style={estilos.negrita}>{textos.titulo}</Text>
                    </Text>
                    <Text style={[estilos.ayuda, { color: VISOR.textoSuave }]}>{textos.ayuda}</Text>
                </View>
                {conCamara ? (
                    <BotonIcono
                        variante="fantasma"
                        icono={linterna ? FlashlightOff : Flashlight}
                        etiqueta={linterna ? "Apagar linterna" : "Encender linterna"}
                        seleccionado={linterna}
                        colorIcono={VISOR.texto}
                        alPresionar={() => setLinterna((l) => !l)}
                    />
                ) : null}
                <BotonIcono variante="fantasma" icono={X} etiqueta="Cerrar escáner" colorIcono={VISOR.texto} alPresionar={alCerrar} />
            </SafeAreaView>
            {!permiso || conCamara ? null : (
                <View style={estilos.sinPermiso}>
                    <Aviso
                        tono="warning"
                        titulo="Sin permiso de cámara"
                        accion={
                            permiso.canAskAgain ? (
                                <Boton variante="secundario" alPresionar={() => void pedirPermiso()}>
                                    Permitir cámara
                                </Boton>
                            ) : undefined
                        }
                    >
                        {permiso.canAskAgain
                            ? "Iris usa la cámara solo para leer el código del equipo."
                            : "Actívalo en los ajustes del teléfono o escribe la serie y la MAC a mano."}
                    </Aviso>
                </View>
            )}
            <View style={estilos.relleno} pointerEvents="none" />
            <SafeAreaView edges={["bottom"]} style={[estilos.pie, { backgroundColor: VISOR.vidrio }]}>
                <Boton variante="secundario" icono={Keyboard} alPresionar={alEscribirAMano}>
                    Escribir a mano
                </Boton>
            </SafeAreaView>
            {estado.candidatos ? <HojaCodigos objetivo={objetivo} candidatos={estado.candidatos} alElegir={alElegir} alSeguir={seguir} alEscribirAMano={alEscribirAMano} /> : null}
        </View>
    );
}

/** Icono de lo que parece: encaja, código de producto, MAC (cuando se pide la serie) o no encaja. */
function iconoDe(c: Candidato): LucideIcon {
    return c.encaja ? CircleCheck : c.clase === "producto" ? Package : c.clase === "mac" ? Info : CircleSlash;
}

/** Hoja inferior con lo detectado: confirmación de un solo candidato o la lista para elegir. */
function HojaCodigos({
    objetivo,
    candidatos,
    alElegir,
    alSeguir,
    alEscribirAMano,
}: {
    objetivo: ObjetivoEscaneo;
    candidatos: Candidato[];
    alElegir: (uso: UsoCodigo) => void;
    alSeguir: () => void;
    alEscribirAMano: () => void;
}) {
    const { colores, radioTarjeta } = useTema();
    const unico = candidatoUnico(candidatos);
    const otros = candidatos.filter((c) => c !== unico);
    return (
        <View
            accessibilityViewIsModal
            accessibilityLabel="Códigos detectados"
            testID="hoja-codigos"
            style={[estilos.hoja, { backgroundColor: colores.fondo, borderColor: colores.borde, borderTopLeftRadius: radioTarjeta, borderTopRightRadius: radioTarjeta }]}
        >
            <ScrollView contentContainerStyle={estilos.contenidoHoja}>
                {unico ? (
                    <>
                        <Text accessibilityRole="header" style={[estilos.tituloHoja, { color: colores.texto }]}>
                            {preguntaConfirmacion(objetivo, unico)}
                        </Text>
                        <FichaCandidato candidato={unico} />
                        <Boton icono={CircleCheck} alPresionar={() => alElegir(unico.uso)}>
                            Usar
                        </Boton>
                        <Boton variante="secundario" icono={ScanLine} alPresionar={alSeguir}>
                            Seguir escaneando
                        </Boton>
                    </>
                ) : (
                    <>
                        <Text accessibilityRole="header" style={[estilos.tituloHoja, { color: colores.texto }]}>
                            {objetivo === "serie" ? "Elige el código del número de serie" : "Elige el código de la MAC"}
                        </Text>
                        <Texto suave>{candidatos.length === 1 ? "1 código detectado" : `${candidatos.length} códigos detectados`}. Toca el que corresponde.</Texto>
                    </>
                )}
                {otros.length > 0 ? (
                    <View style={estilos.lista} testID={unico ? "otros-codigos" : "lista-codigos"}>
                        {unico ? <Texto negrita>Otros códigos detectados</Texto> : null}
                        {otros.map((c) => (
                            <Tocable
                                key={c.valor}
                                etiqueta={`${c.tipoTexto} ${c.formatoTexto}: ${c.valor}. ${c.insignia}`}
                                deshabilitado={!c.elegible}
                                alPresionar={() => alElegir(c.uso)}
                                estilo={[estilos.fila, { borderColor: colores.borde, opacity: c.elegible ? 1 : 0.7 }]}
                                testID="codigo-detectado"
                            >
                                <FichaCandidato candidato={c} />
                            </Tocable>
                        ))}
                    </View>
                ) : null}
                {unico ? null : (
                    <Boton variante="secundario" icono={ScanLine} alPresionar={alSeguir}>
                        Seguir escaneando
                    </Boton>
                )}
                <Boton variante="fantasma" icono={Keyboard} alPresionar={alEscribirAMano}>
                    Escribir a mano
                </Boton>
            </ScrollView>
        </View>
    );
}

/** Tipo y formato, valor en monoespaciada y lo que parece (icono + texto + color). */
function FichaCandidato({ candidato: c }: { candidato: Candidato }) {
    const { colores, fuenteMono } = useTema();
    const Tipo = c.tipo === "qr" ? QrCode : Barcode;
    const repetido = textoRepetido(c.repetidos);
    return (
        <View style={estilos.ficha}>
            <View style={estilos.filaTipo}>
                <Tipo size={16} color={colores.textoSuave} />
                <Texto negrita>{c.tipoTexto}</Texto>
                <Text style={[estilos.formato, { color: colores.textoSuave }]}>{c.formatoTexto}</Text>
            </View>
            {c.uso.serie && c.uso.mac ? (
                <>
                    <Text style={[estilos.valor, { color: colores.texto, fontFamily: fuenteMono }]}>S/N {c.uso.serie}</Text>
                    <Text style={[estilos.valor, { color: colores.texto, fontFamily: fuenteMono }]}>MAC {c.uso.mac}</Text>
                </>
            ) : (
                <Text style={[estilos.valor, { color: colores.texto, fontFamily: fuenteMono }]}>{c.valor}</Text>
            )}
            <Insignia tono={c.tono} icono={iconoDe(c)} texto={c.insignia} />
            {repetido ? <Insignia tono="warning" icono={TriangleAlert} texto={repetido} /> : null}
        </View>
    );
}

const ESQUINAS = [
    { clave: "si", estilo: { top: 0, left: 0, borderTopWidth: 4, borderLeftWidth: 4, borderTopLeftRadius: 8 } },
    { clave: "sd", estilo: { top: 0, right: 0, borderTopWidth: 4, borderRightWidth: 4, borderTopRightRadius: 8 } },
    { clave: "ii", estilo: { bottom: 0, left: 0, borderBottomWidth: 4, borderLeftWidth: 4, borderBottomLeftRadius: 8 } },
    { clave: "id", estilo: { bottom: 0, right: 0, borderBottomWidth: 4, borderRightWidth: 4, borderBottomRightRadius: 8 } },
] as const;

const estilos = StyleSheet.create({
    pantalla: { flex: 1 },
    flex: { flex: 1 },
    barra: { flexDirection: "row", alignItems: "flex-start", gap: 4, paddingHorizontal: 16, paddingBottom: 12, paddingTop: 8 },
    encabezado: { fontSize: 24, lineHeight: 30 },
    negrita: { fontWeight: "700" },
    ayuda: { fontSize: 15, marginTop: 4 },
    velo: { position: "absolute", backgroundColor: VISOR.vidrio },
    recuadro: { position: "absolute" },
    esquina: { position: "absolute", width: 32, height: 32 },
    detectados: { position: "absolute", left: 0, right: 0, textAlign: "center", fontSize: 16, fontWeight: "600" },
    sinPermiso: { padding: 16 },
    relleno: { flex: 1 },
    pie: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 12 },
    hoja: { position: "absolute", left: 0, right: 0, bottom: 0, maxHeight: "80%", borderWidth: 1 },
    contenidoHoja: { padding: 16, gap: 12, paddingBottom: 32 },
    tituloHoja: { fontSize: 18, fontWeight: "600" },
    lista: { gap: 8 },
    fila: { borderWidth: 1, borderRadius: 6, padding: 12 },
    ficha: { gap: 6 },
    filaTipo: { flexDirection: "row", alignItems: "center", gap: 6 },
    formato: { fontSize: 13 },
    valor: { fontSize: 18 },
});
