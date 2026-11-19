"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { AreaTexto, Boton, CampoBusqueda, Casilla, cn, DialogoConfirmar, GrupoSegmentado, MenuArchivos, PanelEmergente, useProteccionSalida, VistaPreviaArchivos, type ArchivoEnVista, type OpcionArchivo } from "@iris/ui";
import { AtSign, BookOpen, Camera, Clock, FileText, Image as IconoImagen, Plus, Send, Smile, X, Zap } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type ClipboardEvent, type KeyboardEvent, type ReactNode } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { EN_OTRA_EMPRESA, esEnOtraEmpresa } from "@/lib/conversaciones/atencion";
import { ACEPTA_DOCUMENTO, ACEPTA_FOTO, claseDeArchivo, MAX_ARCHIVOS, motivoArchivoInvalido, nuevaSubida, subirAdjunto, tipoLegible, type AvanceSubida } from "@/lib/conversaciones/adjuntar";
import { cabeceraIdem, nuevoIdMensaje } from "@/lib/conversaciones/api";
import { idTrasError } from "@/lib/conversaciones/id-envio";
import { guardarBorrador, leerBorrador } from "@/lib/conversaciones/borradores";
import { registrarSalidaProtegida } from "@/lib/conversaciones/salida";
import type { Pendiente } from "@/lib/conversaciones/estado";
import { aplicarRespuesta, tamanoLegible, textoVentana } from "@/lib/conversaciones/presentacion";
import { esSesionCambiada, verificarSesion, type Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, Mensaje, RespuestaRapida } from "@/lib/conversaciones/tipos";
import { useSesion } from "@/lib/sesion";
import { agentesDeCola, useCatalogos } from "./catalogos";
import { SelectorPlantilla } from "./plantillas";

type Modo = "responder" | "nota";

/** Espejo del validador del servidor: texto plano de WhatsApp (≤ 4096). */
export const esquemaMensaje = z.object({ texto: z.string().max(4096, "Hasta 4096 caracteres (límite de WhatsApp).") });

type Props = {
    conv: Conversacion;
    identidad: Identidad;
    ahora: number;
    /** Agrega o actualiza un pendiente; `reemplaza` quita el anterior cuando el reintento cambia de id. */
    alPendiente: (p: Pendiente, reemplaza?: string) => void;
    alConfirmado: (m: Mensaje) => void;
    /** El servidor respondió 200 sin cuerpo (reenvío de un id ya recibido): releer los mensajes. */
    alReenvio: () => void;
    alActualizar: (c: Conversacion) => void;
    recargar: () => Promise<void>;
    abrirAyuda: () => void;
    registrarReintento: (f: (p: Pendiente) => void) => void;
    registrarUsarTexto: (f: (t: string) => void) => void;
    /** «Usar» del asistente (§16.2): el texto a la caja y, al enviar, `sugerenciaId`. */
    registrarUsarSugerencia?: (f: (texto: string, sugerenciaId: string) => void) => void;
    /** Archivos soltados sobre el hilo: pasan a la vista previa del compositor. */
    registrarAdjuntar?: (f: (archivos: File[]) => void) => void;
    /** Junto a «Enviar» en Responder: «Sugerir» del asistente (modo `boton`). */
    accesorio?: ReactNode;
};

const EMOJIS = ["👍", "🙏", "😊", "🙂", "👋", "✅", "📶", "🔧", "📅", "⏰", "📍", "💳", "🧾", "⚠️", "❗", "🎉"];

type Envio = { id: string; modo: Modo; texto: string; menciones: number[]; respuestaRapidaId: number | null; adjuntoId: string | null; sugerenciaId?: string | null; nombreArchivo?: string };

/** Un archivo elegido, todavía sin enviar. La URL de objeto (solo fotos) se libera al quitarlo. */
type Elegido = { id: string; archivo: File; url: string | undefined; estado: ArchivoEnVista["estado"]; mensaje: string | undefined; progreso: number; avance: AvanceSubida };

const OPCIONES_ADJUNTAR: readonly OpcionArchivo[] = [
    { clave: "fotos", etiqueta: "Fotos", icono: IconoImagen, accept: ACEPTA_FOTO, varios: true },
    { clave: "documento", etiqueta: "Documento (PDF)", icono: FileText, accept: ACEPTA_DOCUMENTO, varios: true },
];
const OPCION_CAMARA: OpcionArchivo = { clave: "camara", etiqueta: "Cámara", icono: Camera, accept: ACEPTA_FOTO, camara: true };

/** ¿Pantalla táctil (teléfono o tableta)? Solo ahí «Cámara» abre la cámara de verdad. */
function useTactil(): boolean {
    return useSyncExternalStore(
        (avisar) => {
            const consulta = window.matchMedia("(pointer: coarse)");
            consulta.addEventListener("change", avisar);
            return () => consulta.removeEventListener("change", avisar);
        },
        () => window.matchMedia("(pointer: coarse)").matches,
        () => false,
    );
}

