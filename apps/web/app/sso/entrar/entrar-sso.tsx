"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Campo, Cargando, EstadoVacio, InsigniaEstado } from "@iris/ui";
import { CircleCheck, CircleHelp, Link2, LinkIcon, LogIn, ShieldCheck, ShieldQuestion, UserRoundX, Users } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { requiereCambio, rutaCambiarContrasena } from "@/lib/cuenta/sesion";
import { esquemaCodigoMFA, esquemaInicioSesion, type DatosCodigoMFA, type DatosInicioSesion } from "@/lib/esquemas";
import { quitarFragmentoDeLaUrl } from "@/lib/fragmento";
import { ticketDesdeFragmento } from "@/lib/portal/fragmento";
import { apiSSO, descartarVinculacion, destinoSeguro, mensajeSSO, type CanjeSSO, type MensajeSSO, type UsuarioExterno } from "@/lib/sso/sso";
import { formatearEspera } from "@/lib/tiempo";

type Usuario = Schema<"Usuario">;

type Paso =
    | { paso: "canjeando" }
    | { paso: "sin_enlace" }
    | { paso: "mfa"; destino: string }
    | { paso: "confirmar_cambio"; entrarComo: NonNullable<CanjeSSO["entrarComo"]>; actual: string | null }
    | { paso: "vincular"; externo: UsuarioExterno; destino: string }
    | { paso: "login"; externo: UsuarioExterno; destino: string; aviso?: string }
    | { paso: "login_mfa"; externo: UsuarioExterno; destino: string }
    | { paso: "confirmar_vinculo"; externo: UsuarioExterno; destino: string; yo: Usuario }
    | { paso: "requiere_admin"; externo: UsuarioExterno }
    | { paso: "error"; mensaje: MensajeSSO };

/** Ir al panel con la sesión nueva: carga completa, así el panel lee `/yo` desde cero. */
function ir(ruta: string) {
    window.location.assign(ruta);
}

function irConUsuario(destino: string, usuario: Usuario | undefined) {
    ir(requiereCambio(usuario) ? rutaCambiarContrasena(destino) : destino);
}

function problema(e: unknown): { estado: number; codigo: string | undefined } {
    return e instanceof ErrorApi ? { estado: e.estado, codigo: e.codigo } : { estado: 0, codigo: undefined };
}

/**
 * Autologin desde Argos (`/sso/entrar#<ticket>`). El ticket se lee del fragmento, se borra de la URL
 * (sincronizando el router de Next) y solo vive en memoria. Nunca se vincula una cuenta sin que la
 * persona entre con su contraseña y lo confirme.
 */
