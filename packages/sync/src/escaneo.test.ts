// Escáner por campo: clasificación de lo leído, candidatos por objetivo, recuadro y ventana, con
// etiquetas típicas de ONU Huawei y ZTE (S/N + MAC + EAN) y QR con `SN:…;MAC:…`.
import { describe, expect, it } from "vitest";
import {
    candidatosPara,
    candidatoUnico,
    clasificarCodigo,
    COLECCION_VACIA,
    coleccionLista,
    dentroDelRecuadro,
    gtinValido,
    juntarCodigos,
    normalizarFormato,
    preguntaConfirmacion,
    recuadroCentrado,
    recuadroEnFuente,
    textoElegido,
    textoRepetido,
    textosEscaneo,
    SesionEscaneo,
    type CodigoVisto,
    type EstadoEscaneo,
} from "./campo";

// Etiqueta de una ONU Huawei EG8145V5: S/N (Code 128), GPON SN (hex), MAC, modelo y EAN-13.
const HUAWEI: CodigoVisto[] = [
    { valor: "6901443254538", formato: "ean13" },
    { valor: "EG8145V5", formato: "code128" },
    { valor: "2102311TUN10K5000123", formato: "code128" },
    { valor: "48575443A1B2C3D4", formato: "code128" },
    { valor: "A0B1C2D3E4F5", formato: "code128" },
];

// Etiqueta de una ONU ZTE F670L: S/N, MAC con separadores, EAN-13 y un QR con ambos.
const ZTE: CodigoVisto[] = [
    { valor: "6972632140024", formato: "EAN_13" },
    { valor: "ZTEGC8F1A2B3", formato: "CODE_128" },
    { valor: "a0:b1:c2:d3:e4:f6", formato: "CODE_128" },
    { valor: "F670L", formato: "CODE_128" },
    { valor: "SN:ZTEGC8F1A2B3;MAC:A0B1C2D3E4F6", formato: "QR_CODE" },
];

describe("clasificarCodigo", () => {
    it("reconoce MAC, serie, QR combinado, EAN/UPC y lo que no parece serie", () => {
        expect(clasificarCodigo("A0B1C2D3E4F5", "code128")).toBe("mac");
        expect(clasificarCodigo("a0-b1-c2-d3-e4-f5", "code128")).toBe("mac");
        // 12 dígitos en Code 128: es MAC aunque el dígito verificador cuadre como UPC-A.
        expect(clasificarCodigo("001122334455", "code128")).toBe("mac");
        expect(clasificarCodigo("ZTEGC8F1A2B3", "code128")).toBe("serie");
        expect(clasificarCodigo("2102311TUN10K5000123", "code128")).toBe("serie");
        expect(clasificarCodigo("48575443A1B2C3D4", "code128")).toBe("serie");
        expect(clasificarCodigo("SN:HWTC1234;MAC:a0b1c2d3e4f5", "qr")).toBe("qr_combinado");
        expect(clasificarCodigo('{"sn":"X1","mac":"a0b1c2d3e4f5"}', "qr")).toBe("qr_combinado");
        expect(clasificarCodigo("S/N=HWTC9A1B2C3D", "qr")).toBe("serie");
        expect(clasificarCodigo("MAC: A0B1C2D3E4F5", "qr")).toBe("mac");
        expect(clasificarCodigo("6901443254538", "ean13")).toBe("producto");
        expect(clasificarCodigo("036000291452", "upc_a")).toBe("producto");
        // Un EAN-13 válido dentro de un Code 128 también es del producto.
        expect(clasificarCodigo("4006381333931", "code128")).toBe("producto");
        expect(clasificarCodigo("EG8145V5", "code128")).toBe("desconocido");
        expect(clasificarCodigo("https://isp.gt/onu", "qr")).toBe("desconocido");
        expect(clasificarCodigo("   ", "qr")).toBe("desconocido");
    });

    it("normaliza formatos de expo-camera, BarcodeDetector y ZXing", () => {
        expect(normalizarFormato("qr_code")).toBe("qr");
        expect(normalizarFormato("QR_CODE")).toBe("qr");
        expect(normalizarFormato("upc_a")).toBe("upca");
        expect(normalizarFormato("EAN_13")).toBe("ean13");
        expect(gtinValido("4006381333931")).toBe(true);
        expect(gtinValido("4006381333932")).toBe(false);
    });
});

