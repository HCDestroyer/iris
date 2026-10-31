// «Yo»: quién está en el teléfono, «Mi bodega», actualizar el día, cambiar de empresa y cerrar
// sesión (con lo pendiente: enviar y salir, o salir conservándolo). Como la PWA; el tema sigue al
// del teléfono.
import { Building2, ChevronRight, LogOut, RefreshCw, Warehouse } from "lucide-react-native";
import { useState } from "react";
import { StyleSheet, View } from "react-native";
import { Aviso, Boton, Pila, Tarjeta, Texto, Titulo, Tocable } from "../componentes";
import { useCampo } from "../campo/contexto";
import { porEnviarEnTelefono } from "../campo/yo";
import { useTema } from "../tema/tema";

export function Yo({ empresa, cambiarEmpresa, bajarPaquete }: { empresa: string | null; cambiarEmpresa: (() => void) | undefined; bajarPaquete: () => Promise<void> }) {
    const { estado, sesion, motor, cerrarSesion, notificar, navegar } = useCampo();
    const { colores, radioTarjeta } = useTema();
    const [confirmarEmpresa, setConfirmarEmpresa] = useState(false);
    const [confirmarSalida, setConfirmarSalida] = useState(false);
    const [saliendo, setSaliendo] = useState(false);
    const porEnviar = porEnviarEnTelefono(estado);
    const conSenal = estado.conexion === "en_linea" || estado.conexion === "desconocida";

    const salir = async (conservarPendientes: boolean) => {
        setSaliendo(true);
        try {
            await cerrarSesion({ conservarPendientes });
        } catch {
            notificar({ tono: "error", texto: "No se pudo cerrar la sesión en el teléfono. Vuelve a intentarlo." });
            setSaliendo(false);
        }
    };

    const enviarYSalir = async () => {
        setSaliendo(true);
        try {
            await motor.sincronizar();
        } catch {
            // Lo que no salió se informa abajo.
        }
        const quedan = porEnviarEnTelefono(motor.obtenerEstado());
        if (quedan === 0) {
            await salir(false);
            return;
        }
        setSaliendo(false);
        notificar({ tono: "warning", texto: `No se pudo enviar todo: quedan ${quedan}. Revisa «Pendientes» o sal conservándolos.` });
    };

    return (
        <Pila espacio={14}>
            <View>
                <Titulo>{sesion.nombre}</Titulo>
                <Texto suave>
                    {sesion.correo}
                    {empresa ? ` · ${empresa}` : ""}
                </Texto>
            </View>

            <Tocable
                etiqueta="Mi bodega"
                alPresionar={() => navegar({ tipo: "bodega" })}
                estilo={[estilos.enlace, { borderColor: colores.borde, backgroundColor: colores.tarjeta, borderRadius: radioTarjeta }]}
            >
                <Warehouse size={22} color={colores.primario} />
                <Texto negrita estilo={estilos.flex}>
                    Mi bodega
                </Texto>
                <ChevronRight size={20} color={colores.textoSuave} />
            </Tocable>

            <Tarjeta titulo="Este teléfono">
                <Boton variante="secundario" icono={RefreshCw} alPresionar={() => void bajarPaquete()}>
                    Actualizar órdenes del día
                </Boton>
                {cambiarEmpresa ? (
                    confirmarEmpresa ? (
                        <Aviso
                            tono="warning"
                            titulo={`Quedan ${porEnviar} cosas por enviar en ${empresa ?? "esta empresa"}`}
                            accion={
                                <>
                                    <Boton variante="fantasma" alPresionar={() => setConfirmarEmpresa(false)}>
                                        Quedarme
                                    </Boton>
                                    <Boton variante="secundario" alPresionar={cambiarEmpresa}>
                                        Cambiar de empresa
                                    </Boton>
                                </>
                            }
                        >
                            Se enviarán cuando vuelvas a esta empresa. ¿Cambiar de todos modos?
                        </Aviso>
                    ) : (
                        <Boton variante="secundario" icono={Building2} alPresionar={() => (porEnviar > 0 ? setConfirmarEmpresa(true) : cambiarEmpresa())}>
                            Cambiar de empresa
                        </Boton>
                    )
                ) : null}
            </Tarjeta>

            {confirmarSalida && porEnviar > 0 ? (
                <Aviso
                    tono="warning"
                    titulo={`Tienes ${porEnviar} ${porEnviar === 1 ? "cambio" : "cambios"} sin enviar`}
                    testID="confirmar-salida"
                    accion={
                        <>
                            <Boton variante="fantasma" deshabilitado={saliendo} alPresionar={() => setConfirmarSalida(false)}>
                                Quedarme
                            </Boton>
                            <Boton variante="secundario" deshabilitado={saliendo} alPresionar={() => void salir(true)}>
                                Salir y conservarlos
                            </Boton>
                            {conSenal ? (
                                <Boton cargando={saliendo} alPresionar={() => void enviarYSalir()}>
                                    Enviar y salir
                                </Boton>
                            ) : null}
                        </>
                    }
                >
                    {conSenal
                        ? "Envíalos antes de salir, o sal y consérvalos: se guardan sin datos de clientes y se envían cuando vuelvas a entrar."
                        : "Sin señal no se pueden enviar. Si sales, se conservan sin datos de clientes y se envían cuando vuelvas a entrar."}
                </Aviso>
            ) : (
                <Boton variante="peligro" icono={LogOut} cargando={saliendo} alPresionar={() => (porEnviar > 0 ? setConfirmarSalida(true) : void salir(false))}>
                    {saliendo ? "Cerrando sesión…" : "Cerrar sesión"}
                </Boton>
            )}
        </Pila>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    enlace: { minHeight: 56, flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 16, borderWidth: 1 },
});
