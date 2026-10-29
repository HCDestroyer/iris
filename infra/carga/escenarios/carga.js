// Prueba de carga de Iris (Fase 5): técnicos sincronizando y el panel con 10 000 tickets abiertos,
// mientras el worker corre el cron de SLA. Runbook: docs/runbooks/carga.md.
//
// Escenarios (en paralelo, CARGA_DURACION; 15m por defecto):
//   campo — CARGA_TECNICOS (50) técnicos virtuales, uno por VU. Cada ~30 s (CARGA_INTERVALO_SYNC)
//           mandan sus posiciones GPS (POST /campo/posiciones) y un lote (POST /sync/lote) con
//           lo que harían en la orden en curso: iniciar camino, llegar, marcar el checklist, notas,
//           pausar y reanudar y, al final, marcarla fallida (cliente ausente) para pasar a la
//           siguiente. Cada ~5 min (CARGA_INTERVALO_PAQUETE) piden cambios (GET /campo/cambios) y
//           cada 10 el paquete completo. Una fracción (CARGA_REENVIO, 10 %) reenvía el mismo lote
//           justo después, como un teléfono que no recibió la respuesta: todo debe volver
//           «duplicada».
//   panel — CARGA_PANEL (20) usuarios: 14 de soporte listan tickets con filtros y cursor, abren el
//           detalle (ticket, mensajes, historial) y responden (Idempotency-Key; a veces repetida);
//           6 de despacho listan tickets sin asignar, órdenes del día, técnicos y posiciones.
//
// Umbrales (fallan la prueba): p95 < 500 ms y errores < 1 % en sync/lote y en los listados;
// operaciones rechazadas < 1 %; ningún reenvío que no vuelva «duplicada»; checks > 99 %. La
// ausencia de duplicados en la base la comprueba infra/carga/verificar.sql al terminar.

import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate } from 'k6/metrics';
import { API, azar, cabeceras, elegir, entero, hoyGuatemala, iniciarSesiones, uuidv7 } from './lib.js';

const usuarios = JSON.parse(open(__ENV.CARGA_USUARIOS || '../.salida/usuarios.json'));
const CONTRASENA = __ENV.CARGA_CONTRASENA;
const DURACION = __ENV.CARGA_DURACION || '15m';
const TECNICOS = Math.min(entero('CARGA_TECNICOS', 50), usuarios.tecnicos.length);
const PANEL = Math.min(entero('CARGA_PANEL', 20), usuarios.soporte.length + usuarios.despacho.length);
const INTERVALO_SYNC = entero('CARGA_INTERVALO_SYNC', 30);
const INTERVALO_PAQUETE = entero('CARGA_INTERVALO_PAQUETE', 300);
const REENVIO = entero('CARGA_REENVIO', 0.1);
const EMPRESA = usuarios.empresa;

// Soporte y despacho del panel en la proporción 14:6 (con CARGA_PANEL menor, igual reparto).
const nSoporte = Math.min(usuarios.soporte.length, Math.max(1, Math.round(PANEL * 0.7)));
const panelUsuarios = usuarios.soporte.slice(0, nSoporte).map((u) => ({ ...u, rol: 'soporte' }))
    .concat(usuarios.despacho.slice(0, PANEL - nSoporte).map((u) => ({ ...u, rol: 'despacho' })));

const opsEnviadas = new Counter('ops_enviadas');
const opsRechazadas = new Rate('ops_rechazadas');
const reenvios = new Counter('lotes_reenviados');
const reenvioNoDuplicado = new Counter('reenvio_no_duplicado');
const respuestas = new Counter('tickets_respondidos');
const respuestaRepetidaDistinta = new Counter('respuesta_repetida_distinta');

// Métricas por endpoint en el resumen: k6 solo guarda las submétricas con umbral, así que los
// informativos llevan uno que siempre se cumple.
const informativo = ['max>=0'];
const informativoTasa = ['rate>=0'];
const ENDPOINTS = ['sync_lote', 'posiciones', 'campo_paquete', 'campo_cambios', 'tickets_listar', 'ticket_ver',
    'ticket_mensajes', 'ticket_eventos', 'ticket_responder', 'ordenes_listar', 'tecnicos_listar', 'posiciones_ultimas'];
