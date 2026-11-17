import { describe, expect, it } from "vitest";
import { alCerrarPestana, alSalir, debeSonar, LIBRE, llamadaDe, ocupada, siguiente, type EstadoVoz, type EventoVoz } from "./maquina";
import { desdeCuando, llamadaEnHilo, motivoDeCodigo, textoAviso, textoErrorSolicitud, textoMotivoPermiso, textoPermiso } from "./presentacion";
import type { Llamada } from "./tipos";

/* Máquina de la llamada saliente (contratos/voz.md, E1b): preparar → marcar → hablar, y cada salida. */

const YO = 1;
const PUEDE = { contestar: false, rechazar: false, colgar: true, tomar: false, transferir: false, anotar: false, escucharGrabacion: false };

const saliente = (cambio: Partial<Llamada> = {}): Llamada => ({
    id: "0199bbbb-0000-7000-8000-000000000001",
    origen: "whatsapp",
    direccion: "saliente",
    conversacionId: 41,
    numero: "+50255120041",
    titulo: "Carlos Ruiz",
    contacto: { tipo: "cliente", clienteId: 4101, verificacion: "numero" },
    estado: "marcando",
    usuario: { id: YO, nombre: "Ana López" },
    tramos: [],
    sonoEn: "2026-11-17T16:00:00Z",
    puede: PUEDE,
    ...cambio,
});

const T0 = Date.parse("2026-11-17T16:00:20Z");
const correr = (eventos: EventoVoz[], desde: EstadoVoz = LIBRE) => eventos.reduce((e, ev) => siguiente(e, ev, YO), desde);
const LLAMAR: EventoVoz = { tipo: "llamar", conversacionId: 41, titulo: "Carlos Ruiz" };
const hastaMarcar: EventoVoz[] = [LLAMAR, { tipo: "marcando", llamada: saliente() }];
const hastaHablar: EventoVoz[] = [...hastaMarcar, { tipo: "actualizada", llamada: saliente({ estado: "en_curso", contestadaEn: "2026-11-17T16:00:20Z" }), ahora: T0 }];

