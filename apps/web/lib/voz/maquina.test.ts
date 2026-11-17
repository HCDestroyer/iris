import { describe, expect, it } from "vitest";
import { alCerrarPestana, debeSoltar, debeSonar, LIBRE, llamadaDe, ocupada, siguiente, type EstadoVoz, type EventoVoz } from "./maquina";
import { duracionCorta, llamadaEnHilo, motivoDeCodigo, quienLlama, reloj, textoAviso, textoRequisito } from "./presentacion";
import type { Llamada } from "./tipos";

const YO = 1;
const PUEDE = { contestar: true, rechazar: true, colgar: false, tomar: false, transferir: false, anotar: false, escucharGrabacion: false };

const llamada = (cambio: Partial<Llamada> = {}): Llamada => ({
    id: "0199aaaa-0000-7000-8000-000000000001",
    origen: "whatsapp",
    direccion: "entrante",
    conversacionId: 41,
    numero: "+50255120041",
    titulo: "Carlos Ruiz",
    contacto: { tipo: "cliente", clienteId: 4101, servicioId: 41010, verificacion: "numero" },
    estado: "sonando",
    tramos: [],
    sonoEn: "2026-11-17T16:00:00Z",
    puede: PUEDE,
    ...cambio,
});

const T0 = Date.parse("2026-11-17T16:00:10Z");
const correr = (eventos: EventoVoz[], desde: EstadoVoz = LIBRE) => eventos.reduce((e, ev) => siguiente(e, ev, YO), desde);
const hastaHablar: EventoVoz[] = [
    { tipo: "entrante", llamada: llamada() },
    { tipo: "contestar" },
    { tipo: "reclamada", llamada: llamada({ estado: "conectando" }), venceEn: T0 + 12_000 },
    { tipo: "microfono_listo" },
    { tipo: "respuesta_enviada", llamada: llamada({ estado: "conectando" }) },
    { tipo: "medio_confirmado", llamada: llamada({ estado: "en_curso", usuario: { id: YO, nombre: "Ana López" }, puede: { ...PUEDE, contestar: false, colgar: true, anotar: true } }), ahora: T0 },
];

