#!/usr/bin/env node
// Siembra de la prueba de carga, parte API (docs/runbooks/carga.md). Node 22, sin dependencias.
//
// Crea o completa, por los mismos endpoints que usa el panel (casos de uso auditados):
//   - la empresa de carga (id de Argos CARGA_EMPRESA, 9001 por defecto);
//   - 5 zonas y 10 cuadrillas;
//   - 50 técnicos (usuario con rol tecnico + ficha de agenda con turnos de todo el día) y 8 órdenes
//     programadas hoy (hora de Guatemala) para cada uno: ~400 órdenes del día;
//   - 14 usuarios de soporte y 6 de despacho para el escenario del panel.
//
// Es repetible: busca por correo o nombre antes de crear, solo completa las órdenes «programada» de
// hoy que falten (las que la corrida anterior dejó fallidas no cuentan) y vuelve a fijar la
// contraseña de los usuarios de carga. La contraseña llega por CARGA_CONTRASENA (carga.sh la
// genera al azar en cada corrida); nada de credenciales en el repo. Escribe en CARGA_SALIDA
// (infra/carga/.salida, ignorada por git) usuarios.json con correos e ids, que leen k6 y la siembra
// SQL.
//
// Entrada: CARGA_API (http://127.0.0.1:3505), CARGA_ADMIN_CORREO, CARGA_ADMIN_CONTRASENA,
// CARGA_CONTRASENA, CARGA_TECNICOS (50), CARGA_ORDENES_POR_TECNICO (8), CARGA_SOPORTE (14),
// CARGA_DESPACHO (6), CARGA_EMPRESA (9001), CARGA_SALIDA.

import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const env = (n, d) => process.env[n] ?? d;
const API = env('CARGA_API', 'http://127.0.0.1:3505').replace(/\/$/, '');
const EMPRESA = Number(env('CARGA_EMPRESA', '9001'));
const TECNICOS = Number(env('CARGA_TECNICOS', '50'));
const ORDENES_POR_TECNICO = Number(env('CARGA_ORDENES_POR_TECNICO', '8'));
const SOPORTE = Number(env('CARGA_SOPORTE', '14'));
const DESPACHO = Number(env('CARGA_DESPACHO', '6'));
const SALIDA = env('CARGA_SALIDA', new URL('../.salida', import.meta.url).pathname);
const ZONAS = 5;
const CUADRILLAS = 10;
const ZONA_HORARIA = 'America/Guatemala';

function obligatoria(nombre) {
    const v = process.env[nombre];
    if (!v) {
        console.error(`siembra: falta ${nombre}`);
        process.exit(2);
    }
    return v;
}
const CONTRASENA = obligatoria('CARGA_CONTRASENA');
const ADMIN = { correo: obligatoria('CARGA_ADMIN_CORREO'), contrasena: obligatoria('CARGA_ADMIN_CONTRASENA') };

// --- HTTP con la sesión del administrador (cookie + X-CSRF-Token, como el panel) -------------

let cookie = '';
let csrf = '';

async function pedir(metodo, ruta, cuerpo, { esperado = [200, 201], empresa = true } = {}) {
    const cabeceras = { accept: 'application/json' };
    if (cuerpo !== undefined) cabeceras['content-type'] = 'application/json';
    if (cookie) cabeceras.cookie = cookie;
    if (csrf && metodo !== 'GET') cabeceras['x-csrf-token'] = csrf;
    if (empresa) cabeceras['x-iris-empresa'] = String(EMPRESA);
    for (let intento = 1; ; intento++) {
        const r = await fetch(API + ruta, { method: metodo, headers: cabeceras, body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) });
        const texto = await r.text();
        const json = texto ? JSON.parse(texto) : null;
        if (esperado.includes(r.status)) return { status: r.status, json, r };
        // 429/503 transitorios: reintenta con espera.
        if ((r.status === 429 || r.status >= 500) && intento < 5) {
            await new Promise((ok) => setTimeout(ok, 500 * intento));
            continue;
        }
        throw new Error(`${metodo} ${ruta} → ${r.status}: ${texto.slice(0, 400)}`);
    }
}

async function iniciarSesion() {
    const r = await fetch(API + '/api/v1/sesion', {
        method: 'POST', headers: { 'content-type': 'application/json' },
        body: JSON.stringify(ADMIN),
    });
    if (r.status !== 200) throw new Error(`login del administrador → ${r.status}: ${await r.text()}`);
    const galletas = r.headers.getSetCookie().map((c) => c.split(';')[0]);
    cookie = galletas.join('; ');
    csrf = (await r.json()).csrfToken;
}

async function todas(ruta) {
    const salida = [];
    let cursor = '';
    for (;;) {
        const sep = ruta.includes('?') ? '&' : '?';
        const { json } = await pedir('GET', `${ruta}${sep}limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`);
        salida.push(...json.data);
        if (!json.nextCursor) return salida;
        cursor = json.nextCursor;
    }
}

