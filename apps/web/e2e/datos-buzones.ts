import type { Schema } from "@iris/contratos";

/* Fixtures de buzones para la API falsa, tipados con packages/contratos (sin contraseñas: la API nunca las devuelve). */

type Buzon = Schema<"Buzon">;

export function buzonesIniciales(empresaId: number | null): Buzon[] {
    return [
        {
            id: 1,
            empresaId,
            nombre: "Soporte",
            correo: "soporte@isp.gt",
            imap: { host: "imap.isp.gt", puerto: 993, tls: "tls", usuario: "soporte@isp.gt", carpeta: "INBOX", carpetaProcesados: "Procesados" },
            smtp: { host: "smtp.isp.gt", puerto: 587, tls: "starttls", usuario: "soporte@isp.gt" },
            categoriaId: 2,
            prioridad: "media",
            authservId: "mx.isp.gt",
            activo: true,
            ultimaLecturaEn: "2026-10-28T14:58:00Z",
            ultimoError: null,
            version: 3,
            creadoEn: "2026-10-01T15:00:00Z",
            actualizadoEn: "2026-10-27T15:00:00Z",
        },
        {
            id: 2,
            empresaId,
            nombre: "Facturación",
            correo: "facturas@isp.gt",
            // Un host con «caido» hace fallar la prueba de conexión en la API falsa.
            imap: { host: "imap-caido.isp.gt", puerto: 143, tls: "starttls", usuario: "facturas", carpeta: "INBOX", carpetaProcesados: null },
            categoriaId: null,
            prioridad: "baja",
            authservId: null,
            activo: false,
            ultimaLecturaEn: null,
            ultimoError: "No se pudo iniciar sesión en el servidor IMAP",
            version: 1,
            creadoEn: "2026-10-20T15:00:00Z",
            actualizadoEn: "2026-10-20T15:00:00Z",
        },
    ];
}

/** Resultado de `probar` como lo da la API: códigos y detalles resumidos, nunca la respuesta cruda. */
export function resultadoPrueba(b: Buzon): Schema<"ResultadoPrueba"> {
    const caido = b.imap.host.includes("caido");
    return {
        imap: caido ? { ok: false, codigo: "autenticacion", detalle: null } : { ok: true, codigo: null, detalle: null },
        ...(b.smtp ? { smtp: { ok: true, codigo: null, detalle: null } } : {}),
    };
}