const umbrales = {
    'http_req_duration{endpoint:sync_lote}': ['p(95)<500'],
    'http_req_failed{endpoint:sync_lote}': ['rate<0.01'],
    'http_req_duration{tipo:listado}': ['p(95)<500'],
    'http_req_failed{tipo:listado}': ['rate<0.01'],
    'http_req_duration{endpoint:tickets_listar}': ['p(95)<500'],
    'http_req_duration{endpoint:ordenes_listar}': ['p(95)<500'],
    ops_rechazadas: ['rate<0.01'],
    reenvio_no_duplicado: ['count<1'],
    respuesta_repetida_distinta: ['count<1'],
    checks: ['rate>0.99'],
};
for (const e of ENDPOINTS) {
    umbrales[`http_req_duration{endpoint:${e}}`] ??= informativo;
    umbrales[`http_req_failed{endpoint:${e}}`] ??= informativoTasa;
    umbrales[`http_reqs{endpoint:${e}}`] = ['count>=0'];
}

export const options = {
    setupTimeout: '3m',
    summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max', 'count'],
    thresholds: umbrales,
    scenarios: {
        campo: { executor: 'constant-vus', exec: 'campo', vus: TECNICOS, duration: DURACION, gracefulStop: '30s' },
        panel: { executor: 'constant-vus', exec: 'panel', vus: PANEL, duration: DURACION, gracefulStop: '30s' },
    },
};

export function setup() {
    if (!CONTRASENA) throw new Error('falta CARGA_CONTRASENA');
    const correos = usuarios.tecnicos.slice(0, TECNICOS).map((t) => t.correo).concat(panelUsuarios.map((u) => u.correo));
    return { sesiones: iniciarSesiones(correos, CONTRASENA), inicio: Date.now() };
}

// --- Campo --------------------------------------------------------------------------------------

// Estado del técnico (cada VU tiene el suyo).
let tec = null;

// La PWA de campo se identifica con X-Iris-Canal: campo (sin él, POST /campo/posiciones es 403).
const CAMPO = { 'X-Iris-Canal': 'campo' };

function pedirPaquete(sesion) {
    const r = http.get(`${API}/api/v1/campo/paquete?fecha=${hoyGuatemala()}`, {
        headers: cabeceras(sesion, EMPRESA, CAMPO), tags: { endpoint: 'campo_paquete', tipo: 'lectura' },
    });
    if (!check(r, { 'paquete 200': (x) => x.status === 200 })) return;
    const p = r.json();
    tec.cursor = p.cursor;
    // Las órdenes nuevas del paquete (programadas) entran a la cola; las conocidas se actualizan.
    for (const o of p.ordenes) {
        const conocida = tec.ordenes.find((x) => x.id === o.id);
        const items = o.checklist.filter((i) => ['si_no', 'texto', 'numero', 'medicion'].includes(i.tipo));
        if (conocida) {
            conocida.version = o.version;
        } else if (o.estado === 'programada') {
            tec.ordenes.push({ id: o.id, version: o.version, items, fase: 0, pausada: false });
        }
    }
}

function pedirCambios(sesion) {
    const r = http.get(`${API}/api/v1/campo/cambios?cursor=${encodeURIComponent(tec.cursor || '')}&limit=100`, {
        headers: cabeceras(sesion, EMPRESA, CAMPO), tags: { endpoint: 'campo_cambios', tipo: 'lectura' },
    });
    if (check(r, { 'cambios 200': (x) => x.status === 200 })) {
        tec.cursor = r.json('cursor') || tec.cursor;
    }
}

function valorDe(item, n) {
    switch (item.tipo) {
        case 'si_no': return n % 3 !== 0;
        case 'texto': return `Revisado en sitio (${n})`;
        default: return Math.round(azar(-25, 100) * 10) / 10;
    }
}

