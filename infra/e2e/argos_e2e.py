#!/usr/bin/env python3
"""Prueba de punta a punta Iris ↔ ArgosISP (contrato .claude/integration/contratos/argosisp.md).

La llama infra/e2e/argos.sh con los dos sistemas ya levantados. Solo biblioteca estándar. Cada paso
imprime «OK» o «FALLA» con su evidencia (status, filas, eventos) y el resumen final dice qué pasó.
Variables (las pone argos.sh): E2E_ARGOS_URL, E2E_IRIS_URL, E2E_ARGOS_ADMIN, E2E_ARGOS_CLAVE,
E2E_IRIS_ADMIN, E2E_IRIS_CLAVE, E2E_IRIS_DB, E2E_ARGOS_DB, E2E_CORREOS (carpeta del sumidero SMTP),
E2E_PASOS (lista separada por comas; vacía = todos), E2E_ARGOS_PARAR / E2E_ARGOS_ARRANCAR
(comandos para el paso de Argos caído).
"""
import http.cookiejar
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

ARGOS = os.environ.get("E2E_ARGOS_URL", "http://127.0.0.1:3520").rstrip("/")
IRIS = os.environ.get("E2E_IRIS_URL", "http://127.0.0.1:3521").rstrip("/")
IRIS_DB = os.environ.get("E2E_IRIS_DB", "")
ARGOS_DB = os.environ.get("E2E_ARGOS_DB", "")
CORREOS = os.environ.get("E2E_CORREOS", "")

resultados = []  # (paso, ok, detalle)
estado = {}  # lo que un paso deja al siguiente


class Falla(Exception):
    pass


def registrar(paso, ok, detalle):
    resultados.append((paso, ok, detalle))
    print(f"[{'OK' if ok else 'FALLA'}] {paso}: {detalle}", flush=True)


def esperar_que(cond, segundos=60, cada=1.0, que=""):
    fin = time.time() + segundos
    ultimo = None
    while time.time() < fin:
        ultimo = cond()
        if ultimo:
            return ultimo
        time.sleep(cada)
    raise Falla(f"no ocurrió en {segundos}s: {que} (último: {ultimo!r})")


# HTTP ------------------------------------------------------------------------------------------


class Respuesta:
    def __init__(self, status, cuerpo, cabeceras):
        self.status = status
        self.crudo = cuerpo
        self.cabeceras = cabeceras
        try:
            self.json = json.loads(cuerpo) if cuerpo else None
        except ValueError:
            self.json = None

    def __repr__(self):
        return f"{self.status} {self.crudo[:300]!r}"


