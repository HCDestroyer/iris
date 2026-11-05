// Entrada del técnico (diseño «Login C · Combinada», nov 2026), igual que la PWA y en un solo paso:
// logo y «Hola, técnico», la tarjeta con correo y contraseña (o, si el teléfono recuerda al último
// usuario, su fila con «¿No eres tú?» y solo la contraseña), el ojo de la contraseña, errores en
// línea, «Entrar» y «¿Olvidaste tu contraseña?»; debajo «Usar huella» (solo con una sesión guardada
// y biometría) y el aviso «Sin conexión». El código de 6 dígitos solo si `POST /sesion` responde
// `mfaRequerido`. Mensajes y reglas de `@iris/sync/campo` (`avisoEntrar`). «Entrar con Argos» no va:
// el autologin desde Argos empieza en Argos con un ticket de un solo uso (`/sso/entrar#<ticket>`),
// Iris no tiene cómo iniciarlo.
import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisoEntrar, iniciales, type AvisoEntrar, type UsuarioRecordado } from "@iris/sync/campo";
import { ArrowLeft, CircleHelp, Clock, CloudOff, FingerprintPattern, Lock, Mail } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { StyleSheet, Text, View } from "react-native";
import { ErrorApi, formatearEspera } from "../api/errores";
import { Aviso, Boton, BotonRedondo, Campo, CampoCodigo, Dialogo, LogoIris, Pila, Tarjeta, Texto } from "../componentes";
import { esquemaCodigoMFA, esquemaInicioSesion, type DatosCodigoMFA, type DatosInicioSesion } from "../sesion/esquemas";
import { iniciarSesion, verificarMfa } from "../sesion/sesion";
import { useTema } from "../tema/tema";

/** El error de la API como lo lee `avisoEntrar`. */
export function avisoDeError(error: unknown): AvisoEntrar {
    if (error instanceof ErrorApi) {
        return avisoEntrar({ estado: error.estado, codigo: error.codigo, titulo: error.titulo, detalle: error.detalle, problema: error.problema });
    }
    return avisoEntrar({ estado: -1 });
}

function useBloqueo() {
    const [hasta, setHasta] = useState<number | null>(null);
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        if (hasta === null) {
            return undefined;
        }
        const id = setInterval(() => {
            const t = Date.now();
            setAhora(t);
            if (t >= hasta) {
                setHasta(null);
            }
        }, 1000);
        return () => clearInterval(id);
    }, [hasta]);
    const restante = hasta === null ? 0 : Math.max(0, Math.ceil((hasta - ahora) / 1000));
    return {
        restante,
        bloquear: (segundos: number) => {
            const t = Date.now();
            setAhora(t);
            setHasta(t + segundos * 1000);
        },
        liberar: () => setHasta(null),
    };
}

export interface PropsEntrar {
    /** Recibe el usuario de la sesión completa (con MFA, tras el código). */
    alEntrar: (usuario: Schema<"Usuario"> | undefined) => void;
    motivo?: "expirada" | "salida";
    /** El último usuario de este teléfono (nombre y correo): solo pide la contraseña. */
    recordado?: UsuarioRecordado | null;
    /** «¿No eres tú?»: olvida al usuario recordado (y vuelve a pedir el correo). */
    alOlvidarRecordado?: () => void;
    /** «Usar huella»: solo con una sesión guardada y biometría (si no, no se pasa y no aparece). */
    huella?: (() => Promise<void>) | null;
    /** Aviso «Sin conexión» de abajo (con o sin sesión guardada que sirva). */
    sinConexion?: { titulo: string; detalle: string } | null;
}