describe("candidatosPara", () => {
    it("serie en una ONU Huawei: las series primero, el EAN al final, MAC y modelo en medio", () => {
        const c = candidatosPara("serie", HUAWEI);
        expect(c.map((x) => x.valor)).toEqual(["2102311TUN10K5000123", "48575443A1B2C3D4", "EG8145V5", "A0B1C2D3E4F5", "6901443254538"]);
        expect(c[0]).toMatchObject({ encaja: true, elegible: true, insignia: "Parece serie", tipoTexto: "Código de barras", formatoTexto: "Code 128", uso: { serie: "2102311TUN10K5000123" } });
        expect(c.find((x) => x.valor === "EG8145V5")?.insignia).toBe("No parece serie");
        expect(c.find((x) => x.valor === "A0B1C2D3E4F5")?.insignia).toBe("Parece MAC");
        expect(c.at(-1)).toMatchObject({ clase: "producto", encaja: false, insignia: "Parece código de producto (EAN/UPC)", formatoTexto: "EAN-13" });
        // Varias series: no hay confirmación de uno solo.
        expect(candidatoUnico(c)).toBeNull();
    });

    it("un EAN/UPC nunca va primero para la serie, aunque sea lo único que se vio antes", () => {
        const c = candidatosPara("serie", [
            { valor: "6901443254538", formato: "ean13" },
            { valor: "EG8145V5", formato: "code128" },
        ]);
        expect(c[0]?.valor).toBe("EG8145V5");
        expect(candidatoUnico(c)).toBeNull();
    });

    it("MAC en una ONU ZTE: la MAC y el QR encajan; lo demás es «No es una MAC» y no se elige", () => {
        const c = candidatosPara("mac", ZTE);
        expect(c.slice(0, 2).map((x) => x.valor)).toEqual(["a0:b1:c2:d3:e4:f6", "SN:ZTEGC8F1A2B3;MAC:A0B1C2D3E4F6"]);
        expect(c[0]).toMatchObject({ insignia: "Parece MAC", elegible: true, uso: { mac: "A0:B1:C2:D3:E4:F6" } });
        expect(c[1]).toMatchObject({ tipo: "qr", tipoTexto: "QR", insignia: "QR con serie y MAC", uso: { serie: "ZTEGC8F1A2B3", mac: "A0:B1:C2:D3:E4:F6" } });
        for (const resto of c.slice(2)) {
            expect(resto).toMatchObject({ encaja: false, elegible: false, insignia: "No es una MAC", uso: {} });
        }
        expect(c.at(-1)?.clase).toBe("producto");
    });

    it("una sola serie entre otros códigos se confirma, con la pregunta y el aviso", () => {
        const c = candidatosPara("serie", [
            { valor: "ZTEGC8F1A2B3", formato: "code128" },
            { valor: "6972632140024", formato: "ean13" },
        ]);
        const unico = candidatoUnico(c);
        expect(unico?.valor).toBe("ZTEGC8F1A2B3");
        expect(unico && preguntaConfirmacion("serie", unico)).toBe("¿Usar ZTEGC8F1A2B3 como número de serie?");
        expect(textoElegido({ serie: "ZTEGC8F1A2B3" })).toBe("Serie: ZTEGC8F1A2B3");
        const qr = candidatosPara("serie", [{ valor: "SN:HWTC1234AB;MAC:a0b1c2d3e4f5", formato: "qr" }])[0];
        expect(qr && preguntaConfirmacion("serie", qr)).toBe("¿Usar la serie HWTC1234AB y la MAC A0:B1:C2:D3:E4:F5?");
        expect(textoElegido({ serie: "HWTC1234AB", mac: "A0:B1:C2:D3:E4:F5" })).toBe("Serie: HWTC1234AB · MAC: A0:B1:C2:D3:E4:F5");
    });

    it("deduplica por valor y marca lo que ya está en otro equipo de la orden", () => {
        const c = candidatosPara(
            "serie",
            [
                { valor: "ZTEGC8F1A2B3", formato: "code128" },
                { valor: " ZTEGC8F1A2B3 ", formato: "code128" },
                { valor: "SN:HWTC1234AB;MAC:a0b1c2d3e4f5", formato: "qr" },
            ],
            [{ serie: "ztegc8f1a2b3", mac: null }, { serie: "OTRA", mac: "a0-b1-c2-d3-e4-f5" }],
        );
        expect(c).toHaveLength(2);
        expect(c[0]?.repetidos).toEqual(["serie"]);
        expect(c[1]?.repetidos).toEqual(["mac"]);
        expect(textoRepetido(["serie"])).toBe("Esta serie ya está en otro equipo de esta orden");
        expect(textoRepetido(["serie", "mac"])).toBe("Esta serie y esta MAC ya están en otro equipo de esta orden");
        expect(textoRepetido([])).toBeNull();
    });

    it("encabezado y ayuda según el objetivo", () => {
        expect(textosEscaneo("serie")).toEqual({ titulo: "Número de serie", ayuda: "Apunta solo al código junto a S/N", botonCampo: "Escanear número de serie" });
        expect(textosEscaneo("mac")).toEqual({ titulo: "MAC", ayuda: "Apunta solo al código junto a MAC", botonCampo: "Escanear MAC" });
    });
});