def pedir(opener, metodo, url, cuerpo=None, cabeceras=None):
    datos = None
    cab = dict(cabeceras or {})
    if cuerpo is not None:
        datos = json.dumps(cuerpo).encode()
        cab["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=datos, method=metodo, headers=cab)
    try:
        with opener.open(req, timeout=30) as r:
            return Respuesta(r.status, r.read().decode(errors="replace"), dict(r.headers))
    except urllib.error.HTTPError as e:
        return Respuesta(e.code, e.read().decode(errors="replace"), dict(e.headers))
    except (urllib.error.URLError, ConnectionError, TimeoutError) as e:
        return Respuesta(0, str(e), {})


class Argos:
    def __init__(self):
        self.op = urllib.request.build_opener()
        r = pedir(self.op, "POST", ARGOS + "/api/v1/auth/login",
                  {"email": os.environ["E2E_ARGOS_ADMIN"], "password": os.environ["E2E_ARGOS_CLAVE"]})
        if r.status != 200:
            raise Falla(f"login en Argos: {r}")
        self.token = r.json["token"]

    def __call__(self, metodo, ruta, cuerpo=None, clave=None):
        cab = {"Authorization": "Bearer " + self.token}
        if clave:
            cab["Idempotency-Key"] = clave
        return pedir(self.op, metodo, ARGOS + "/api/v1" + ruta, cuerpo, cab)


class Iris:
    def __init__(self):
        self.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        r = pedir(self.op, "POST", IRIS + "/api/v1/sesion",
                  {"correo": os.environ["E2E_IRIS_ADMIN"], "contrasena": os.environ["E2E_IRIS_CLAVE"]})
        if r.status != 200:
            raise Falla(f"login en Iris: {r}")
        self.csrf = r.json["csrfToken"]

    def __call__(self, metodo, ruta, cuerpo=None, clave=None, cabeceras=None):
        cab = {"X-CSRF-Token": self.csrf}
        if metodo != "GET":
            cab["Idempotency-Key"] = clave or str(uuid.uuid4())
        cab.update(cabeceras or {})
        return pedir(self.op, metodo, IRIS + "/api/v1" + ruta, cuerpo, cab)


def sql(db, consulta, *args):
    """Una consulta de solo lectura con psql; devuelve filas como listas de texto."""
    cmd = ["psql", db, "-X", "-At", "-F", "\t", "-v", "ON_ERROR_STOP=1"]
    for i, a in enumerate(args, 1):
        cmd += ["-v", f"p{i}={a}"]
    r = subprocess.run(cmd, input=consulta, capture_output=True, text=True, check=False)
    if r.returncode != 0:
        raise Falla(f"psql: {r.stderr.strip()}")
    return [linea.split("\t") for linea in r.stdout.splitlines() if linea]


def uno(db, consulta, *args):
    filas = sql(db, consulta, *args)
    return filas[0][0] if filas else None


def exigir(cond, mensaje):
    if not cond:
        raise Falla(mensaje)


# Datos ------------------------------------------------------------------------------------------

EMPRESA = 1  # la empresa principal que crea el seed de Argos
H = {"X-Iris-Empresa": str(EMPRESA)}
SVG_FIRMA = '<svg xmlns="http://www.w3.org/2000/svg" width="200" height="80"><path d="M 10 40 L 60 20 L 120 60 L 190 30"/></svg>'
# PNG de 1×1 (la foto de la ONU del checklist).
PNG = bytes.fromhex("89504e470d0a1a0a0000000d4948445200000001000000010806000000"
                    "1f15c4890000000d4944415478da63f8ffff3f0005fe02fea7d6a4f90000000049454e44ae426082")


def iris_db(consulta, *args):
    return sql(IRIS_DB, consulta, *args)


def argos_db(consulta, *args):
    return sql(ARGOS_DB, consulta, *args)


# Pasos ------------------------------------------------------------------------------------------


def paso_conexion(a, i):
    """§1.3: enlace generado en Argos, canje desde Iris, conector activo en los dos lados."""
    r = a("POST", "/integraciones/iris/enlace", {})
    exigir(r.status in (200, 201), f"generar enlace en Argos: {r}")
    enlace = r.json["enlace"]
    r = i("POST", "/integraciones/argosisp/conectar", {"enlace": enlace})
    exigir(r.status == 201, f"canje desde Iris: {r}")
    con = r.json
    estado["conector"] = con["id"]
    exigir(con["estado"] == "apagado" and con["remotoId"], f"conector recién canjeado: {con}")
    # Argos guardó el iris_pat_ (§1.2) y el receptor de Iris.
    cfg = a("GET", "/integraciones/iris").json
    exigir(cfg["webhook_salida_url"] == con["webhookUrl"] and cfg["token"] == "***", f"conector en Argos: {cfg}")
    r = i("POST", f"/integraciones/conectores/{con['id']}/activar", {"version": con["version"]})
    exigir(r.status == 200 and r.json["estado"] == "activo", f"activar en Iris: {r}")
    r = a("PUT", "/integraciones/iris", {"version": cfg["version"], "activo": True})
    exigir(r.status == 200 and r.json["activo"], f"activar en Argos: {r}")
    # Argos no informa empresas en el canje (§1.4): Iris las registra a mano (hallazgo).
    empresas = con.get("empresas") or []
    if EMPRESA not in empresas:
        r = i("POST", "/empresas", {"id": EMPRESA, "nombre": "Empresa principal de Argos"})
        exigir(r.status in (201, 409), f"registrar la empresa {EMPRESA} en Iris: {r}")
    token_ok = uno(IRIS_DB, """SELECT count(*) FROM nucleo.tokens_api t JOIN nucleo.usuarios u ON u.id = t.usuario_id
        WHERE u.tipo = 'integracion' AND u.correo = 'integracion.argosisp@iris.local' AND t.revocado_en IS NULL""")
    registrar("1 conexión", True, f"conector Iris {con['id']} activo (remoto {con['remotoId']}); Argos activo con "
              f"receptor {cfg['webhook_salida_url']}; iris_pat_ vigentes del usuario de integración: {token_ok}; "
              f"empresas informadas por el canje: {empresas or 'ninguna (registrada a mano)'}")


def paso_datos_argos(a):
    """Catálogo mínimo en Argos: zona, router con red IPv4, plan v4, bodega y dos ONU con serie."""
    z = a("POST", "/zonas", {"nombre": "Zona Centro E2E", "empresa_sugerida_id": EMPRESA})
    exigir(z.status == 201, f"zona: {z}")
    r = a("POST", "/routers", {"nombre": "RB-E2E", "host": "192.0.2.10", "nas_ip": "192.0.2.10",
                               "radius_secret": "secreto-radius-e2e", "api_usuario": "iris", "api_password": "clave-router-e2e",
                               "empresa_sugerida_id": EMPRESA, "zona_id": z.json["id"], "activo": True})
    exigir(r.status == 201, f"router: {r}")
    estado["router"] = r.json["id"]
    red = a("POST", f"/routers/{r.json['id']}/redes-ipv4", {"cidr": "100.64.10.0/24", "tipo": "pool", "nombre": "clientes-e2e",
                                                          "gateway": "100.64.10.1"})
    exigir(red.status == 201, f"red IPv4: {red}")
    p = a("POST", "/planes", {"nombre": "Plan 30 v4 E2E", "precio": "300.00", "bajada_kbps": 30000, "subida_kbps": 10000,
                             "bajada_limitada_kbps": 512, "subida_limitada_kbps": 512, "global": True, "activo": True,
                             "dual_stack": "v4"})
    exigir(p.status == 201, f"plan: {p}")
    estado["plan"] = p.json["id"]
    b = a("POST", "/bodegas", {"empresa_id": EMPRESA, "nombre": "Bodega central E2E", "tipo": "bodega"})
    exigir(b.status == 201, f"bodega: {b}")
    estado["bodega"] = b.json["id"]
    pr = a("POST", "/productos", {"empresa_id": EMPRESA, "sku": "ONU-E2E", "nombre": "ONU GPON E2E", "tipo": "equipo",
                                 "seguimiento": "serie"})
    exigir(pr.status == 201, f"producto: {pr}")
    for serie in ("ONU-E2E-0001", "ONU-E2E-0002"):
        s = a("POST", "/inventario/saldos-iniciales", {"producto_id": pr.json["id"], "bodega_id": b.json["id"], "cantidad": "1",
                                                      "unidad": {"serie": serie, "mac": None}, "costo_unitario": "350.00",
                                                      "motivo": "Saldo inicial e2e"}, clave="e2e-saldo-" + serie)
        exigir(s.status == 201, f"saldo inicial {serie}: {s}")
    c = a("POST", "/clientes", {"nombre": "Ana Pérez E2E", "email": "ana.e2e@cliente-e2e.test", "telefono": "55551234",
                               "direccion": "Zona 1, Guatemala", "coordenadas": "14.6349,-90.5069", "zona_id": z.json["id"]},
          clave="e2e-cliente-ana")
    exigir(c.status == 201, f"cliente: {c}")
    estado["cliente_ana"] = c.json["id"]
    registrar("datos de Argos", True, f"zona {z.json['id']}, router {r.json['id']} (red 100.64.10.0/24), plan {p.json['id']} "
              f"(v4), bodega {b.json['id']} con ONU-E2E-0001/0002, cliente {c.json['id']}")


def paso_lecturas(i):
    """§2.1: planes, búsqueda y ficha a través de Iris (lecturas servidor a servidor con el atk_)."""
    # Catálogos: la sincronización corre al activar y al arrancar el worker.
    esperar_que(lambda: uno(IRIS_DB, "SELECT count(*) FROM integracion.argos_routers WHERE id = :'p1'", estado["router"]) == "1",
                90, 2, "router en la instantánea de Iris")
    emp_router = uno(IRIS_DB, "SELECT empresa_id FROM integracion.argos_routers WHERE id = :'p1'", estado["router"])
    planes = i("GET", f"/comercial/planes?routerId={estado['router']}", cabeceras=H)
    ok_planes = planes.status == 200 and any(p["id"] == estado["plan"] for p in planes.json.get("data", []))
    registrar("2a lecturas: planes y catálogos", ok_planes and emp_router == str(EMPRESA),
              f"GET /comercial/planes {planes.status} ({len((planes.json or {}).get('data', []))} planes); router en Iris con "
              f"empresa_id {emp_router} (Argos manda empresa_sugerida_id)")
    b = i("GET", "/clientes?q=Ana&limit=5", cabeceras=H)
    f = i("GET", f"/clientes/{estado['cliente_ana']}", cabeceras=H)
    encontrado = b.status == 200 and any(c["id"] == estado["cliente_ana"] for c in b.json.get("data", []))
    registrar("2b lecturas: búsqueda y ficha del cliente", encontrado and f.status == 200,
              f"GET /clientes?q=Ana → {b.status} {len((b.json or {}).get('data', []))} resultados; GET /clientes/{estado['cliente_ana']} → "
              f"{f.status} {(f.json or {}).get('type', '')}. En Argos el cliente no tiene empresa (la tienen sus servicios, "
              "multiempresa.md) y Iris filtra por cliente.empresa_id")


def esperar_entrante(evento, desde_id, segundos=60):
    def hay():
        filas = iris_db("SELECT id, webhook_id, empresa_id, resultado, coalesce(error, '') FROM integracion.entrantes "
                        "WHERE evento = :'p1' AND id > :'p2' AND procesado_en IS NOT NULL ORDER BY id DESC LIMIT 1", evento, desde_id)
        return filas[0] if filas else None
    return esperar_que(hay, segundos, 1, f"webhook {evento} procesado en Iris")


def ultimo_entrante():
    return int(uno(IRIS_DB, "SELECT coalesce(max(id), 0) FROM integracion.entrantes") or 0)


def paso_webhooks(a, i):
    """§2.3: servicio.creado, cliente.actualizado con empresas[], firma, deduplicación y retiro."""
    desde = ultimo_entrante()
    s = a("POST", "/servicios", {"cliente_id": estado["cliente_ana"], "router_id": estado["router"], "plan_id": estado["plan"],
                                 "empresa_id": EMPRESA, "direccion": "Zona 1", "coordenadas": "14.6349,-90.5069"},
          clave="e2e-servicio-ana")
    exigir(s.status == 201, f"servicio en Argos: {s}")
    estado["servicio_ana"] = s.json["id"]
    e1 = esperar_entrante("servicio.creado", desde)
    registrar("3a webhook servicio.creado", True, f"entrante {e1[0]} ({e1[1]}), empresa {e1[2]}, resultado {e1[3]} "
              "(el cliente no está en la instantánea: se lee al abrir su ficha)")

    desde = ultimo_entrante()
    c = a("GET", f"/clientes/{estado['cliente_ana']}").json
    r = a("PUT", f"/clientes/{estado['cliente_ana']}", {"version": c["version"], "nombre": "Ana Pérez López E2E"})
    exigir(r.status == 200, f"renombrar en Argos: {r}")
    e2 = esperar_entrante("cliente.actualizado", desde)
    payload = uno(IRIS_DB, "SELECT payload::text FROM integracion.entrantes WHERE id = :'p1'", e2[0])
    con_empresas = '"empresas": [1]' in payload
    registrar("3b webhook cliente.actualizado con empresas[]", con_empresas,
              f"entrante {e2[0]}: {payload}; resultado {e2[3]} (sin instantánea del cliente: «ignorado» por diseño)")

    # Teléfono: Argos no emite cliente.actualizado (solo nombre, tipo, código y NIT).
    desde = ultimo_entrante()
    c = a("GET", f"/clientes/{estado['cliente_ana']}").json
    a("PUT", f"/clientes/{estado['cliente_ana']}", {"version": c["version"], "telefono": "55559999"})
    time.sleep(12)
    nuevos = uno(IRIS_DB, "SELECT count(*) FROM integracion.entrantes WHERE evento = 'cliente.actualizado' AND id > :'p1'", desde)
    registrar("3c cambio de teléfono en Argos", nuevos != "0",
              f"webhooks cliente.actualizado tras cambiar el teléfono: {nuevos} (Iris muestra teléfono y dirección, §2.2)")

    # Firma y deduplicación: se repite el último envío con el secreto del receptor.
    secreto = os.environ.get("E2E_SECRETO_WEBHOOK") or ""
    if not secreto and os.environ.get("E2E_SECRETO_CMD"):
        secreto = subprocess.run(["bash", "-c", os.environ["E2E_SECRETO_CMD"]], capture_output=True, text=True).stdout.strip()
    if secreto:
        import hashlib
        import hmac
        url = f"{IRIS}/api/v1/integraciones/argosisp/webhook/{estado['conector']}"
        cuerpo = {"id": e2[1], "evento": "cliente.actualizado", "ocurrido_en": "2026-01-01T00:00:00Z",
                  "payload": {"cliente_id": estado["cliente_ana"], "campos": ["nombre"], "empresas": [EMPRESA], "empresa_id": EMPRESA}}

        def enviar(c, sec, marca=None):
            marca = str(marca or int(time.time()))
            b = json.dumps(c, separators=(",", ":")).encode()
            firma = hmac.new(sec.encode(), marca.encode() + b"." + b, hashlib.sha256).hexdigest()
            return _post_crudo(url, b, {"Content-Type": "application/json", "X-Webhook-Id": c["id"],
                                        "X-Webhook-Timestamp": marca, "X-Webhook-Signature": "v1=" + firma})
        dup = enviar(cuerpo, secreto)
        mala = enviar(dict(cuerpo, id="argos-e2e-firma-mala"), "otro-secreto-cualquiera")
        vieja = enviar(dict(cuerpo, id="argos-e2e-marca-vieja"), secreto, int(time.time()) - 600)
        ok = (dup.status == 200 and (dup.json or {}).get("duplicado") is True and mala.status == 401
              and "firma_invalida" in mala.crudo and vieja.status == 401 and "firma_vencida" in vieja.crudo)
        registrar("3d firma y deduplicación", ok, f"repetido {dup}; firma de otro secreto {mala.status}; marca de hace 10 min {vieja.status}")
    else:
        registrar("3d firma y deduplicación", False, "sin E2E_SECRETO_CMD: no se pudo leer el secreto del receptor")

    desde = ultimo_entrante()
    r = a("POST", f"/servicios/{estado['servicio_ana']}/retirar", {"motivo": "Prueba e2e de retiro"}, clave="e2e-retiro-ana")
    exigir(r.status == 200 and r.json["estado"] == "retirado", f"retirar en Argos: {r}")
    e3 = esperar_entrante("servicio.estado_cambiado", desde)
    evento = uno(IRIS_DB, "SELECT payload::text FROM integracion.outbox WHERE evento = 'argos.servicio_retirado' "
                          "AND (payload->>'servicio_id')::bigint = :'p1'", estado["servicio_ana"])
    registrar("3e servicio.estado_cambiado a retirado", e3[3] == "aplicado" and evento is not None,
              f"entrante {e3[0]} {e3[3]}; argos.servicio_retirado: {evento} (sin equipos en comodato no se propone orden)")


def _post_crudo(url, cuerpo, cabeceras):
    req = urllib.request.Request(url, data=cuerpo, method="POST", headers=cabeceras)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return Respuesta(r.status, r.read().decode(), dict(r.headers))
    except urllib.error.HTTPError as e:
        return Respuesta(e.code, e.read().decode(), dict(e.headers))


def prospecto_hasta_contrato(i, nombre, correo, lat, lon):
    p = i("POST", "/prospectos", {"origen": "referido", "nombre": nombre, "telefono": "55557777", "correo": correo,
                                 "direccion": "Zona 10, Guatemala", "lat": lat, "lon": lon, "empresaId": EMPRESA}, cabeceras=H)
    exigir(p.status == 201, f"prospecto: {p}")
    pid = p.json["id"]

    def ver():
        return i("GET", f"/prospectos/{pid}", cabeceras=H).json
    auto = i("POST", f"/prospectos/{pid}/factibilidad", {"version": ver()["version"]}, cabeceras=H)
    estado.setdefault("factibilidad_auto", f"{auto.status} {(auto.json or {}).get('resultado')}/{(auto.json or {}).get('motivo')}")
    v = ver()
    if v["estado"] == "nuevo":
        i("POST", f"/prospectos/{pid}/contactar", {"version": v["version"]}, cabeceras=H)
    f = i("POST", f"/prospectos/{pid}/factibilidad-manual", {"version": ver()["version"], "resultado": "viable",
                                                            "notas": "Visita técnica e2e: caja NAP a 120 m con puertos libres.",
                                                            "routerId": estado["router"]}, cabeceras=H)
    exigir(f.status == 200, f"factibilidad manual: {f}")
    c = i("POST", f"/prospectos/{pid}/cotizaciones", {"version": ver()["version"], "lineas": [
        {"tipo": "plan", "planId": estado["plan"]}, {"tipo": "instalacion", "descripcion": "Instalación", "precioUnitario": "150.00"}]},
        cabeceras=H)
    exigir(c.status == 201, f"cotización: {c}")
    exigir(i("POST", f"/cotizaciones/{c.json['id']}/aceptar", {"version": c.json["version"]}, cabeceras=H).status == 200, "aceptar")
    ct = i("POST", f"/prospectos/{pid}/contrato", {"version": ver()["version"]}, cabeceras=H)
    exigir(ct.status == 201, f"contrato: {ct}")
    fi = i("POST", f"/contratos/{ct.json['id']}/firmar", {"version": ct.json["version"], "firmanteNombre": nombre,
                                                         "firmanteDocumento": "1234567890101", "svg": SVG_FIRMA}, cabeceras=H)
    exigir(fi.status == 200 and fi.json["estado"] == "firmado", f"firmar: {fi}")
    return pid, c.json


def ganar(i, pid):
    v = i("GET", f"/prospectos/{pid}", cabeceras=H).json
    g = i("POST", f"/prospectos/{pid}/ganar", {"version": v["version"], "routerId": estado["router"]}, cabeceras=H)
    exigir(g.status == 200 and g.json["estado"] == "ganado", f"ganar: {g}")
    return g.json


def alta_de(i, pid):
    return (i("GET", f"/prospectos/{pid}", cabeceras=H).json or {}).get("alta") or {}


def paso_alta(a, i):
    """§3.2: prospecto ganado → POST /clientes y POST /servicios en Argos con la idempotencia de §3.1."""
    pid, cot = prospecto_hasta_contrato(i, "Carlos Méndez E2E", "carlos.e2e@cliente-e2e.test", 14.6011, -90.5130)
    estado["prospecto"] = pid
    ganar(i, pid)
    alta = esperar_que(lambda: (lambda x: x if x.get("estado") == "completa" else None)(alta_de(i, pid)), 120, 2, "alta completa")
    pend = iris_db("SELECT operacion, idempotency_key, estado, intentos FROM integracion.pendientes "
                   "WHERE idempotency_key IN (:'p1', :'p2') ORDER BY id", f"iris:cliente:{pid}", f"iris:servicio:{pid}")
    cli = argos_db("SELECT id, codigo, codigo_externo FROM core.clientes WHERE codigo_externo = :'p1'", f"iris:prospecto:{pid}")
    srv = argos_db("SELECT id, estado, empresa_id, plan_id, router_id FROM core.servicios WHERE id = :'p1'", alta.get("servicioArgosId"))
    estado["orden"] = alta.get("ordenId")
    estado["servicio_alta"] = alta.get("servicioArgosId")
    ok = (len(cli) == 1 and srv and srv[0][1] == "pendiente_instalacion" and srv[0][2] == str(EMPRESA)
          and all(p[2] == "hecho" for p in pend) and alta.get("ordenId"))
    registrar("4a alta en Argos desde el prospecto", bool(ok),
              f"factibilidad automática {estado.get('factibilidad_auto')} (routers de Argos sin coordenadas); cotización "
              f"{cot['numero']} {cot['totalMensual']}/mes con el plan de Argos; pendientes {pend}; Argos cliente {cli}, servicio {srv}; "
              f"orden de instalación {alta.get('ordenId')}")


class Tecnico(Iris):
    def __init__(self):
        self.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        r = pedir(self.op, "POST", IRIS + "/api/v1/sesion", {"correo": "tecnico@iris-e2e.test", "contrasena": "tecnico-e2e-12345"})
        if r.status != 200:
            raise Falla(f"login del técnico: {r}")
        self.csrf = r.json["csrfToken"]


def paso_instalacion(a, i):
    """§3: la orden de instalación se cierra (checklist, foto, ONU, firma) y Argos activa el servicio y el comodato."""
    oid = estado["orden"]
    u = i("POST", "/usuarios", {"nombre": "Técnico E2E", "correo": "tecnico@iris-e2e.test", "contrasena": "tecnico-e2e-12345",
                               "roles": [{"rol": "tecnico"}], "empresas": [EMPRESA]})
    exigir(u.status == 201, f"usuario técnico: {u}")
    tid = u.json["id"]
    f = i("POST", "/tecnicos", {"usuarioId": tid, "bodegaId": estado["bodega"]}, cabeceras=H)
    exigir(f.status == 201, f"ficha del técnico: {f}")
    t = i("POST", f"/tecnicos/{f.json['id']}/turnos", {"version": f.json["version"],
                                                      "turnos": [{"dia": d, "inicio": "00:00", "fin": "24:00"} for d in range(1, 8)]}, cabeceras=H)
    exigir(t.status == 200, f"turnos: {t}")
    o = i("GET", f"/ordenes/{oid}", cabeceras=H).json
    inicio = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 300))
    r = i("POST", f"/ordenes/{oid}/programar", {"version": o["version"], "tecnicoId": tid, "inicio": inicio}, cabeceras=H)
    exigir(r.status == 200, f"programar: {r}")
    tec = Tecnico()
    r = tec("POST", f"/ordenes/{oid}/llegar", {"version": r.json["version"], "lat": 14.6011, "lon": -90.5130}, cabeceras=H)
    exigir(r.status == 200, f"llegar: {r}")
    o = tec("GET", f"/ordenes/{oid}", cabeceras=H).json
    # Foto de la ONU (tus 1.0): alta, subida y registro en la orden.
    adj_id = _subir_adjunto(tec, oid, "foto", "foto_onu", PNG)
    # Ítems obligatorios del checklist (el de la orden reúne los de todos sus formularios).
    por_tipo = {"si_no": True, "numero": 1, "medicion": -19.5, "texto": "ok e2e", "foto": adj_id}
    marcados = []
    for item in o.get("checklist") or []:
        if not item.get("obligatorio"):
            continue
        valor = (item.get("opciones") or ["?"])[0] if item["tipo"] == "seleccion" else por_tipo[item["tipo"]]
        r = tec("POST", f"/ordenes/{oid}/checklist", {"itemId": item["itemId"], "valor": valor}, cabeceras=H)
        exigir(r.status == 200, f"checklist {item['codigo']}: {r}")
        marcados.append(item["codigo"])
    r = tec("POST", f"/ordenes/{oid}/equipos-instalados", {"serie": "ONU-E2E-0001", "modelo": "ONU GPON E2E"}, cabeceras=H)
    exigir(r.status == 200, f"equipo instalado: {r}")
    firma_id = _subir_adjunto(tec, oid, "firma", None, PNG)
    r = tec("POST", f"/ordenes/{oid}/firma", {"nombre": "Carlos Méndez", "documento": "1234567890101", "svg": SVG_FIRMA,
                                             "adjuntoId": firma_id}, cabeceras=H)
    exigir(r.status == 200, f"firma: {r}")
    o = tec("GET", f"/ordenes/{oid}", cabeceras=H).json
    desde_correos = len(os.listdir(CORREOS)) if CORREOS and os.path.isdir(CORREOS) else 0
    r = tec("POST", f"/ordenes/{oid}/cerrar", {"version": o["version"], "resultado": "exitosa", "lat": 14.6011, "lon": -90.5130}, cabeceras=H)
    exigir(r.status == 200 and r.json["estado"] == "cerrada", f"cerrar: {r}")
    sid = estado["servicio_alta"]

    def pendientes_hechos():
        filas = iris_db("SELECT operacion, estado, intentos, coalesce(ultimo_status::text, ''), left(coalesce(ultimo_error, ''), 160) "
                        "FROM integracion.pendientes WHERE entidad = 'orden' AND entidad_id = :'p1' ORDER BY id", oid)
        return filas if filas and all(f[1] in ("hecho", "fallido_negocio", "agotado") for f in filas) else None
    pend = esperar_que(pendientes_hechos, 150, 3, "pendientes de la orden resueltos")
    srv = argos_db("SELECT estado, instalado_en IS NOT NULL, coalesce(planta::text, '') FROM core.servicios WHERE id = :'p1'", sid)
    comodato = argos_db("SELECT u.serie, u.estado, coalesce(u.servicio_id::text, '') FROM inventario.unidades u "
                        "WHERE u.serie = 'ONU-E2E-0001'") if _hay_tabla("inventario.unidades") else []
    en_comodato = bool(comodato) and comodato[0][1] == "en_comodato" and comodato[0][2] == str(sid)
    registrar("4b instalación cerrada → activar-instalacion en Argos",
              bool(srv) and srv[0][0] == "activo" and all(p[1] == "hecho" for p in pend) and en_comodato,
              f"checklist {marcados}, foto (tus), ONU-E2E-0001 y firma; pendientes de la orden {pend}; servicio {sid} en Argos "
              f"(estado, instalado, planta) {srv}; ONU {comodato}")

    if not (srv and srv[0][0] == "activo"):
        # Diagnóstico: el mismo cuerpo que manda Iris, y con la version del servicio (la exige Argos 1.3.0).
        cuerpo = json.loads(uno(IRIS_DB, "SELECT payload::text FROM integracion.pendientes WHERE entidad = 'orden' "
                                          "AND entidad_id = :'p1' AND operacion = 'activar_instalacion'", oid) or "{}")
        sin = a("POST", f"/servicios/{sid}/activar-instalacion", cuerpo, clave=f"e2e-diag-sin-version-{oid}")
        version = a("GET", f"/servicios/{sid}").json["version"]
        con = a("POST", f"/servicios/{sid}/activar-instalacion", dict(cuerpo, version=version), clave=f"e2e-diag-con-version-{oid}")
        registrar("4b' diagnóstico: activar-instalacion a mano en Argos", con.status == 200,
                  f"cuerpo de Iris {cuerpo} → {sin.status} {(sin.json or {}).get('errors')}; con version {version} → {con.status} "
                  f"{(con.json or {}).get('estado')}")

    def bienvenida():
        if CORREOS and os.path.isdir(CORREOS):
            for n in sorted(os.listdir(CORREOS))[desde_correos:]:
                with open(os.path.join(CORREOS, n), "rb") as fh:
                    texto = fh.read().decode(errors="replace")
                if "carlos.e2e@cliente-e2e.test" in texto:
                    return n
        return None
    try:
        correo = esperar_que(bienvenida, 90, 3, "correo de bienvenida de Argos")
        registrar("4c bienvenida de Argos", True, f"correo {correo} a carlos.e2e@cliente-e2e.test en el sumidero SMTP")
    except Falla as e:
        registrar("4c bienvenida de Argos", False, str(e))


