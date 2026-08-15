-- Vuelta atrás del esquema inicial. En orden inverso a las dependencias.

DROP TABLE IF EXISTS import_log;
DROP TABLE IF EXISTS feriado;
DROP TABLE IF EXISTS sucursal_config;
DROP TABLE IF EXISTS device_alias;
DROP TABLE IF EXISTS stop;
DROP TABLE IF EXISTS track_day;
DROP TABLE IF EXISTS track_point;
DROP TABLE IF EXISTS gpx_file;
DROP TABLE IF EXISTS drive_source;
DROP TABLE IF EXISTS supervision;
DROP TABLE IF EXISTS trabajador;
DROP TABLE IF EXISTS sucursal;

DROP TYPE IF EXISTS estado_dia;
DROP TYPE IF EXISTS calidad_punto;
DROP TYPE IF EXISTS origen_fecha;
DROP TYPE IF EXISTS estado_fichero;
DROP TYPE IF EXISTS tipo_fuente;
