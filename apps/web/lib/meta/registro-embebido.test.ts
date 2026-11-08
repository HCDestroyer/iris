import { afterEach, describe, expect, it, vi } from "vitest";
import { leerMensaje, origenDeFacebook, RegistroEmbebido, type ResultadoRegistro } from "./registro-embebido";

const FIN = { type: "WA_EMBEDDED_SIGNUP", event: "FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING", data: { waba_id: "980198427658004", phone_number_id: "106540352242922" } };

describe("origen de los mensajes", () => {
    it("solo facebook.com, www y web, por https", () => {
        for (const o of ["https://facebook.com", "https://www.facebook.com", "https://web.facebook.com"]) expect(origenDeFacebook(o)).toBe(true);
        for (const o of ["http://www.facebook.com", "https://www.facebook.com.evil.example", "https://evilfacebook.com", "https://facebook.com:8443", "null", "", "https://m.facebook.com.mx"]) expect(origenDeFacebook(o)).toBe(false);
    });
});

describe("mensaje del registro", () => {
    it("terminado con los ids (objeto o JSON)", () => {
        expect(leerMensaje(FIN)).toEqual({ tipo: "terminado", wabaId: "980198427658004", phoneNumberId: "106540352242922" });
        expect(leerMensaje(JSON.stringify({ ...FIN, event: "FINISH" }))).toEqual({ tipo: "terminado", wabaId: "980198427658004", phoneNumberId: "106540352242922" });
    });

    it("ids que no son dígitos: error, nunca se mandan", () => {
        expect(leerMensaje({ ...FIN, data: { waba_id: "98<script>", phone_number_id: "1" } })).toMatchObject({ tipo: "error" });
        expect(leerMensaje({ ...FIN, data: {} })).toMatchObject({ tipo: "error" });
    });

    it("cancelado y error de Meta", () => {
        expect(leerMensaje({ type: "WA_EMBEDDED_SIGNUP", event: "CANCEL", data: { current_step: "PHONE_NUMBER_SETUP" } })).toEqual({ tipo: "cancelado", paso: "PHONE_NUMBER_SETUP" });
        expect(leerMensaje({ type: "WA_EMBEDDED_SIGNUP", event: "ERROR", data: { error_message: "Algo falló", error_code: 524126 } })).toEqual({ tipo: "error", mensaje: "Algo falló", codigo: "524126" });
    });

    it("lo que no es del registro se ignora", () => {
        expect(leerMensaje({ type: "OTRA_COSA" })).toBeNull();
        expect(leerMensaje("no es json")).toBeNull();
        expect(leerMensaje(null)).toBeNull();
    });
});

describe("RegistroEmbebido", () => {
    afterEach(() => vi.useRealTimers());

    function nuevo() {
        const resultados: ResultadoRegistro[] = [];
        const r = new RegistroEmbebido((x) => resultados.push(x), 1_000);
        return { r, resultados };
    }

    it("junta el código y los ids en cualquier orden y avisa una sola vez", () => {
        const a = nuevo();
        a.r.alMensaje("https://www.facebook.com", FIN);
        expect(a.resultados).toEqual([]);
        a.r.alLogin({ authResponse: { code: "AQB-codigo" } });
        expect(a.resultados).toEqual([{ tipo: "listo", code: "AQB-codigo", wabaId: "980198427658004", phoneNumberId: "106540352242922" }]);
        a.r.alMensaje("https://www.facebook.com", FIN);
        a.r.alLogin({ authResponse: { code: "otro" } });
        expect(a.resultados).toHaveLength(1);

        const b = nuevo();
        b.r.alLogin({ authResponse: { code: "AQB-codigo" } });
        b.r.alMensaje("https://web.facebook.com", FIN);
        expect(b.resultados[0]).toMatchObject({ tipo: "listo", code: "AQB-codigo" });
    });

    it("ignora mensajes de otro origen aunque digan WA_EMBEDDED_SIGNUP", () => {
        const a = nuevo();
        expect(a.r.alMensaje("https://evil.example", FIN)).toBe(false);
        a.r.alLogin({ authResponse: { code: "AQB" } });
        expect(a.resultados).toEqual([]);
        a.r.alMensaje("https://www.facebook.com", FIN);
        expect(a.resultados[0]).toMatchObject({ tipo: "listo" });
    });

    it("sin código: cancelado (con el paso) o el error que dijo Meta", () => {
        const a = nuevo();
        a.r.alMensaje("https://www.facebook.com", { type: "WA_EMBEDDED_SIGNUP", event: "CANCEL", data: { current_step: "WABA_SETUP" } });
        a.r.alLogin({ authResponse: null, status: "unknown" });
        expect(a.resultados).toEqual([{ tipo: "cancelado", paso: "WABA_SETUP" }]);

        const b = nuevo();
        b.r.alMensaje("https://www.facebook.com", { type: "WA_EMBEDDED_SIGNUP", event: "ERROR", data: { error_message: "No", error_code: "1" } });
        b.r.alLogin(undefined);
        expect(b.resultados).toEqual([{ tipo: "error", motivo: "meta", mensaje: "No", codigo: "1" }]);

        const c = nuevo();
        c.r.alLogin({});
        expect(c.resultados).toEqual([{ tipo: "cancelado", paso: null }]);
    });

    it("con código pero sin ids a tiempo: error (el código ya no alcanzaría)", () => {
        vi.useFakeTimers();
        const a = nuevo();
        a.r.alLogin({ authResponse: { code: "AQB" } });
        vi.advanceTimersByTime(999);
        expect(a.resultados).toEqual([]);
        vi.advanceTimersByTime(1);
        expect(a.resultados[0]).toMatchObject({ tipo: "error", motivo: "sin_datos" });
        a.r.alMensaje("https://www.facebook.com", FIN);
        expect(a.resultados).toHaveLength(1);
    });

    it("abandonar: nada más se avisa", () => {
        const a = nuevo();
        a.r.abandonar();
        a.r.alMensaje("https://www.facebook.com", FIN);
        a.r.alLogin({ authResponse: { code: "AQB" } });
        expect(a.resultados).toEqual([]);
        expect(a.r.cerrado).toBe(true);
    });
});