export function EntrarSSO() {
    const [inicio] = useState(() => {
        const leido = ticketDesdeFragmento(window.location.hash);
        if (leido === null) return { paso: { paso: "sin_enlace" } as Paso, ticket: null };
        if ("invalido" in leido) return { paso: { paso: "error", mensaje: mensajeSSO(401, "ticket_invalido") } as Paso, ticket: null };
        return { paso: { paso: "canjeando" } as Paso, ticket: leido.ticket };
    });
    const ticket = useRef<string | null>(inicio.ticket);
    const enviado = useRef(false);
    const [paso, setPaso] = useState<Paso>(inicio.paso);

    const atender = useCallback(async (r: CanjeSSO) => {
        const destino = destinoSeguro(r.destino?.ruta);
        if (r.estado === "sesion") {
            ticket.current = null;
            irConUsuario(destino, r.sesion?.usuario);
            return;
        }
        if (r.estado === "mfa") {
            ticket.current = null;
            setPaso({ paso: "mfa", destino });
            return;
        }
        if (r.estado === "confirmar_cambio" && r.entrarComo) {
            // El ticket sigue en memoria (no se consumió) para «Entrar como…».
            let actual: string | null = null;
            try {
                actual = (await resolver(api().GET("/api/v1/yo"))).usuario.nombre;
            } catch {
                actual = null;
            }
            setPaso({ paso: "confirmar_cambio", entrarComo: r.entrarComo, actual });
            return;
        }
        if (r.estado === "vincular" && r.externo) {
            ticket.current = null;
            setPaso({ paso: "vincular", externo: r.externo, destino });
            return;
        }
        setPaso({ paso: "error", mensaje: mensajeSSO(0, undefined) });
    }, []);

    const canjear = useCallback(
        async (confirmar: boolean) => {
            const t = ticket.current;
            if (!t) return;
            setPaso({ paso: "canjeando" });
            try {
                const r = await resolver(apiSSO().POST("/api/v1/sso/canjear", { body: confirmar ? { ticket: t, confirmar: true } : { ticket: t } }));
                await atender(r);
            } catch (e) {
                // Nunca se reintenta un ticket que la API rechazó.
                ticket.current = null;
                const { estado, codigo } = problema(e);
                setPaso({ paso: "error", mensaje: mensajeSSO(estado, codigo) });
            }
        },
        [atender],
    );

    useEffect(() => {
        quitarFragmentoDeLaUrl();
        if (ticket.current && !enviado.current) {
            enviado.current = true;
            void canjear(false);
        }
    }, [canjear]);

    switch (paso.paso) {
        case "canjeando":
            return (
                <div className="grid place-items-center py-10">
                    <Cargando texto="Entrando desde Argos…" visible />
                </div>
            );
        case "sin_enlace":
            return (
                <Pantalla icono={LinkIcon} titulo="Abre Iris desde Argos" descripcion="Esta página funciona con el enlace «Ver en Iris» de Argos.">
                    <EnlaceLogin />
                </Pantalla>
            );
        case "error":
            return (
                <Pantalla icono={UserRoundX} titulo={paso.mensaje.titulo} descripcion={paso.mensaje.detalle}>
                    <EnlaceLogin />
                </Pantalla>
            );
        case "mfa":
            return <PasoCodigo alListo={(u) => irConUsuario(paso.destino, u)} alSalir={() => setPaso({ paso: "error", mensaje: mensajeSSO(401, "ticket_invalido") })} />;
        case "confirmar_cambio":
            return (
                <Pantalla
                    icono={Users}
                    titulo="Hay una sesión abierta con otra cuenta"
                    descripcion={
                        <>
                            {paso.actual ? `Ya hay una sesión de ${paso.actual}. ` : "Ya hay una sesión abierta en este navegador. "}
                            ¿Cerrarla y entrar como {paso.entrarComo.nombreIris}, la cuenta vinculada a «{paso.entrarComo.nombreExterno}» ({paso.entrarComo.idExterno}) de Argos?
                        </>
                    }
                >
                    <div className="grid gap-2">
                        <Boton className="w-full" onClick={() => void canjear(true)}>
                            <LogIn />
                            Entrar como {paso.entrarComo.nombreIris}
                        </Boton>
                        <Boton
                            variante="secundario"
                            className="w-full"
                            onClick={() => {
                                ticket.current = null;
                                ir("/");
                            }}
                        >
                            {paso.actual ? `Quedarme como ${paso.actual}` : "Quedarme con la sesión actual"}
                        </Boton>
                    </div>
                </Pantalla>
            );
        case "vincular":
            return (
                <Pantalla icono={Link2} titulo="Tu cuenta de Argos no está vinculada" descripcion="Para entrar desde Argos, vincúlala una vez con tu cuenta de Iris. Primero inicia sesión con tu contraseña de Iris.">
                    <DatosExterno externo={paso.externo} />
                    <div className="grid gap-2">
                        <Boton className="w-full" onClick={() => setPaso({ paso: "login", externo: paso.externo, destino: paso.destino })}>
                            <LogIn />
                            Iniciar sesión para vincular
                        </Boton>
                        <BotonNoFuiYo texto="No fui yo / Cancelar" />
                    </div>
                </Pantalla>
            );
        case "login":
            return (
                <PasoLogin
                    aviso={paso.aviso}
                    alEntrar={async (mfa) => {
                        if (mfa) {
                            setPaso({ paso: "login_mfa", externo: paso.externo, destino: paso.destino });
                            return;
                        }
                        const yo = await resolver(api().GET("/api/v1/yo"));
                        setPaso({ paso: "confirmar_vinculo", externo: paso.externo, destino: paso.destino, yo: yo.usuario });
                    }}
                />
            );
        case "login_mfa":
            return (
                <PasoCodigo
                    alListo={async () => {
                        const yo = await resolver(api().GET("/api/v1/yo"));
                        setPaso({ paso: "confirmar_vinculo", externo: paso.externo, destino: paso.destino, yo: yo.usuario });
                    }}
                    alSalir={() => setPaso({ paso: "login", externo: paso.externo, destino: paso.destino })}
                />
            );
        case "confirmar_vinculo":
            return (
                <ConfirmarVinculo
                    externo={paso.externo}
                    yo={paso.yo}
                    alVincular={(destino) => irConUsuario(destinoSeguro(destino), paso.yo)}
                    alRequerirAdmin={() => setPaso({ paso: "requiere_admin", externo: paso.externo })}
                    alPedirLogin={(aviso) => setPaso({ paso: "login", externo: paso.externo, destino: paso.destino, aviso })}
                    alError={(mensaje) => setPaso({ paso: "error", mensaje })}
                />
            );
        case "requiere_admin":
            return (
                <Pantalla
                    icono={ShieldQuestion}
                    titulo="Pide a un administrador de Iris que vincule tu cuenta"
                    descripcion="Por seguridad, esta vinculación la hace un administrador de Iris (en Usuarios, «Cuentas vinculadas»). Dale estos datos de tu cuenta de Argos."
                >
                    <DatosExterno externo={paso.externo} />
                    <Boton variante="secundario" className="w-full" onClick={() => ir("/")}>
                        Ir al panel
                    </Boton>
                </Pantalla>
            );
    }
}