// Arma las operaciones del siguiente paso de la orden en curso (5 lotes por orden).
function siguientesOperaciones(o) {
    const ahora = new Date();
    const op = (tipo, datos) => ({
        id: uuidv7(), tipo, ordenId: o.id, versionBase: o.version,
        ocurridoEn: new Date(ahora.getTime() - Math.floor(azar(1, 25)) * 1000).toISOString(), datos,
    });
    const gps = { lat: 14.6 + azar(-0.05, 0.05), lon: -90.5 + azar(-0.05, 0.05), precision: 8 };
    const marcar = (k) => (o.items[k] ? [op('checklist.marcar', { itemId: o.items[k].itemId, valor: valorDe(o.items[k], k) })] : []);
    const ops = [];
    switch (o.fase) {
        case 0:
            ops.push(op('orden.iniciar_camino', gps));
            break;
        case 1:
            ops.push(op('orden.llegar', gps), ...marcar(0));
            break;
        case 2: {
            const nota = op('nota.agregar', {});
            // El id de la operación va en el texto: verificar.sql cuenta notas repetidas por texto.
            nota.datos.texto = `Cliente atendido; se revisa el equipo. [carga ${nota.id}]`;
            ops.push(...marcar(1), ...marcar(2), nota);
            break;
        }
        case 3:
            if (Math.random() < 0.3) {
                ops.push(op('orden.pausar', { motivo: 'clima' }));
                o.pausada = true;
            } else {
                ops.push(...marcar(3));
            }
            break;
        default:
            if (o.pausada) ops.push(op('orden.reanudar', {}));
            ops.push(...marcar(4), ...marcar(5));
            ops.push(op('orden.fallar', { motivoFalla: 'cliente_ausente', lat: gps.lat, lon: gps.lon }));
    }
    // Las operaciones del mismo lote llevan ocurridoEn creciente (el orden en que pasaron).
    ops.forEach((x, k) => { x.ocurridoEn = new Date(ahora.getTime() - (ops.length - k) * 1000).toISOString(); });
    return ops;
}

function enviarLote(sesion, cuerpo) {
    return http.post(`${API}/api/v1/sync/lote`, cuerpo, {
        headers: cabeceras(sesion, EMPRESA, CAMPO), tags: { endpoint: 'sync_lote', tipo: 'escritura' },
    });
}

// Usuario de cada VU. exec.vu.idInTest se numera entre todos los escenarios (campo y panel se
// intercalan), así que el índice sale de la primera iteración del VU dentro de su escenario: las
// primeras N iteraciones (una por VU, todas más largas que el arranque) reciben 0…N−1.
let indice = null;
const indiceDelVU = () => (indice === null ? (indice = exec.scenario.iterationInTest) : indice);

