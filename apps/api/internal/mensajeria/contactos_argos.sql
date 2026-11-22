-- Contactos del cliente en la atención (contrato argosisp.md §3.5 y §3.8, pedidos B22 y B25). La
-- caché es de integracion (argos_contactos): aquí solo se lee.

-- name: ContactosArgosDelNumero :many
-- Los contactos de la caché con ese número (solo dígitos) en la empresa, con si su cliente (y la
-- persona a cargo) están en la instantánea. Sin el contacto titular: sus números propios ya están en
-- la instantánea (teléfono y móvil del cliente) y los demás son números adicionales (§3.2), que Iris
-- trata como un contacto no verificado (§3.5, `adicional`): no asocian solos ni dan el rol de titular.
SELECT c.contacto_id, c.cliente_id, c.empresa_id, c.nombre, c.relacion, c.es_titular, c.servicios, c.consultar, c.reportar,
    c.pagar, c.avisos, c.persona_cliente_id, c.version,
    EXISTS (SELECT 1 FROM integracion.argos_clientes ac WHERE ac.id = c.cliente_id
        AND (@empresa::bigint = 0 OR ac.empresa_id = @empresa::bigint OR @empresa::bigint = ANY (ac.empresas) OR ac.sin_servicios)) AS cliente_en_empresa,
    (c.persona_cliente_id IS NOT NULL AND EXISTS (SELECT 1 FROM integracion.argos_clientes ap WHERE ap.id = c.persona_cliente_id)) AS persona_conocida
FROM integracion.argos_contactos c
WHERE NOT c.es_titular
  AND EXISTS (SELECT 1 FROM integracion.argos_contacto_telefonos t WHERE t.contacto_id = c.contacto_id AND t.digitos = @digitos::text)
  AND (@empresa::bigint = 0 OR c.empresa_id IS NULL OR c.empresa_id = @empresa::bigint)
ORDER BY c.cliente_id, c.contacto_id
LIMIT 50;

-- name: ServiciosDeClientesInstantanea :many
-- Los servicios de esos clientes (código y nombre de A45) en la empresa.
SELECT id, cliente_id, empresa_id, codigo, nombre, estado FROM integracion.argos_servicios
WHERE cliente_id = ANY (@clientes::bigint[]) AND (@empresa::bigint = 0 OR empresa_id = @empresa::bigint)
ORDER BY cliente_id, id;

-- name: ServicioInstantaneaPorID :one
SELECT id, cliente_id, empresa_id, codigo, nombre, estado FROM integracion.argos_servicios WHERE id = @id;

-- name: EleccionesClienteConv :one
SELECT count(*)::bigint FROM mensajeria.elecciones_cliente WHERE conversacion_id = @conversacion_id AND NOT acierto;

-- name: EleccionesClienteNumero :one
SELECT count(*)::bigint FROM mensajeria.elecciones_cliente
WHERE coalesce(empresa_id, 0) = @empresa::bigint AND numero = @numero AND creado_en >= @desde AND NOT acierto;

-- name: InsertarEleccionCliente :exec
INSERT INTO mensajeria.elecciones_cliente (empresa_id, conversacion_id, numero, acierto, cliente_id, usuario_id, creado_en)
VALUES (sqlc.narg('empresa_id'), @conversacion_id, @numero, @acierto, sqlc.narg('cliente_id'), sqlc.narg('usuario_id'), @creado_en);

-- name: ContactoArgosDeCliente :one
-- El contacto de la caché con ese id, solo si es de ese cliente.
SELECT * FROM integracion.argos_contactos WHERE contacto_id = @contacto_id AND cliente_id = @cliente_id;

-- name: NumeroBuscadoEn :one
-- Cuándo se buscó por última vez el número en los contactos de Argos, si fue después de @desde.
SELECT buscado_en FROM mensajeria.contactos_buscados WHERE empresa = @empresa AND digitos = @digitos AND buscado_en >= @desde;

-- name: MarcarNumeroBuscado :exec
INSERT INTO mensajeria.contactos_buscados (empresa, digitos, buscado_en) VALUES (@empresa, @digitos, @buscado_en)
ON CONFLICT (empresa, digitos) DO UPDATE SET buscado_en = EXCLUDED.buscado_en;