export function Entrar({ alEntrar, motivo, recordado: recordadoInicial = null, alOlvidarRecordado, huella = null, sinConexion = null }: PropsEntrar) {
    const { colores, campo } = useTema();
    const [paso, setPaso] = useState<"credenciales" | "mfa">("credenciales");
    const [recordado, setRecordado] = useState<UsuarioRecordado | null>(recordadoInicial);
    const [aviso, setAviso] = useState<AvisoEntrar | null>(
        motivo === "expirada" ? { tono: "info", titulo: "Tu sesión terminó", detalle: "Vuelve a entrar para continuar.", campo: null } : null,
    );
    const [olvido, setOlvido] = useState(false);
    const [usandoHuella, setUsandoHuella] = useState(false);
    const bloqueo = useBloqueo();
    const credenciales = useForm<DatosInicioSesion>({ resolver: zodResolver(esquemaInicioSesion), defaultValues: { correo: recordadoInicial?.correo ?? "", contrasena: "" } });
    const mfa = useForm<{ codigo: string }, unknown, DatosCodigoMFA>({ resolver: zodResolver(esquemaCodigoMFA), defaultValues: { codigo: "" } });

    const enviarCredenciales = credenciales.handleSubmit(async (datos) => {
        setAviso(null);
        try {
            const sesion = await iniciarSesion(datos);
            bloqueo.liberar();
            if (sesion.mfaRequerido) {
                mfa.reset({ codigo: "" });
                setPaso("mfa");
                return;
            }
            alEntrar(sesion.usuario);
        } catch (error) {
            if (error instanceof ErrorApi && error.estado === 429) {
                bloqueo.bloquear(error.reintentarEn ?? 15 * 60);
                return;
            }
            if (error instanceof ErrorApi && error.estado === 422) {
                for (const nombre of ["correo", "contrasena"] as const) {
                    const mensaje = error.errorDeCampo(nombre);
                    if (mensaje) {
                        credenciales.setError(nombre, { message: mensaje });
                    }
                }
            }
            const a = avisoDeError(error);
            if (a.campo === "contrasena") {
                credenciales.setError("contrasena", { message: a.titulo });
                return;
            }
            setAviso(a);
        }
    });

    const enviarCodigo = mfa.handleSubmit(async (datos) => {
        setAviso(null);
        try {
            const sesion = await verificarMfa(datos);
            alEntrar(sesion.usuario);
        } catch (error) {
            const a = avisoDeError(error);
            if (a.campo === "codigo") {
                mfa.setError("codigo", { message: a.titulo });
                mfa.setValue("codigo", "");
                return;
            }
            if (error instanceof ErrorApi && (error.codigo === "mfa_bloqueado" || error.codigo === "no_autenticado")) {
                credenciales.setValue("contrasena", "");
                setPaso("credenciales");
            }
            setAviso(a);
        }
    });

    const avisoVisible = aviso ? (
        <Aviso tono={aviso.tono} titulo={aviso.titulo} testID="aviso-entrar">
            {aviso.detalle}
        </Aviso>
    ) : null;

    if (paso === "mfa") {
        const { errors, isSubmitting } = mfa.formState;
        return (
            <Pila espacio={20}>
                <BotonRedondo
                    icono={ArrowLeft}
                    etiqueta="Volver"
                    fondo={campo.panel}
                    tinta={colores.texto}
                    borde={colores.borde}
                    alPresionar={() => {
                        setAviso(null);
                        setPaso("credenciales");
                    }}
                />
                <View style={estilos.titulos}>
                    <Text accessibilityRole="header" style={[estilos.titulo, { color: colores.texto }]}>
                        Código de verificación
                    </Text>
                    <Texto suave>Solo te lo pedimos porque tienes la verificación en dos pasos activa.</Texto>
                </View>
                {avisoVisible}
                <Tarjeta testID="tarjeta-codigo">
                    <Controller
                        control={mfa.control}
                        name="codigo"
                        render={({ field }) => (
                            <CampoCodigo etiqueta="Escribe los 6 dígitos de tu app de autenticación" valor={field.value} alCambiar={field.onChange} error={errors.codigo?.message} />
                        )}
                    />
                    <Boton cargando={isSubmitting} alPresionar={() => void enviarCodigo()} estilo={estilos.pildora}>
                        {isSubmitting ? "Verificando…" : "Verificar"}
                    </Boton>
                </Tarjeta>
            </Pila>
        );
    }

    const { errors, isSubmitting } = credenciales.formState;
    const bloqueado = bloqueo.restante > 0;
    return (
        <Pila espacio={20}>
            <View style={estilos.titulos}>
                <LogoIris tamano={56} />
                <Text accessibilityRole="header" style={[estilos.titulo, { color: colores.texto }]}>
                    Hola, técnico
                </Text>
                <Texto suave>Entra para ver tus órdenes de hoy.</Texto>
            </View>
            {bloqueado ? (
                <Aviso tono="warning" icono={Clock} titulo={`Demasiados intentos, espera ${formatearEspera(bloqueo.restante)}`} testID="aviso-bloqueo">
                    Por seguridad bloqueamos este correo un momento. Podrás volver a intentar cuando termine la espera.
                </Aviso>
            ) : (
                avisoVisible
            )}
            <Tarjeta testID="tarjeta-entrar">
                {recordado ? (
                    <View style={estilos.recordado} testID="usuario-recordado">
                        <View style={[estilos.iniciales, { backgroundColor: colores.primario }]}>
                            <Text style={[estilos.textoIniciales, { color: colores.sobrePrimario }]}>{iniciales(recordado.nombre) || "?"}</Text>
                        </View>
                        <View style={estilos.flex}>
                            <Texto negrita>{recordado.nombre}</Texto>
                            <Texto suave>{recordado.correo}</Texto>
                        </View>
                        <Boton
                            variante="fantasma"
                            alPresionar={() => {
                                setRecordado(null);
                                credenciales.reset({ correo: "", contrasena: "" });
                                setAviso(null);
                                alOlvidarRecordado?.();
                            }}
                        >
                            ¿No eres tú?
                        </Boton>
                    </View>
                ) : (
                    <Controller
                        control={credenciales.control}
                        name="correo"
                        render={({ field }) => (
                            <Campo
                                etiqueta="Correo"
                                icono={Mail}
                                valor={field.value}
                                alCambiar={field.onChange}
                                onBlur={field.onBlur}
                                keyboardType="email-address"
                                autoCapitalize="none"
                                autoCorrect={false}
                                autoComplete="email"
                                textContentType="username"
                                returnKeyType="next"
                                error={errors.correo?.message}
                            />
                        )}
                    />
                )}
                <Controller
                    control={credenciales.control}
                    name="contrasena"
                    render={({ field }) => (
                        <Campo
                            etiqueta="Contraseña"
                            icono={Lock}
                            secreto
                            valor={field.value}
                            alCambiar={field.onChange}
                            onBlur={field.onBlur}
                            autoCapitalize="none"
                            autoCorrect={false}
                            autoComplete="current-password"
                            textContentType="password"
                            returnKeyType="go"
                            onSubmitEditing={() => void enviarCredenciales()}
                            error={errors.contrasena?.message}
                            {...(recordado ? { autoFocus: true } : {})}
                        />
                    )}
                />
                <Boton cargando={isSubmitting} deshabilitado={bloqueado || usandoHuella} alPresionar={() => void enviarCredenciales()} estilo={estilos.pildora}>
                    {isSubmitting ? "Entrando…" : "Entrar"}
                </Boton>
                <Boton variante="fantasma" alPresionar={() => setOlvido(true)}>
                    ¿Olvidaste tu contraseña?
                </Boton>
            </Tarjeta>
            {huella ? (
                <Boton
                    variante="secundario"
                    icono={FingerprintPattern}
                    cargando={usandoHuella}
                    deshabilitado={isSubmitting}
                    estilo={estilos.pildora}
                    alPresionar={() =>
                        void (async () => {
                            setUsandoHuella(true);
                            try {
                                await huella();
                            } finally {
                                setUsandoHuella(false);
                            }
                        })()
                    }
                >
                    Usar huella
                </Boton>
            ) : null}
            {sinConexion ? (
                <Aviso tono="warning" icono={CloudOff} titulo={sinConexion.titulo} testID="aviso-sin-conexion">
                    {sinConexion.detalle}
                </Aviso>
            ) : null}
            <Dialogo abierto={olvido} alCerrar={() => setOlvido(false)} titulo="¿Olvidaste tu contraseña?" testID="dialogo-olvido">
                <View style={estilos.filaAyuda}>
                    <CircleHelp size={20} color={colores.primario} />
                    <Texto estilo={estilos.flex}>
                        Pídele a tu supervisor o al administrador de Iris que te asigne una contraseña temporal. Al entrar con ella, Iris te pedirá cambiarla.
                    </Texto>
                </View>
                <Boton alPresionar={() => setOlvido(false)} estilo={estilos.pildora}>
                    Entendido
                </Boton>
            </Dialogo>
        </Pila>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    titulos: { gap: 8 },
    titulo: { fontSize: 32, lineHeight: 36, fontWeight: "700", letterSpacing: -0.96 },
    pildora: { minHeight: 52, borderRadius: 999 },
    recordado: { flexDirection: "row", alignItems: "center", gap: 12 },
    iniciales: { width: 48, height: 48, borderRadius: 24, alignItems: "center", justifyContent: "center" },
    textoIniciales: { fontSize: 17, fontWeight: "700" },
    filaAyuda: { flexDirection: "row", gap: 10, alignItems: "flex-start" },
});
