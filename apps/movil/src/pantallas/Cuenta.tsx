// Pantallas propias de «Yo» (diseño aprobado por el PM, nov 2026), cada una con su lugar en la pila
// de vistas (`{ tipo: "yo", seccion }`) y su «Atrás»:
// - «Cambiar contraseña»: actual, nueva y repetir con el ojo; validación espejo de la API con las
//   ayudas en verde; `POST /yo/contrasena` con `Idempotency-Key`; 422 por campo; 429 cierra la
//   sesión (lleva a entrar); sin señal no se intenta (no va a la cola).
// - «Verificación en dos pasos»: apagada, `POST /yo/mfa` una vez por intento (clave nueva: no es
//   repetible) con «Abrir app» (`otpauth://`), la clave en grupos de 4 y el código de 6 dígitos →
//   `POST /yo/mfa/activar`; activa, solo informa (desactivar no existe: la reinicia un admin).
// Salir con algo escrito («Atrás» de la pantalla o de Android) pregunta antes.
import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import {
    agruparSecreto,
    cuerpoContrasenaPropia,
    enlaceOtpauth,
    esquemaCodigoActivar,
    esquemaContrasenaPropia,
    LONGITUD_MINIMA_CONTRASENA,
    reglasContrasena,
    TEXTO_ACTIVAR_DOS_PASOS,
    TEXTO_REINICIAR_DOS_PASOS,
    TEXTO_SESIONES_CONTRASENA,
    TEXTO_SIN_SENAL_CUENTA,
    textoBloqueoContrasena,
    type DatosContrasenaPropia,
} from "@iris/sync/campo";
import { CircleCheck, Clock, CloudOff, ExternalLink, Share2, ShieldCheck } from "lucide-react-native";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { Linking, Share, StyleSheet, Text, View } from "react-native";
import { ErrorApi, formatearEspera } from "../api/errores";
import { Aviso, Boton, Campo, CampoCodigo, Cargando, Dialogo, Pila, Tarjeta, Texto } from "../componentes";
import { AyudaCumplida, EncabezadoPantalla, NumeroPaso } from "../componentes/cuenta";
import { useAtrasAndroid } from "../campo/atras";
import { useCampo } from "../campo/contexto";
import { activarDosPasos, cambiarContrasena, configurarDosPasos, nuevaClave, useCuenta } from "../campo/cuenta";
import { useTema } from "../tema/tema";

/** ¿Hay señal para hablar con la API? (lo de la cuenta no se guarda para después). */
function useConSenal(): boolean {
    const { estado } = useCampo();
    return estado.conexion !== "sin_conexion";
}

/**
 * Volver a «Yo» preguntando antes si hay algo escrito: el «Atrás» de la pantalla y el de Android
 * (que se atiende aquí antes que la pila de vistas). Devuelve `volver` y la pregunta.
 */
function useSalida(sucio: boolean): { volver: () => void; pregunta: ReactNode } {
    const { navegar } = useCampo();
    const [preguntando, setPreguntando] = useState(false);
    const irAYo = useCallback(() => navegar({ tipo: "yo" }), [navegar]);
    const volver = useCallback(() => (sucio ? setPreguntando(true) : irAYo()), [sucio, irAYo]);
    useAtrasAndroid(
        useCallback(() => {
            if (!sucio) {
                return false;
            }
            setPreguntando(true);
            return true;
        }, [sucio]),
    );
    const pregunta = (
        <Dialogo abierto={preguntando} alCerrar={() => setPreguntando(false)} titulo="¿Salir sin guardar?" testID="salir-sin-guardar">
            <Texto>Lo que escribiste aquí no se guardó.</Texto>
            <Boton
                variante="peligro"
                alPresionar={() => {
                    setPreguntando(false);
                    irAYo();
                }}
            >
                Salir sin guardar
            </Boton>
            <Boton variante="secundario" alPresionar={() => setPreguntando(false)}>
                Seguir editando
            </Boton>
        </Dialogo>
    );
    return { volver, pregunta };
}

