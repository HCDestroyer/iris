import { describe, expect, it } from "vitest";
import { ajustesDePrueba, entradaPrincipal, PERFIL_POR_DEFECTO, valoresPrincipal, type PerfilAsistente } from "./fase3";
import { perfilDeValores, valoresPerfil } from "./fase3b";
import type { AjustesAsistente } from "./tipos";
import {
    campoDeErrorVisitas,
    conVisitas,
    diasQueOfrece,
    entradaVisitas,
    esquemaVisitas,
    resumenVisitas,
    textoAnticipacion,
    textoBloques,
    valoresVisitas,
    visitaExigeIdentidad,
    visitasDeValores,
    type Visitas,
} from "./visitas";

const FABRICA: Visitas = {
    tiposPermitidos: ["visita"],
    maxAbiertasPorCliente: 1,
    exigeTicket: true,
    permitirConMora: false,
    bloques: [
        { inicio: "08:00", fin: "12:00" },
        { inicio: "13:00", fin: "17:00" },
    ],
    anticipacionHoras: 12,
    diasAdelante: 7,
    ofertasPorDia: 3,
    visitasPorDia: 1,
    visitasPorDiaEmpresa: 20,
};

const GENERAL: PerfilAsistente = { ...PERFIL_POR_DEFECTO, acciones: { ...PERFIL_POR_DEFECTO.acciones, agendar_visita: true, dar_saldo: true }, exigeVerificacionFuerte: { dar_saldo: false } };
const NOCHE: PerfilAsistente = { ...PERFIL_POR_DEFECTO, codigo: "noche", nombre: "Noche", exigeVerificacionFuerte: { dar_saldo: true, agendar_visita: true } };

const BASE: AjustesAsistente & { visitas: Visitas } = {
    activo: true,
    proveedor: "ollama",
    url: "http://10.0.0.20:11434",
    claveGuardadaEn: null,
    modelo: "qwen3:32b",
    modo: "automatico",
    fuentes: { manuales: true, argos: true, horus: true, historial: true },
    topeTokensMes: 5_000_000,
    precioEntradaMillon: null,
    precioSalidaMillon: null,
    autonomoDisponible: true,
    modoAutonomo: "sin_nadie",
    perfiles: [GENERAL, NOCHE],
    visitas: FABRICA,
    version: 4,
};