describe("máquina de la llamada saliente (E1b)", () => {
    it("el camino feliz: se prepara, marca y, cuando el cliente contesta, se habla", () => {
        const preparando = correr([LLAMAR]);
        expect(preparando).toEqual({ fase: "llamando", paso: "preparando", conversacionId: 41, titulo: "Carlos Ruiz", llamada: null });
        expect(ocupada(preparando)).toBe(true);
        expect(debeSonar(preparando)).toBe(false);
        expect(llamadaDe(preparando)).toBeNull();
        const marcando = correr(hastaMarcar);
        expect(marcando).toMatchObject({ fase: "llamando", paso: "marcando", llamada: { estado: "marcando" } });
        // La respuesta SDP llega (aviso `llamada_respuesta`) y la llamada sigue marcando: aún no se habla.
        expect(siguiente(marcando, { tipo: "actualizada", llamada: saliente() }, YO)).toMatchObject({ fase: "llamando", paso: "marcando" });
        const hablando = correr(hastaHablar);
        expect(hablando).toMatchObject({ fase: "en_curso", desde: T0, silenciada: false, llamada: { direccion: "saliente", estado: "en_curso" } });
    });

    it("no se empieza otra con una llamada entrando, viva o sin anotar; sí tras un aviso", () => {
        const entrante = saliente({ id: "0199aaaa-0000-7000-8000-000000000009", direccion: "entrante", estado: "sonando", puede: { ...PUEDE, contestar: true } });
        const sonando = siguiente(LIBRE, { tipo: "entrante", llamada: entrante }, YO);
        expect(sonando.fase).toBe("sonando");
        expect(siguiente(sonando, LLAMAR, YO)).toBe(sonando);
        const hablando = correr(hastaHablar);
        expect(siguiente(hablando, LLAMAR, YO)).toBe(hablando);
        const terminada = siguiente(hablando, { tipo: "colgar", ahora: T0 + 5000 }, YO);
        expect(siguiente(terminada, LLAMAR, YO)).toBe(terminada);
        const aviso = correr([...hastaMarcar, { tipo: "plazo_vencido" }]);
        expect(siguiente(aviso, LLAMAR, YO).fase).toBe("llamando");
        // Y mientras llamo, una entrante no me interrumpe.
        const marcando = correr(hastaMarcar);
        expect(siguiente(marcando, { tipo: "entrante", llamada: entrante }, YO)).toBe(marcando);
    });

    it("el cliente no contesta, rechaza o pasa el minuto: «No contestó», con texto de saliente", () => {
        for (const estado of ["perdida", "rechazada"] as const) {
            const fin = correr([...hastaMarcar, { tipo: "actualizada", llamada: saliente({ estado, motivoFin: "nadie_contesto" }) }]);
            expect(fin).toMatchObject({ fase: "aviso", motivo: "no_contesto", titulo: "Carlos Ruiz", saliente: true });
        }
        expect(correr([...hastaMarcar, { tipo: "plazo_vencido" }])).toMatchObject({ fase: "aviso", motivo: "no_contesto", saliente: true });
        expect(textoAviso("no_contesto", undefined, true).titulo).toBe("No contestó");
        // WhatsApp la rechaza después de crearla: se dice.
        expect(correr([...hastaMarcar, { tipo: "actualizada", llamada: saliente({ estado: "fallida", motivoFin: "error_meta" }) }])).toMatchObject({ fase: "aviso", motivo: "meta_rechazo", saliente: true });
    });

    it("colgar mientras marca no deja nota; colgar hablando sí", () => {
        expect(correr([...hastaMarcar, { tipo: "colgar", ahora: T0 }])).toBe(LIBRE);
        expect(correr([LLAMAR, { tipo: "colgar", ahora: T0 }])).toBe(LIBRE);
        // El servidor confirma después que quedó `terminada` (`agente_colgo`): ya no cambia nada.
        expect(siguiente(LIBRE, { tipo: "actualizada", llamada: saliente({ estado: "terminada", motivoFin: "agente_colgo" }) }, YO)).toBe(LIBRE);
        expect(correr([...hastaHablar, { tipo: "colgar", ahora: T0 + 61_000 }])).toMatchObject({ fase: "terminada", duracionSeg: 61, corte: false });
    });

    it("el tope de minutos: antes de hablar es un aviso; hablando, la llamada termina con ese motivo", () => {
        expect(correr([...hastaMarcar, { tipo: "actualizada", llamada: saliente({ estado: "terminada", motivoFin: "tope_alcanzado" }) }])).toMatchObject({ fase: "aviso", motivo: "tope_alcanzado" });
        const cortada = correr([...hastaHablar, { tipo: "actualizada", llamada: saliente({ estado: "terminada", motivoFin: "tope_alcanzado", duracionSeg: 90 }) }]);
        expect(cortada).toMatchObject({ fase: "terminada", duracionSeg: 90, corte: false, llamada: { motivoFin: "tope_alcanzado" } });
    });

    it("los errores de `llamar` se dicen con el nombre del contacto y como saliente", () => {
        const fallo = correr([LLAMAR, { tipo: "fallo", motivo: "sin_permiso" }]);
        expect(fallo).toEqual({ fase: "aviso", motivo: "sin_permiso", titulo: "Carlos Ruiz", quien: undefined, detalle: undefined, saliente: true });
        expect(correr([LLAMAR, { tipo: "fallo", motivo: "error", detalle: "El número no existe en WhatsApp" }])).toMatchObject({ motivo: "error", detalle: "El número no existe en WhatsApp" });
        expect(correr([LLAMAR, { tipo: "abandonar" }])).toBe(LIBRE);
    });

    it("regla de oro: desde que sale `llamar` dejarla es colgarla; antes, no hay nada que colgar", () => {
        const preparando = correr([LLAMAR]);
        const marcando = correr(hastaMarcar);
        const aviso = (motivo: "sin_microfono" | "medio_fallido" | "otro_navegador" | "no_contesto"): EstadoVoz => ({ fase: "aviso", motivo, titulo: "Carlos Ruiz", saliente: true });
        // Micrófono negado o error definitivo del servidor: no se creó nada.
        expect(alSalir(preparando, aviso("sin_microfono"), "preparando", false)).toBeNull();
        expect(alSalir(preparando, LIBRE, "preparando", false)).toBeNull();
        // `llamar` salió y no ha respondido (o se perdió la respuesta): puede existir → colgar.
        expect(alSalir(preparando, LIBRE, "llamando", false)).toBe("colgar");
        expect(alSalir(preparando, aviso("medio_fallido"), "llamando", false)).toBe("colgar");
        // Marcando: «Colgar», el plazo, el medio que falla, irse.
        expect(alSalir(marcando, LIBRE, "llamando", false)).toBe("colgar");
        expect(alSalir(marcando, aviso("no_contesto"), "llamando", false)).toBe("colgar");
        // La llamada sigue en otro navegador mío: aquí no se toca.
        expect(alSalir(marcando, aviso("otro_navegador"), "llamando", false)).toBeNull();
        // Sigue ocupada (pasó a hablar): nada.
        expect(alSalir(marcando, correr(hastaHablar), "en_curso", false)).toBeNull();
        // Hablando, como cualquier llamada.
        const hablando = correr(hastaHablar);
        expect(alSalir(hablando, siguiente(hablando, { tipo: "medio_caido", ahora: T0 + 1000 }, YO), "en_curso", false)).toBe("colgar");
        expect(alSalir(hablando, siguiente(hablando, { tipo: "actualizada", llamada: saliente({ estado: "terminada" }) }, YO), "en_curso", false)).toBeNull();
    });

    it("al cerrar la pestaña: `colgar?propia=true` si marca; sin id (llamar en vuelo) no hay a quién colgar", () => {
        expect(alCerrarPestana(correr(hastaMarcar), "llamando", false)).toEqual({ accion: "colgar", id: saliente().id, propia: true });
        expect(alCerrarPestana(correr([LLAMAR]), "preparando", false)).toBeNull();
        expect(alCerrarPestana(correr([LLAMAR]), "llamando", false)).toBeNull();
        expect(alCerrarPestana(correr(hastaHablar), "en_curso", false)).toEqual({ accion: "colgar", id: saliente().id });
    });
});