describe("máquina de la llamada entrante (contratos/voz.md §3.3)", () => {
    it("el camino feliz: suena, se reclama, micrófono, SDP, medio confirmado y se habla", () => {
        const pasos = hastaHablar.map((_, i) => correr(hastaHablar.slice(0, i + 1)));
        expect(pasos.map((e) => (e.fase === "contestando" ? `contestando:${e.paso}` : e.fase))).toEqual(["sonando", "contestando:reclamando", "contestando:microfono", "contestando:negociando", "contestando:confirmando", "en_curso"]);
        const hablando = pasos.at(-1);
        expect(hablando).toMatchObject({ fase: "en_curso", desde: T0, silenciada: false });
        // El timbre solo suena mientras espera; ya no al contestar.
        expect(pasos.map(debeSonar)).toEqual([true, false, false, false, false, false]);
        expect(pasos.map(ocupada)).toEqual([false, true, true, true, true, true]);
        expect(pasos[2]).toMatchObject({ venceEn: T0 + 12_000 });
    });

    it("los pasos fuera de orden no hacen nada (una respuesta tardía no reabre una llamada)", () => {
        expect(siguiente(LIBRE, { tipo: "contestar" }, YO)).toBe(LIBRE);
        expect(siguiente(LIBRE, { tipo: "reclamada", llamada: llamada(), venceEn: 1 }, YO)).toBe(LIBRE);
        const sonando = correr(hastaHablar.slice(0, 1));
        expect(siguiente(sonando, { tipo: "medio_confirmado", llamada: llamada(), ahora: T0 }, YO)).toBe(sonando);
        const reclamando = correr(hastaHablar.slice(0, 2));
        expect(siguiente(reclamando, { tipo: "respuesta_enviada", llamada: llamada() }, YO)).toBe(reclamando);
        expect(siguiente(reclamando, { tipo: "microfono_listo" }, YO)).toBe(reclamando);
    });

    it("otra persona la contesta mientras me suena: deja de sonar y dice quién", () => {
        const sonando = correr(hastaHablar.slice(0, 1));
        const e = siguiente(sonando, { tipo: "actualizada", llamada: llamada({ estado: "conectando", usuario: { id: 7, nombre: "Andrea Castillo" }, puede: { ...PUEDE, contestar: false } }) }, YO);
        expect(e).toMatchObject({ fase: "aviso", motivo: "otro_la_contesto", quien: "Andrea Castillo" });
        expect(textoAviso("otro_la_contesto", "Andrea Castillo").titulo).toBe("La contestó Andrea Castillo");
        expect(debeSonar(e)).toBe(false);
    });

    it("pierdo la carrera al reclamar (409 `llamada_ya_contestada`): mensaje llano y se cierra", () => {
        const reclamando = correr(hastaHablar.slice(0, 2));
        expect(motivoDeCodigo("llamada_ya_contestada", 409)).toBe("otro_la_contesto");
        const e = siguiente(reclamando, { tipo: "fallo", motivo: "otro_la_contesto", quien: "Andrea Castillo" }, YO);
        expect(e).toMatchObject({ fase: "aviso", motivo: "otro_la_contesto" });
        // No hay reserva mía que soltar.
        expect(debeSoltar(reclamando, e)).toBeNull();
        expect(siguiente(e, { tipo: "cerrar" }, YO)).toBe(LIBRE);
    });

    it("otra pestaña mía la tiene (regla `misma_persona`): aquí solo se cierra, sin mensaje", () => {
        expect(siguiente(correr(hastaHablar.slice(0, 2)), { tipo: "otra_pestana" }, YO)).toBe(LIBRE);
        expect(siguiente(correr(hastaHablar.slice(0, 1)), { tipo: "otra_pestana" }, YO)).toBe(LIBRE);
        // Y la que sí habla no se entera.
        const hablando = correr(hastaHablar);
        expect(siguiente(hablando, { tipo: "otra_pestana" }, YO)).toBe(hablando);
        // Mi propia llamada actualizada (soy yo quien la tiene) no es «otra persona».
        const sonando = correr(hastaHablar.slice(0, 1));
        expect(siguiente(sonando, { tipo: "actualizada", llamada: llamada({ estado: "en_curso", usuario: { id: YO, nombre: "Ana López" } }) }, YO)).toBe(LIBRE);
    });

    it("vence la reserva (12 s) o el plazo de confirmar (5 s): mensaje llano y hay que soltarla", () => {
        for (const n of [3, 4, 5]) {
            const antes = correr(hastaHablar.slice(0, n));
            const e = siguiente(antes, { tipo: "plazo_vencido" }, YO);
            expect(e).toMatchObject({ fase: "aviso", motivo: "reserva_vencida" });
            expect(debeSoltar(antes, e)).toBe(llamada().id);
        }
        // El servidor la devolvió a `sonando` por su cuenta (soltó mi reserva).
        const negociando = correr(hastaHablar.slice(0, 4));
        expect(siguiente(negociando, { tipo: "actualizada", llamada: llamada({ estado: "sonando" }) }, YO)).toMatchObject({ fase: "aviso", motivo: "reserva_vencida" });
        // Mientras solo estoy reclamando, que siga `sonando` es lo normal.
        const reclamando = correr(hastaHablar.slice(0, 2));
        expect(siguiente(reclamando, { tipo: "actualizada", llamada: llamada({ estado: "sonando" }) }, YO)).toMatchObject({ fase: "contestando", paso: "reclamando" });
        // En `libre` un plazo viejo no hace nada.
        expect(siguiente(LIBRE, { tipo: "plazo_vencido" }, YO)).toBe(LIBRE);
    });

    it("el cliente cuelga antes de conectar, o falla algo: se dice y, si la reclamé, se suelta", () => {
        const negociando = correr(hastaHablar.slice(0, 4));
        const colgo = siguiente(negociando, { tipo: "actualizada", llamada: llamada({ estado: "terminada" }) }, YO);
        expect(colgo).toMatchObject({ fase: "aviso", motivo: "termino_antes" });
        expect(debeSoltar(negociando, colgo)).toBeNull();
        for (const motivo of ["microfono_denegado", "sin_microfono", "medio_fallido", "sin_red", "meta_rechazo", "error"] as const) {
            const e = siguiente(negociando, { tipo: "fallo", motivo }, YO);
            expect(e).toMatchObject({ fase: "aviso", motivo });
            expect(debeSoltar(negociando, e)).toBe(llamada().id);
            expect(textoAviso(motivo).titulo).not.toBe("");
        }
        // Mientras suena y nadie contesta: se pierde y deja de sonar, sin aviso.
        const sonando = correr(hastaHablar.slice(0, 1));
        expect(siguiente(sonando, { tipo: "actualizada", llamada: llamada({ estado: "perdida" }) }, YO)).toBe(LIBRE);
        // Ya no puedo contestarla (rechacé o me retiraron): deja de sonar.
        expect(siguiente(sonando, { tipo: "actualizada", llamada: llamada({ puede: { ...PUEDE, contestar: false } }) }, YO)).toBe(LIBRE);
    });

    it("rechazar deja de sonar aquí; una llamada que no puedo contestar no suena", () => {
        expect(siguiente(correr(hastaHablar.slice(0, 1)), { tipo: "rechazar" }, YO)).toBe(LIBRE);
        expect(siguiente(LIBRE, { tipo: "entrante", llamada: llamada({ puede: { ...PUEDE, contestar: false } }) }, YO)).toBe(LIBRE);
        expect(siguiente(LIBRE, { tipo: "entrante", llamada: llamada({ estado: "perdida" }) }, YO)).toBe(LIBRE);
    });

    it("una segunda llamada no interrumpe: ni mientras suena otra, ni contestando, ni hablando", () => {
        const otra = llamada({ id: "0199aaaa-0000-7000-8000-000000000002", titulo: "Lucía Ramos" });
        for (const n of [1, 2, 4, 6]) {
            const antes = correr(hastaHablar.slice(0, n));
            expect(llamadaDe(siguiente(antes, { tipo: "entrante", llamada: otra }, YO))?.id).toBe(llamada().id);
            // Y lo que le pase a la otra no toca a esta.
            expect(siguiente(antes, { tipo: "actualizada", llamada: { ...otra, estado: "terminada" } }, YO)).toBe(antes);
        }
        // La misma llamada entregada dos veces (el feed puede repetir) no cambia nada.
        const sonando = correr(hastaHablar.slice(0, 1));
        expect(siguiente(sonando, { tipo: "entrante", llamada: llamada() }, YO)).toEqual(sonando);
        // Tras un aviso, una llamada nueva sí suena.
        const aviso = siguiente(correr(hastaHablar.slice(0, 2)), { tipo: "fallo", motivo: "error" }, YO);
        expect(siguiente(aviso, { tipo: "entrante", llamada: otra }, YO)).toMatchObject({ fase: "sonando" });
    });

    it("en curso: silenciar, colgar, que cuelgue el cliente o que se caiga el medio", () => {
        const hablando = correr(hastaHablar);
        expect(siguiente(hablando, { tipo: "silenciar", silenciada: true }, YO)).toMatchObject({ fase: "en_curso", silenciada: true });
        const colgue = siguiente(hablando, { tipo: "colgar", ahora: T0 + 72_000 }, YO);
        expect(colgue).toMatchObject({ fase: "terminada", duracionSeg: 72, corte: false });
        // Colgó el cliente: llega por el feed con la duración del servidor.
        const colgo = siguiente(hablando, { tipo: "actualizada", llamada: llamada({ estado: "terminada", duracionSeg: 95, terminadaEn: "2026-11-17T16:01:45Z" }) }, YO);
        expect(colgo).toMatchObject({ fase: "terminada", duracionSeg: 95, corte: false });
        // Se cayó la red: termina aquí y se dice que fue un corte.
        expect(siguiente(hablando, { tipo: "medio_caido", ahora: T0 + 30_000 }, YO)).toMatchObject({ fase: "terminada", duracionSeg: 30, corte: true });
        expect(siguiente(hablando, { tipo: "actualizada", llamada: llamada({ estado: "terminada", motivoFin: "error_medio", duracionSeg: 15 }) }, YO)).toMatchObject({ corte: true });
        // Tras colgar, la confirmación del servidor actualiza la llamada (para la nota) sin reabrir nada.
        const confirmada = siguiente(colgue, { tipo: "actualizada", llamada: llamada({ estado: "terminada", duracionSeg: 73, puede: { ...PUEDE, anotar: true } }) }, YO);
        expect(confirmada).toMatchObject({ fase: "terminada", duracionSeg: 72, llamada: { puede: { anotar: true } } });
        expect(siguiente(confirmada, { tipo: "cerrar" }, YO)).toBe(LIBRE);
        // Mientras está la nota abierta, otra entrante no la tapa.
        expect(siguiente(colgue, { tipo: "entrante", llamada: llamada({ id: "otra" }) }, YO)).toBe(colgue);
    });

    it("al cerrar la pestaña: nada si solo suena, `soltar` si la reclamé, `colgar` si hablo", () => {
        const en = (n: number) => alCerrarPestana(correr(hastaHablar.slice(0, n)));
        expect(en(0)).toBeNull();
        expect(en(1)).toBeNull();
        // Reclamando todavía no es mía (la respuesta no llegó): no se suelta nada.
        expect(en(2)).toBeNull();
        expect([en(3), en(4), en(5)]).toEqual(Array(3).fill({ accion: "soltar", id: llamada().id }));
        expect(en(6)).toEqual({ accion: "colgar", id: llamada().id });
    });
});