// Corre fn sobre items con a lo más n en paralelo.
async function enParalelo(items, n, fn) {
    const resultados = new Array(items.length);
    let i = 0;
    await Promise.all(Array.from({ length: Math.min(n, items.length) }, async () => {
        while (i < items.length) {
            const k = i++;
            resultados[k] = await fn(items[k], k);
        }
    }));
    return resultados;
}

// --- Fechas en Guatemala ------------------------------------------------------------------------

function hoyGuatemala() {
    // en-CA da AAAA-MM-DD.
    return new Intl.DateTimeFormat('en-CA', { timeZone: ZONA_HORARIA, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
}
// Guatemala no tiene horario de verano: UTC−6 todo el año.
const enGuatemala = (fecha, hh, mm) => new Date(`${fecha}T${String(hh).padStart(2, '0')}:${String(mm).padStart(2, '0')}:00-06:00`);

// --- Siembra ------------------------------------------------------------------------------------

async function asegurarEmpresa() {
    const { json } = await pedir('GET', '/api/v1/empresas?limit=100', undefined, { empresa: false });
    if (json.data.some((e) => e.id === EMPRESA)) return;
    await pedir('POST', '/api/v1/empresas', { id: EMPRESA, nombre: 'ISP de carga', nit: 'CF' }, { empresa: false });
}

async function asegurarZonas() {
    const { json } = await pedir('GET', '/api/v1/zonas?todas=true');
    const porNombre = new Map(json.data.map((z) => [z.nombre, z]));
    const zonas = [];
    for (let i = 1; i <= ZONAS; i++) {
        const nombre = `Carga zona ${i}`;
        zonas.push(porNombre.get(nombre) ?? (await pedir('POST', '/api/v1/zonas', { nombre })).json);
    }
    return zonas;
}

async function asegurarCuadrillas(zonas) {
    const { json } = await pedir('GET', '/api/v1/cuadrillas?todas=true');
    const porNombre = new Map(json.data.map((c) => [c.nombre, c]));
    const cuadrillas = [];
    for (let i = 1; i <= CUADRILLAS; i++) {
        const nombre = `Carga cuadrilla ${i}`;
        const zona = zonas[(i - 1) % zonas.length];
        cuadrillas.push(porNombre.get(nombre) ?? (await pedir('POST', '/api/v1/cuadrillas', { nombre, zonaIds: [zona.id] })).json);
    }
    return cuadrillas;
}

// Crea el usuario o, si ya existe, le vuelve a fijar la contraseña de esta corrida.
async function asegurarUsuario(existentes, correo, nombre, rol) {
    const u = existentes.get(correo);
    if (!u) {
        return (await pedir('POST', '/api/v1/usuarios', {
            nombre, correo, contrasena: CONTRASENA, tipo: 'persona', empresas: [EMPRESA], roles: [{ rol }],
        })).json;
    }
    return (await pedir('POST', `/api/v1/usuarios/${u.id}/editar`, { version: u.version, contrasena: CONTRASENA })).json;
}

const TURNOS = [1, 2, 3, 4, 5, 6, 7].map((dia) => ({ dia, inicio: '00:00', fin: '23:59' }));
const TIPOS = ['visita', 'mantenimiento', 'visita', 'cambio_equipo', 'visita', 'instalacion', 'mantenimiento', 'retiro'];

async function asegurarTecnico(usuario, cuadrilla, i) {
    const { json } = await pedir('GET', `/api/v1/tecnicos?usuarioId=${usuario.id}&activo=`);
    let ficha = json.data[0];
    if (!ficha) {
        ficha = (await pedir('POST', '/api/v1/tecnicos', {
            usuarioId: usuario.id, cuadrillaId: cuadrilla.id, habilidades: ['fibra', 'inalambrico'],
            baseLat: 14.55 + (i % 10) / 100, baseLon: -90.6 + Math.floor(i / 10) / 50,
        })).json;
    }
    if (!ficha.turnos || ficha.turnos.length < 7) {
        ficha = (await pedir('POST', `/api/v1/tecnicos/${ficha.id}/turnos`, { version: ficha.version, turnos: TURNOS })).json;
    }
    return ficha;
}

// Completa hasta ORDENES_POR_TECNICO órdenes «programada» hoy para el técnico, cada una en su
// hueco de 30 min (cita de 25 min) desde las 06:00 y dando la vuelta al día (los turnos de carga
// cubren las 24 h): 48 huecos, unas 6 corridas completas por día. Las que ya existen hoy (en
// cualquier estado) ocupan su hueco. Si el día se llena, avisa: make carga-limpiar.
async function asegurarOrdenes(usuario, cuadrilla, zona, fecha, i) {
    const desde = enGuatemala(fecha, 0, 0).toISOString();
    const hasta = enGuatemala(fecha, 23, 59).toISOString();
    const deHoy = await todas(`/api/v1/ordenes?tecnicoId=${usuario.id}&desde=${encodeURIComponent(desde)}&hasta=${encodeURIComponent(hasta)}`);
    const programadas = deHoy.filter((o) => o.estado === 'programada').length;
    const ocupados = deHoy.filter((o) => o.programadaInicio && o.programadaFin)
        .map((o) => [new Date(o.programadaInicio).getTime(), new Date(o.programadaFin).getTime()]);
    const libre = (a, b) => ocupados.every(([x, y]) => b <= x || a >= y);
    let creadas = 0;
    for (let hueco = 0; programadas + creadas < ORDENES_POR_TECNICO && hueco < 47; hueco++) {
        const minutos = (6 * 60 + hueco * 30) % (24 * 60) + (i % 5);
        const inicio = enGuatemala(fecha, Math.floor(minutos / 60), minutos % 60);
        if (!libre(inicio.getTime(), inicio.getTime() + 25 * 60 * 1000)) continue;
        const k = programadas + creadas;
        const orden = (await pedir('POST', '/api/v1/ordenes', {
            tipo: TIPOS[(i + k) % TIPOS.length], zonaId: zona.id, cuadrillaId: cuadrilla.id,
            direccion: `Zona ${1 + ((i + k) % 21)}, ${1 + (k % 12)}a avenida ${10 + i}-${20 + k}`,
            lat: 14.55 + ((i * 7 + k) % 40) / 400, lon: -90.6 + ((i * 3 + k) % 40) / 400,
            contacto: { nombre: `Cliente carga ${i + 1}-${k + 1}`, telefono: `5${String(1000000 + i * 100 + k).slice(-7)}` },
            descripcion: 'Orden de la prueba de carga (infra/carga)',
        })).json;
        const fin = new Date(inicio.getTime() + 25 * 60 * 1000);
        await pedir('POST', `/api/v1/ordenes/${orden.id}/programar`, {
            version: orden.version, tecnicoId: usuario.id, inicio: inicio.toISOString(), fin: fin.toISOString(),
        });
        creadas++;
    }
    if (programadas + creadas < ORDENES_POR_TECNICO) {
        console.warn(`siembra api: ${usuario.correo} tiene solo ${programadas + creadas} órdenes programadas hoy (día lleno; make carga-limpiar)`);
    }
    return { programadas: programadas + creadas, creadas };
}

async function main() {
    const t0 = Date.now();
    await iniciarSesion();
    await asegurarEmpresa();
    const zonas = await asegurarZonas();
    const cuadrillas = await asegurarCuadrillas(zonas);
    const existentes = new Map((await todas('/api/v1/usuarios?q=carga.local')).map((u) => [u.correo, u]));
    const fecha = hoyGuatemala();

    const tecnicos = await enParalelo([...Array(TECNICOS).keys()], 8, async (i) => {
        const n = String(i + 1).padStart(3, '0');
        const cuadrilla = cuadrillas[i % cuadrillas.length];
        const zona = zonas.find((z) => z.id === cuadrilla.zonaIds[0]) ?? zonas[0];
        const usuario = await asegurarUsuario(existentes, `tecnico${n}@carga.local`, `Técnico carga ${n}`, 'tecnico');
        await asegurarTecnico(usuario, cuadrilla, i);
        const ordenes = await asegurarOrdenes(usuario, cuadrilla, zona, fecha, i);
        return { id: usuario.id, correo: usuario.correo, cuadrillaId: cuadrilla.id, zonaId: zona.id, ...ordenes };
    });
    const panel = await enParalelo([...Array(SOPORTE + DESPACHO).keys()], 8, async (i) => {
        const rol = i < SOPORTE ? 'soporte' : 'despacho';
        const n = String(i < SOPORTE ? i + 1 : i - SOPORTE + 1).padStart(3, '0');
        const u = await asegurarUsuario(existentes, `${rol}${n}@carga.local`, `${rol === 'soporte' ? 'Soporte' : 'Despacho'} carga ${n}`, rol);
        return { id: u.id, correo: u.correo, rol };
    });

    mkdirSync(SALIDA, { recursive: true });
    const salida = {
        empresa: EMPRESA, fecha, zonas: zonas.map((z) => z.id), cuadrillas: cuadrillas.map((c) => c.id),
        tecnicos: tecnicos.map(({ id, correo, cuadrillaId, zonaId }) => ({ id, correo, cuadrillaId, zonaId })),
        soporte: panel.filter((u) => u.rol === 'soporte').map(({ id, correo }) => ({ id, correo })),
        despacho: panel.filter((u) => u.rol === 'despacho').map(({ id, correo }) => ({ id, correo })),
    };
    writeFileSync(join(SALIDA, 'usuarios.json'), JSON.stringify(salida, null, 2));
    const creadas = tecnicos.reduce((s, t) => s + t.creadas, 0);
    const programadas = tecnicos.reduce((s, t) => s + t.programadas, 0);
    console.log(`siembra api: empresa ${EMPRESA}, ${zonas.length} zonas, ${cuadrillas.length} cuadrillas, ${tecnicos.length} técnicos, ` +
        `${salida.soporte.length} soporte, ${salida.despacho.length} despacho; órdenes programadas hoy (${fecha}): ${programadas} ` +
        `(${creadas} nuevas) en ${((Date.now() - t0) / 1000).toFixed(1)} s`);
}

main().catch((e) => {
    console.error('siembra api:', e.message);
    process.exit(1);
});