describe("textos de las salientes y del permiso de llamada", () => {
    it("los errores de `llamar`, en llano", () => {
        expect(motivoDeCodigo("sin_permiso_llamada", 422)).toBe("sin_permiso");
        expect(motivoDeCodigo("limite_llamadas", 422)).toBe("limite_llamadas");
        expect(motivoDeCodigo("limite_llamadas", 422, "colgadas_marcando")).toBe("limite_colgadas");
        expect(motivoDeCodigo("tope_alcanzado", 422)).toBe("tope_alcanzado");
        expect(motivoDeCodigo("llamada_en_curso", 409)).toBe("otra_llamada");
        expect(motivoDeCodigo("devolucion_cerrada", 409)).toBe("devolucion_cerrada");
        expect(motivoDeCodigo("meta_rechazo", 502)).toBe("meta_rechazo");
        expect(motivoDeCodigo("llamadas_inactivas", 422)).toBe("llamadas_inactivas");
        expect(textoAviso("limite_colgadas").detalle).toContain("antes de que contestara");
        expect(textoAviso("tope_alcanzado").titulo).toContain("tope de minutos");
        // Los mismos motivos se dicen distinto si era una saliente.
        expect(textoAviso("otra_llamada").detalle).toContain("contestar");
        expect(textoAviso("otra_llamada", undefined, true).titulo).toBe("Ya hay una llamada en curso");
        expect(textoAviso("sin_red", undefined, true).titulo).toBe("No se pudo hacer la llamada");
        expect(textoAviso("error", undefined, true).titulo).toBe("No se pudo hacer la llamada");
    });

    it("la saliente en el hilo: quién la hizo; sin contestar en ámbar (no hay nada que devolver)", () => {
        const base = { id: "x", direccion: "saliente" as const, tieneNota: false, usuario: { id: 1, nombre: "Ana López" } };
        expect(llamadaEnHilo({ ...base, estado: "marcando" })).toEqual({ icono: "saliente", tono: "info", titulo: "Llamando al cliente…", detalle: "La hizo Ana López" });
        expect(llamadaEnHilo({ ...base, estado: "terminada", duracionSeg: 125 })).toEqual({ icono: "saliente", tono: "ok", titulo: "Llamada al cliente", detalle: "La hizo Ana López · 2 min 5 s" });
        expect(llamadaEnHilo({ ...base, estado: "perdida" })).toMatchObject({ icono: "saliente", tono: "aviso", titulo: "Se le llamó y no contestó" });
        expect(llamadaEnHilo({ ...base, estado: "terminada", duracionSeg: 60, motivoFin: "tope_alcanzado" }).detalle).toContain("tope de minutos");
        expect(llamadaEnHilo({ ...base, estado: "fallida" })).toMatchObject({ tono: "aviso", titulo: "La llamada al cliente no se pudo conectar" });
        // La entrante perdida sigue siendo la única en rojo.
        expect(llamadaEnHilo({ ...base, direccion: "entrante", estado: "perdida" }).tono).toBe("error");
    });

    it("el permiso en llano: sin pedir, pedido, concedido hasta…, negado y revocado", () => {
        expect(textoPermiso({ estado: "sin_pedir" })).toMatchObject({ tono: "neutro", titulo: "Sin permiso todavía" });
        expect(textoPermiso({ estado: "sin_pedir" }).detalle).toContain("ya venció");
        expect(textoPermiso({ estado: "pedido" })).toMatchObject({ tono: "info", titulo: "Permiso pedido" });
        expect(textoPermiso({ estado: "temporal", venceEn: "2026-11-24T18:00:00Z" })).toMatchObject({ tono: "ok", titulo: "Permiso concedido hasta el 24 de noviembre" });
        expect(textoPermiso({ estado: "permanente" })).toMatchObject({ tono: "ok", titulo: "Permiso concedido" });
        expect(textoPermiso({ estado: "negado" }).tono).toBe("aviso");
        expect(textoPermiso({ estado: "revocado" })).toMatchObject({ tono: "error", titulo: "WhatsApp quitó el permiso" });
        expect(textoMotivoPermiso("llamadas_inactivas")).toContain("no tiene las llamadas");
        expect(textoMotivoPermiso("no_autorizado")).toContain("no puede llamar");
        // `sin_permiso_llamada` ya lo dice el estado.
        expect(textoMotivoPermiso("sin_permiso_llamada")).toBeNull();
        expect(textoMotivoPermiso(undefined)).toBeNull();
    });

    it("los 422 de «Pedir permiso»: el límite dice desde cuándo (hora de Guatemala)", () => {
        expect(desdeCuando("2026-11-19T20:30:00Z")).toBe("a partir del 19 de noviembre, 14:30");
        expect(desdeCuando(undefined)).toBeNull();
        expect(desdeCuando("mañana")).toBeNull();
        expect(textoErrorSolicitud("limite_solicitudes", "2026-11-19T20:30:00Z")).toBe("WhatsApp deja pedirlo una vez cada 24 horas y dos veces cada 7 días. Se le puede volver a pedir a partir del 19 de noviembre, 14:30.");
        expect(textoErrorSolicitud("limite_solicitudes", undefined)).toBe("WhatsApp deja pedirlo una vez cada 24 horas y dos veces cada 7 días.");
        expect(textoErrorSolicitud("permiso_ya_vigente", undefined)).toContain("ya se le puede llamar");
        expect(textoErrorSolicitud("sin_plantilla_permiso", undefined)).toContain("plantilla de permiso");
        // BAJA y lo demás: lo que diga el servidor.
        expect(textoErrorSolicitud("baja", undefined)).toBeNull();
    });
});