function AvisoSinSenal() {
    return (
        <Aviso tono="warning" icono={CloudOff} titulo="Hace falta señal" testID="aviso-sin-senal">
            {TEXTO_SIN_SENAL_CUENTA}
        </Aviso>
    );
}

/** 429 o sesión vencida: la API ya cerró la sesión; se vuelve a entrar conservando lo pendiente. */
function SesionCerrada({ titulo, detalle }: { titulo: string; detalle: string }) {
    const { cerrarSesion, notificar } = useCampo();
    const [saliendo, setSaliendo] = useState(false);
    return (
        <Aviso
            tono="warning"
            icono={Clock}
            titulo={titulo}
            testID="aviso-sesion-cerrada"
            accion={
                <Boton
                    cargando={saliendo}
                    alPresionar={() => {
                        setSaliendo(true);
                        cerrarSesion({ conservarPendientes: true }).catch(() => {
                            setSaliendo(false);
                            notificar({ tono: "error", texto: "No se pudo cerrar la sesión en el teléfono. Vuelve a intentarlo." });
                        });
                    }}
                >
                    Ir a iniciar sesión
                </Boton>
            }
        >
            {detalle}
        </Aviso>
    );
}

type ErrorPantalla = { tipo: "sin_senal" } | { tipo: "sesion"; titulo: string; detalle: string } | { tipo: "otro"; titulo: string; detalle: string };

function errorDePantalla(e: unknown, que: string): ErrorPantalla {
    if (e instanceof ErrorApi && e.estado === 0) {
        return { tipo: "sin_senal" };
    }
    if (e instanceof ErrorApi && e.estado === 429) {
        return { tipo: "sesion", ...textoBloqueoContrasena(formatearEspera(e.reintentarEn ?? 15 * 60)) };
    }
    if (e instanceof ErrorApi && e.estado === 401) {
        return { tipo: "sesion", titulo: "Tu sesión terminó", detalle: "Vuelve a entrar. Lo pendiente sigue guardado en el teléfono." };
    }
    return { tipo: "otro", titulo: `No se pudo ${que}`, detalle: e instanceof ErrorApi ? (e.detalle ?? e.titulo) : "Vuelve a intentarlo." };
}

function AvisoError({ error, reintentar }: { error: ErrorPantalla; reintentar: () => void }) {
    if (error.tipo === "sesion") {
        return <SesionCerrada titulo={error.titulo} detalle={error.detalle} />;
    }
    const sinSenal = error.tipo === "sin_senal";
    return (
        <Aviso
            tono={sinSenal ? "warning" : "error"}
            icono={sinSenal ? CloudOff : undefined}
            titulo={sinSenal ? "Sin señal" : error.titulo}
            testID="aviso-error-cuenta"
            accion={
                <Boton variante="secundario" alPresionar={reintentar}>
                    Reintentar
                </Boton>
            }
        >
            {sinSenal ? "No llegó a Iris. Busca señal y vuelve a intentarlo." : error.detalle}
        </Aviso>
    );
}

// --- Cambiar contraseña -----------------------------------------------------------------------

