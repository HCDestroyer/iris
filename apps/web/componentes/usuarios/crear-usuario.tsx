"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import {
    asignarErrores,
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    Campo,
    CampoSelector,
    Casilla,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    type ErrorDeCampo,
    ErrorFormulario,
    GrupoSegmentado,
    useCambiosSinGuardar,
} from "@iris/ui";
import { Bot, CircleCheck, Eye, EyeOff, Plus, RotateCw, User, UserPlus, WandSparkles, X } from "lucide-react";
import { useState } from "react";
import { useFieldArray, useForm, useWatch, type Path, type UseFormReturn } from "react-hook-form";
import { BotonCopiar, useBorradorPortapapeles } from "@/componentes/cuenta/boton-copiar";
import type { Rol } from "@/lib/accesos/presentacion";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { generarContrasenaTemporal } from "@/lib/cuenta/esquemas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import {
    cuerpoCrearUsuario,
    esquemaCrearUsuario,
    faltanEnCrear,
    esRolIntegracion,
    ROL_VACIO,
    ROLES_INTEGRACION,
    valoresCrearUsuario,
    type DatosCrearUsuario,
    type Usuario,
} from "@/lib/usuarios/esquemas";

type Props = {
    abierto: boolean;
    alCerrar: () => void;
    /** La API creó el usuario (201). El diálogo pasa a mostrar la contraseña una sola vez. */
    alCrear: (u: Usuario) => void;
    /** Desde «Nuevo técnico»: persona con este rol (global), sin elegir tipo ni roles. */
    rolFijo?: string;
    /** Botón del resultado («Ver usuario», «Continuar con la ficha»…); sin él, «Listo». */
    textoContinuar?: string;
    alContinuar?: (u: Usuario) => void;
};

const NOMBRES_INTEGRACION: Record<(typeof ROLES_INTEGRACION)[number], string> = { integracion_argos: "Integración Argos", integracion_horus: "Integración Horus" };

const CAMPOS = ["nombre", "correo", "tipo", "contrasena", "empresas"] as const;

/**
 * «Nuevo usuario» (`POST /usuarios`, permiso `usuarios.crear`): nombre, correo, tipo, contraseña
 * temporal (con «Generar» y «Copiar»), roles iniciales con alcance y empresas (solo las de quien
 * lo crea; obligatorias si la API las exige). Al crear, muestra la contraseña una sola vez.
 */
export function DialogoCrearUsuario({ abierto, alCerrar, alCrear, rolFijo, textoContinuar, alContinuar }: Props) {
    const [creado, setCreado] = useState<{ usuario: Usuario; contrasena: string } | null>(null);
    // Del diálogo entero: pasar al resultado no borra lo recién copiado; cerrarlo sí.
    const portapapeles = useBorradorPortapapeles(abierto);
    return (
        <DialogoProtegido
            open={abierto}
            onOpenChange={(a) => {
                if (!a) {
                    setCreado(null);
                    alCerrar();
                }
            }}
        >
            {abierto ? (
                creado ? (
                    <Resultado
                        {...creado}
                        portapapeles={portapapeles}
                        textoContinuar={textoContinuar}
                        alContinuar={() => {
                            setCreado(null);
                            if (alContinuar) alContinuar(creado.usuario);
                            else alCerrar();
                        }}
                    />
                ) : (
                    <Formulario
                        rolFijo={rolFijo}
                        portapapeles={portapapeles}
                        alCrear={(usuario, contrasena) => {
                            setCreado({ usuario, contrasena });
                            alCrear(usuario);
                        }}
                    />
                )
            ) : null}
        </DialogoProtegido>
    );
}