def _subir_adjunto(cli, oid, tipo, clave, datos):
    """Alta (POST /adjuntos) y subida tus 1.0 de un adjunto de la orden; devuelve su id."""
    import hashlib
    adj = str(uuid.uuid4())
    ad = cli("POST", "/adjuntos", {"id": adj, "entidad": "orden", "entidadId": str(oid), "tipo": tipo, "claveChecklist": clave,
                                  "mime": "image/png", "tamano": len(datos), "sha256": hashlib.sha256(datos).hexdigest()}, cabeceras=H)
    exigir(ad.status in (200, 201), f"alta del adjunto {tipo}: {ad}")
    sub = _tus(cli, ad.json["urlSubida"], datos)
    exigir(sub.status == 204, f"subida tus del adjunto {tipo}: {sub}")
    return adj


def _hay_tabla(nombre):
    return uno(ARGOS_DB, "SELECT to_regclass(:'p1') IS NOT NULL", nombre) == "t"


def _tus(cli, ruta, datos):
    req = urllib.request.Request(IRIS + ruta, data=datos, method="PATCH", headers={
        "X-CSRF-Token": cli.csrf, "Tus-Resumable": "1.0.0", "Upload-Offset": "0",
        "Content-Type": "application/offset+octet-stream", **H})
    try:
        with cli.op.open(req, timeout=30) as r:
            return Respuesta(r.status, r.read().decode(errors="replace"), dict(r.headers))
    except urllib.error.HTTPError as e:
        return Respuesta(e.code, e.read().decode(errors="replace"), dict(e.headers))