export function CambiarContrasena() {
    const { navegar, notificar } = useCampo();
    const { colores } = useTema();
    const conSenal = useConSenal();
    const clave = useRef<string | null>(null);
    const [error, setError] = useState<ErrorPantalla | null>(null);
    const formulario = useForm<DatosContrasenaPropia>({ resolver: zodResolver(esquemaContrasenaPropia), defaultValues: { actual: "", nueva: "", repetir: "" }, reValidateMode: "onChange" });
    const { errors, isSubmitting, isDirty } = formulario.formState;
    const valores = useWatch({ control: formulario.control });
    const reglas = reglasContrasena({ actual: valores.actual ?? "", nueva: valores.nueva ?? "", repetir: valores.repetir ?? "" });
    const { volver, pregunta } = useSalida(isDirty && error?.tipo !== "sesion");

    // El manejador se arma al tocar (no en el render): la clave vive en un ref.
    const enviar = () =>
        formulario.handleSubmit(async (datos) => {
            setError(null);
            clave.current ??= nuevaClave();
            try {
                await cambiarContrasena(cuerpoContrasenaPropia(datos), clave.current);
                clave.current = null;
                formulario.reset({ actual: "", nueva: "", repetir: "" });
                notificar({ tono: "success", texto: "Contraseña cambiada" });
                navegar({ tipo: "yo" });
            } catch (e) {
                // Sin respuesta se reintenta con la misma clave; con respuesta (guardada), con otra.
                if (!(e instanceof ErrorApi && e.estado === 0)) {
                    clave.current = null;
                }
                if (e instanceof ErrorApi && e.estado === 422 && e.errores.length > 0) {
                    let alguno = false;
                    for (const nombre of ["actual", "nueva"] as const) {
                        const mensaje = e.errorDeCampo(nombre);
                        if (mensaje) {
                            alguno = true;
                            formulario.setError(nombre, { message: mensaje });
                        }
                    }
                    if (e.errorDeCampo("actual")) {
                        formulario.setValue("actual", "");
                    }
                    if (alguno) {
                        return;
                    }
                }
                setError(errorDePantalla(e, "cambiar la contraseña"));
            }
        })();

    return (
        <Pila espacio={18}>
            <EncabezadoPantalla titulo="Cambiar contraseña" alVolver={volver} />
            {!conSenal ? <AvisoSinSenal /> : null}
            {error ? <AvisoError error={error} reintentar={() => void enviar()} /> : null}
            {error?.tipo === "sesion" ? null : (
                <>
                    <Tarjeta testID="tarjeta-contrasena">
                        <Controller
                            control={formulario.control}
                            name="actual"
                            render={({ field }) => (
                                <Campo etiqueta="Contraseña actual" secreto valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} error={errors.actual?.message} autoComplete="current-password" textContentType="password" autoCapitalize="none" testID="campo-actual" />
                            )}
                        />
                        <View style={estilos.campo}>
                            <Controller
                                control={formulario.control}
                                name="nueva"
                                render={({ field }) => (
                                    <Campo etiqueta="Nueva contraseña" secreto valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} error={errors.nueva?.message} autoComplete="new-password" textContentType="newPassword" autoCapitalize="none" testID="campo-nueva" />
                                )}
                            />
                            {errors.nueva ? null : <AyudaCumplida texto={`${LONGITUD_MINIMA_CONTRASENA} caracteres o más`} cumplida={reglas.largo} testID="ayuda-largo" />}
                        </View>
                        <View style={estilos.campo}>
                            <Controller
                                control={formulario.control}
                                name="repetir"
                                render={({ field }) => (
                                    <Campo etiqueta="Repite la nueva" secreto valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} error={errors.repetir?.message} autoComplete="new-password" textContentType="newPassword" autoCapitalize="none" testID="campo-repetir" />
                                )}
                            />
                            {errors.repetir ? null : <AyudaCumplida texto="Coinciden" cumplida={reglas.coinciden} testID="ayuda-coinciden" />}
                        </View>
                    </Tarjeta>
                    <Text style={[estilos.nota, { color: colores.textoSuave }]}>{TEXTO_SESIONES_CONTRASENA}</Text>
                    <Boton cargando={isSubmitting} deshabilitado={!conSenal} alPresionar={() => void enviar()} estilo={estilos.pildora} testID="guardar-contrasena">
                        {isSubmitting ? "Guardando…" : "Guardar contraseña"}
                    </Boton>
                </>
            )}
            {pregunta}
        </Pila>
    );
}

// --- Verificación en dos pasos ----------------------------------------------------------------

type Configuracion = { estado: "inactiva" } | { estado: "pidiendo" } | { estado: "lista"; mfa: Schema<"MFAConfigurado"> } | { estado: "error"; error: ErrorPantalla };