describe("el asistente agenda visitas (§17.10)", () => {
    it("de fábrica viene apagado y, sin pedir documento, basta el número", () => {
        expect(PERFIL_POR_DEFECTO.acciones.agendar_visita).toBe(false);
        expect(PERFIL_POR_DEFECTO.limites.diasAdelanteMax).toBe(7);
        expect(visitaExigeIdentidad(PERFIL_POR_DEFECTO)).toBe(false);
        expect(visitaExigeIdentidad(NOCHE)).toBe(true);
    });

    it("si el servidor no manda `visitas`, no hay nada que mostrar ni que guardar", () => {
        const { visitas: _v, ...viejo } = BASE;
        void _v;
        const sin: AjustesAsistente = viejo;
        expect(conVisitas(sin)).toBe(false);
        expect(conVisitas(BASE)).toBe(true);
        // La pantalla simple tampoco lo manda: ausente = se deja como está.
        expect(entradaPrincipal(BASE, valoresPrincipal(BASE, []))).not.toHaveProperty("visitas");
    });

    it("lo guardado llega al formulario tal cual y vuelve completo, sin valores inventados", () => {
        const v = valoresVisitas(BASE, FABRICA);
        expect(v).toEqual({
            tipos: { visita: true, mantenimiento: false },
            bloques: FABRICA.bloques,
            anticipacionHoras: "12",
            diasAdelante: "7",
            exigeTicket: true,
            permitirConMora: false,
            pedirDocumento: false,
            maxAbiertasPorCliente: "1",
            ofertasPorDia: "3",
            visitasPorDia: "1",
            visitasPorDiaEmpresa: "20",
        });
        expect(esquemaVisitas.safeParse(v).success).toBe(true);
        expect(visitasDeValores(v, FABRICA)).toEqual(FABRICA);
    });

    it("guardar manda el bloque completo y solo toca `agendar_visita` de la verificación del perfil general", () => {
        const d = { ...valoresVisitas(BASE, FABRICA), tipos: { visita: true, mantenimiento: true }, permitirConMora: true, pedirDocumento: true, diasAdelante: "14", visitasPorDiaEmpresa: "50" };
        const e = entradaVisitas(BASE, d);
        expect(e.visitas).toEqual({ ...FABRICA, tiposPermitidos: ["visita", "mantenimiento"], permitirConMora: true, diasAdelante: 14, visitasPorDiaEmpresa: 50 });
        expect(e.perfiles?.[0]).toEqual({ ...GENERAL, exigeVerificacionFuerte: { dar_saldo: false, agendar_visita: true } });
        expect(e.perfiles?.[1]).toEqual(NOCHE);
        expect(e).toMatchObject({ version: 4, proveedor: "ollama" });
        expect(e).not.toHaveProperty("modoAutonomo");
    });

    it("el orden de los tipos se conserva: el asistente usa el primero", () => {
        const guardado = { ...FABRICA, tiposPermitidos: ["mantenimiento", "visita"] };
        const d = valoresVisitas(BASE, guardado);
        expect(visitasDeValores(d, guardado).tiposPermitidos).toEqual(["mantenimiento", "visita"]);
        expect(visitasDeValores({ ...d, tipos: { visita: true, mantenimiento: false } }, guardado).tiposPermitidos).toEqual(["visita"]);
        // Un tipo que el panel no conoce no se muestra y no viaja.
        expect(valoresVisitas(BASE, { ...FABRICA, tiposPermitidos: ["otro"] }).tipos).toEqual({ visita: false, mantenimiento: false });
    });

    it("espejo del servidor: rangos, al menos un tipo y horarios en orden y sin cruzarse", () => {
        const v = valoresVisitas(BASE, FABRICA);
        const falla = (cambio: Partial<typeof v>) => esquemaVisitas.safeParse({ ...v, ...cambio }).success === false;
        expect(falla({ tipos: { visita: false, mantenimiento: false } })).toBe(true);
        expect(falla({ bloques: [] })).toBe(true);
        expect(falla({ bloques: Array.from({ length: 7 }, (_, i) => ({ inicio: `${String(8 + i).padStart(2, "0")}:00`, fin: `${String(8 + i).padStart(2, "0")}:30` })) })).toBe(true);
        expect(falla({ bloques: [{ inicio: "12:00", fin: "08:00" }] })).toBe(true);
        expect(falla({ bloques: [{ inicio: "08:00", fin: "12:00" }, { inicio: "11:00", fin: "15:00" }] })).toBe(true);
        expect(falla({ bloques: [{ inicio: "13:00", fin: "17:00" }, { inicio: "08:00", fin: "12:00" }] })).toBe(true);
        expect(falla({ bloques: [{ inicio: "", fin: "" }] })).toBe(true);
        // Seguidos (el fin de uno es el inicio del otro) sí valen.
        expect(falla({ bloques: [{ inicio: "08:00", fin: "12:00" }, { inicio: "12:00", fin: "16:00" }] })).toBe(false);
        for (const [campo, malos, buenos] of [
            ["anticipacionHoras", ["-1", "169", ""], ["0", "168"]],
            ["diasAdelante", ["0", "31"], ["1", "30"]],
            ["maxAbiertasPorCliente", ["0", "6"], ["1", "5"]],
            ["ofertasPorDia", ["0", "11"], ["1", "10"]],
            ["visitasPorDia", ["0", "6"], ["1", "5"]],
            ["visitasPorDiaEmpresa", ["0", "501", "1.5"], ["1", "500"]],
        ] as const) {
            for (const m of malos) expect(falla({ [campo]: m })).toBe(true);
            for (const b of buenos) expect(falla({ [campo]: b })).toBe(false);
        }
    });

    it("el 422 del servidor va al campo que nombra", () => {
        expect(campoDeErrorVisitas("visitas.visitasPorDiaEmpresa")).toBe("visitasPorDiaEmpresa");
        expect(campoDeErrorVisitas("body.visitas.maxAbiertasPorCliente")).toBe("maxAbiertasPorCliente");
        expect(campoDeErrorVisitas("visitas.tiposPermitidos")).toBe("tipos");
        expect(campoDeErrorVisitas("visitas.tiposPermitidos[1]")).toBe("tipos");
        expect(campoDeErrorVisitas("visitas.bloques")).toBe("bloques");
        expect(campoDeErrorVisitas("visitas.bloques[2]")).toBe("bloques.2.inicio");
        // Lo que no es de esta pantalla (o un campo que el panel no conoce) va al aviso general.
        for (const f of ["body.url", "visitas.campoNuevo", "perfiles[0].limites.diasAdelanteMax", ""]) expect(campoDeErrorVisitas(f)).toBeNull();
    });

    it("resumen llano de «Cómo agenda»", () => {
        expect(textoBloques(FABRICA.bloques)).toBe("de 8 a 12 y de 13 a 17");
        expect(textoBloques([{ inicio: "07:30", fin: "11:00" }])).toBe("de 7:30 a 11");
        expect(textoBloques(null)).toBe("sin horarios");
        expect(textoAnticipacion(0)).toBe("sin anticipación");
        expect(textoAnticipacion(1)).toBe("con 1 hora de anticipación");
        expect(textoAnticipacion(12)).toBe("con 12 horas de anticipación");
        expect(textoAnticipacion(48)).toBe("con 2 días de anticipación");
        expect(resumenVisitas(BASE, FABRICA)).toEqual([
            "Agenda visita técnica, de 8 a 12 y de 13 a 17.",
            "Ofrece con 12 horas de anticipación y hasta 7 días adelante.",
            "Crea un ticket antes si no hay. Si el servicio está cortado por mora, lo pasa a una persona.",
        ]);
        // «Días adelante» de Límites acorta lo que ofrece.
        const corto = { perfiles: [{ ...GENERAL, limites: { ...GENERAL.limites, diasAdelanteMax: 2 }, exigeVerificacionFuerte: { dar_saldo: true, agendar_visita: true } }] };
        expect(diasQueOfrece(corto, FABRICA)).toBe(2);
        expect(resumenVisitas(corto, { ...FABRICA, exigeTicket: false, permitirConMora: true })).toEqual([
            "Agenda visita técnica, de 8 a 12 y de 13 a 17.",
            "Ofrece con 12 horas de anticipación y hasta 2 días adelante.",
            "Agenda aunque el servicio esté cortado por mora. Antes pide DPI o NIT.",
        ]);
    });

    it("las otras pantallas no borran «pedir documento antes de agendar»", () => {
        const conDocumento: AjustesAsistente = { ...BASE, perfiles: [{ ...GENERAL, exigeVerificacionFuerte: { dar_saldo: false, agendar_visita: true } }, NOCHE] };
        const v = valoresPrincipal(conDocumento, []);
        // La pantalla simple (saldo) conserva lo de las visitas…
        expect(entradaPrincipal(conDocumento, v).perfiles?.[0]?.exigeVerificacionFuerte).toEqual({ dar_saldo: false, agendar_visita: true });
        // …«Pruébalo» lo manda con el formulario sin guardar…
        expect(ajustesDePrueba(v, conDocumento, []).perfil).toMatchObject({ acciones: { agendar_visita: true }, exigeVerificacionFuerte: { dar_saldo: false, agendar_visita: true } });
        // …y el editor de perfiles por horario también.
        expect(perfilDeValores(valoresPerfil(NOCHE), "noche", NOCHE).exigeVerificacionFuerte).toEqual({ dar_saldo: true, agendar_visita: true });
    });
});
