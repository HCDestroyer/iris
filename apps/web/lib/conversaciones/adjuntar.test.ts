import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { claseDeArchivo, MAX_ADJUNTO, motivoArchivoInvalido, nuevaSubida, offsetValido, PARTE, SubidaCortada, subirAdjunto, tipoAdjunto, tipoLegible } from "./adjuntar";

const archivo = (nombre: string, tipo: string, tamano: number) => new File([new Uint8Array(tamano)], nombre, { type: tipo });

describe("tipo del adjunto", () => {
    it("registra el tipo real del archivo: una imagen es foto; un PDF o un video, documento", () => {
        expect(tipoAdjunto("image/png")).toBe("foto");
        expect(tipoAdjunto("image/jpeg")).toBe("foto");
        expect(tipoAdjunto("application/pdf")).toBe("documento");
        expect(tipoAdjunto("video/mp4")).toBe("documento");
    });
});

describe("validación de adjuntos antes de subir", () => {
    it("solo fotos (JPG, PNG, WebP) y PDF: lo demás se dice por qué", () => {
        for (const tipo of ["image/jpeg", "image/png", "image/webp", "application/pdf"]) expect(motivoArchivoInvalido({ type: tipo, size: 10 })).toBeNull();
        expect(motivoArchivoInvalido({ type: "video/mp4", size: 10 })).toMatch(/videos no se pueden enviar/);
        expect(motivoArchivoInvalido({ type: "image/svg+xml", size: 10 })).toBe("Solo fotos (JPG, PNG, WebP) o PDF.");
        expect(motivoArchivoInvalido({ type: "text/html", size: 10 })).toBe("Solo fotos (JPG, PNG, WebP) o PDF.");
        expect(motivoArchivoInvalido({ type: "", size: 10 })).toBe("Solo fotos (JPG, PNG, WebP) o PDF.");
    });

    it("vacío y más de 16 MB", () => {
        expect(motivoArchivoInvalido({ type: "image/png", size: 0 })).toBe("El archivo está vacío.");
        expect(motivoArchivoInvalido({ type: "image/png", size: MAX_ADJUNTO })).toBeNull();
        expect(motivoArchivoInvalido({ type: "image/png", size: MAX_ADJUNTO + 1 })).toBe("El archivo pasa de 16 MB.");
    });

    it("solo los mapas de bits se pintan como imagen: un SVG o un HTML van como documento", () => {
        expect(claseDeArchivo("image/png")).toBe("imagen");
        expect(claseDeArchivo("image/webp")).toBe("imagen");
        expect(claseDeArchivo("image/svg+xml")).toBe("documento");
        expect(claseDeArchivo("text/html")).toBe("documento");
        expect(claseDeArchivo("application/pdf")).toBe("documento");
        expect(claseDeArchivo("video/mp4")).toBe("video");
    });

    it("tipo legible para la vista previa", () => {
        expect(tipoLegible({ type: "application/pdf", name: "a.pdf" })).toBe("PDF");
        expect(tipoLegible({ type: "image/jpeg", name: "a.jpg" })).toBe("JPG");
        expect(tipoLegible({ type: "video/quicktime", name: "a.mov" })).toBe("Video");
        expect(tipoLegible({ type: "", name: "datos.xlsx" })).toBe("XLSX");
        expect(tipoLegible({ type: "", name: "sin-extension" })).toBe("Archivo");
    });
});