export function DosPasos() {
    const { sesion } = useCampo();
    const cuenta = useCuenta(sesion.empresaActiva);
    if (cuenta.estado === "cargando") {
        return (
            <Pila espacio={18}>
                <EncabezadoDosPasos />
                <Cargando etiqueta="Leyendo tu cuenta" />
            </Pila>
        );
    }
    if (cuenta.estado === "error") {
        return (
            <Pila espacio={18}>
                <EncabezadoDosPasos />
                <AvisoError error={errorDePantalla(cuenta.error, "leer tu cuenta")} reintentar={cuenta.reintentar} />
            </Pila>
        );
    }
    return cuenta.yo.usuario.mfaActivo ? <DosPasosActiva /> : <ActivarDosPasos correo={cuenta.yo.usuario.correo} />;
}

function EncabezadoDosPasos({ volver }: { volver?: () => void }) {
    const { navegar } = useCampo();
    return <EncabezadoPantalla titulo="Verificación en dos pasos" alVolver={volver ?? (() => navegar({ tipo: "yo" }))} />;
}

function DosPasosActiva() {
    return (
        <Pila espacio={18}>
            <EncabezadoDosPasos />
            <Aviso tono="success" icono={ShieldCheck} titulo="Activa" testID="dos-pasos-activa">
                Al entrar en un equipo nuevo, Iris te pide el código de tu app de códigos.
            </Aviso>
            <Texto suave>{TEXTO_REINICIAR_DOS_PASOS}</Texto>
        </Pila>
    );
}