export function campo(datos) {
    const t = usuarios.tecnicos[indiceDelVU() % TECNICOS];
    const sesion = datos.sesiones[t.correo];
    if (tec === null) {
        tec = { dispositivo: `carga-${t.id}-${uuidv7().slice(-12)}`, ordenes: [], cursor: '', tick: 0 };
        sleep(azar(0, INTERVALO_SYNC)); // los teléfonos no sincronizan todos en el mismo segundo
        pedirPaquete(sesion);
    }
    const inicio = Date.now();
    const cadaPaquete = Math.max(1, Math.round(INTERVALO_PAQUETE / INTERVALO_SYNC));
    tec.tick++;
    if (tec.tick % cadaPaquete === 0) {
        if (tec.tick % (2 * cadaPaquete) === 0) pedirPaquete(sesion);
        else pedirCambios(sesion);
    }

    // Posiciones GPS: 3 puntos de los últimos 30 s (uno cada 10 s).
    const puntos = [20, 10, 0].map((s) => ({
        lat: 14.6 + azar(-0.05, 0.05), lon: -90.5 + azar(-0.05, 0.05), precision: azar(3, 20), velocidad: azar(0, 15),
        rumbo: azar(0, 359), registradaEn: new Date(inicio - s * 1000 - Math.floor(azar(0, 999))).toISOString(),
    }));
    const rp = http.post(`${API}/api/v1/campo/posiciones`, JSON.stringify({ dispositivoId: tec.dispositivo, enviadoEn: new Date().toISOString(), puntos }), {
        headers: cabeceras(sesion, EMPRESA, CAMPO), tags: { endpoint: 'posiciones', tipo: 'escritura' },
    });
    check(rp, { 'posiciones 2xx': (x) => x.status >= 200 && x.status < 300 });

    // Lote de la orden en curso (la primera que no terminó).
    const orden = tec.ordenes.find((o) => o.fase <= 4);
    if (orden) {
        const operaciones = siguientesOperaciones(orden);
        const cuerpo = JSON.stringify({ dispositivoId: tec.dispositivo, plataforma: 'k6', enviadoEn: new Date().toISOString(), cursor: tec.cursor, operaciones });
        const r = enviarLote(sesion, cuerpo);
        opsEnviadas.add(operaciones.length);
        if (check(r, { 'sync/lote 200': (x) => x.status === 200 })) {
            const resultados = r.json('resultados') || [];
            for (const res of resultados) {
                opsRechazadas.add(res.estado === 'rechazada', { tipo: (operaciones.find((o) => o.id === res.id) || {}).tipo });
                if (res.estado === 'rechazada') console.warn(`rechazada ${t.correo} orden ${orden.id}: ${JSON.stringify(res.error)}`);
                if (res.orden) orden.version = res.orden.version;
            }
            orden.fase++;
            // Reenvío del mismo lote (respuesta perdida): todo debe volver «duplicada».
            if (Math.random() < REENVIO) {
                reenvios.add(1);
                const r2 = enviarLote(sesion, cuerpo);
                const ok = r2.status === 200 && (r2.json('resultados') || []).every((x, k) =>
                    x.estado === 'duplicada' || (x.estado === 'rechazada' && resultados[k].estado === 'rechazada'));
                if (!check(r2, { 'reenvío todo duplicada': () => ok })) {
                    reenvioNoDuplicado.add(1);
                    console.warn(`reenvío no duplicado ${t.correo}: ${r2.status} ${r2.body}`);
                }
            }
        }
    }
    sleep(Math.max(0, INTERVALO_SYNC + azar(-3, 3) - (Date.now() - inicio) / 1000));
}

// --- Panel --------------------------------------------------------------------------------------

const ESTADOS_ABIERTOS = 'nuevo,abierto,en_espera';
const BUSQUEDAS = ['internet', 'lenta', 'Router', 'factura', 'TK-05', 'Cliente 12', 'antena'];

function listarTickets(sesion, yo, filtros) {
    let consulta = `estado=${filtros.estado || ESTADOS_ABIERTOS}&limit=50`;
    const r = Math.random();
    if (filtros.sinAsignar) consulta += '&sinAsignar=true';
    else if (r < 0.2) consulta += `&prioridad=${elegir(['critica', 'alta', 'alta,critica'])}`;
    else if (r < 0.35) consulta += `&categoriaId=${Math.ceil(azar(0, 9))}`;
    else if (r < 0.55) consulta += `&asignadoA=${yo}`;
    else if (r < 0.65) consulta += '&sinAsignar=true';
    else if (r < 0.75) consulta += `&q=${encodeURIComponent(elegir(BUSQUEDAS))}`;
    let tickets = [];
    let cursor = '';
    // Primera página y, a veces, una o dos más con el cursor.
    const paginas = 1 + (Math.random() < 0.4 ? Math.ceil(azar(0, 2)) : 0);
    for (let p = 0; p < paginas; p++) {
        const res = http.get(`${API}/api/v1/tickets?${consulta}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`, {
            headers: cabeceras(sesion, EMPRESA), tags: { endpoint: 'tickets_listar', tipo: 'listado' },
        });
        if (!check(res, { 'tickets 200': (x) => x.status === 200 })) break;
        const j = res.json();
        tickets = tickets.concat(j.data);
        if (!j.nextCursor) break;
        cursor = j.nextCursor;
        sleep(azar(0.5, 2));
    }
    return tickets;
}