def paso_retiro_comodato(a, i):
    """§2.3: servicio.estado_cambiado a retirado con un equipo en comodato → Iris propone la orden de retiro."""
    sid = estado.get("servicio_alta")
    exigir(sid, "sin servicio del alta")
    desde = ultimo_entrante()
    r = a("POST", f"/servicios/{sid}/retirar", {"motivo": "Prueba e2e: retiro con ONU en comodato"}, clave=f"e2e-retiro-{sid}")
    exigir(r.status == 200, f"retirar en Argos: {r}")
    eventos = esperar_que(lambda: (lambda f: f if any(x[0] == "servicio.estado_cambiado" for x in f) else None)(
        iris_db("SELECT evento, resultado FROM integracion.entrantes WHERE id > :'p1' AND procesado_en IS NOT NULL ORDER BY id", desde)),
        60, 2, "webhook del retiro procesado")

    def orden_retiro():
        filas = iris_db("SELECT o.id, o.estado, o.origen FROM campo.ordenes o JOIN campo.tipos_orden t ON t.id = o.tipo_id "
                        "WHERE t.codigo = 'retiro' AND o.servicio_id = :'p1'", sid)
        return filas or None
    try:
        orden = esperar_que(orden_retiro, 60, 2, "orden de retiro propuesta")
        registrar("3f retiro con equipo en comodato", True, f"webhooks {eventos}; orden de retiro {orden}")
    except Falla as e:
        registrar("3f retiro con equipo en comodato", False, f"webhooks {eventos}; {e}")