describe("recuadro", () => {
    const recuadro = { x: 100, y: 100, ancho: 200, alto: 100 };

    it("cuentan solo los códigos con su centro dentro; sin posición cuentan todos", () => {
        const codigos: CodigoVisto[] = [
            { valor: "DENTRO", formato: "code128", caja: { x: 120, y: 120, ancho: 100, alto: 40 } },
            { valor: "FUERA", formato: "code128", caja: { x: 10, y: 300, ancho: 100, alto: 40 } },
            { valor: "BORDE", formato: "code128", caja: { x: 250, y: 80, ancho: 120, alto: 30 } }, // centro (310, 95): fuera
            { valor: "ESQUINAS", formato: "qr", esquinas: [{ x: 150, y: 110 }, { x: 190, y: 110 }, { x: 190, y: 150 }, { x: 150, y: 150 }] },
            { valor: "SIN_POSICION", formato: "code128" },
            { valor: "CAJA_VACIA", formato: "code128", caja: { x: 0, y: 0, ancho: 0, alto: 0 }, esquinas: [] },
        ];
        expect(dentroDelRecuadro(codigos, recuadro).map((c) => c.valor)).toEqual(["DENTRO", "ESQUINAS", "SIN_POSICION", "CAJA_VACIA"]);
    });

    it("recuadro centrado del visor en un teléfono de 390 px", () => {
        expect(recuadroCentrado({ ancho: 390, alto: 700 })).toEqual({ x: 39, y: 256.4, ancho: 312, alto: 187.2 });
        expect(recuadroCentrado({ ancho: 1280, alto: 800 }).ancho).toBe(340);
    });

    it("pasa el recuadro de la vista (object-fit: cover) al cuadro de la cámara", () => {
        // Cámara 1280×720 en una vista de 390×520: escala 0,7222 y se recorta a los lados.
        const r = recuadroEnFuente({ x: 39, y: 166, ancho: 312, alto: 188 }, { ancho: 1280, alto: 720 }, { ancho: 390, alto: 520 });
        expect(r.ancho).toBeCloseTo(432, 0);
        expect(r.alto).toBeCloseTo(260.3, 0);
        expect(r.x + r.ancho / 2).toBeCloseTo(640, 0);
        expect(r.y + r.alto / 2).toBeCloseTo(360, 0);
        // Sin medidas del video: se deja como está.
        expect(recuadroEnFuente(recuadro, { ancho: 0, alto: 0 }, { ancho: 390, alto: 520 })).toEqual(recuadro);
    });
});