function soporte(sesion, u) {
    const tickets = listarTickets(sesion, u.id, {});
    sleep(azar(1, 3));
    if (tickets.length === 0) return;
    const t = elegir(tickets);
    const h = { headers: cabeceras(sesion, EMPRESA) };
    const det = http.get(`${API}/api/v1/tickets/${t.id}`, { ...h, tags: { endpoint: 'ticket_ver', tipo: 'lectura' } });
    check(det, { 'ticket 200': (x) => x.status === 200 });
    const msj = http.get(`${API}/api/v1/tickets/${t.id}/mensajes?limit=50`, { ...h, tags: { endpoint: 'ticket_mensajes', tipo: 'lectura' } });
    check(msj, { 'mensajes 200': (x) => x.status === 200 });
    const evs = http.get(`${API}/api/v1/tickets/${t.id}/eventos?limit=50`, { ...h, tags: { endpoint: 'ticket_eventos', tipo: 'lectura' } });
    check(evs, { 'eventos 200': (x) => x.status === 200 });
    sleep(azar(2, 6)); // leer y escribir la respuesta
    if (Math.random() < 0.35) {
        const clave = uuidv7();
        const cuerpo = JSON.stringify({ cuerpo: `Hola, revisamos su caso y un técnico le contactará hoy. [carga ${clave}]` });
        const p = { headers: cabeceras(sesion, EMPRESA, { 'Idempotency-Key': clave }), tags: { endpoint: 'ticket_responder', tipo: 'escritura' } };
        const r = http.post(`${API}/api/v1/tickets/${t.id}/responder`, cuerpo, p);
        if (check(r, { 'responder 200': (x) => x.status === 200 })) {
            respuestas.add(1);
            // A veces el panel reintenta (doble clic, red): misma clave → mismo mensaje.
            if (Math.random() < 0.1) {
                const r2 = http.post(`${API}/api/v1/tickets/${t.id}/responder`, cuerpo, p);
                const igual = r2.status === 200 && r2.json('mensaje.id') === r.json('mensaje.id');
                if (!check(r2, { 'responder repetido = mismo mensaje': () => igual })) {
                    respuestaRepetidaDistinta.add(1);
                    console.warn(`responder repetido distinto: ${r2.status} ${r2.body}`);
                }
            }
        } else {
            console.warn(`responder ${t.id}: ${r.status} ${r.body}`);
        }
    }
}

function despacho(sesion) {
    const h = { headers: cabeceras(sesion, EMPRESA) };
    listarTickets(sesion, 0, { sinAsignar: true });
    sleep(azar(1, 3));
    const hoy = hoyGuatemala();
    const desde = new Date(`${hoy}T00:00:00-06:00`).toISOString();
    const hasta = new Date(`${hoy}T23:59:59-06:00`).toISOString();
    const filtro = elegir(['', '&estado=programada,en_camino,en_sitio,pausada', `&cuadrillaId=${elegir(usuarios.cuadrillas)}`, `&zonaId=${elegir(usuarios.zonas)}`]);
    const ord = http.get(`${API}/api/v1/ordenes?desde=${encodeURIComponent(desde)}&hasta=${encodeURIComponent(hasta)}&limit=50${filtro}`, {
        ...h, tags: { endpoint: 'ordenes_listar', tipo: 'listado' },
    });
    check(ord, { 'ordenes 200': (x) => x.status === 200 });
    sleep(azar(1, 3));
    const pos = http.get(`${API}/api/v1/campo/posiciones/ultimas?maxAntiguedadMin=30`, { ...h, tags: { endpoint: 'posiciones_ultimas', tipo: 'listado' } });
    check(pos, { 'posiciones 200': (x) => x.status === 200 });
    if (Math.random() < 0.3) {
        const tecs = http.get(`${API}/api/v1/tecnicos?cuadrillaId=${elegir(usuarios.cuadrillas)}&limit=50`, { ...h, tags: { endpoint: 'tecnicos_listar', tipo: 'listado' } });
        check(tecs, { 'tecnicos 200': (x) => x.status === 200 });
    }
}

