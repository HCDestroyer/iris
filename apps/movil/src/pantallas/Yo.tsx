// «Yo» (diseño aprobado por el PM, nov 2026): el nombre y «Rol · Empresa» de `GET /yo`; Apariencia
// (Teléfono · Claro · Oscuro, guardada en el teléfono); Seguridad (cambiar contraseña y
// verificación en dos pasos, cada una en su pantalla, y «Desbloquear con huella»); Avisos
// («Notificaciones», con su pantalla propia, en la app nativa); Este teléfono
// («Mi bodega», «Por enviar», actualizar el día, cambiar de empresa) y «Cerrar sesión» (con lo
// pendiente: enviar y salir, o salir conservándolo). Como la PWA.
import { APARIENCIAS, estadoDosPasos, lineaPerfil, totalPorEnviar, type Apariencia } from "@iris/sync/campo";
import { Bell, Building2, CloudUpload, Fingerprint, KeyRound, LogOut, Moon, Package, RefreshCw, ShieldCheck, Smartphone, Sun, type LucideIcon } from "lucide-react-native";
import { useEffect, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { ErrorApi } from "../api/errores";
import { Aviso, Boton, Pila } from "../componentes";
import { BotonContornoPeligro, FilaAjuste, FilaInterruptor, GrupoFilas, PuntoEstado, Seccion, Segmentado } from "../componentes/cuenta";
import { useCampo } from "../campo/contexto";
import { useCuenta } from "../campo/cuenta";
import { porEnviarEnTelefono } from "../campo/yo";
import { activarHuella, biometriaDisponible, huellaActiva } from "../sesion/sesion";
import { useApariencia, useTema } from "../tema/tema";
import { usePush } from "../push/contexto";
import { resumenNotificaciones } from "../push/textos";
import { CambiarContrasena, DosPasos } from "./Cuenta";
import { Notificaciones } from "./Notificaciones";

const ICONOS_APARIENCIA: Record<Apariencia, LucideIcon> = { telefono: Smartphone, claro: Sun, oscuro: Moon };
const OPCIONES_APARIENCIA = APARIENCIAS.map((a) => ({ ...a, icono: ICONOS_APARIENCIA[a.valor] }));

export function Yo({ empresa, cambiarEmpresa, bajarPaquete }: { empresa: string | null; cambiarEmpresa: (() => void) | undefined; bajarPaquete: () => Promise<void> }) {
    const { vista } = useCampo();
    const seccion = vista.tipo === "yo" ? vista.seccion : undefined;
    if (seccion === "contrasena") {
        return <CambiarContrasena />;
    }
    if (seccion === "dos_pasos") {
        return <DosPasos />;
    }
    if (seccion === "notificaciones") {
        return <Notificaciones />;
    }
    return <Perfil empresa={empresa} cambiarEmpresa={cambiarEmpresa} bajarPaquete={bajarPaquete} />;
}

function Perfil({ empresa, cambiarEmpresa, bajarPaquete }: { empresa: string | null; cambiarEmpresa: (() => void) | undefined; bajarPaquete: () => Promise<void> }) {
    const { estado, sesion, motor, cerrarSesion, notificar, navegar } = useCampo();
    const { colores } = useTema();
    const { apariencia, elegirApariencia } = useApariencia();
    const cuenta = useCuenta(sesion.empresaActiva);
    const huella = useBiometria();
    const [confirmarEmpresa, setConfirmarEmpresa] = useState(false);
    const [confirmarSalida, setConfirmarSalida] = useState(false);
    const [saliendo, setSaliendo] = useState(false);
    const porEnviar = porEnviarEnTelefono(estado);
    const enCola = totalPorEnviar(estado);
    const conSenal = estado.conexion === "en_linea" || estado.conexion === "desconocida";
    const yo = cuenta.estado === "listo" ? cuenta.yo : null;
    const linea = lineaPerfil(yo?.usuario.roles, yo?.empresaActiva?.nombre ?? empresa);
    const dosPasos = estadoDosPasos(yo ? yo.usuario.mfaActivo : null);
    const push = usePush();
    const prefs = push?.preferencias.estado === "listo" ? push.preferencias.preferencias : null;

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
        notificar({ tono: "warning", texto: `No se pudo enviar todo: quedan ${quedan}. Revisa «Por enviar» o sal conservándolos.` });
    };

    return (
        <Pila espacio={20}>
            <View style={estilos.perfil} testID="perfil-yo">
                <Text accessibilityRole="header" style={[estilos.nombre, { color: colores.texto }]}>
                    {sesion.nombre}
                </Text>
                {linea ? <Text style={[estilos.linea, { color: colores.textoSuave }]}>{linea}</Text> : null}
            </View>

            <Seccion titulo="Apariencia">
                <Segmentado
                    etiqueta="Apariencia"
                    opciones={OPCIONES_APARIENCIA}
                    valor={apariencia}
                    testID="apariencia"
                    alCambiar={(a) => {
                        elegirApariencia(a);
                        notificar({ tono: "success", texto: a === "telefono" ? "Iris sigue el tema del teléfono." : a === "claro" ? "Iris en modo claro." : "Iris en modo oscuro." });
                    }}
                />
            </Seccion>

            {push ? (
                <Seccion titulo="Avisos">
                    <GrupoFilas testID="grupo-avisos">
                        <FilaAjuste
                            icono={Bell}
                            titulo="Notificaciones"
                            detalle={resumenNotificaciones(prefs, push.permiso)}
                            alPresionar={() => navegar({ tipo: "yo", seccion: "notificaciones" })}
                            testID="fila-notificaciones"
                        />
                    </GrupoFilas>
                </Seccion>
            ) : null}

            <Seccion titulo="Seguridad">
                <GrupoFilas testID="grupo-seguridad">
                    <FilaAjuste icono={KeyRound} titulo="Cambiar contraseña" alPresionar={() => navegar({ tipo: "yo", seccion: "contrasena" })} />
                    <FilaAjuste
                        icono={ShieldCheck}
                        titulo="Verificación en dos pasos"
                        etiqueta={dosPasos ? `Verificación en dos pasos: ${dosPasos.texto}` : "Verificación en dos pasos"}
                        detalle={
                            cuenta.estado === "error" && !(cuenta.error instanceof ErrorApi && cuenta.error.estado === 0)
                                ? "No se pudo leer si está activa."
                                : dosPasos?.tono === "success"
                                  ? "Te pide un código al entrar"
                                  : dosPasos
                                    ? "Recomendada: protege tu cuenta"
                                    : null
                        }
                        derecha={dosPasos ? <PuntoEstado tono={dosPasos.tono} texto={dosPasos.texto} testID="estado-dos-pasos" /> : null}
                        alPresionar={() => navegar({ tipo: "yo", seccion: "dos_pasos" })}
                    />
                    {huella ? <DesbloquearConHuella /> : null}
                </GrupoFilas>
            </Seccion>

            <Seccion titulo="Este teléfono">
                <GrupoFilas>
                    <FilaAjuste icono={Package} titulo="Mi bodega" alPresionar={() => navegar({ tipo: "bodega" })} />
                    {/* «Por enviar» se abre desde aquí (y desde la píldora de estado). */}
                    <FilaAjuste
                        icono={CloudUpload}
                        titulo="Por enviar"
                        etiqueta={enCola > 0 ? `Por enviar (${enCola})` : "Por enviar"}
                        derecha={
                            enCola > 0 ? (
                                <Text style={[estilos.valor, estilos.negrita, { color: colores.advertencia }]}>{String(enCola)}</Text>
                            ) : (
                                <Text style={[estilos.valor, { color: colores.textoSuave }]}>Todo enviado</Text>
                            )
                        }
                        alPresionar={() => navegar({ tipo: "pendientes" })}
                    />
                    <FilaAjuste icono={RefreshCw} titulo="Actualizar órdenes del día" alPresionar={() => void bajarPaquete()} />
                    {cambiarEmpresa ? (
                        <FilaAjuste icono={Building2} titulo="Cambiar de empresa" detalle={empresa} alPresionar={() => (porEnviar > 0 ? setConfirmarEmpresa(true) : cambiarEmpresa())} />
                    ) : null}
                </GrupoFilas>
                {cambiarEmpresa && confirmarEmpresa ? (
                    <Aviso
                        tono="warning"
                        titulo={`Quedan ${porEnviar} cosas por enviar en ${empresa ?? "esta empresa"}`}
                        accion={
                            <>
                                <Boton variante="fantasma" alPresionar={() => setConfirmarEmpresa(false)}>
                                    Quedarme
                                </Boton>
                                <Boton variante="secundario" alPresionar={cambiarEmpresa}>
                                    Cambiar de todos modos
                                </Boton>
                            </>
                        }
                    >
                        Se enviarán cuando vuelvas a esta empresa. ¿Cambiar de todos modos?
                    </Aviso>
                ) : null}
            </Seccion>

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
                <BotonContornoPeligro icono={LogOut} cargando={saliendo} testID="cerrar-sesion" alPresionar={() => (porEnviar > 0 ? setConfirmarSalida(true) : void salir(false))}>
                    {saliendo ? "Cerrando sesión…" : "Cerrar sesión"}
                </BotonContornoPeligro>
            )}
        </Pila>
    );
}

