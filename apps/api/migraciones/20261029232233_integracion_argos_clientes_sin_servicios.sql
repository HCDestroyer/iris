-- +goose Up
-- «Sin servicios» solo si Argos lo dijo de forma concluyente (la lista consolidada de todas las
-- empresas vino vacía). Una lista vacía por el alcance del token no lo es: ese cliente no se ve.
-- Un cliente sin servicios se ve en todas las empresas (como en Argos), pero su estado de cuenta y
-- el acceso al portal solo con empresas.todas.
ALTER TABLE integracion.argos_clientes ADD COLUMN sin_servicios boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE integracion.argos_clientes DROP COLUMN sin_servicios;