export function panel(datos) {
    const u = panelUsuarios[indiceDelVU() % panelUsuarios.length];
    const sesion = datos.sesiones[u.correo];
    if (u.rol === 'soporte') soporte(sesion, u);
    else despacho(sesion);
    sleep(azar(2, 5));
}

// --- Resumen ------------------------------------------------------------------------------------

function fila(nombre, m, reqs, fallos, segundos) {
    if (!m) return null;
    const v = m.values;
    const f = (x) => (x === undefined ? '-' : x.toFixed(0)).padStart(7);
    const n = reqs ? reqs.values.count : v.count;
    return `${nombre.padEnd(20)} ${String(n).padStart(7)} ${(n / segundos).toFixed(2).padStart(7)} ${f(v.med)} ${f(v['p(95)'])} ${f(v['p(99)'])} ${f(v.max)} ${((fallos ? fallos.values.rate : 0) * 100).toFixed(2).padStart(6)}%`;
}

export function handleSummary(data) {
    const segundos = data.state.testRunDurationMs / 1000;
    const m = data.metrics;
    const lineas = [
        `Iris · prueba de carga (${DURACION}, ${TECNICOS} técnicos, ${PANEL} usuarios del panel) — ${new Date().toISOString()}`,
        '',
        `${'endpoint'.padEnd(20)} ${'reqs'.padStart(7)} ${'req/s'.padStart(7)} ${'p50'.padStart(7)} ${'p95'.padStart(7)} ${'p99'.padStart(7)} ${'max'.padStart(7)} ${'error'.padStart(7)}`,
    ];
    for (const e of ENDPOINTS) {
        const l = fila(e, m[`http_req_duration{endpoint:${e}}`], m[`http_reqs{endpoint:${e}}`], m[`http_req_failed{endpoint:${e}}`], segundos);
        if (l) lineas.push(l);
    }
    lineas.push(fila('(listados)', m['http_req_duration{tipo:listado}'], null, m['http_req_failed{tipo:listado}'], segundos));
    lineas.push(fila('(todas)', m.http_req_duration, m.http_reqs, m.http_req_failed, segundos));
    const val = (k, campo) => (m[k] ? m[k].values[campo] : 0);
    lineas.push('', `operaciones de sync enviadas: ${val('ops_enviadas', 'count')}, rechazadas: ${(val('ops_rechazadas', 'rate') * 100).toFixed(2)} %`,
        `lotes reenviados: ${val('lotes_reenviados', 'count')}, reenvíos que no volvieron «duplicada»: ${val('reenvio_no_duplicado', 'count')}`,
        `tickets respondidos: ${val('tickets_respondidos', 'count')}, respuestas repetidas con otro mensaje: ${val('respuesta_repetida_distinta', 'count')}`,
        `checks: ${(val('checks', 'rate') * 100).toFixed(2)} %`, '', 'Umbrales:');
    for (const [nombre, metrica] of Object.entries(m)) {
        for (const [expr, t] of Object.entries(metrica.thresholds || {})) {
            if (['max>=0', 'rate>=0', 'count>=0'].includes(expr)) continue;
            lineas.push(`  ${t.ok ? 'ok   ' : 'FALLA'} ${nombre}: ${expr}`);
        }
    }
    const texto = lineas.join('\n') + '\n';
    const dir = __ENV.CARGA_RESUMEN_DIR || '../.salida';
    // setup_data lleva las cookies de sesión de todos los usuarios: nunca va al resumen (artefacto).
    const publico = { ...data };
    delete publico.setup_data;
    return { stdout: texto, [`${dir}/resumen.txt`]: texto, [`${dir}/resumen.json`]: JSON.stringify(publico, null, 2) };
}