function ActivarDosPasos({ correo }: { correo: string }) {
    const { navegar, notificar } = useCampo();
    const { colores, fuenteMono } = useTema();
    const conSenal = useConSenal();
    const [config, setConfig] = useState<Configuracion>({ estado: "inactiva" });
    const [intento, setIntento] = useState(0);
    const [error, setError] = useState<ErrorPantalla | null>(null);
    const claveActivar = useRef<string | null>(null);
    const pedido = useRef(-1);
    const formulario = useForm<{ codigo: string }, unknown, Schema<"EntradaActivarMFA">>({ resolver: zodResolver(esquemaCodigoActivar), defaultValues: { codigo: "" } });
    const { errors, isSubmitting, isDirty } = formulario.formState;
    const { volver, pregunta } = useSalida(isDirty && error?.tipo !== "sesion");

    // Un secreto por intento: `POST /yo/mfa` no se repite con la misma clave (409), así que cada
    // intento lleva una nueva y nunca se vuelve a pedir en otro render.
    useEffect(() => {
        if (!conSenal || pedido.current === intento) {
            return;
        }
        pedido.current = intento;
        let vivo = true;
        setConfig({ estado: "pidiendo" });
        configurarDosPasos(nuevaClave())
            .then((mfa) => vivo && setConfig({ estado: "lista", mfa }))
            .catch((e: unknown) => vivo && setConfig({ estado: "error", error: errorDePantalla(e, "preparar la verificación") }));
        return () => {
            vivo = false;
        };
    }, [conSenal, intento]);

    const activar = () =>
        formulario.handleSubmit(async (datos) => {
            setError(null);
            claveActivar.current ??= nuevaClave();
            try {
                await activarDosPasos(datos, claveActivar.current);
            } catch (e) {
                if (!(e instanceof ErrorApi && e.estado === 0)) {
                    claveActivar.current = null;
                }
                // Un reintento tras una respuesta perdida: ya quedó activa.
                if (!(e instanceof ErrorApi && e.codigo === "mfa_activo")) {
                    if (e instanceof ErrorApi && e.estado === 422) {
                        formulario.setError("codigo", { message: e.errorDeCampo("codigo") ?? e.detalle ?? "El código no coincide; revisa la hora del teléfono" });
                        formulario.setValue("codigo", "");
                        return;
                    }
                    setError(errorDePantalla(e, "activar la verificación"));
                    return;
                }
            }
            claveActivar.current = null;
            formulario.reset({ codigo: "" });
            notificar({ tono: "success", texto: "Verificación en dos pasos activada" });
            navegar({ tipo: "yo" });
        })();

    const enlace = config.estado === "lista" ? enlaceOtpauth(config.mfa, correo) : null;
    const secreto = config.estado === "lista" ? agruparSecreto(config.mfa.secreto) : null;

    return (
        <Pila espacio={18}>
            <EncabezadoDosPasos volver={volver} />
            {!conSenal ? <AvisoSinSenal /> : null}
            {config.estado === "error" ? <AvisoError error={config.error} reintentar={() => setIntento((i) => i + 1)} /> : null}
            {error ? <AvisoError error={error} reintentar={() => void activar()} /> : null}
            {config.estado === "pidiendo" ? <Cargando etiqueta="Preparando tu clave" /> : null}
            {config.estado === "lista" && enlace && secreto && error?.tipo !== "sesion" ? (
                <>
                    <Tarjeta testID="tarjeta-dos-pasos">
                        <View style={estilos.paso}>
                            <NumeroPaso numero={1} />
                            <View style={estilos.cuerpoPaso}>
                                <Text accessibilityRole="header" style={[estilos.tituloPaso, { color: colores.texto }]}>
                                    Abre tu app de códigos
                                </Text>
                                <Texto suave>Google Authenticator, Microsoft Authenticator o similar.</Texto>
                                <View style={estilos.botones}>
                                    <View style={estilos.flex}>
                                        <Boton
                                            variante="secundario"
                                            icono={ExternalLink}
                                            alPresionar={() =>
                                                void Linking.openURL(enlace).catch(() => notificar({ tono: "warning", texto: "No hay una app de códigos que lo abra. Escribe la clave a mano." }))
                                            }
                                        >
                                            Abrir app
                                        </Boton>
                                    </View>
                                    <View style={estilos.flex}>
                                        {/* Sin módulo de portapapeles en la app: la hoja del sistema trae «Copiar». */}
                                        <Boton variante="secundario" icono={Share2} etiqueta="Copiar clave" alPresionar={() => void Share.share({ message: config.mfa.secreto }).catch(() => undefined)}>
                                            Copiar clave
                                        </Boton>
                                    </View>
                                </View>
                                <Text selectable accessibilityLabel={`Clave: ${secreto}`} testID="clave-dos-pasos" style={[estilos.clave, { color: colores.textoSuave, fontFamily: fuenteMono }]}>
                                    {secreto}
                                </Text>
                            </View>
                        </View>
                        <View style={estilos.cuerpoPaso}>
                            <View style={estilos.cabezaPaso}>
                                <NumeroPaso numero={2} />
                                <Text accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={[estilos.tituloPaso, estilos.flex, { color: colores.texto }]}>
                                    Escribe el código de 6 dígitos
                                </Text>
                            </View>
                            {/* Las 6 casillas a lo ancho de la tarjeta (44 px cada una caben a 390 px). */}
                            <Controller
                                control={formulario.control}
                                name="codigo"
                                render={({ field }) => <CampoCodigo etiqueta="Escribe el código de 6 dígitos" ocultarEtiqueta valor={field.value} alCambiar={field.onChange} error={errors.codigo?.message} />}
                            />
                        </View>
                    </Tarjeta>
                    <Text style={[estilos.nota, { color: colores.textoSuave }]}>{TEXTO_ACTIVAR_DOS_PASOS}</Text>
                    <Boton icono={CircleCheck} cargando={isSubmitting} deshabilitado={!conSenal} alPresionar={() => void activar()} estilo={estilos.pildora} testID="activar-dos-pasos">
                        {isSubmitting ? "Activando…" : "Activar"}
                    </Boton>
                </>
            ) : null}
            {pregunta}
        </Pila>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    campo: { gap: 6 },
    nota: { fontSize: 15, lineHeight: 21, paddingHorizontal: 6 },
    pildora: { minHeight: 52, borderRadius: 999 },
    paso: { flexDirection: "row", gap: 12, alignItems: "flex-start" },
    cuerpoPaso: { flex: 1, gap: 8 },
    cabezaPaso: { flexDirection: "row", alignItems: "center", gap: 12 },
    tituloPaso: { fontSize: 17, fontWeight: "700", paddingTop: 3 },
    botones: { flexDirection: "row", gap: 8 },
    clave: { fontSize: 16, letterSpacing: 2 },
});