def paso_caida(a, i):
    """Argos caído: los pendientes se reintentan y, al volver, el alta se completa sin duplicar."""
    parar, arrancar = os.environ.get("E2E_ARGOS_PARAR"), os.environ.get("E2E_ARGOS_ARRANCAR")
    if not parar or not arrancar:
        registrar("5 Argos caído", False, "sin E2E_ARGOS_PARAR/E2E_ARGOS_ARRANCAR")
        return
    pid, _ = prospecto_hasta_contrato(i, "Diana Caída E2E", "diana.e2e@cliente-e2e.test", 14.62, -90.52)
    subprocess.run(["bash", "-c", parar], check=True)
    try:
        ganar(i, pid)
        clave = f"iris:cliente:{pid}"
        fallo = esperar_que(lambda: (lambda f: f[0] if f and int(f[0][1]) >= 1 else None)(
            iris_db("SELECT estado, intentos, coalesce(ultimo_error, ''), proximo_intento > now() FROM integracion.pendientes "
                    "WHERE idempotency_key = :'p1'", clave)), 90, 2, "primer intento fallido con Argos caído")
    finally:
        subprocess.run(["bash", "-c", arrancar], check=True)
    alta = esperar_que(lambda: (lambda x: x if x.get("estado") == "completa" else None)(alta_de(i, pid)), 240, 5,
                       "alta completa al volver Argos")
    clientes = argos_db("SELECT count(*) FROM core.clientes WHERE codigo_externo = :'p1'", f"iris:prospecto:{pid}")[0][0]
    servicios = argos_db("SELECT count(*) FROM core.servicios s JOIN core.clientes c ON c.id = s.cliente_id "
                         "WHERE c.codigo_externo = :'p1'", f"iris:prospecto:{pid}")[0][0]
    pend = iris_db("SELECT operacion, estado, intentos FROM integracion.pendientes WHERE idempotency_key IN (:'p1', :'p2') ORDER BY id",
                   clave, f"iris:servicio:{pid}")
    registrar("5 Argos caído y de vuelta", clientes == "1" and servicios == "1",
              f"con Argos caído: {fallo}; al volver: alta {alta.get('estado')}, pendientes {pend}; en Argos {clientes} cliente y "
              f"{servicios} servicio con codigo_externo iris:prospecto:{pid}")