describe("ventana de escaneo", () => {
    it("junta los códigos distintos desde el primero y espera sin ninguno", () => {
        let c = juntarCodigos(COLECCION_VACIA, [], 0);
        expect(c.desde).toBeNull();
        expect(coleccionLista(c, 5000)).toBe(false);
        c = juntarCodigos(c, [{ valor: "ZTEGC8F1A2B3", formato: "code128" }], 1000);
        expect(c.desde).toBe(1000);
        // Un solo cuadro no basta: la lista sale al terminar la ventana.
        expect(coleccionLista(c, 1000)).toBe(false);
        c = juntarCodigos(c, [{ valor: "ZTEGC8F1A2B3", formato: "code128" }, { valor: "A0B1C2D3E4F6", formato: "code128" }], 1400);
        expect(c.codigos.map((x) => x.valor)).toEqual(["ZTEGC8F1A2B3", "A0B1C2D3E4F6"]);
        expect(c.desde).toBe(1000);
        expect(coleccionLista(c, 1799)).toBe(false);
        expect(coleccionLista(c, 1800)).toBe(true);
        // Mismo código otra vez: no cambia nada.
        expect(juntarCodigos(c, [{ valor: "A0B1C2D3E4F6", formato: "code128" }], 1900)).toBe(c);
    });
});

describe("SesionEscaneo", () => {
    function sesion(objetivo: "serie" | "mac", existentes: { serie?: string; mac?: string }[] = []) {
        let pendiente: (() => void) | null = null;
        const avisos: string[] = [];
        const estados: EstadoEscaneo[] = [];
        const s = new SesionEscaneo({
            objetivo,
            existentes: () => existentes,
            alCambiar: (e) => estados.push(e),
            alPrimerCodigo: () => avisos.push("vibrar"),
            ahora: () => 0,
            programar: (fn) => {
                pendiente = fn;
                return () => {
                    pendiente = null;
                };
            },
        });
        return {
            s,
            avisos,
            estados,
            terminarVentana() {
                const fn = pendiente;
                pendiente = null;
                fn?.();
            },
            hayPendiente: () => pendiente !== null,
        };
    }

    it("no acepta con un cuadro: junta durante la ventana y luego muestra la lista (sin elegir)", () => {
        const t = sesion("serie");
        t.s.detectar([]);
        expect(t.hayPendiente()).toBe(false);
        t.s.detectar([{ valor: "6901443254538", formato: "ean13" }]);
        expect(t.avisos).toEqual(["vibrar"]);
        expect(t.s.estado).toEqual({ detectados: 1, candidatos: null });
        t.s.detectar(HUAWEI);
        expect(t.s.estado.detectados).toBe(5);
        t.terminarVentana();
        expect(t.s.estado.candidatos?.[0]?.valor).toBe("2102311TUN10K5000123");
        expect(t.s.estado.candidatos?.at(-1)?.valor).toBe("6901443254538");
        // Con la lista abierta no se junta más.
        t.s.detectar([{ valor: "OTRO12345678", formato: "code128" }]);
        expect(t.s.estado.detectados).toBe(5);
        expect(t.avisos).toHaveLength(1);
    });

    it("«Seguir escaneando» empieza otra ventana; liberar cancela la lista programada", () => {
        const t = sesion("mac", [{ mac: "A0:B1:C2:D3:E4:F6" }]);
        t.s.detectar([{ valor: "A0B1C2D3E4F6", formato: "code128" }]);
        t.terminarVentana();
        expect(t.s.estado.candidatos?.[0]?.repetidos).toEqual(["mac"]);
        t.s.seguir();
        expect(t.s.estado).toEqual({ detectados: 0, candidatos: null });
        t.s.detectar([{ valor: "SN:ZTEGC8F1A2B3;MAC:A0B1C2D3E4F7", formato: "qr" }]);
        expect(t.avisos).toHaveLength(2);
        t.s.liberar();
        expect(t.hayPendiente()).toBe(false);
        expect(t.estados.at(-1)).toEqual({ detectados: 1, candidatos: null });
    });
});