function Formulario({
    rolFijo,
    portapapeles,
    alCrear,
}: {
    rolFijo: string | undefined;
    portapapeles: ReturnType<typeof useBorradorPortapapeles>;
    alCrear: (u: Usuario, contrasena: string) => void;
}) {
    const { yo, puede } = useSesion();
    const empresasDisponibles = yo.empresas ?? [];
    const conEmpresas = yo.modoEmpresa !== "sin_conector" && empresasDisponibles.length > 0;
    const empresasObligatorias = yo.modoEmpresa !== "sin_conector" && !yo.todasLasEmpresas;
    // La API solo deja crear integraciones con empresas.todas e integraciones.conectar globales.
    const puedeIntegracion = puede(["integraciones.conectar"]) && yo.todasLasEmpresas;
    const esquema = esquemaCrearUsuario({ empresasObligatorias });
    const iniciales = valoresCrearUsuario(rolFijo);
    // Con una sola empresa posible y obligatoria, ya va elegida.
    if (empresasObligatorias && empresasDisponibles.length === 1 && empresasDisponibles[0]) {
        iniciales.empresas = [String(empresasDisponibles[0].id)];
    }
    const form = useForm<DatosCrearUsuario>({ resolver: zodResolver(esquema), defaultValues: iniciales, mode: "onTouched" });
    const { errors, isSubmitting, isDirty } = form.formState;
    useCambiosSinGuardar(isDirty, isSubmitting);
    const valores = useWatch({ control: form.control }) as DatosCrearUsuario;
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [sueltos, setSueltos] = useState<ErrorDeCampo[]>([]);
    const faltan = faltanEnCrear(valores, empresasObligatorias && conEmpresas);

    async function enviar(d: DatosCrearUsuario) {
        setError(null);
        setSueltos([]);
        try {
            const u = await resolver(api().POST("/api/v1/usuarios", { params: { header: { "Idempotency-Key": clave.actual() } }, body: cuerpoCrearUsuario(d) }));
            clave.renovar();
            // La contraseña no se queda en el formulario: solo en el resultado, una vez.
            form.reset(valoresCrearUsuario(rolFijo));
            avisar({ titulo: `Usuario ${u.nombre} creado` });
            alCrear(u, d.tipo === "persona" ? d.contrasena : "");
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 422 && e.errores.length > 0) {
                const campos: Path<DatosCrearUsuario>[] = [...CAMPOS, ...d.roles.flatMap((_, i) => [`roles.${i}.rol`, `roles.${i}.zonaId`, `roles.${i}.cuadrillaId`] as const)];
                const restantes = asignarErrores(e.errores, campos, (campo, err) => form.setError(campo, err));
                if (restantes.length === 0) return;
                setSueltos(restantes);
            } else if (e instanceof ErrorApi && e.codigo === "correo_en_uso") {
                form.setError("correo", { type: "servidor", message: e.detalle ?? "Ya existe un usuario con ese correo" });
                return;
            }
            setError(e);
        }
    }

    return (
        <DialogoContenido
            titulo={rolFijo ? "Crear usuario del técnico" : "Nuevo usuario"}
            descripcion={rolFijo ? "Se crea con el rol Técnico; luego vuelves a la ficha con el usuario elegido." : "Una persona que entra al panel o a la app de campo, o una integración que usa un token."}
            data-testid="crear-usuario"
        >
            <form noValidate className="grid gap-4" aria-label="Nuevo usuario" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" autoFocus autoComplete="off" maxLength={120} error={errors.nombre?.message} {...form.register("nombre")} />
                    <Campo etiqueta="Correo" type="email" autoComplete="off" spellCheck={false} autoCapitalize="off" maxLength={254} error={errors.correo?.message} {...form.register("correo")} />
                </div>
                {rolFijo || !puedeIntegracion ? null : (
                    <GrupoSegmentado<DatosCrearUsuario["tipo"]>
                        etiqueta="Tipo"
                        valor={valores.tipo}
                        alCambiar={(v) => {
                            form.setValue("tipo", v, { shouldDirty: true });
                            // Una integración no tiene contraseña y solo lleva su rol de contrato.
                            if (v === "integracion") {
                                form.setValue("contrasena", "", { shouldValidate: form.formState.isSubmitted });
                                form.setValue("roles", [{ ...ROL_VACIO, rol: ROLES_INTEGRACION[0] }], { shouldDirty: true });
                            } else {
                                form.setValue("roles", [], { shouldDirty: true });
                            }
                        }}
                        opciones={[
                            { valor: "persona", etiqueta: "Persona", icono: User },
                            { valor: "integracion", etiqueta: "Integración", icono: Bot },
                        ]}
                    />
                )}
                {valores.tipo === "persona" ? (
                    <CampoContrasena form={form} portapapeles={portapapeles} />
                ) : (
                    <Aviso tono="info" titulo="Sin contraseña">
                        Una integración entra solo con un token de API (`POST /api/v1/tokens` con su `usuarioId`). Los conectores de Argos y Horus
                        crean el suyo solos al conectarse.
                    </Aviso>
                )}
                {rolFijo ? (
                    <p className="flex items-center gap-2 text-sm">
                        <span className="text-muted-foreground">Rol:</span>
                        <span className="font-medium">Técnico</span>
                        <span className="text-muted-foreground">· todas las zonas</span>
                    </p>
                ) : (
                    <RolesIniciales form={form} puedeVerRoles={puede(["roles.ver"])} conAgenda={puede(["agenda.ver"]) && valores.tipo === "persona"} integracion={valores.tipo === "integracion"} />
                )}
                {conEmpresas ? <EmpresasIniciales form={form} empresas={empresasDisponibles} obligatorias={empresasObligatorias} /> : null}
                {error ? <AvisoCrear error={error} sueltos={sueltos} /> : sueltos.length > 0 ? <ErrorFormulario titulo="Revisa los datos" errores={sueltos} /> : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <BotonEnviar faltan={faltan} enviando={isSubmitting} textoEnviando="Creando…">
                        <UserPlus />
                        Crear usuario
                    </BotonEnviar>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}

function CampoContrasena({ form, portapapeles }: { form: UseFormReturn<DatosCrearUsuario>; portapapeles: ReturnType<typeof useBorradorPortapapeles> }) {
    const contrasena = useWatch({ control: form.control, name: "contrasena" });
    const [visible, setVisible] = useState(false);
    return (
        <div className="grid gap-2">
            <Campo
                etiqueta="Contraseña temporal"
                type={visible ? "text" : "password"}
                autoComplete="off"
                spellCheck={false}
                autoCapitalize="off"
                className="[&_input]:font-mono [&_input]:tracking-wide"
                ayuda="Mínimo 12 caracteres. Entrégala en persona o por teléfono, nunca por correo ni chat."
                error={form.formState.errors.contrasena?.message}
                {...form.register("contrasena")}
            />
            <div className="flex flex-wrap items-start gap-2">
                <Boton
                    type="button"
                    variante="secundario"
                    onClick={() => form.setValue("contrasena", generarContrasenaTemporal(), { shouldValidate: true, shouldDirty: true })}
                >
                    <WandSparkles />
                    Generar
                </Boton>
                <Boton type="button" variante="contorno" onClick={() => setVisible((v) => !v)} aria-pressed={visible}>
                    {visible ? <EyeOff /> : <Eye />}
                    {visible ? "Ocultar" : "Mostrar"}
                </Boton>
                <BotonCopiar texto={contrasena} etiqueta="Copiar" copiado="Copiada" sensible borrador={portapapeles} disabled={!contrasena} />
            </div>
        </div>
    );
}

function RolesIniciales({
    form,
    puedeVerRoles,
    conAgenda,
    integracion,
}: {
    form: UseFormReturn<DatosCrearUsuario>;
    puedeVerRoles: boolean;
    conAgenda: boolean;
    /** Integración: solo sus roles de contrato, sin alcance. */
    integracion: boolean;
}) {
    const filas = useFieldArray({ control: form.control, name: "roles" });
    const valores = useWatch({ control: form.control, name: "roles" });
    const roles = useLectura<Rol[]>(async (signal) =>
        puedeVerRoles ? ((await resolver(api().GET("/api/v1/roles", { params: { query: { limit: 200 } }, signal }))).data ?? []).filter((r) => r.activo) : [],
    );
    const zonas = useLectura<Schema<"Zona">[]>(async (signal) => (conAgenda ? ((await resolver(api().GET("/api/v1/zonas", { signal }))).data ?? []) : []));
    const cuadrillas = useLectura<Schema<"Cuadrilla">[]>(async (signal) => (conAgenda ? ((await resolver(api().GET("/api/v1/cuadrillas", { signal }))).data ?? []) : []));
    const errores = form.formState.errors.roles;
    // Una integración solo lleva su rol de contrato: lista fija, sin leer los roles.
    const opciones: { id: string; codigo: string; nombre: string }[] = integracion
        ? ROLES_INTEGRACION.map((codigo) => ({ id: codigo, codigo, nombre: NOMBRES_INTEGRACION[codigo] }))
        : roles.estado === "listo"
          ? roles.datos.filter((r) => !esRolIntegracion(r.codigo)).map((r) => ({ id: String(r.id), codigo: r.codigo, nombre: r.nombre }))
          : [];
    const listos = integracion || roles.estado === "listo";

    if (!puedeVerRoles && !integracion) {
        return (
            <fieldset className="grid gap-2 rounded-md border p-3">
                <legend className="px-1 text-sm font-medium">Roles</legend>
                <p className="text-sm text-muted-foreground">No puedes ver los roles: se crea sin roles y un administrador se los asigna después en su ficha.</p>
            </fieldset>
        );
    }
    return (
        <fieldset className="grid gap-3 rounded-md border p-3" aria-label="Roles iniciales">
            <legend className="px-1 text-sm font-medium">Roles</legend>
            {roles.estado === "error" && !integracion ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo cargar la lista de roles"
                    accion={
                        <Boton type="button" variante="secundario" tamano="sm" onClick={roles.reintentar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {mensajeDeError(roles.error).titulo}
                </Aviso>
            ) : null}
            {filas.fields.length === 0 ? <p className="text-sm text-muted-foreground">Sin roles: no podrá hacer nada en Iris hasta que se los asignen.</p> : null}
            <ul className="grid gap-3" aria-label="Roles a asignar">
                {filas.fields.map((f, i) => {
                    const v = valores?.[i] ?? ROL_VACIO;
                    const err = errores?.[i];
                    return (
                        <li key={f.id} className="grid gap-3 rounded-md border border-border p-3" data-fila-rol={i}>
                            <div className="flex items-end gap-2">
                                <CampoSelector
                                    etiqueta={`Rol ${i + 1}`}
                                    className="min-w-0 flex-1"
                                    disabled={!listos}
                                    error={err?.rol?.message}
                                    {...form.register(`roles.${i}.rol`)}
                                >
                                    <option value="">{listos ? "Elige un rol…" : roles.estado === "cargando" ? "Cargando roles…" : "Elige un rol…"}</option>
                                    {opciones.map((r) => (
                                        <option key={r.id} value={r.codigo}>
                                            {r.nombre}
                                        </option>
                                    ))}
                                </CampoSelector>
                                <Boton type="button" variante="fantasma" tamano="sm" onClick={() => filas.remove(i)} aria-label={`Quitar el rol ${i + 1}`} className="mb-0.5">
                                    <X />
                                </Boton>
                            </div>
                            {conAgenda ? (
                                <GrupoSegmentado<"global" | "zona" | "cuadrilla">
                                    etiqueta={`Alcance del rol ${i + 1}`}
                                    valor={v.alcance}
                                    alCambiar={(a) => form.setValue(`roles.${i}.alcance`, a, { shouldDirty: true, shouldValidate: form.formState.isSubmitted })}
                                    opciones={[
                                        { valor: "global", etiqueta: "Todas las zonas" },
                                        { valor: "zona", etiqueta: "Una zona" },
                                        { valor: "cuadrilla", etiqueta: "Una cuadrilla" },
                                    ]}
                                />
                            ) : null}
                            {v.alcance === "zona" ? (
                                <CampoSelector etiqueta="Zona" disabled={zonas.estado !== "listo"} error={err?.zonaId?.message} {...form.register(`roles.${i}.zonaId`)}>
                                    <option value="">Elige la zona…</option>
                                    {(zonas.estado === "listo" ? zonas.datos : []).map((z) => (
                                        <option key={z.id} value={String(z.id)}>
                                            {z.nombre}
                                        </option>
                                    ))}
                                </CampoSelector>
                            ) : null}
                            {v.alcance === "cuadrilla" ? (
                                <CampoSelector etiqueta="Cuadrilla" disabled={cuadrillas.estado !== "listo"} error={err?.cuadrillaId?.message} {...form.register(`roles.${i}.cuadrillaId`)}>
                                    <option value="">Elige la cuadrilla…</option>
                                    {(cuadrillas.estado === "listo" ? cuadrillas.datos : []).map((c) => (
                                        <option key={c.id} value={String(c.id)}>
                                            {c.nombre}
                                        </option>
                                    ))}
                                </CampoSelector>
                            ) : null}
                        </li>
                    );
                })}
            </ul>
            <div>
                <Boton type="button" variante="secundario" tamano="sm" onClick={() => filas.append({ ...ROL_VACIO })} disabled={filas.fields.length >= 50}>
                    <Plus />
                    Agregar rol
                </Boton>
            </div>
        </fieldset>
    );
}

function EmpresasIniciales({ form, empresas, obligatorias }: { form: UseFormReturn<DatosCrearUsuario>; empresas: readonly Schema<"EmpresaResumen">[]; obligatorias: boolean }) {
    const elegidas = useWatch({ control: form.control, name: "empresas" }) ?? [];
    const error = form.formState.errors.empresas;
    const mensaje = error?.message ?? error?.root?.message;
    return (
        <fieldset className="grid gap-2 rounded-md border p-3" aria-label="Empresas">
            <legend className="px-1 text-sm font-medium">Empresas{obligatorias ? "" : " (opcional)"}</legend>
            <p className="text-sm text-muted-foreground">
                {obligatorias
                    ? "Al menos una: solo puedes darle las empresas en que operas tú."
                    : "Sin empresas, solo un administrador global podrá administrarlo."}
            </p>
            <div className="grid gap-2 sm:grid-cols-2">
                {empresas.map((e) => {
                    const id = String(e.id);
                    return (
                        <Casilla
                            key={e.id}
                            etiqueta={e.nombre}
                            checked={elegidas.includes(id)}
                            onChange={(ev) =>
                                form.setValue("empresas", ev.target.checked ? [...elegidas, id] : elegidas.filter((x) => x !== id), {
                                    shouldDirty: true,
                                    shouldValidate: form.formState.isSubmitted,
                                })
                            }
                        />
                    );
                })}
            </div>
            {mensaje ? (
                <p className="text-sm text-error" role="alert">
                    {mensaje}
                </p>
            ) : null}
        </fieldset>
    );
}

function AvisoCrear({ error, sueltos }: { error: unknown; sueltos: ErrorDeCampo[] }) {
    const codigo = error instanceof ErrorApi ? error.codigo : undefined;
    if (codigo === "sin_conexion" || codigo === "api_no_disponible") {
        return (
            <ErrorFormulario
                titulo="No pudimos contactar a Iris"
                detalle="Puede que no se haya creado. Revisa tu conexión y vuelve a enviar: se usa la misma clave, así que no se creará dos veces."
            />
        );
    }
    if (error instanceof ErrorApi && error.estado === 403) {
        return <ErrorFormulario titulo="No puedes crear este usuario" detalle={mensajeDeError(error).detalle ?? "Te falta un permiso o una empresa que intentas darle."} errores={sueltos} />;
    }
    const { titulo, detalle } = mensajeDeError(error);
    return <ErrorFormulario titulo={`No se pudo crear el usuario: ${titulo}`} detalle={detalle} errores={sueltos} />;
}

function Resultado({
    usuario,
    contrasena,
    portapapeles,
    textoContinuar,
    alContinuar,
}: {
    usuario: Usuario;
    contrasena: string;
    portapapeles: ReturnType<typeof useBorradorPortapapeles>;
    textoContinuar: string | undefined;
    alContinuar: () => void;
}) {
    return (
        <DialogoContenido titulo="Usuario creado">
            <div className="grid gap-4" data-testid="usuario-creado">
                <div className="flex items-start gap-3" role="status">
                    <CircleCheck aria-hidden className="mt-0.5 size-5 shrink-0 text-success" />
                    <p className="text-sm">
                        <span className="font-medium">{usuario.nombre}</span> ({usuario.correo}) ya puede {usuario.tipo === "persona" ? "entrar a Iris." : "recibir un token de API."}
                    </p>
                </div>
                {contrasena ? (
                    <div className="grid gap-2">
                        <p className="text-sm font-medium">Contraseña temporal</p>
                        <div className="flex flex-wrap items-center gap-2">
                            <code className="rounded-md border bg-background-secondary px-3 py-2 font-mono text-base tracking-wide break-all" data-testid="contrasena-creada">
                                {contrasena}
                            </code>
                            <BotonCopiar texto={contrasena} etiqueta="Copiar" copiado="Copiada" sensible borrador={portapapeles} />
                        </div>
                        <Aviso tono="warning" titulo="Se muestra solo esta vez">
                            Entrégala en persona o por teléfono. Al entrar, Iris le pedirá cambiarla antes de cualquier otra cosa. Si la
                            copiaste, se borra del portapapeles a los 45 s o al cerrar este diálogo.
                        </Aviso>
                    </div>
                ) : null}
                <DialogoPie>
                    <Boton onClick={alContinuar}>{textoContinuar ?? "Listo"}</Boton>
                </DialogoPie>
            </div>
        </DialogoContenido>
    );
}
