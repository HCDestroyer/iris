// Utilidades comunes de los escenarios k6 de carga (docs/runbooks/carga.md).

import http from 'k6/http';
import { randomBytes } from 'k6/crypto';

export const API = (__ENV.CARGA_API || 'http://127.0.0.1:3505').replace(/\/$/, '');

export function entero(nombre, defecto) {
    const v = __ENV[nombre];
    return v === undefined || v === '' ? defecto : Number(v);
}

// UUID v7 (ms de Unix + azar), como los genera el teléfono (packages/sync).
export function uuidv7() {
    const b = new Uint8Array(randomBytes(16));
    let t = Date.now();
    for (let i = 5; i >= 0; i--) {
        b[i] = t % 256;
        t = Math.floor(t / 256);
    }
    b[6] = (b[6] & 0x0f) | 0x70;
    b[8] = (b[8] & 0x3f) | 0x80;
    const h = Array.from(b, (x) => (x < 16 ? '0' : '') + x.toString(16)).join('');
    return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

export const azar = (min, max) => min + Math.random() * (max - min);
export const elegir = (lista) => lista[Math.floor(Math.random() * lista.length)];

// Inicia sesión como el panel y la PWA de campo (cookie HttpOnly + X-CSRF-Token): el rol técnico
// no crea tokens de API, y así se mide el mismo camino que usan los usuarios.
export function iniciarSesiones(correos, contrasena) {
    const sesiones = {};
    for (let i = 0; i < correos.length; i += 10) {
        const lote = correos.slice(i, i + 10);
        const respuestas = http.batch(lote.map((correo) => ({
            method: 'POST', url: `${API}/api/v1/sesion`,
            body: JSON.stringify({ correo, contrasena }),
            params: { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'sesion', tipo: 'setup' }, responseType: 'text' },
        })));
        respuestas.forEach((r, k) => {
            if (r.status !== 200) {
                throw new Error(`login de ${lote[k]} → ${r.status} ${r.body}`);
            }
            const sesion = r.cookies.iris_sesion[0].value;
            const csrf = r.json('csrfToken');
            sesiones[lote[k]] = { cookie: `iris_sesion=${sesion}; iris_csrf=${csrf}`, csrf };
        });
    }
    return sesiones;
}

export function cabeceras(sesion, empresa, extra) {
    return Object.assign({
        'Content-Type': 'application/json',
        Accept: 'application/json',
        Cookie: sesion.cookie,
        'X-CSRF-Token': sesion.csrf,
        'X-Iris-Empresa': String(empresa),
    }, extra || {});
}

// Hora de Guatemala (UTC−6 todo el año) como AAAA-MM-DD.
export function hoyGuatemala() {
    return new Date(Date.now() - 6 * 3600 * 1000).toISOString().slice(0, 10);
}
