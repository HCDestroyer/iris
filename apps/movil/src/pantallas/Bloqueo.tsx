// Pantallas de bloqueo al abrir la app (reporte del PM, APK 0.18.0): con la opción B del login
// aprobada (logo centrado, título, explicación corta, el contenido y «Usar otra cuenta»). Toda
// pantalla que no deja seguir —también en sus estados de error— tiene salida: «Usar otra cuenta»
// cierra la sesión y lleva a entrar, y el «Atrás» de Android hace lo mismo (nunca sale de la app
// dejando a la persona atrapada al volver).
// - «Crea tu contraseña»: entró con una contraseña temporal. La cambia aquí mismo con el formulario
//   de «Yo» (`POST /yo/contrasena`, lo único que la sesión limitada puede hacer además de salir) y
//   entra directo a la app.
import type { Schema } from "@iris/contratos";
import { TEXTO_SESIONES_CONTRASENA } from "@iris/sync/campo";
import { Clock, CloudOff, LogOut } from "lucide-react-native";
import { useCallback, useState, type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Aviso, Boton, LogoIris, Pila, Texto } from "../componentes";
import { useAtrasAndroid } from "../campo/atras";
import { useTema } from "../tema/tema";
import { errorDePantalla, useFormularioContrasena, type ErrorPantalla } from "./Cuenta";

/**
 * Marco de una pantalla de bloqueo: logo centrado, título y explicación (como «Iris Campo» del
 * login), el contenido y, al pie, «Usar otra cuenta». «Atrás» de Android = «Usar otra cuenta».
 */
export function PantallaBloqueo({
    titulo,
    explicacion,
    children,
    alUsarOtraCuenta,
    testID,
}: {
    titulo: string;
    explicacion: ReactNode;
    children?: ReactNode;
    /** Cierra la sesión y lleva a entrar. Devuelve una promesa mientras sale. */
    alUsarOtraCuenta: () => Promise<void> | void;
    testID?: string;
}) {
    const { colores } = useTema();
    const [saliendo, setSaliendo] = useState(false);
    const salir = useCallback(() => {
        if (saliendo) {
            return;
        }
        setSaliendo(true);
        void Promise.resolve(alUsarOtraCuenta()).finally(() => setSaliendo(false));
    }, [alUsarOtraCuenta, saliendo]);
    useAtrasAndroid(
        useCallback(() => {
            salir();
            return true;
        }, [salir]),
    );
    return (
        <Pila espacio={20}>
            <View style={estilos.marca} testID={testID}>
                <LogoIris tamano={64} />
                <Text accessibilityRole="header" style={[estilos.titulo, { color: colores.texto }]}>
                    {titulo}
                </Text>
                <Texto suave estilo={estilos.centrado}>
                    {explicacion}
                </Texto>
            </View>
            {children}
            <Boton variante="fantasma" icono={LogOut} cargando={saliendo} alPresionar={salir} testID="usar-otra-cuenta">
                Usar otra cuenta
            </Boton>
        </Pila>
    );
}

export interface PropsCrearContrasena {
    /** Nombre de pila para el saludo. */
    nombre: string;
    /** La contraseña quedó cambiada: entra a la app. */
    alListo: (usuario: Schema<"Usuario">) => void;
    /** «Usar otra cuenta» (y «Atrás» de Android). */
    alUsarOtraCuenta: () => Promise<void> | void;
    /** La API cerró la sesión (429 por intentos, o 401): a entrar. */
    alSesionCerrada: () => void;
}

/** «Crea tu contraseña»: la temporal, la nueva y repetirla; al guardar, entra directo. */
export function CrearContrasena({ nombre, alListo, alUsarOtraCuenta, alSesionCerrada }: PropsCrearContrasena) {
    const { colores } = useTema();
    const [error, setError] = useState<ErrorPantalla | null>(null);
    const { enviar, campos, enviando } = useFormularioContrasena({
        etiquetaActual: "Contraseña temporal",
        alCambiada: alListo,
        alError: (e) => setError(errorDePantalla(e, "guardar tu contraseña")),
    });
    const guardar = () => {
        setError(null);
        return enviar();
    };
    const saludo = nombre ? `Hola, ${nombre}. ` : "";
    return (
        <PantallaBloqueo
            titulo="Crea tu contraseña"
            explicacion={`${saludo}Entraste con una contraseña temporal. Elige una nueva que solo tú conozcas para seguir.`}
            alUsarOtraCuenta={alUsarOtraCuenta}
            testID="crear-contrasena"
        >
            {error?.tipo === "sesion" ? (
                <Aviso
                    tono="warning"
                    icono={Clock}
                    titulo={error.titulo}
                    testID="aviso-sesion-cerrada"
                    accion={<Boton alPresionar={alSesionCerrada}>Ir a iniciar sesión</Boton>}
                >
                    {error.detalle}
                </Aviso>
            ) : (
                <>
                    {error ? (
                        <Aviso
                            tono={error.tipo === "sin_senal" ? "warning" : "error"}
                            icono={error.tipo === "sin_senal" ? CloudOff : undefined}
                            titulo={error.tipo === "sin_senal" ? "Sin señal" : error.titulo}
                            testID="aviso-error-contrasena"
                            accion={
                                <Boton variante="secundario" alPresionar={() => void guardar()}>
                                    Reintentar
                                </Boton>
                            }
                        >
                            {error.tipo === "sin_senal" ? "No llegó a Iris. Busca señal y vuelve a intentarlo." : error.detalle}
                        </Aviso>
                    ) : null}
                    {campos}
                    <Text style={[estilos.nota, { color: colores.textoSuave }]}>{TEXTO_SESIONES_CONTRASENA}</Text>
                    <Boton variante="tinta" cargando={enviando} alPresionar={() => void guardar()} estilo={estilos.pildora} testID="guardar-contrasena">
                        {enviando ? "Guardando…" : "Guardar y entrar"}
                    </Boton>
                </>
            )}
        </PantallaBloqueo>
    );
}

const estilos = StyleSheet.create({
    marca: { alignItems: "center", gap: 10, paddingTop: 24, paddingBottom: 8 },
    titulo: { fontSize: 22, lineHeight: 28, fontWeight: "700", letterSpacing: -0.2, textAlign: "center" },
    centrado: { textAlign: "center" },
    nota: { fontSize: 15, lineHeight: 21, paddingHorizontal: 6 },
    pildora: { minHeight: 52, borderRadius: 999 },
});
