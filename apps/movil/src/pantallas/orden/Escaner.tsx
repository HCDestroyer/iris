// Escáner de serie/MAC con la cámara (expo-camera, lector de códigos nativo: QR, Code 128, Code 39,
// EAN, Data Matrix…). Sin permiso se explica y se escribe a mano.
import { CameraView, useCameraPermissions, type BarcodeType } from "expo-camera";
import { X } from "lucide-react-native";
import { useRef } from "react";
import { Modal, StyleSheet, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Aviso, Boton, Texto } from "../../componentes";
import { useTema } from "../../tema/tema";

export const TIPOS_CODIGO: BarcodeType[] = ["qr", "code128", "code39", "code93", "datamatrix", "ean13", "ean8", "upc_a", "upc_e", "itf14", "codabar", "pdf417", "aztec"];

export function Escaner({ abierto, alLeer, alCerrar }: { abierto: boolean; alLeer: (texto: string) => void; alCerrar: () => void }) {
    const { colores } = useTema();
    const [permiso, pedirPermiso] = useCameraPermissions();
    // El lector entrega el mismo código varias veces por segundo: solo vale el primero.
    const leido = useRef(false);
    return (
        <Modal visible={abierto} animationType="slide" onRequestClose={alCerrar} onShow={() => {
                leido.current = false;
            }}>
            <SafeAreaView style={[estilos.pantalla, { backgroundColor: colores.fondo }]}>
                <View style={estilos.barra}>
                    <Texto negrita>Escanear serie o MAC</Texto>
                    <Boton variante="fantasma" icono={X} alPresionar={alCerrar}>
                        Cerrar
                    </Boton>
                </View>
                {!permiso ? null : permiso.granted ? (
                    <CameraView
                        style={estilos.camara}
                        facing="back"
                        barcodeScannerSettings={{ barcodeTypes: TIPOS_CODIGO }}
                        onBarcodeScanned={(r) => {
                            if (!leido.current && r.data) {
                                leido.current = true;
                                alLeer(r.data);
                            }
                        }}
                    />
                ) : (
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
                <Texto suave estilo={estilos.ayuda}>
                    Apunta al código de barras o QR de la etiqueta del equipo.
                </Texto>
            </SafeAreaView>
        </Modal>
    );
}

const estilos = StyleSheet.create({
    pantalla: { flex: 1 },
    barra: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingHorizontal: 16, paddingVertical: 8 },
    camara: { flex: 1 },
    sinPermiso: { flex: 1, padding: 16 },
    ayuda: { padding: 16 },
});
