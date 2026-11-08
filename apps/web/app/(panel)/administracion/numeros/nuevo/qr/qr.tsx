"use client";

import { avisar, Aviso, Boton, Cargando, Casilla, cn, SeccionFormulario } from "@iris/ui";
import {
    AppWindow,
    Ban,
    Check,
    CircleCheck,
    CircleX,
    EyeOff,
    Gauge,
    Info,
    LoaderCircle,
    Megaphone,
    QrCode,
    RotateCw,
    Smartphone,
    SquareArrowOutUpRight,
    TriangleAlert,
    User,
    Users,
    type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { CargaRegistro } from "@/componentes/carga-registro";
import { PiePagina } from "@/componentes/numeros/pie-pagina";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { crearCanalQr, leerRegistroMeta } from "@/lib/meta/api";
import { errorQr, etiquetaModo, textoMotivoQr } from "@/lib/meta/presentacion";
import { RegistroEmbebido, type ResultadoRegistro } from "@/lib/meta/registro-embebido";
import { cargarSdk, opcionesLogin, type SdkFacebook } from "@/lib/meta/sdk";
import { PERMISO_CONEXION_META, type RegistroMeta } from "@/lib/meta/tipos";
import { useSesion } from "@/lib/sesion";

const LISTA = "/administracion/numeros";
/** Nombre con que nace el número; se cambia en «Terminar de configurar». */
export const NOMBRE_INICIAL_QR = "WhatsApp Business";

type Estado = { fase: "inicio" } | { fase: "esperando" } | { fase: "creando" } | { fase: "error"; codigo: string; detalle: string | null };

/**
 * `/administracion/numeros/nuevo/qr`: conectar con QR un número que ya se usa en la app WhatsApp
 * Business (registro embebido de Meta, coexistencia; contrato §15.2).
 *
 * 1. Al abrir, lee `GET /conexion-meta/registro` y carga el SDK de Facebook (solo en esta pantalla).
 * 2. «Conectar con Meta» llama `FB.login` dentro del mismo clic (si no, el navegador bloquea la
 *    ventana) y escucha los `message` de Facebook (`WA_EMBEDDED_SIGNUP`), validando `event.origin`.
 * 3. Con el `code` y los ids, `POST /canales/qr` **en el acto** (el código vive ~30 s) y abre
 *    «Número conectado». El `code` nunca se guarda ni se muestra.
 */
export function ConectarConQr() {
    const lectura = useLectura((signal) => leerRegistroMeta(signal));
    return (
        <CargaRegistro lectura={lectura} que="la conexión con Meta" noEncontrado="Conexión no encontrada">
            {(r) => <Flujo registro={r} />}
        </CargaRegistro>
    );
}

function Flujo({ registro }: { registro: RegistroMeta }) {
    const router = useRouter();
    const { yo, puede } = useSesion();
    const [sdk, setSdk] = useState<{ estado: "cargando" } | { estado: "listo"; fb: SdkFacebook } | { estado: "error" }>({ estado: "cargando" });
    const [intentoSdk, setIntentoSdk] = useState(0);
    const [estado, setEstado] = useState<Estado>({ fase: "inicio" });
    const [leido, setLeido] = useState(false);
    const actual = useRef<{ registro: RegistroEmbebido; quitar: () => void } | null>(null);

    const disponible = registro.disponible && Boolean(registro.appId && registro.configId);

    useEffect(() => {
        if (!disponible || !registro.appId) return undefined;
        let vivo = true;
        cargarSdk(registro.appId, registro.graphVersion)
            .then((fb) => vivo && setSdk({ estado: "listo", fb }))
            .catch(() => vivo && setSdk({ estado: "error" }));
        return () => {
            vivo = false;
        };
    }, [disponible, registro.appId, registro.graphVersion, intentoSdk]);

    // Al salir de la pantalla: nada más llega ni se manda.
    useEffect(
        () => () => {
            actual.current?.registro.abandonar();
            actual.current?.quitar();
            actual.current = null;
        },
        [],
    );

    function soltar() {
        actual.current?.registro.abandonar();
        actual.current?.quitar();
        actual.current = null;
    }

    async function alTerminar(r: ResultadoRegistro) {
        soltar();
        if (r.tipo === "cancelado") return setEstado({ fase: "error", codigo: "cancelado", detalle: r.paso ? `Paso: ${r.paso}` : null });
        if (r.tipo === "error") return setEstado({ fase: "error", codigo: r.motivo, detalle: [r.mensaje, r.codigo ? `Código ${r.codigo}` : null].filter(Boolean).join(" · ") || null });
        setEstado({ fase: "creando" });
        try {
            const canal = await crearCanalQr({ code: r.code, wabaId: r.wabaId, phoneNumberId: r.phoneNumberId, nombre: NOMBRE_INICIAL_QR, empresaId: yo.empresaActiva?.id ?? null }, nuevaClaveIdempotencia());
            avisar({ titulo: "Número conectado" });
            router.replace(`${LISTA}/${canal.id}/conectado`);
        } catch (e) {
            const codigo = e instanceof ErrorApi ? (e.codigo === "validacion" ? "error_meta" : e.codigo) : "error_meta";
            setEstado({ fase: "error", codigo, detalle: e instanceof ErrorApi ? (e.detalle ?? null) : null });
        }
    }

    /** Dentro del clic: abre la ventana de Meta (sin `await` antes, o el navegador la bloquea). */
    function conectar() {
        if (sdk.estado !== "listo" || !registro.configId) return;
        soltar();
        const reg = new RegistroEmbebido((r) => void alTerminar(r));
        const alMensaje = (e: MessageEvent) => {
            reg.alMensaje(e.origin, e.data);
        };
        window.addEventListener("message", alMensaje);
        actual.current = { registro: reg, quitar: () => window.removeEventListener("message", alMensaje) };
        setEstado({ fase: "esperando" });
        sdk.fb.login((respuesta) => reg.alLogin(respuesta), opcionesLogin(registro.configId, registro.featureType, registro.sessionInfoVersion));
    }

    function cancelar() {
        soltar();
        setEstado({ fase: "inicio" });
    }

    if (!disponible) {
        return (
            <Pantalla subtitulo="No se puede conectar con QR todavía.">
                <Aviso tono="warning" titulo={textoMotivoQr(registro.motivo)}>
                    {puede([PERMISO_CONEXION_META]) && registro.motivo !== "servicio_central_no_disponible" ? (
                        <Link href="/administracion/conexion-meta" className="font-medium text-primary underline-offset-2 hover:underline">
                            Configurar la conexión con Meta
                        </Link>
                    ) : (
                        "Mientras tanto, conecta el número con los datos de la API."
                    )}
                </Aviso>
                <PiePagina>
                    <Boton asChild variante="contorno">
                        <Link href="/administracion/numeros/nuevo/api">Usar datos de la API</Link>
                    </Boton>
                </PiePagina>
            </Pantalla>
        );
    }

    if (estado.fase === "error") {
        const e = errorQr(estado.codigo);
        return (
            <Pantalla subtitulo="No se pudo conectar el número.">
                <section className="surface-card grid justify-items-center gap-3 p-6 text-center sm:p-8" data-error-qr={estado.codigo} aria-live="polite">
                    <span className={cn("grid size-14 place-items-center rounded-full", estado.codigo === "cancelado" ? "bg-background-secondary text-muted-foreground" : "bg-error/10 text-error")}>
                        {estado.codigo === "cancelado" ? <CircleX aria-hidden className="size-7" /> : <Ban aria-hidden className="size-7" />}
                    </span>
                    <h2 className="text-lg font-semibold">{e.titulo}</h2>
                    <p className="max-w-prose text-sm text-muted-foreground">{e.texto}</p>
                    {e.revisa ? (
                        <ul className="grid list-disc gap-1 pl-5 text-left text-sm">
                            {e.revisa.map((r) => (
                                <li key={r}>{r}</li>
                            ))}
                        </ul>
                    ) : null}
                    {estado.detalle ? <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{estado.detalle}</p> : null}
                    <div className="flex flex-wrap justify-center gap-2">
                        {e.ofrecerConexion && puede([PERMISO_CONEXION_META]) ? (
                            <Boton asChild variante="contorno">
                                <Link href="/administracion/conexion-meta">Revisar la conexión con Meta</Link>
                            </Boton>
                        ) : null}
                        {e.ofrecerApi ? (
                            <Boton asChild variante="contorno">
                                <Link href="/administracion/numeros/nuevo/api">Usar datos de la API</Link>
                            </Boton>
                        ) : null}
                        <Boton onClick={() => setEstado({ fase: "inicio" })}>
                            <RotateCw aria-hidden />
                            Reintentar
                        </Boton>
                    </div>
                </section>
                <p className="text-center text-sm text-muted-foreground">No se guardó ningún dato del número.</p>
            </Pantalla>
        );
    }

    if (estado.fase === "esperando" || estado.fase === "creando") {
        const creando = estado.fase === "creando";
        return (
            <Pantalla subtitulo={creando ? "Meta respondió. Conectando el número…" : "Completa los pasos en la ventana de Meta."}>
                <section className="surface-card grid justify-items-center gap-3 p-6 text-center sm:p-8" data-fase={estado.fase} aria-live="polite">
                    <span className="grid size-14 place-items-center rounded-full bg-primary/10 text-primary">
                        <LoaderCircle aria-hidden className="size-7 animate-spin motion-reduce:animate-none" />
                    </span>
                    <h2 className="text-lg font-semibold">{creando ? "Conectando el número…" : "Esperando a Meta…"}</h2>
                    <p className="max-w-prose text-sm text-muted-foreground">
                        {creando ? "No cierres esta página: tarda unos segundos." : "La ventana de Meta está abierta. No cierres esta página: cuando confirmes en el teléfono, Iris sigue solo."}
                    </p>
                    {creando ? (
                        <Cargando texto="Conectando con Meta…" />
                    ) : (
                        <div className="flex flex-wrap justify-center gap-2">
                            <Boton variante="contorno" onClick={conectar}>
                                <AppWindow aria-hidden />
                                Abrir otra vez la ventana de Meta
                            </Boton>
                            <Boton variante="contorno" onClick={cancelar}>
                                Cancelar
                            </Boton>
                        </div>
                    )}
                </section>
                {creando ? null : (
                    <SeccionFormulario titulo="Dónde vas">
                        <Pasos actual={2} />
                        <Aviso tono="neutral" icono={Info} titulo="¿No ves la ventana?">
                            Tu navegador pudo bloquearla. Permite ventanas emergentes para {typeof window === "undefined" ? "este sitio" : window.location.hostname} y pulsa «Abrir otra vez la ventana de Meta».
                        </Aviso>
                    </SeccionFormulario>
                )}
            </Pantalla>
        );
    }

    return (
        <Pantalla subtitulo={`Usa en Iris el número de WhatsApp Business que ya tienes.${yo.empresaActiva ? ` Se agrega a ${yo.empresaActiva.nombre}.` : ""}`}>
            <SeccionFormulario titulo="Antes de empezar">
                <ListaIconos
                    items={[
                        { icono: Smartphone, titulo: "App WhatsApp Business 2.24.17 o más reciente", texto: "Actualízala en la tienda. La app personal de WhatsApp no sirve." },
                        { icono: User, titulo: "Acceso a Meta Business", texto: "Una cuenta de Facebook con permiso de administrador en el portafolio. Si no tienes portafolio, la ventana de Meta te deja crear uno." },
                        { icono: QrCode, titulo: "El teléfono a mano y con internet", texto: "Vas a confirmar desde la app con un QR o un código." },
                        { icono: AppWindow, titulo: "Permite ventanas emergentes", texto: "El registro de Meta se abre en una ventana aparte." },
                    ]}
                />
                <Pasos actual={0} />
            </SeccionFormulario>
            <SeccionFormulario
                titulo={
                    <span className="flex items-center gap-2">
                        <TriangleAlert aria-hidden className="size-5 text-warning" />
                        Lo que cambia al conectar
                    </span>
                }
                descripcion="Lo pone Meta para todos los números conectados así."
            >
                <ul className="grid gap-3 text-sm sm:grid-cols-2">
                    {[
                        { icono: CircleCheck, texto: "La app sigue en el teléfono. Lo que mandes desde el teléfono también aparece en Iris." },
                        { icono: Megaphone, texto: "Las listas de difusión quedan de solo lectura." },
                        { icono: EyeOff, texto: "Se apagan en chats 1:1: ver una vez, ubicación en tiempo real y mensajes temporales." },
                        { icono: Users, texto: "Los grupos no pasan a Iris." },
                        { icono: Gauge, texto: "Hasta unos 20 mensajes por segundo." },
                        { icono: Smartphone, texto: "Si no abres la app en el teléfono por unos 14 días, Meta desconecta el número." },
                    ].map((x) => (
                        <li key={x.texto} className="flex items-start gap-2">
                            <x.icono aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />
                            {x.texto}
                        </li>
                    ))}
                </ul>
            </SeccionFormulario>
            <SeccionFormulario titulo={<span className="sr-only">Confirmación</span>}>
                <Casilla etiqueta="Leí lo que cambia en la app del teléfono." checked={leido} onChange={(e) => setLeido(e.target.checked)} />
            </SeccionFormulario>
            {sdk.estado === "error" ? (
                <Aviso
                    tono="error"
                    titulo={errorQr("sdk").titulo}
                    accion={
                        <Boton
                            variante="contorno"
                            tamano="sm"
                            onClick={() => {
                                setSdk({ estado: "cargando" });
                                setIntentoSdk((n) => n + 1);
                            }}
                        >
                            <RotateCw aria-hidden />
                            Reintentar
                        </Boton>
                    }
                >
                    {errorQr("sdk").texto}
                </Aviso>
            ) : null}
            <PiePagina nota={`Conexión con Meta: ${etiquetaModo(registro.modo)}.`}>
                <Boton asChild variante="contorno">
                    <Link href="/administracion/numeros/nuevo">Cancelar</Link>
                </Boton>
                <Boton onClick={conectar} disabled={!leido || sdk.estado !== "listo"} title={!leido ? "Marca «Leí lo que cambia en la app del teléfono»" : undefined}>
                    {sdk.estado === "cargando" ? <LoaderCircle aria-hidden className="animate-spin motion-reduce:animate-none" /> : <SquareArrowOutUpRight aria-hidden />}
                    {sdk.estado === "cargando" ? "Preparando…" : "Conectar con Meta"}
                </Boton>
            </PiePagina>
        </Pantalla>
    );
}

function Pantalla({ subtitulo, children }: { subtitulo: string; children: ReactNode }) {
    return (
        // Con su propio pie fijo: como `FormularioPagina`, oculta la barra inferior del teléfono.
        <div data-slot="formulario-pagina" className="mx-auto grid w-full max-w-3xl min-w-0 gap-4 [&>*]:min-w-0">
            <header className="grid gap-1">
                <h1 className="text-xl font-semibold tracking-[-0.015em] sm:text-2xl">Conectar con QR</h1>
                <p className="text-sm text-muted-foreground">{subtitulo}</p>
            </header>
            {children}
        </div>
    );
}

function ListaIconos({ items }: { items: readonly { icono: LucideIcon; titulo: string; texto: string }[] }) {
    return (
        <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
            {items.map((x) => (
                <li key={x.titulo} className="flex items-start gap-3 p-3 sm:p-4">
                    <x.icono aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <div className="grid gap-0.5 text-sm">
                        <p className="font-medium">{x.titulo}</p>
                        <p className="text-muted-foreground">{x.texto}</p>
                    </div>
                </li>
            ))}
        </ul>
    );
}

const PASOS = [
    { titulo: "Entra a Meta", texto: "Se abre una ventana de Meta. Entra con Facebook y elige tu portafolio comercial." },
    { titulo: "Escribe tu número", texto: "El mismo de la app WhatsApp Business. Elige «Conectar tu app WhatsApp Business»." },
    { titulo: "Confirma en el teléfono", texto: "En la app, abre el aviso de Meta y escanea el QR o escribe el código." },
] as const;

/** Los 3 pasos en Meta; `actual` (1–3) va resaltado y los anteriores, hechos. 0 = sin empezar. */
function Pasos({ actual }: { actual: number }) {
    return (
        <ol className="grid gap-3 sm:grid-cols-3">
            {PASOS.map((p, i) => {
                const n = i + 1;
                const hecho = n < actual;
                return (
                    <li key={p.titulo} aria-current={n === actual ? "step" : undefined} className={cn("grid content-start gap-1.5 rounded-xl bg-background-secondary p-3", n === actual && "border border-primary/40 bg-primary/5")}>
                        <span className="flex items-center gap-2 font-medium">
                            <span className={cn("grid size-6 shrink-0 place-items-center rounded-full text-xs font-semibold", hecho ? "bg-primary text-primary-foreground" : "border border-primary/40 bg-primary/10 text-primary")}>
                                {hecho ? <Check aria-hidden className="size-3.5" /> : n}
                            </span>
                            {p.titulo}
                        </span>
                        <span className="text-sm text-muted-foreground">{p.texto}</span>
                    </li>
                );
            })}
        </ol>
    );
}