/**
 * Adjuntos (como WhatsApp Web): el clip abre un menú (Fotos, Documento y, en el teléfono, Cámara);
 * también se puede pegar una imagen o soltar archivos sobre el hilo. Lo elegido no se envía solo:
 * queda en una vista previa sobre el texto (que hace de comentario), donde se revisa, se quita y
 * se ve qué no sirve antes de subir nada. La API admite un adjunto por mensaje: cada archivo sale
 * en su mensaje, en orden, y el comentario va con el primero.
 *
 * Compositor único (diseño del PM, nov. 2026: «teléfono primero»): pestañas Responder / Nota
 * interna y el tiempo que queda de la ventana de 24 h en una línea fina; el texto crece de 1 a 6
 * líneas con tope (18 % del alto en el teléfono, 30 % en escritorio) y después se desplaza por
 * dentro, para que el hilo siga a la vista con el teclado abierto. En el teléfono va en una fila
 * con «+» (despliega las herramientas) y «Enviar» redondo; en escritorio, la barra de íconos
 * (adjuntar, plantillas, respuestas rápidas, emoji) siempre visible y «Enviar» en teal. Las
 * respuestas rápidas salen en chips solo con la caja vacía. Fuera de la ventana, al cliente solo se le puede mandar una plantilla
 * aprobada en ese número (lo dice en una línea). Enter envía; Mayús + Enter baja de línea.
 *
 * Antes de guardar un borrador o de enviar, se comprueba con `GET /yo` que la sesión del navegador
 * sigue siendo la de quien está en pantalla: si cambió, nada sale (ver `lib/conversaciones/sesion`).
 */
