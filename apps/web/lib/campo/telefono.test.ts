import type { TelefonoMio } from "@iris/telefono";
import { describe, expect, it } from "vitest";
import { duracionLlana, enlaceTelefono, filasLlamadas, finDeLlamada, formaEmpresa, numerosDeOrden, senalDebil, textoSinEmpresa, type Llamada } from "./telefono";

const mio = (cambio: Partial<TelefonoMio> = {}): TelefonoMio => ({
    disponible: true,
    motivo: null,
    extension: "311",
    dispositivos: ["panel", "app"],
    puede: { llamar: true, transferir: false, recibir: true },
    limite: { porDia: 60, usadasHoy: 0 },
    ...cambio,
});

const tel = (tipo: "telefono" | "movil", original: string, e164: string | null) => ({ tipo, original, e164, mostrar: e164 ? `+502 ${e164.slice(4, 8)} ${e164.slice(8)}` : original });

describe("los números de la orden (pantalla 24)", () => {
    it("contacto en sitio, móvil y principal, uno por clave y sin repetir", () => {
        const n = numerosDeOrden({
            contacto: { nombre: "María", telefono: "5512 3480" },
            cliente: { telefonos: [tel("telefono", "2433 1020", "+50224331020"), tel("movil", "5512 3480", "+50255123480")] } as never,
        });
        expect(n.map((x) => [x.clave, x.telefono.e164])).toEqual([
            ["contacto", "+50255123480"],
            ["principal", "+50224331020"],
        ]);
        expect(n[0]?.etiqueta).toBe("Contacto en sitio");
        const primero = n[0];
        expect(primero && enlaceTelefono(primero.telefono)).toBe("tel:+50255123480");
    });

    it("sin contacto en sitio: el móvil y el principal de Argos", () => {
        const n = numerosDeOrden({ contacto: { nombre: null, telefono: null }, cliente: { telefonos: [tel("movil", "5512 3480", "+50255123480"), tel("telefono", "2433 1020", "+50224331020")] } as never });
        expect(n.map((x) => x.clave)).toEqual(["movil", "principal"]);
    });

    it("sin números: nada que ofrecer", () => {
        expect(numerosDeOrden({ contacto: { nombre: null, telefono: null }, cliente: null } as never)).toEqual([]);
    });
});

describe("¿se ofrece «Llamar por la empresa»?", () => {
    it("solo con señal y con el teléfono disponible para la app", () => {
        expect(formaEmpresa("en_linea", mio())).toEqual({ ofrecer: true });
        expect(formaEmpresa("sin_conexion", mio())).toEqual({ ofrecer: false, motivo: "sin_senal" });
        expect(formaEmpresa("en_linea", null)).toEqual({ ofrecer: false, motivo: "revisando" });
        expect(formaEmpresa("en_linea", mio({ disponible: false, motivo: "sin_extension" }))).toEqual({ ofrecer: false, motivo: "sin_extension" });
        expect(formaEmpresa("en_linea", mio({ disponible: false, motivo: "telefono_apagado" }))).toEqual({ ofrecer: false, motivo: "no_disponible" });
        expect(formaEmpresa("en_linea", mio({ dispositivos: ["panel"] }))).toEqual({ ofrecer: false, motivo: "no_disponible" });
        expect(formaEmpresa("en_linea", mio({ puede: { llamar: false, transferir: false, recibir: true } }))).toEqual({ ofrecer: false, motivo: "no_disponible" });
    });

    it("el aviso en llano de cada caso (24b y 31)", () => {
        expect(textoSinEmpresa("sin_senal")?.titulo).toBe("Sin señal");
        expect(textoSinEmpresa("sin_extension")?.titulo).toBe("Todavía no tienes extensión de la empresa");
        expect(textoSinEmpresa("revisando")).toBeNull();
    });
});

describe("la llamada", () => {
    it("la señal débil (2G o mucha latencia) no alcanza; sin la API, sí", () => {
        expect(senalDebil({ effectiveType: "2g" })).toBe(true);
        expect(senalDebil({ effectiveType: "4g", rtt: 2000 })).toBe(true);
        expect(senalDebil({ effectiveType: "4g", rtt: 100 })).toBe(false);
        expect(senalDebil(undefined)).toBe(false);
    });

    it("cómo terminó (pantallas 34 y 36)", () => {
        expect(finDeLlamada("Terminated", true)).toBe("terminada");
        expect(finDeLlamada("Busy", false)).toBe("ocupado");
        expect(finDeLlamada("No Answer", false)).toBe("no_contesto");
        expect(finDeLlamada("Unavailable", false)).toBe("no_contesto");
        expect(finDeLlamada("Connection Error", false)).toBe("fallo");
    });

    it("duración en llano", () => {
        expect(duracionLlana(45)).toBe("45 s");
        expect(duracionLlana(192)).toBe("3 min 12 s");
        expect(duracionLlana(120)).toBe("2 min");
    });
});

describe("la tarjeta «Llamadas» (pantallas 34 y 35)", () => {
    const base = { empresaId: 1, direccion: "saliente", numero: "+50255123480", titulo: "", contacto: { tipo: "desconocido" }, tramos: [], puede: {}, version: 1 } as unknown as Llamada;
    it("por la empresa con su resultado, las directas y las que siguen en la cola, de la más nueva a la más vieja", () => {
        const filas = filasLlamadas(
            [
                { ...base, id: "a", origen: "centralita", estado: "terminada", sonoEn: "2026-11-21T16:42:00Z", contestadaEn: "2026-11-21T16:42:10Z", duracionSeg: 192 },
                { ...base, id: "b", origen: "centralita", estado: "terminada", sonoEn: "2026-11-21T16:20:00Z" },
                { ...base, id: "c", origen: "celular", estado: "terminada", sonoEn: "2026-11-21T16:10:00Z" },
            ],
            [{ id: "d", ocurridoEn: "2026-11-21T16:30:00Z" }, { id: "c", ocurridoEn: "2026-11-21T16:10:00Z" }],
            "311",
        );
        expect(filas.map((f) => [f.id, f.tipo, f.pendiente])).toEqual([
            ["a", "empresa", false],
            ["d", "directa", true],
            ["b", "empresa", false],
            ["c", "directa", false],
        ]);
        expect(filas[0]).toMatchObject({ titulo: "Por la empresa · tú (ext. 311)", detalle: "10:42 · 3 min 12 s · Contestó", tono: "success" });
        expect(filas[2]).toMatchObject({ detalle: "10:20 · No contestó", tono: "warning" });
        expect(filas[3]).toMatchObject({ titulo: "Llamada directa desde tu celular", detalle: "10:10 · sin duración" });
    });
});
