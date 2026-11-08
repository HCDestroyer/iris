// Límite de errores de la raíz: si una pantalla falla al dibujarse, apaga el GPS y borra la marca en
// el acto (sin esperar la cola del rastreo, que puede haber quedado a medias) y muestra un error con
// «Reintentar». Lo pendiente sigue guardado en el teléfono.
import { TriangleAlert } from "lucide-react-native";
import { Component, type ErrorInfo, type ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import { Aviso, Boton } from "../componentes";
import { PantallaBloqueo } from "./Bloqueo";
import { apagarGpsYa, type GpsFondo } from "../campo/ubicacion";
import type { AlmacenClaves } from "../entorno";
import { useTema } from "../tema/tema";

interface Props {
    children: ReactNode;
    claves: () => AlmacenClaves;
    gps: Pick<GpsFondo, "detener">;
    /** «Usar otra cuenta»: cierra la sesión del teléfono; luego la app vuelve a abrir (al login). */
    usarOtraCuenta?: () => Promise<void>;
}

export class RecuperarError extends Component<Props, { error: Error | null }> {
    override state: { error: Error | null } = { error: null };

    static getDerivedStateFromError(error: Error) {
        return { error };
    }

    override componentDidCatch(error: Error, info: ErrorInfo): void {
        void apagarGpsYa(this.props.claves(), this.props.gps);
        console.error("Iris Campo: error de pantalla", error, info.componentStack);
    }

    override render() {
        if (this.state.error) {
            const reintentar = () => this.setState({ error: null });
            const usarOtraCuenta = this.props.usarOtraCuenta;
            return (
                <PantallaError
                    alReintentar={reintentar}
                    alUsarOtraCuenta={async () => {
                        await usarOtraCuenta?.().catch(() => undefined);
                        reintentar();
                    }}
                />
            );
        }
        return this.props.children;
    }
}

function PantallaError({ alReintentar, alUsarOtraCuenta }: { alReintentar: () => void; alUsarOtraCuenta: () => Promise<void> }) {
    const { campo } = useTema();
    return (
        <View style={[estilos.pantalla, { backgroundColor: campo.lienzo }]}>
            <PantallaBloqueo titulo="Algo falló en la app" explicacion="Puedes volver a intentarlo o entrar con otra cuenta." alUsarOtraCuenta={alUsarOtraCuenta}>
                <Aviso tono="error" icono={TriangleAlert} titulo="Algo falló en la app" testID="pantalla-error" accion={<Boton alPresionar={alReintentar}>Reintentar</Boton>}>
                    Por seguridad se apagó la ubicación. Lo que hiciste sigue guardado en el teléfono.
                </Aviso>
            </PantallaBloqueo>
        </View>
    );
}

const estilos = StyleSheet.create({
    pantalla: { flex: 1, justifyContent: "center", paddingHorizontal: 20, paddingVertical: 32 },
});
