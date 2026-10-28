// Componentes de la app de campo: cámara, firma y escáner.
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Camara } from "./componentes/camara";
import { abrirEscaner, Escaner, lectorNativo, type Lector } from "./componentes/escaner";
import { hayFirma, MAX_SVG_FIRMA, simplificar, trazosASvg, type Trazo } from "./componentes/firma";
import { LienzoFirma } from "./componentes/lienzo-firma";

describe("firma", () => {
    it("convierte trazos a un SVG con viewBox del lienzo y rutas M/L", () => {
        const trazos: Trazo[] = [
            [
                { x: 10, y: 10 },
                { x: 20.04, y: 15.06 },
                { x: 30, y: 30 },
            ],
            [{ x: 50, y: 50 }],
        ];
        const svg = trazosASvg(trazos, 300, 150);
        expect(svg).toMatch(/^<svg xmlns="http:\/\/www.w3.org\/2000\/svg" viewBox="0 0 300 150" width="300" height="150">/);
        expect(svg).toContain('d="M10 10L20 15.1L30 30M50 50L50.1 50"');
        expect(svg).toContain('stroke="#111111"');
        expect(svg.length).toBeGreaterThanOrEqual(10);
    });

    it("simplifica puntos repetidos y conserva el último", () => {
        const t: Trazo = [
            { x: 0, y: 0 },
            { x: 0.1, y: 0 },
            { x: 0.2, y: 0 },
            { x: 5, y: 0 },
            { x: 5.1, y: 0 },
        ];
        expect(simplificar(t, 1)).toEqual([
            { x: 0, y: 0 },
            { x: 5, y: 0 },
            { x: 5.1, y: 0 },
        ]);
    });

    it("nunca pasa del tope de la API (200 000 caracteres)", () => {
        const enorme: Trazo[] = Array.from({ length: 40 }, (_, j) => Array.from({ length: 2000 }, (_, i) => ({ x: (i * 0.7) % 390, y: (j * 5 + i * 0.37) % 200 })));
        const svg = trazosASvg(enorme, 390, 200);
        expect(svg.length).toBeLessThanOrEqual(MAX_SVG_FIRMA);
        expect(svg).toContain("<path");
    });

    it("hayFirma distingue un lienzo vacío", () => {
        expect(hayFirma([])).toBe(false);
        expect(hayFirma([[]])).toBe(false);
        expect(hayFirma([[{ x: 1, y: 1 }]])).toBe(true);
    });

    it("LienzoFirma: área accesible y botón para borrar", () => {
        const html = renderToStaticMarkup(<LienzoFirma alCambiar={() => undefined} />);
        expect(html).toContain('role="img"');
        expect(html).toContain('aria-label="Firma del cliente: vacía"');
        expect(html).toContain("touch-none");
        expect(html).toContain("Borrar firma");
    });
});

describe("Camara", () => {
    it("input de archivo con capture dentro de su etiqueta, oculto pero enfocable", () => {
        const html = renderToStaticMarkup(
            <Camara id="foto" alElegir={() => undefined} aria-label="Tomar foto de la fachada">
                Tomar foto
            </Camara>,
        );
        expect(html).toMatch(/<label for="foto"[^>]*data-slot="camara"/);
        expect(html).toMatch(/<input id="foto" type="file" accept="image\/\*" capture="environment"[^>]*class="sr-only"/);
        expect(html).toContain('aria-label="Tomar foto de la fachada"');
        expect(html).toContain("Tomar foto");
    });

    it("deshabilitada no se puede usar", () => {
        const html = renderToStaticMarkup(
            <Camara alElegir={() => undefined} disabled camara="user">
                Foto
            </Camara>,
        );
        expect(html).toContain('aria-disabled="true"');
        expect(html).toContain('capture="user"');
        expect(html).toMatch(/<input[^>]*disabled=""/);
    });
});

describe("Escaner", () => {
    it("muestra la vista de la cámara mientras abre y permite cerrar", () => {
        const html = renderToStaticMarkup(<Escaner alLeer={() => undefined} alCerrar={() => undefined} />);
        expect(html).toContain("<video");
        expect(html).toContain("Abriendo la cámara…");
        expect(html).toContain("Cerrar escáner");
    });

    it("sin BarcodeDetector no hay lector nativo (se usa ZXing)", () => {
        expect(lectorNativo()).toBeNull();
    });

    describe("abrirEscaner: la cámara nunca queda encendida", () => {
        function flujoFalso() {
            const pistas = [{ detenida: false, stop() { this.detenida = true; } }];
            return { flujo: { getTracks: () => pistas } as unknown as MediaStream, pistas };
        }
        const lector = (): Lector & { liberado: boolean } => {
            const l = { liberado: false, leer: () => Promise.resolve(null), liberar: () => { l.liberado = true; } };
            return l;
        };

        it("si se cierra mientras getUserMedia espera el permiso, apaga las pistas al resolver", async () => {
            const { flujo, pistas } = flujoFalso();
            let activo = true;
            let resolver: (f: MediaStream) => void = () => undefined;
            const pendiente = abrirEscaner(
                { obtenerFlujo: () => new Promise((r) => (resolver = r)), conectar: () => Promise.resolve(), crearLector: () => Promise.resolve(lector()) },
                () => activo,
            );
            activo = false; // desmontado
            resolver(flujo);
            expect(await pendiente).toBeNull();
            expect(pistas.every((p) => p.detenida)).toBe(true);
        });

        it("si se cierra durante play() o la carga del lector, apaga y libera", async () => {
            for (const etapa of ["conectar", "lector"] as const) {
                const { flujo, pistas } = flujoFalso();
                const l = lector();
                let activo = true;
                const r = await abrirEscaner(
                    {
                        obtenerFlujo: () => Promise.resolve(flujo),
                        conectar: () => {
                            if (etapa === "conectar") activo = false;
                            return Promise.resolve();
                        },
                        crearLector: () => {
                            if (etapa === "lector") activo = false;
                            return Promise.resolve(l);
                        },
                    },
                    () => activo,
                );
                expect(r).toBeNull();
                expect(pistas[0]?.detenida).toBe(true);
                expect(l.liberado).toBe(etapa === "lector");
            }
        });

        it("si play() falla, apaga las pistas y propaga el error", async () => {
            const { flujo, pistas } = flujoFalso();
            await expect(
                abrirEscaner({ obtenerFlujo: () => Promise.resolve(flujo), conectar: () => Promise.reject(new Error("AbortError")), crearLector: () => Promise.resolve(lector()) }, () => true),
            ).rejects.toThrow("AbortError");
            expect(pistas[0]?.detenida).toBe(true);
        });

        it("abierto y activo: devuelve flujo y lector sin apagar", async () => {
            const { flujo, pistas } = flujoFalso();
            const r = await abrirEscaner({ obtenerFlujo: () => Promise.resolve(flujo), conectar: () => Promise.resolve(), crearLector: () => Promise.resolve(lector()) }, () => true);
            expect(r?.flujo).toBe(flujo);
            expect(pistas[0]?.detenida).toBe(false);
        });
    });
});