export function Compositor({ conv, identidad, ahora, alPendiente, alConfirmado, alReenvio, alActualizar, recargar, abrirAyuda, registrarReintento, registrarUsarTexto, registrarUsarSugerencia, registrarAdjuntar, accesorio }: Props) {
    const { puede } = useSesion();
    const { respuestas, agentes } = useCatalogos();
    const [modo, setModo] = useState<Modo>("responder");
    const form = useForm({ resolver: zodResolver(esquemaMensaje), defaultValues: { texto: "" } });
    const texto = useWatch({ control: form.control, name: "texto" });
    const [respuestaId, setRespuestaId] = useState<number | null>(null);
    // Sugerencia del asistente que se pasó a la caja: viaja con el envío (aunque se edite).
    const [sugerenciaId, setSugerenciaId] = useState<string | null>(null);
    const [menciones, setMenciones] = useState<number[]>([]);
    const [error, setError] = useState<string | null>(null);
    const [elegidos, setElegidos] = useState<Elegido[]>([]);
    const elegidosAhora = useRef<Elegido[]>([]);
    const [enviandoArchivos, setEnviandoArchivos] = useState(false);
    const enviandoAhora = useRef(false);
    const tactil = useTactil();
    // Teléfono: las herramientas (adjuntar, plantillas, respuestas, emoji…) se despliegan con «+».
    const [herramientas, setHerramientas] = useState(false);
    const router = useRouter();
    // Con archivos por enviar o subiendo, cambiar de conversación (o salir) pregunta antes: al
    // salir, el envío se corta y lo que quede no sale.
    const proteccion = useProteccionSalida(
        elegidos.some((x) => x.estado !== "invalido"),
        (ruta) => router.push(ruta, { scroll: false }),
    );
    // …también cuando la bandeja cambia de conversación por código («La atiendo», «Ver» de un aviso).
    const salirProtegido = proteccion.salir;
    useEffect(() => registrarSalidaProtegida(salirProtegido, () => elegidosAhora.current.some((x) => x.estado !== "invalido")), [salirProtegido]);
    // El comentario ya tomado para un envío de archivos que aún no salió, y a quién guardárselo.
    const pieEnVuelo = useRef<string | null>(null);
    const paraBorrador = useRef({ identidad, id: conv.id });
    useEffect(() => {
        paraBorrador.current = { identidad, id: conv.id };
    }, [identidad, conv.id]);
    const campo = useRef<HTMLTextAreaElement | null>(null);
    const ventana = textoVentana(conv.ventana, ahora);
    const cerrada = ventana === null;

    // Borradores: solo con la sesión confirmada para esta persona y empresa.
    const [verificada, setVerificada] = useState(false);
    useEffect(() => {
        let vivo = true;
        verificarSesion(identidad).then(
            () => {
                if (!vivo) return;
                setVerificada(true);
                const b = leerBorrador(identidad, conv.id, modo);
                if (b && !form.getValues("texto")) form.reset({ texto: b });
            },
            () => undefined,
        );
        return () => {
            vivo = false;
        };
        // Al abrir la conversación y al cambiar de pestaña.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [identidad, conv.id, modo]);
    useEffect(() => {
        if (!verificada) return;
        const t = setTimeout(() => guardarBorrador(identidad, conv.id, modo, texto), 400);
        return () => clearTimeout(t);
    }, [verificada, identidad, conv.id, modo, texto]);

    /** Manda el mensaje; `false` si quedó como pendiente con error (se reintenta desde el hilo). */
    async function mandar(e: Envio): Promise<boolean> {
        const pendiente: Pendiente = { id: e.id, modo: e.modo, texto: e.texto || (e.adjuntoId ? (e.nombreArchivo ? `Archivo: ${e.nombreArchivo}` : "Archivo adjunto") : ""), creadoEn: new Date().toISOString(), error: null };
        alPendiente(pendiente);
        try {
            await verificarSesion(identidad);
            const m =
                e.modo === "nota"
                    ? await resolver(
                          api().POST("/api/v1/mensajeria/conversaciones/{id}/notas", {
                              params: { path: { id: conv.id }, header: cabeceraIdem(e.id) },
                              body: { id: e.id, texto: e.texto, menciones: e.menciones },
                          }),
                      )
                    : await resolver(
                          api().POST("/api/v1/mensajeria/conversaciones/{id}/mensajes", {
                              params: { path: { id: conv.id }, header: cabeceraIdem(e.id) },
                              body: {
                                  id: e.id,
                                  ...(e.texto ? { texto: e.texto } : {}),
                                  ...(e.adjuntoId ? { adjuntoId: e.adjuntoId } : {}),
                                  ...(e.respuestaRapidaId ? { respuestaRapidaId: e.respuestaRapidaId } : {}),
                                  ...(e.sugerenciaId ? { sugerenciaId: e.sugerenciaId } : {}),
                              },
                          }),
                      );
            // 200 sin cuerpo (reenvío de un id que el servidor ya tenía): se relee el hilo.
            if (m) alConfirmado(m);
            else alReenvio();
            return true;
        } catch (err) {
            // La sugerencia venció entre «Usar» y «Enviar»: el mensaje sale igual, sin ella.
            if (err instanceof ErrorApi && err.codigo === "sugerencia_no_vigente" && e.sugerenciaId) {
                const sin: Envio = { ...e, id: idTrasError(err, e.id), sugerenciaId: null };
                envios.current.delete(e.id);
                envios.current.set(sin.id, sin);
                alPendiente({ ...pendiente, id: sin.id }, e.id);
                return mandar(sin);
            }
            const definitivo = esEnOtraEmpresa(err);
            const texto = esSesionCambiada(err)
                ? "La sesión de este navegador cambió: no se envió"
                : definitivo
                  ? `No se envió: ${EN_OTRA_EMPRESA.titulo.toLowerCase()}`
                  : err instanceof ErrorApi && err.codigo === "fuera_de_ventana"
                    ? "La ventana de 24 h se cerró: usa una plantilla"
                    : err instanceof ErrorApi && err.codigo === "adjunto_no_listo"
                      ? "No se envió: el archivo no está listo o no es de esta conversación"
                      : `No se envió: ${mensajeDeError(err).titulo}`;
            // Sin red se reintenta con el mismo id; si el servidor respondió, guardó ese resultado con
            // esa clave y el siguiente intento lleva un id nuevo (id del cuerpo y clave juntos).
            const id = idTrasError(err, e.id);
            if (id === e.id) alPendiente({ ...pendiente, error: texto, definitivo });
            else {
                envios.current.delete(e.id);
                envios.current.set(id, { ...e, id });
                alPendiente({ ...pendiente, id, error: texto, definitivo }, e.id);
            }
            if (err instanceof ErrorApi && (err.codigo === "fuera_de_ventana" || err.estado === 409)) void recargar();
            return false;
        }
    }

    // El hilo reintenta un pendiente con su id: el mismo tras un fallo de red (el servidor no lo
    // duplica), uno nuevo si el servidor ya había respondido (ver `mandar`).
    const envios = useRef(new Map<string, Envio>());
    useEffect(() => {
        registrarReintento((p) => {
            const e = envios.current.get(p.id);
            if (e) void mandar(e);
        });
        registrarUsarTexto((t) => {
            setModo("responder");
            const actual = form.getValues("texto");
            form.setValue("texto", actual ? `${actual}\n\n${t}` : t, { shouldDirty: true });
            campo.current?.focus();
        });
        registrarAdjuntar?.(agregarArchivos);
        registrarUsarSugerencia?.((t, id) => {
            setModo("responder");
            const actual = form.getValues("texto").trim();
            form.setValue("texto", actual ? `${actual}\n\n${t}` : t, { shouldDirty: true });
            setSugerenciaId(id);
            campo.current?.focus();
        });
    });

    function enviar() {
        const limpio = form.getValues("texto").trim();
        if (limpio.length > 4096) {
            form.setError("texto", { message: "Hasta 4096 caracteres (límite de WhatsApp)." });
            return;
        }
        if (modo === "responder" && elegidosAhora.current.some((x) => x.estado !== "invalido")) {
            void enviarArchivos();
            return;
        }
        if (!limpio) return;
        const e: Envio = {
            id: nuevoIdMensaje(),
            modo,
            texto: limpio,
            menciones: modo === "nota" ? menciones : [],
            respuestaRapidaId: modo === "responder" ? respuestaId : null,
            adjuntoId: null,
            sugerenciaId: modo === "responder" ? sugerenciaId : null,
        };
        envios.current.set(e.id, e);
        form.reset({ texto: "" });
        guardarBorrador(identidad, conv.id, modo, "");
        setRespuestaId(null);
        setSugerenciaId(null);
        setMenciones([]);
        setError(null);
        void mandar(e);
    }

    /** Cambia la lista de elegidos (el estado y su copia al día, que usa el envío en curso). */
    function cambiarElegidos(f: (lista: Elegido[]) => Elegido[]) {
        elegidosAhora.current = f(elegidosAhora.current);
        setElegidos(elegidosAhora.current);
    }
    const actualizarElegido = (id: string, cambios: Partial<Elegido>) => cambiarElegidos((l) => l.map((x) => (x.id === id ? { ...x, ...cambios } : x)));

    function quitarElegido(id: string) {
        const url = elegidosAhora.current.find((x) => x.id === id)?.url;
        if (url) URL.revokeObjectURL(url);
        cambiarElegidos((l) => l.filter((x) => x.id !== id));
    }
    // Al cerrar la conversación se corta el envío en curso y se liberan las vistas previas que quedaron.
    const montado = useRef(true);
    useEffect(() => {
        montado.current = true;
        return () => {
            montado.current = false;
            for (const x of elegidosAhora.current) if (x.url) URL.revokeObjectURL(x.url);
            // Se salió antes de que saliera el primer archivo: el comentario no se pierde. Se guarda
            // de borrador AHORA (no cuando termine la parte en vuelo): si la persona vuelve enseguida,
            // el compositor nuevo ya lo encuentra.
            const texto = pieEnVuelo.current;
            pieEnVuelo.current = null;
            if (texto) {
                const { identidad: quien, id } = paraBorrador.current;
                const previo = leerBorrador(quien, id, "responder");
                guardarBorrador(quien, id, "responder", previo?.trim() ? `${texto}\n\n${previo}` : texto);
            }
        };
    }, []);

    /** Del menú, del portapapeles o soltados sobre el hilo: a la vista previa, sin subir nada todavía. */
    function agregarArchivos(archivos: File[]) {
        if (archivos.length === 0) return;
        if (cerrada) {
            setError("Con la ventana de 24 h cerrada solo se puede mandar una plantilla, no archivos.");
            return;
        }
        setModo("responder");
        const caben = Math.max(0, MAX_ARCHIVOS - elegidosAhora.current.length);
        setError(archivos.length > caben ? `Hasta ${MAX_ARCHIVOS} archivos por vez: ${archivos.length - caben === 1 ? "quedó fuera 1" : `quedaron fuera ${archivos.length - caben}`}.` : null);
        const nuevos = archivos.slice(0, caben).map((archivo): Elegido => {
            const motivo = motivoArchivoInvalido(archivo);
            // Solo los mapas de bits que se pueden enviar se muestran como imagen (un SVG o un HTML, nunca).
            const url = !motivo && claseDeArchivo(archivo.type) === "imagen" ? URL.createObjectURL(archivo) : undefined;
            return { id: nuevoIdMensaje(), archivo, url, estado: motivo ? "invalido" : "listo", mensaje: motivo ?? undefined, progreso: 0, avance: nuevaSubida() };
        });
        cambiarElegidos((l) => [...l, ...nuevos]);
        campo.current?.focus();
    }

    /**
     * Sube y envía lo elegido, de uno en uno y en orden (un adjunto por mensaje). El comentario va
     * con el primero. Si uno falla al subir, se detiene ahí con «Reintentar»: lo ya subido no se repite.
     */
    async function enviarArchivos() {
        if (enviandoAhora.current) return;
        enviandoAhora.current = true;
        setEnviandoArchivos(true);
        setError(null);
        // El comentario se toma UNA vez, al empezar, y la caja queda vacía: lo que se escriba mientras
        // suben los archivos es otro mensaje y nunca sale como pie de una foto. Va solo con el primer
        // archivo, con la respuesta rápida y la sugerencia del asistente que traía.
        let pie: { texto: string; respuestaRapidaId: number | null; sugerenciaId: string | null } | null = {
            texto: form.getValues("texto").trim(),
            respuestaRapidaId: respuestaId,
            sugerenciaId,
        };
        pieEnVuelo.current = pie.texto || null;
        form.reset({ texto: "" });
        guardarBorrador(identidad, conv.id, "responder", "");
        setRespuestaId(null);
        setSugerenciaId(null);
        /** Si se detiene antes de usarlo, el comentario vuelve a la caja (delante de lo que haya). */
        const devolverPie = () => {
            if (!pie || !montado.current) return;
            const { texto: textoPie, respuestaRapidaId: respuestaPie, sugerenciaId: sugerenciaPie } = pie;
            pie = null;
            pieEnVuelo.current = null;
            const escrito = form.getValues("texto");
            if (textoPie) form.setValue("texto", escrito.trim() ? `${textoPie}\n\n${escrito}` : textoPie, { shouldDirty: true });
            // La respuesta rápida o la sugerencia que se eligió mientras subía se respeta.
            setRespuestaId((actual) => actual ?? respuestaPie);
            setSugerenciaId((actual) => actual ?? sugerenciaPie);
        };
        try {
            await verificarSesion(identidad);
            for (const id of elegidosAhora.current.filter((x) => x.estado !== "invalido").map((x) => x.id)) {
                // Se salió de la conversación a mitad: no se sube ni se envía nada más.
                if (!montado.current) return;
                const actual = elegidosAhora.current.find((x) => x.id === id);
                if (!actual) continue;
                actualizarElegido(id, { estado: "subiendo", mensaje: undefined });
                let adjuntoId: string;
                try {
                    adjuntoId = await subirAdjunto(actual.archivo, conv.id, { avance: actual.avance, alAvanzar: (progreso) => actualizarElegido(id, { progreso }), seguir: () => montado.current });
                } catch (err) {
                    if (!montado.current) return;
                    actualizarElegido(id, { estado: "error", mensaje: esSesionCambiada(err) ? "La sesión de este navegador cambió: no se subió." : `No se pudo subir: ${mensajeDeError(err).titulo}` });
                    devolverPie();
                    return;
                }
                if (!montado.current) return;
                actualizarElegido(id, { estado: "enviando", progreso: 1 });
                const e: Envio = {
                    id: nuevoIdMensaje(),
                    modo: "responder",
                    texto: pie?.texto ?? "",
                    menciones: [],
                    respuestaRapidaId: pie?.respuestaRapidaId ?? null,
                    adjuntoId,
                    sugerenciaId: pie?.sugerenciaId ?? null,
                    nombreArchivo: actual.archivo.name,
                };
                pie = null;
                pieEnVuelo.current = null;
                envios.current.set(e.id, e);
                const enviado = await mandar(e);
                // Ya está en el hilo (enviado, o pendiente con su «Reintentar»).
                quitarElegido(id);
                if (!enviado) {
                    if (elegidosAhora.current.some((x) => x.estado !== "invalido")) setError("Un archivo no se envió: reinténtalo en el hilo. Los demás siguen aquí; pulsa Enviar para continuar.");
                    return;
                }
            }
        } catch (err) {
            if (!montado.current) return;
            setError(esSesionCambiada(err) ? "La sesión de este navegador cambió: no se subió nada." : `No se pudo enviar: ${mensajeDeError(err).titulo}`);
            cambiarElegidos((l) => l.map((x) => (x.estado === "subiendo" || x.estado === "enviando" ? { ...x, estado: "listo" } : x)));
            devolverPie();
        } finally {
            enviandoAhora.current = false;
            if (montado.current) setEnviandoArchivos(false);
        }
    }

    /**
     * Pegar una imagen (o un archivo) del portapapeles: a la vista previa. Si el portapapeles trae
     * también texto (Excel y Word copian las celdas como texto **y** como imagen), se pega el texto
     * como siempre y la imagen no se adjunta.
     */
    function alPegar(e: ClipboardEvent<HTMLTextAreaElement>) {
        const archivos = [...e.clipboardData.files];
        if (archivos.length === 0) return;
        if (e.clipboardData.types.includes("text/plain") && e.clipboardData.getData("text/plain").trim() !== "") return;
        e.preventDefault();
        agregarArchivos(archivos);
    }

    function alTeclear(e: KeyboardEvent<HTMLTextAreaElement>) {
        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            enviar();
        }
    }

    function usarRespuesta(r: RespuestaRapida) {
        const t = aplicarRespuesta(r.texto, conv.titulo);
        const actual = form.getValues("texto").trim();
        form.setValue("texto", actual ? `${actual} ${t}` : t, { shouldDirty: true });
        setRespuestaId(r.id);
        campo.current?.focus();
    }

    const deLaCola = useMemo(
        () => (respuestas.estado === "listo" && respuestas.datos ? respuestas.datos.filter((r) => r.colaId === null || r.colaId === conv.cola?.id) : []),
        [respuestas, conv.cola?.id],
    );
    const candidatos = agentes.estado === "listo" ? agentesDeCola(agentes.datos, conv.cola?.id).filter((a) => a.id !== identidad.usuarioId) : [];
    const soloPlantilla = modo === "responder" && cerrada;
    const porEnviar = modo === "responder" ? elegidos : [];
    const validos = porEnviar.filter((x) => x.estado !== "invalido").length;
    const enVista: ArchivoEnVista[] = porEnviar.map((x) => ({
        id: x.id,
        nombre: x.archivo.name || "Imagen pegada",
        detalle: `${tipoLegible(x.archivo)} · ${tamanoLegible(x.archivo.size)}`,
        clase: claseDeArchivo(x.archivo.type),
        url: x.url,
        estado: x.estado,
        mensaje: x.mensaje,
        progreso: x.progreso,
    }));
    // Borrar todo el texto suelta la sugerencia: lo que se escriba después ya no sale de ella.
    const { ref: refCampo, ...registro } = form.register("texto", { onChange: (e: { target: { value: string } }) => !e.target.value.trim() && setSugerenciaId(null) });

    const vacio = !texto.trim();
    const puedeEnviar = !enviandoArchivos && (!vacio || validos > 0);
    const textoEnviar = modo === "nota" ? "Guardar nota" : "Enviar";
    const barra = (
        <>
            {modo === "responder" ? (
                <>
                    <MenuArchivos
                        etiqueta="Adjuntar"
                        opciones={tactil ? [...OPCIONES_ADJUNTAR, OPCION_CAMARA] : OPCIONES_ADJUNTAR}
                        alElegir={agregarArchivos}
                        disabled={cerrada || enviandoArchivos || elegidos.length >= MAX_ARCHIVOS}
                        className="text-muted-foreground"
                    />
                    {puede(["manuales.ver"]) ? (
                        <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Manuales" title="Manuales" onClick={abrirAyuda}>
                            <BookOpen />
                        </Boton>
                    ) : null}
                    {soloPlantilla ? null : <SelectorPlantilla conv={conv} identidad={identidad} alEnviado={alConfirmado} alReenvio={alReenvio} alActualizar={alActualizar} />}
                    {cerrada ? null : <MenuRespuestas respuestas={deLaCola} alElegir={usarRespuesta} />}
                    {/* «Sugerir» (asistente en modo botón): en el teléfono va en la fila, siempre a la vista. */}
                    {cerrada || !accesorio ? null : <span className="hidden @min-[34rem]/conv:contents">{accesorio}</span>}
                </>
            ) : (
                <MenuMenciones candidatos={candidatos} elegidos={menciones} alCambiar={setMenciones} />
            )}
            {soloPlantilla ? null : <MenuEmoji alElegir={(e) => form.setValue("texto", `${form.getValues("texto")}${e}`, { shouldDirty: true })} />}
        </>
    );

    return (
        <div className="shrink-0 px-3 pt-1.5 pb-2.5 @min-[700px]/conv:px-3.5" data-slot="compositor">
            <DialogoConfirmar
                open={proteccion.pendiente !== null}
                onOpenChange={(abierto) => !abierto && proteccion.seguir()}
                titulo={enviandoArchivos ? "Todavía se están enviando archivos" : "Hay archivos sin enviar"}
                descripcion={enviandoArchivos ? "Si sales ahora, el envío se corta: lo que no haya salido no se envía." : "Si sales ahora, los archivos que elegiste no se envían."}
                textoCancelar="Seguir aquí"
                textoConfirmar="Salir"
                peligro
                alConfirmar={proteccion.confirmar}
            />
            {/* Las respuestas rápidas son atajos para empezar: solo con la caja vacía. */}
            {modo === "responder" && !cerrada && vacio && deLaCola.length > 0 ? (
                <div
                    role="group"
                    aria-label="Respuestas rápidas"
                    data-desborde-permitido
                    data-slot="chips-respuestas"
                    className="flex gap-1.5 overflow-x-auto pb-1.5 [mask-image:linear-gradient(90deg,#000_85%,transparent)] [scrollbar-width:none]"
                >
                    {deLaCola.slice(0, 8).map((r) => (
                        <Boton key={r.id} variante="secundario" tamano="sm" className="h-7 shrink-0 rounded-full px-2.5 text-xs font-medium" onClick={() => usarRespuesta(r)} title={r.texto}>
                            <Zap className="size-3.5 text-primary" />
                            {r.titulo}
                        </Boton>
                    ))}
                </div>
            ) : null}
            {/* El foco se marca en toda la caja (no en el texto, que queda recortado por ella). */}
            <div
                data-slot="caja-compositor"
                className={cn(
                    "min-w-0 rounded-[0.875rem] border bg-card shadow-xs transition-[border-color,box-shadow] focus-within:border-primary focus-within:shadow-[0_0_0_1px_var(--hx-primary)] motion-reduce:transition-none",
                    modo === "nota" ? "border-dashed border-warning/60 bg-warning/5" : "border-border",
                )}
            >
                <div className="flex min-w-0 items-center gap-1 px-1.5 pt-1">
                    <GrupoSegmentado
                        etiqueta="Tipo de mensaje"
                        className="bg-transparent"
                        claseOpcion="h-7 px-2.5 text-xs"
                        valor={modo}
                        alCambiar={(v) => {
                            setModo(v);
                            form.clearErrors();
                        }}
                        opciones={[
                            { valor: "responder", etiqueta: "Responder" },
                            { valor: "nota", etiqueta: "Nota interna" },
                        ]}
                    />
                    <span className="ml-auto flex min-w-0 items-center gap-1 pr-1 text-xs text-muted-foreground" data-ventana={cerrada ? "cerrada" : "abierta"}>
                        <Clock aria-hidden className="size-3.5 shrink-0" />
                        <span className="truncate @max-[30rem]/conv:hidden">{ventana ? ventana.largo : "Ventana de 24 h cerrada"}</span>
                        <span className="hidden whitespace-nowrap @max-[30rem]/conv:inline">{ventana ? ventana.corto : "Cerrada"}</span>
                    </span>
                </div>
                {soloPlantilla ? (
                    <div className="grid gap-2 px-3 pt-2 pb-2.5" data-slot="solo-plantilla">
                        <p className="text-sm text-muted-foreground">El cliente no escribe hace más de 24 h: WhatsApp solo deja mandarle una plantilla aprobada en este número.</p>
                        {conv.contacto.tipo === "cliente" && !conv.contacto.verificado ? (
                            <p className="text-sm text-warning">Falta que un supervisor confirme el cliente para mandarle plantillas.</p>
                        ) : null}
                        <SelectorPlantilla conv={conv} identidad={identidad} alEnviado={alConfirmado} alReenvio={alReenvio} alActualizar={alActualizar} principal />
                    </div>
                ) : (
                    <>
                        {modo === "nota" && menciones.length > 0 ? (
                            <div className="flex flex-wrap gap-1 px-3 pt-1.5">
                                {menciones.map((id) => {
                                    const a = candidatos.find((x) => x.id === id);
                                    return (
                                        <span key={id} className="inline-flex h-6 max-w-full items-center gap-1 rounded-full bg-elevated pr-0.5 pl-2 text-xs">
                                            <span className="truncate">@{a?.nombre ?? id}</span>
                                            <Boton variante="fantasma" tamano="icono-sm" className="size-5" aria-label={`Quitar a ${a?.nombre ?? id}`} onClick={() => setMenciones((m) => m.filter((x) => x !== id))}>
                                                <X />
                                            </Boton>
                                        </span>
                                    );
                                })}
                            </div>
                        ) : null}
                        {porEnviar.length > 0 ? (
                            <VistaPreviaArchivos etiqueta="Archivos por enviar" archivos={enVista} alQuitar={quitarElegido} alReintentar={() => void enviarArchivos()} className="px-2 pt-1.5">
                                {validos > 1 ? <p className="truncate px-1 text-xs text-muted-foreground">{validos} archivos · Cada archivo sale en su propio mensaje, en este orden. El comentario va con el primero.</p> : null}
                            </VistaPreviaArchivos>
                        ) : null}
                        <label className="sr-only" htmlFor={`compositor-${conv.id}`}>
                            {modo === "nota" ? "Nota interna (solo la ve el equipo)" : `Mensaje para ${conv.titulo}`}
                        </label>
                        {/* Teléfono (contenedor angosto): «+», el texto y «Enviar» en una fila. */}
                        <div className="flex min-w-0 items-end gap-1 px-1 pb-1 @min-[34rem]/conv:px-1.5 @min-[34rem]/conv:pb-0" data-slot="fila-compositor">
                            <Boton
                                variante="fantasma"
                                tamano="icono"
                                className="shrink-0 text-primary @min-[34rem]/conv:hidden"
                                aria-label={herramientas ? "Ocultar herramientas" : "Adjuntar y más herramientas"}
                                aria-expanded={herramientas}
                                aria-controls={`herramientas-${conv.id}`}
                                onClick={() => setHerramientas((v) => !v)}
                                data-slot="mas-herramientas"
                            >
                                <Plus className={cn("transition-transform motion-reduce:transition-none", herramientas && "rotate-45")} />
                            </Boton>
                            <AreaTexto
                                id={`compositor-${conv.id}`}
                                rows={1}
                                maxLength={4096}
                                placeholder={modo === "nota" ? "Nota para el equipo; el cliente no la ve" : validos > 0 ? "Comentario (opcional)" : "Escribe un mensaje"}
                                className="block max-h-[min(8.5rem,18dvh)] min-h-10 w-full min-w-0 flex-1 resize-none rounded-none border-0 bg-transparent px-1.5 py-2 text-base leading-[1.4] [overflow-wrap:anywhere] shadow-none [field-sizing:content] focus:ring-0 focus-visible:ring-0 focus-visible:outline-none! @min-[34rem]/conv:max-h-[min(11rem,30dvh)] @min-[34rem]/conv:px-1.5 sm:text-sm"
                                onKeyDown={alTeclear}
                                onPaste={alPegar}
                                aria-invalid={form.formState.errors.texto ? true : undefined}
                                {...registro}
                                ref={(el) => {
                                    refCampo(el);
                                    campo.current = el;
                                }}
                            />
                            {modo === "responder" && !cerrada && accesorio ? <span className="flex shrink-0 items-center self-center @min-[34rem]/conv:hidden">{accesorio}</span> : null}
                            <Boton
                                variante="primario"
                                tamano="icono"
                                className="shrink-0 rounded-full! @min-[34rem]/conv:hidden"
                                onClick={enviar}
                                disabled={!puedeEnviar}
                                aria-label={textoEnviar}
                                title={`${textoEnviar} (Enter)`}
                                data-slot="enviar-telefono"
                            >
                                <Send />
                            </Boton>
                        </div>
                    </>
                )}
                {form.formState.errors.texto?.message || error ? (
                    <p role="alert" className="px-3 pb-1 text-xs text-error">
                        {form.formState.errors.texto?.message ?? error}
                    </p>
                ) : null}
                <div
                    id={`herramientas-${conv.id}`}
                    className={cn("min-w-0 flex-wrap items-center gap-0.5 px-1.5 pt-0.5 pb-1.5", herramientas || soloPlantilla ? "flex" : "hidden", "@min-[34rem]/conv:flex")}
                    data-slot="barra-compositor"
                >
                    {barra}
                    {soloPlantilla ? null : (
                        <Boton variante="primario" tamano="sm" className="ml-auto hidden h-8 px-3 @min-[34rem]/conv:inline-flex" onClick={enviar} disabled={!puedeEnviar} title={`${textoEnviar} (Enter)`}>
                            {textoEnviar}
                            <Send />
                        </Boton>
                    )}
                </div>
            </div>
        </div>
    );
}

function MenuRespuestas({ respuestas, alElegir }: { respuestas: readonly RespuestaRapida[]; alElegir: (r: RespuestaRapida) => void }) {
    const [abierto, setAbierto] = useState(false);
    const [q, setQ] = useState("");
    const lista = respuestas.filter((r) => `${r.atajo} ${r.titulo} ${r.texto}`.toLowerCase().includes(q.trim().toLowerCase()));
    return (
        <PanelEmergente
            titulo="Respuestas rápidas"
            tituloVisible
            abierto={abierto}
            alCambiar={setAbierto}
            lado="top start"
            className="w-[min(20rem,calc(100vw-2rem))]"
            disparador={
                <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Respuestas rápidas" title="Respuestas rápidas">
                    <Zap />
                </Boton>
            }
        >
            <div className="grid gap-2">
                <CampoBusqueda etiqueta="Buscar respuestas" etiquetaVisible={false} tamano="sm" placeholder="Buscar o escribir /atajo" valor={q} alCambiar={setQ} autoFocus />
                <ul className="grid max-h-64 gap-0.5 overflow-y-auto">
                    {lista.length === 0 ? <li className="px-2 py-1.5 text-sm text-muted-foreground">Sin respuestas rápidas.</li> : null}
                    {lista.map((r) => (
                        <li key={r.id}>
                            <Boton
                                variante="fantasma"
                                className="h-auto w-full flex-col items-start gap-0 px-2 py-1.5 text-left"
                                onClick={() => {
                                    alElegir(r);
                                    setAbierto(false);
                                }}
                            >
                                <span className="flex w-full min-w-0 items-baseline gap-2">
                                    <span className="truncate text-sm font-semibold">{r.titulo}</span>
                                    <span className="ml-auto shrink-0 font-mono text-xs text-muted-foreground">{r.atajo}</span>
                                </span>
                                <span className="line-clamp-2 w-full text-xs font-normal text-muted-foreground">{r.texto}</span>
                            </Boton>
                        </li>
                    ))}
                </ul>
            </div>
        </PanelEmergente>
    );
}

function MenuMenciones({ candidatos, elegidos, alCambiar }: { candidatos: readonly { id: number; nombre: string; supervisa?: boolean }[]; elegidos: readonly number[]; alCambiar: (ids: number[]) => void }) {
    return (
        <PanelEmergente
            titulo="Mencionar"
            tituloVisible
            lado="top start"
            className="w-[min(18rem,calc(100vw-2rem))]"
            disparador={
                <Boton variante="fantasma" tamano="sm" className="h-8 px-2 text-muted-foreground" title="Mencionar a un compañero">
                    <AtSign />
                    Mencionar
                </Boton>
            }
        >
            <div className="grid max-h-64 gap-0.5 overflow-y-auto">
                {candidatos.length === 0 ? <p className="text-sm text-muted-foreground">No hay compañeros en esta cola.</p> : null}
                {candidatos.map((a) => (
                    <Casilla
                        key={a.id}
                        etiqueta={`${a.nombre}${a.supervisa ? " · supervisa" : ""}`}
                        checked={elegidos.includes(a.id)}
                        onChange={(e) => alCambiar(e.target.checked ? [...elegidos, a.id] : elegidos.filter((x) => x !== a.id))}
                    />
                ))}
            </div>
        </PanelEmergente>
    );
}

function MenuEmoji({ alElegir }: { alElegir: (e: string) => void }) {
    return (
        <PanelEmergente
            titulo="Emoji"
            lado="top start"
            className="w-auto p-1.5"
            disparador={
                <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Emoji" title="Emoji">
                    <Smile />
                </Boton>
            }
        >
            <div className="grid grid-cols-8 gap-0.5">
                {EMOJIS.map((e) => (
                    <Boton key={e} variante="fantasma" tamano="icono-sm" className="text-lg" aria-label={`Insertar ${e}`} onClick={() => alElegir(e)}>
                        {e}
                    </Boton>
                ))}
            </div>
        </PanelEmergente>
    );
}