def paso_sso(a, i):
    """«Ver en Iris» desde Argos (§1.3, autologin): Argos pide el ticket a Iris con el iris_pat_."""
    r = a("POST", "/integraciones/iris/abrir", {"destino": {"tipo": "orden", "id_externo": str(estado.get("orden") or 1)}})
    ok = r.status in (200, 201) and "/sso/entrar#" in r.crudo
    registrar("6 «Ver en Iris» (SSO)", ok, f"POST /integraciones/iris/abrir en Argos → {r}")


PASOS = [
    ("datos", lambda a, i: paso_datos_argos(a)),
    ("conexion", paso_conexion),
    ("lecturas", lambda a, i: paso_lecturas(i)),
    ("webhooks", paso_webhooks),
    ("alta", paso_alta),
    ("instalacion", paso_instalacion),
    ("retiro_comodato", paso_retiro_comodato),
    ("caida", paso_caida),
    ("sso", paso_sso),
]


def main():
    elegidos = [p for p in os.environ.get("E2E_PASOS", "").split(",") if p]
    a, i = Argos(), Iris()
    for nombre, paso in PASOS:
        if elegidos and nombre not in elegidos:
            continue
        try:
            paso(a, i)
        except Falla as e:
            registrar(nombre, False, str(e))
        except Exception as e:  # noqa: BLE001 — el reporte sigue con el resto de los pasos
            registrar(nombre, False, f"error inesperado: {e!r}")
    print("\n=== Resumen ===")
    for paso, ok, _ in resultados:
        print(f"{'OK   ' if ok else 'FALLA'} {paso}")
    return 0 if all(ok for _, ok, _ in resultados) else 1


if __name__ == "__main__":
    sys.exit(main())
