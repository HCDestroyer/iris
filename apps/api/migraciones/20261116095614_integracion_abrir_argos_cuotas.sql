-- +goose Up
-- Tope de enlaces de autologin Iris → Argos por persona y minuto (contrato con Argos 0.7.0 §1.3). En
-- la base para que valga con varias réplicas de la API. Una fila por usuario: la ventana vigente.
CREATE TABLE integracion.abrir_argos_cuotas (
    usuario_id bigint PRIMARY KEY REFERENCES nucleo.usuarios (id) ON DELETE CASCADE,
    inicio     timestamptz NOT NULL,
    pedidos    integer     NOT NULL CHECK (pedidos > 0)
);

-- +goose Down
DROP TABLE integracion.abrir_argos_cuotas;