function Pantalla({ icono, titulo, descripcion, children }: { icono: typeof LinkIcon; titulo: string; descripcion: ReactNode; children?: ReactNode }) {
    return (
        <div className="grid gap-5">
            <EstadoVacio icono={icono} titulo={titulo} descripcion={descripcion} className="px-0 py-2" />
            {children}
        </div>
    );
}

/** Descarta la vinculación pendiente (borra la cookie en la API) y vuelve al inicio, sin vincular. */
function BotonNoFuiYo({ texto, disabled }: { texto: string; disabled?: boolean }) {
    const [saliendo, setSaliendo] = useState(false);
    return (
        <Boton
            variante="secundario"
            className="w-full"
            disabled={disabled || saliendo}
            onClick={async () => {
                setSaliendo(true);
                await descartarVinculacion();
                ir("/");
            }}
        >
            {saliendo ? "Saliendo…" : texto}
        </Boton>
    );
}

function EnlaceLogin() {
    return (
        <Boton variante="secundario" className="w-full" onClick={() => ir("/entrar")}>
            <LogIn />
            Entrar con mi cuenta de Iris
        </Boton>
    );
}

function DatosExterno({ externo }: { externo: UsuarioExterno }) {
    return (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 rounded-lg border bg-elevated/40 p-3 text-sm" aria-label="Cuenta de Argos" data-testid="cuenta-argos">
            <dt className="text-muted-foreground">Id en Argos</dt>
            <dd className="font-mono break-all">{externo.idExterno}</dd>
            <dt className="text-muted-foreground">Nombre</dt>
            <dd className="break-words">{externo.nombre || "—"}</dd>
            <dt className="text-muted-foreground">Correo</dt>
            <dd className="grid justify-items-start gap-1 break-all">
                {externo.correo || "—"}
                {externo.correoVerificado ? (
                    <InsigniaEstado tono="success" icono={CircleCheck} texto="Verificado en Argos" />
                ) : (
                    <InsigniaEstado tono="neutral" icono={CircleHelp} texto="Sin verificar en Argos" />
                )}
            </dd>
        </dl>
    );
}