/** ¿Tiene el teléfono biometría? (sin ella no se ofrece «Desbloquear con huella»). */
function useBiometria(): boolean {
    const [disponible, setDisponible] = useState(false);
    useEffect(() => {
        let vivo = true;
        void biometriaDisponible().then((d) => vivo && setDisponible(d));
        return () => {
            vivo = false;
        };
    }, []);
    return disponible;
}

/**
 * «Desbloquear con huella» (apagado por defecto): encendido, la app pide la huella al abrir (la
 * contraseña sigue a mano); apagado, abre con la sesión guardada sin pedir nada.
 */
function DesbloquearConHuella() {
    const { notificar } = useCampo();
    const [activa, setActiva] = useState(() => huellaActiva());
    return (
        <View testID="tarjeta-huella">
            <FilaInterruptor
                icono={Fingerprint}
                titulo="Desbloquear con huella"
                valor={activa}
                testID="interruptor-huella"
                alCambiar={(v) => {
                    activarHuella(v);
                    setActiva(v);
                    notificar({ tono: "success", texto: v ? "Iris te pedirá la huella al abrir." : "Iris abrirá sin pedir la huella." });
                }}
            />
        </View>
    );
}

const estilos = StyleSheet.create({
    perfil: { gap: 4, paddingHorizontal: 6 },
    nombre: { fontSize: 32, fontWeight: "700", letterSpacing: -0.6 },
    linea: { fontSize: 16 },
    valor: { fontSize: 15 },
    negrita: { fontWeight: "700" },
});