describe("subida por partes", () => {
    type Llamada = { metodo: string; ruta: string; offset: string | null; bytes: number; cuerpo: unknown };

    // El cliente de la API se crea una vez y se queda con `fetch`: un solo doble para todo el archivo.
    let llamadas: Llamada[] = [];
    type Fallo = number | { estado: number; offset?: string } | null;
    let fallar: (n: number, l: Llamada) => Fallo = () => null;
    beforeAll(() => {
        vi.stubGlobal("fetch", async (entrada: Request | string | URL, inicio?: RequestInit) => {
            const peticion = entrada instanceof Request ? entrada : new Request(String(entrada), inicio);
            const datos = await peticion.arrayBuffer();
            const esAlta = peticion.method === "POST";
            const l: Llamada = {
                metodo: peticion.method,
                ruta: new URL(peticion.url, "http://x").pathname,
                offset: peticion.headers.get("upload-offset"),
                bytes: datos.byteLength,
                cuerpo: esAlta ? JSON.parse(new TextDecoder().decode(datos)) : null,
            };
            llamadas.push(l);
            const fallo = fallar(llamadas.length, l);
            const estado = typeof fallo === "number" ? fallo : (fallo?.estado ?? null);
            const offset = typeof fallo === "object" && fallo ? fallo.offset : undefined;
            // HEAD: por dónde va el servidor (sin cuerpo).
            if (peticion.method === "HEAD") return new Response(null, { status: estado ?? 200, headers: offset === undefined ? {} : { "Upload-Offset": offset } });
            if (estado) {
                return new Response(JSON.stringify({ type: "urn:iris:problema:x", title: "Falló", status: estado }), {
                    status: estado,
                    headers: { "Content-Type": "application/problem+json", ...(offset === undefined ? {} : { "Upload-Offset": offset }) },
                });
            }
            return esAlta ? new Response(JSON.stringify({ id: "x" }), { status: 201, headers: { "Content-Type": "application/json" } }) : new Response(null, { status: 204 });
        });
    });
    afterAll(() => vi.unstubAllGlobals());

    /** API falsa: anota las llamadas; `fallo` decide el estado de cada una (por orden). */
    function conApi(fallo: (n: number, l: Llamada) => Fallo = () => null) {
        llamadas = [];
        fallar = fallo;
        return llamadas;
    }

    it("registra el archivo y lo sube en partes, avisando del avance", async () => {
        const llamadas = conApi();
        const avances: number[] = [];
        const tamano = PARTE * 2 + 100;
        const id = await subirAdjunto(archivo("plano.png", "image/png", tamano), 41, { alAvanzar: (f) => avances.push(Math.round(f * 100)) });
        expect(llamadas.map((l) => [l.metodo, l.offset, l.bytes])).toEqual([
            ["POST", null, expect.any(Number)],
            ["PATCH", "0", PARTE],
            ["PATCH", String(PARTE), PARTE],
            ["PATCH", String(PARTE * 2), 100],
        ]);
        expect(llamadas[0]?.cuerpo).toMatchObject({ id, entidad: "conversacion", entidadId: "41", tipo: "foto", mime: "image/png", tamano });
        expect((llamadas[0]?.cuerpo as { sha256: string }).sha256).toMatch(/^[0-9a-f]{64}$/);
        expect(llamadas[1]?.ruta).toBe(`/api/v1/adjuntos/${id}/subida`);
        expect(avances.at(0)).toBe(0);
        expect(avances.at(-1)).toBe(100);
        expect([...avances].sort((a, b) => a - b)).toEqual(avances);
    });

    it("un PDF se registra como documento", async () => {
        const llamadas = conApi();
        await subirAdjunto(archivo("contrato.pdf", "application/pdf", 50), 7);
        expect(llamadas[0]?.cuerpo).toMatchObject({ tipo: "documento", mime: "application/pdf", entidadId: "7" });
    });

    it("si una parte falla, reintentar sigue donde quedó: ni se registra otra vez ni se repite lo subido", async () => {
        // Falla la segunda parte (la 3.ª llamada).
        const llamadas = conApi((n) => (n === 3 ? 500 : null));
        const avance = nuevaSubida();
        const a = archivo("plano.png", "image/png", PARTE * 2 + 100);
        await expect(subirAdjunto(a, 41, { avance })).rejects.toMatchObject({ estado: 500 });
        expect(avance).toMatchObject({ registrado: true, enviado: PARTE });
        const id = await subirAdjunto(a, 41, { avance });
        expect(id).toBe(avance.id);
        expect(llamadas.filter((l) => l.metodo === "POST")).toHaveLength(1);
        expect(llamadas.filter((l) => l.metodo === "PATCH").map((l) => l.offset)).toEqual(["0", String(PARTE), String(PARTE), String(PARTE * 2)]);
    });

    it("409 con `Upload-Offset`: sigue desde donde dice el servidor, sin registrar otro adjunto", async () => {
        // La 1.ª parte llegó pero su respuesta se perdió: el panel reintenta desde 0 y el servidor dice «voy por PARTE».
        const llamadas = conApi((n) => (n === 2 ? { estado: 409, offset: String(PARTE) } : null));
        const avance = nuevaSubida();
        const primero = avance.id;
        const id = await subirAdjunto(archivo("plano.png", "image/png", PARTE * 2 + 100), 41, { avance });
        expect(id).toBe(primero);
        expect(llamadas.filter((l) => l.metodo === "POST")).toHaveLength(1);
        expect(llamadas.filter((l) => l.metodo === "PATCH").map((l) => [l.offset, l.bytes])).toEqual([
            ["0", PARTE],
            [String(PARTE), PARTE],
            [String(PARTE * 2), 100],
        ]);
    });

    it("409 sin cabecera: pregunta con HEAD y sigue desde ahí; si ya estaba completo, termina", async () => {
        const llamadas = conApi((n, l) => (l.metodo === "HEAD" ? { estado: 200, offset: "300" } : n === 2 ? 409 : null));
        const avance = nuevaSubida();
        const id = await subirAdjunto(archivo("foto.png", "image/png", 300), 41, { avance });
        expect(id).toBe(avance.id);
        expect(llamadas.map((l) => l.metodo)).toEqual(["POST", "PATCH", "HEAD"]);
        expect(avance.enviado).toBe(300);
    });

    it("409 con un offset que no sirve (mayor que el archivo, negativo, texto) y sin HEAD: adjunto nuevo de cero, una sola vez", async () => {
        for (const offset of ["999999", "-1", "abc"]) {
            const llamadas = conApi((n, l) => (l.metodo === "HEAD" ? 404 : n === 2 ? { estado: 409, offset } : null));
            const avance = nuevaSubida();
            const primero = avance.id;
            const id = await subirAdjunto(archivo("foto.png", "image/png", 300), 41, { avance });
            expect(id).not.toBe(primero);
            expect(llamadas.map((l) => l.metodo)).toEqual(["POST", "PATCH", "HEAD", "POST", "PATCH"]);
            expect((llamadas[3]?.cuerpo as { id: string }).id).toBe(id);
        }
        // Si no hay forma de avanzar, se rinde (no entra en bucle).
        conApi((_n, l) => (l.metodo === "PATCH" ? 409 : l.metodo === "HEAD" ? 404 : null));
        await expect(subirAdjunto(archivo("foto.png", "image/png", 300), 41)).rejects.toMatchObject({ estado: 409 });
    });

    it("un servidor que siempre responde 409 con el mismo offset no deja la subida en bucle", async () => {
        const llamadas = conApi((_n, l) => (l.metodo === "PATCH" ? { estado: 409, offset: "0" } : l.metodo === "HEAD" ? 404 : null));
        await expect(subirAdjunto(archivo("foto.png", "image/png", 300), 41)).rejects.toMatchObject({ estado: 409 });
        expect(llamadas.length).toBeLessThan(12);
    });

    it("`offsetValido`: solo enteros entre 0 y el tamaño", () => {
        expect(offsetValido("0", 10)).toBe(0);
        expect(offsetValido("10", 10)).toBe(10);
        for (const malo of [null, "", "11", "-1", "1.5", "1e3", "abc", "0x10"]) expect(offsetValido(malo, 10)).toBeNull();
    });

    it("`seguir` en falso corta antes de mandar la siguiente parte", async () => {
        const llamadas = conApi();
        let partes = 0;
        await expect(subirAdjunto(archivo("plano.png", "image/png", PARTE * 3), 41, { seguir: () => partes++ < 1 })).rejects.toBeInstanceOf(SubidaCortada);
        expect(llamadas.filter((l) => l.metodo === "PATCH")).toHaveLength(1);
    });
});