/** Login normal (con su MFA) para vincular: la sesión debe abrirse después del canje. */
function PasoLogin({ aviso: avisoInicial, alEntrar }: { aviso?: string | undefined; alEntrar: (mfa: boolean) => Promise<void> }) {
    const [aviso, setAviso] = useState<string | null>(avisoInicial ?? null);
    const form = useForm<DatosInicioSesion>({ resolver: zodResolver(esquemaInicioSesion), defaultValues: { correo: "", contrasena: "" } });
    const { errors, isSubmitting } = form.formState;

    async function enviar(d: DatosInicioSesion) {
        setAviso(null);
        try {
            const s = await resolver(api().POST("/api/v1/sesion", { body: d }));
            await alEntrar(s.mfaRequerido);
        } catch (e) {
            const { estado } = problema(e);
            if (estado === 401) setAviso("Correo o contraseña incorrectos.");
            else if (estado === 429) setAviso(`Demasiados intentos. Espera ${formatearEspera((e as ErrorApi).reintentarEn ?? 15 * 60)} antes de volver a intentarlo.`);
            else setAviso("No se pudo iniciar sesión. Inténtalo de nuevo.");
        }
    }

    return (
        <form className="grid gap-5" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)} aria-labelledby="titulo-login-sso">
            <div className="grid gap-1">
                <h1 id="titulo-login-sso" className="text-xl font-semibold tracking-[-0.015em]">
                    Inicia sesión en Iris
                </h1>
                <p className="text-sm text-muted-foreground">Con tu cuenta de Iris, para vincularla con tu cuenta de Argos.</p>
            </div>
            {aviso ? (
                <Aviso tono="error" titulo="No se pudo iniciar sesión">
                    {aviso}
                </Aviso>
            ) : null}
            <Campo etiqueta="Correo" type="email" autoComplete="username" error={errors.correo?.message} {...form.register("correo")} />
            <Campo etiqueta="Contraseña" type="password" autoComplete="current-password" error={errors.contrasena?.message} {...form.register("contrasena")} />
            <Boton type="submit" className="w-full" disabled={isSubmitting}>
                <LogIn />
                {isSubmitting ? "Entrando…" : "Entrar"}
            </Boton>
        </form>
    );
}

function PasoCodigo({ alListo, alSalir }: { alListo: (u: Usuario | undefined) => void | Promise<void>; alSalir: () => void }) {
    const [aviso, setAviso] = useState<string | null>(null);
    const form = useForm<{ codigo: string }, unknown, DatosCodigoMFA>({ resolver: zodResolver(esquemaCodigoMFA), defaultValues: { codigo: "" } });
    const { errors, isSubmitting } = form.formState;

    async function enviar(d: DatosCodigoMFA) {
        setAviso(null);
        try {
            const s = await resolver(api().POST("/api/v1/sesion/mfa", { body: d }));
            await alListo(s.usuario);
        } catch (e) {
            const { estado, codigo } = problema(e);
            if (codigo === "codigo_invalido") {
                form.setError("codigo", { message: "Código incorrecto o ya usado. Revisa tu app de autenticación." });
                form.setValue("codigo", "");
                return;
            }
            if (codigo === "mfa_bloqueado" || estado === 429) setAviso("Demasiados códigos incorrectos. Espera unos minutos y vuelve a empezar.");
            else if (estado === 401) setAviso("La sesión venció. Vuelve a empezar.");
            else setAviso("No se pudo verificar el código. Inténtalo de nuevo.");
        }
    }

    return (
        <form className="grid gap-5" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)} aria-labelledby="titulo-mfa-sso">
            <div className="grid gap-1.5">
                <ShieldCheck aria-hidden className="size-6 text-primary" />
                <h1 id="titulo-mfa-sso" className="text-xl font-semibold tracking-[-0.015em]">
                    Verificación en dos pasos
                </h1>
                <p className="text-sm text-muted-foreground">Escribe el código de 6 dígitos de tu app de autenticación.</p>
            </div>
            {aviso ? (
                <Aviso tono="error" titulo="No se pudo verificar">
                    {aviso}
                </Aviso>
            ) : null}
            <Campo
                etiqueta="Código de verificación"
                inputMode="numeric"
                autoComplete="one-time-code"
                autoFocus
                maxLength={7}
                className="[&_input]:text-center [&_input]:font-mono [&_input]:text-lg [&_input]:tracking-[0.4em] [&_input]:tabular-nums"
                error={errors.codigo?.message}
                {...form.register("codigo")}
            />
            <div className="grid gap-2">
                <Boton type="submit" className="w-full" disabled={isSubmitting}>
                    <ShieldCheck />
                    {isSubmitting ? "Verificando…" : "Verificar"}
                </Boton>
                <Boton variante="fantasma" className="w-full" onClick={alSalir}>
                    Volver
                </Boton>
            </div>
        </form>
    );
}

