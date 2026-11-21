-- +goose Up
-- Prospectos registrados desde Argos (contrato argosisp.md 0.10.0 §3.6, A44 / B23): una persona de
-- Argos registra en Iris a alguien interesado con POST /integraciones/argosisp/prospectos.

-- Origen argos_panel: lo registró una persona en el panel de Argos.
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_origen_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_origen_check
    CHECK (origen IN ('panel', 'web', 'referido', 'argos', 'argos_panel', 'otro'));

ALTER TABLE comercial.prospectos
    -- residencial | empresarial (los planes de Argos se filtran por modo, §3.4.5).
    ADD COLUMN modo text CHECK (modo IN ('residencial', 'empresarial')),
    -- Quién lo registró en Argos (X-Iris-Actor: usuario:<id de Argos>); solo con argos_panel.
    ADD COLUMN registrado_por_argos text,
    -- Cliente de Argos que eligió quien lo registró (o null: cliente nuevo). Al ganar, Iris no crea
    -- ni edita ese cliente (§3.6).
    ADD COLUMN cliente_existente_id bigint,
    -- false = Iris no lo tenía en su instantánea y Argos no respondió: se concilia después.
    ADD COLUMN cliente_validado boolean,
    -- Al ganar: quién lo ganó (X-Iris-Actor de los contactos que se crean en Argos, A42) y los
    -- permisos que puede dar a esos contactos (nunca más que los suyos, §10.5 ajuste i).
    ADD COLUMN ganado_por bigint REFERENCES nucleo.usuarios (id),
    ADD COLUMN permisos_contacto jsonb,
    ADD CONSTRAINT prospectos_cliente_existente_check CHECK ((cliente_existente_id IS NULL) = (cliente_validado IS NULL));

-- Teléfonos del prospecto (1 a 5, uno principal), con índice por número y empresa: el aviso de
-- duplicado y el chat lo ubican por cualquiera de sus números. numero en E.164.
CREATE TABLE comercial.prospecto_telefonos (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    prospecto_id bigint      NOT NULL REFERENCES comercial.prospectos (id),
    empresa_id   bigint,
    posicion     smallint    NOT NULL,
    numero       text        NOT NULL CHECK (numero ~ '^\+[1-9][0-9]{7,14}$'),
    whatsapp     boolean     NOT NULL,
    principal    boolean     NOT NULL,
    nombre       text        CHECK (nombre IS NULL OR length(nombre) BETWEEN 1 AND 200),
    relacion     text        CHECK (relacion IN ('titular', 'familiar', 'otro')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (relacion IS DISTINCT FROM 'titular' OR principal)
);
CREATE UNIQUE INDEX prospecto_telefonos_numero_uq ON comercial.prospecto_telefonos (prospecto_id, numero);
CREATE UNIQUE INDEX prospecto_telefonos_principal_uq ON comercial.prospecto_telefonos (prospecto_id) WHERE principal;
CREATE INDEX prospecto_telefonos_empresa_numero_idx ON comercial.prospecto_telefonos (empresa_id, numero);

-- Límites de §3.6: 30 números por minuto por actor de Argos y 300 por empresa. Una fila por
-- petición aceptada para contar (se purga sola: solo importa el último minuto).
CREATE TABLE comercial.registros_argos (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor      text        NOT NULL,
    empresa_id bigint      NOT NULL,
    numeros    integer     NOT NULL CHECK (numeros BETWEEN 1 AND 5),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX registros_argos_actor_idx ON comercial.registros_argos (actor, created_at);
CREATE INDEX registros_argos_empresa_idx ON comercial.registros_argos (empresa_id, created_at);

-- Webhook a Argos (§6.6, 0.10.0): prospecto.estado_cambiado de los prospectos argos_panel.
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja', 'prospecto.estado_cambiado'));

-- Pendientes hacia Argos con X-Iris-Actor (contrato §3.5: contactos.gestionar la exige; §3.4).
ALTER TABLE integracion.pendientes
    ADD COLUMN actor text CHECK (actor IS NULL OR actor ~ '^usuario:[A-Za-z0-9_-]{1,64}$');

-- +goose Down
ALTER TABLE integracion.pendientes DROP COLUMN actor;
DELETE FROM mensajeria.webhooks_salida WHERE evento = 'prospecto.estado_cambiado';
ALTER TABLE mensajeria.webhooks_salida DROP CONSTRAINT webhooks_salida_evento_check;
ALTER TABLE mensajeria.webhooks_salida ADD CONSTRAINT webhooks_salida_evento_check
    CHECK (evento IN ('aviso.entregado', 'aviso.fallido', 'contacto.baja'));
DROP TABLE comercial.registros_argos;
DROP TABLE comercial.prospecto_telefonos;
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_cliente_existente_check;
ALTER TABLE comercial.prospectos DROP COLUMN permisos_contacto, DROP COLUMN ganado_por,
    DROP COLUMN cliente_validado, DROP COLUMN cliente_existente_id,
    DROP COLUMN registrado_por_argos, DROP COLUMN modo;
UPDATE comercial.prospectos SET origen = 'argos' WHERE origen = 'argos_panel';
ALTER TABLE comercial.prospectos DROP CONSTRAINT prospectos_origen_check;
ALTER TABLE comercial.prospectos ADD CONSTRAINT prospectos_origen_check
    CHECK (origen IN ('panel', 'web', 'referido', 'argos', 'otro'));