describe("textos de la voz", () => {
    it("reloj y duración", () => {
        expect(reloj(0)).toBe("00:00");
        expect(reloj(72)).toBe("01:12");
        expect(reloj(3727)).toBe("1:02:07");
        expect(duracionCorta(25)).toBe("25 s");
        expect(duracionCorta(65)).toBe("1 min 5 s");
        expect(duracionCorta(600)).toBe("10 min");
    });

    it("quién llama: el cliente o «Número desconocido», nunca un nombre inventado", () => {
        expect(quienLlama(llamada())).toEqual({ nombre: "Carlos Ruiz", conocido: true });
        expect(quienLlama(llamada({ contacto: { tipo: "desconocido" }, titulo: "+50255120041" }))).toEqual({ nombre: "Número desconocido", conocido: false });
        // Un perfil de WhatsApp sin cliente asociado no cuenta como cliente.
        expect(quienLlama(llamada({ contacto: { tipo: "desconocido" }, titulo: "Carlitos" })).conocido).toBe(false);
    });

    it("la llamada en el hilo: icono, tono y texto por estado; solo la perdida pide actuar", () => {
        const en = (c: Partial<Parameters<typeof llamadaEnHilo>[0]>) => llamadaEnHilo({ id: "x", direccion: "entrante", estado: "terminada", tieneNota: false, ...c });
        expect(en({ estado: "terminada", atendidaPor: "persona", usuario: { id: 7, nombre: "Andrea Castillo" }, duracionSeg: 185 })).toEqual({ icono: "contestada", tono: "ok", titulo: "Llamada de WhatsApp", detalle: "La atendió Andrea Castillo · 3 min 5 s" });
        expect(en({ estado: "perdida", duracionSeg: 25 })).toEqual({ icono: "perdida", tono: "error", titulo: "Llamada perdida de WhatsApp", detalle: "Sonó 25 s y nadie contestó" });
        expect(en({ estado: "rechazada" })).toMatchObject({ icono: "rechazada", tono: "neutro" });
        expect(en({ estado: "fuera_de_horario" })).toMatchObject({ icono: "fuera_de_horario", tono: "aviso", titulo: "Llamó fuera de horario" });
        expect(en({ estado: "fallida" })).toMatchObject({ tono: "aviso" });
        expect(en({ estado: "en_curso", usuario: { id: 7, nombre: "Andrea Castillo" } })).toMatchObject({ tono: "info", titulo: "En llamada", detalle: "Con Andrea Castillo" });
        expect(Object.values([en({ estado: "terminada" }), en({ estado: "rechazada" }), en({ estado: "fuera_de_horario" })]).every((x) => x.tono !== "error")).toBe(true);
    });

    it("errores del servidor y requisitos de Meta, en llano", () => {
        expect(motivoDeCodigo("reserva_vencida", 409)).toBe("reserva_vencida");
        expect(motivoDeCodigo("llamada_terminada", 409)).toBe("termino_antes");
        expect(motivoDeCodigo("no_recibe_llamadas", 422)).toBe("no_recibe_llamadas");
        expect(motivoDeCodigo("meta_rechazo", 502)).toBe("meta_rechazo");
        expect(motivoDeCodigo("sin_conexion", 0)).toBe("sin_red");
        expect(motivoDeCodigo("otro", 500)).toBe("error");
        expect(textoRequisito({ codigo: "limite_mensajeria", cumple: false, aviso: false, detalle: "Hoy: 250 al día." })).toEqual({ nombre: "Puede escribirle a 2,000 personas al día o más", detalle: "Hoy: 250 al día.", estado: "falta" });
        expect(textoRequisito({ codigo: "plantilla_permiso", cumple: false, aviso: true, detalle: "" }).estado).toBe("aviso");
        expect(textoRequisito({ codigo: "webhook_calls", cumple: true, aviso: false, detalle: "" }).estado).toBe("cumple");
    });
});