/** Confirmación explícita: nunca se vincula sin que la persona pulse «Vincular». */
function ConfirmarVinculo({
    externo,
    yo,
    alVincular,
    alRequerirAdmin,
    alPedirLogin,
    alError,
}: {
    externo: UsuarioExterno;
    yo: Usuario;
    alVincular: (destino: string) => void;
    alRequerirAdmin: () => void;
    alPedirLogin: (aviso: string) => void;
    alError: (m: MensajeSSO) => void;
}) {
    const [enviando, setEnviando] = useState(false);
    const [clave] = useState(nuevaClaveIdempotencia);

    async function vincular() {
        setEnviando(true);
        try {
            const r = await resolver(
                api().POST("/api/v1/sso/vincular", { body: { id_externo: externo.idExterno }, params: { header: { "Idempotency-Key": clave } } }),
            );
            alVincular(r.destino.ruta);
        } catch (e) {
            const { estado, codigo } = problema(e);
            if (codigo === "vinculo_requiere_admin") alRequerirAdmin();
            else if (codigo === "sesion_anterior_al_canje") alPedirLogin("Inicia sesión otra vez: la sesión tiene que abrirse después de venir de Argos.");
            else alError(mensajeSSO(estado, codigo));
        } finally {
            setEnviando(false);
        }
    }

    return (
        <div className="grid gap-5" aria-labelledby="titulo-confirmar-vinculo">
            <div className="grid gap-1">
                <h1 id="titulo-confirmar-vinculo" className="text-xl font-semibold tracking-[-0.015em]">
                    ¿Vincular estas cuentas?
                </h1>
                <p className="text-sm text-muted-foreground" data-testid="texto-vincular">
                    Vincular la cuenta de Argos {externo.idExterno}
                    {externo.nombre ? ` (${externo.nombre}${externo.correo ? `, ${externo.correo}` : ""})` : externo.correo ? ` (${externo.correo})` : ""} con tu cuenta de Iris {yo.nombre} ({yo.correo}).
                    Después, «Ver en Iris» te dejará entrar sin contraseña.
                </p>
            </div>
            <DatosExterno externo={externo} />
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 rounded-lg border p-3 text-sm" aria-label="Cuenta de Iris">
                <dt className="text-muted-foreground">Cuenta de Iris</dt>
                <dd className="break-words">{yo.nombre}</dd>
                <dt className="text-muted-foreground">Correo</dt>
                <dd className="break-all">{yo.correo}</dd>
            </dl>
            <div className="grid gap-2">
                <Boton className="w-full" onClick={() => void vincular()} disabled={enviando}>
                    <Link2 />
                    {enviando ? "Vinculando…" : "Vincular"}
                </Boton>
                <BotonNoFuiYo texto="No fui yo" disabled={enviando} />
            </div>
        </div>
    );
}
