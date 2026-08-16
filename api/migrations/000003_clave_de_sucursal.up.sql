-- Una sucursal, una fila. Da igual cómo esté escrito su nombre.
--
-- Las sucursales se crean solas con el nombre de la cuenta de Google que comparte
-- cada carpeta, y esas cuentas están escritas de todas las formas: "Camagüey
-- Procovar", "santiagoprocovar", "Holguin.Procovar@…". El resultado fue el
-- previsible: siete filas "Guantánamo", cuatro "Holguin", y "santiagoprocovar"
-- conviviendo con "Santiago". Un gerente de una de ellas no vería lo que quedó
-- guardado en la otra, que es exactamente lo que este sistema no puede permitirse.
--
-- La identidad pasa a ser una CLAVE normalizada —sin tildes, sin espacios, en
-- minúsculas— y el nombre se queda solo para leerlo. Así "Las Tunas", "lastunas" y
-- "LAS TUNAS" son la misma sucursal.

ALTER TABLE sucursal ADD COLUMN IF NOT EXISTS clave TEXT;

-- El "procovar" del final también sobra aquí: en producción hay filas llamadas
-- "santiagoprocovar" y "guantanamoprocovar" conviviendo con "Santiago" y
-- "Guantánamo", y son la misma sucursal.
UPDATE sucursal
SET clave = regexp_replace(
    regexp_replace(
        lower(translate(nombre, 'áéíóúüñÁÉÍÓÚÜÑ', 'aeiouunAEIOUUN')),
        '[^a-z0-9]', '', 'g'),
    'procovar$', '')
WHERE clave IS NULL OR clave = '';

-- Y si al quitarlo queda vacío (una cuenta que se llamaba solo "procovar"), se deja
-- el nombre normalizado: mejor una sucursal rara que una sin clave.
UPDATE sucursal
SET clave = regexp_replace(lower(translate(nombre, 'áéíóúüñÁÉÍÓÚÜÑ', 'aeiouunAEIOUUN')), '[^a-z0-9]', '', 'g')
WHERE clave = '';

-- De cada grupo sobrevive el nombre que mejor se lee, que es el que verá el gerente:
-- primero los que NO arrastran el apellido de la empresa ("Guantánamo" antes que
-- "guantanamoprocovar"), y entre esos el más largo ("Las Tunas" antes que
-- "lastunas").
WITH ganadora AS (
    SELECT DISTINCT ON (clave) clave, id
    FROM sucursal
    ORDER BY clave,
             (lower(nombre) LIKE '%procovar') ASC,
             length(nombre) DESC,
             created_at
),
perdedoras AS (
    SELECT s.id, g.id AS destino
    FROM sucursal s
    JOIN ganadora g ON g.clave = s.clave
    WHERE s.id <> g.id
),
mueve_trabajadores AS (
    UPDATE trabajador t SET sucursal_id = p.destino
    FROM perdedoras p WHERE t.sucursal_id = p.id
    RETURNING 1
),
mueve_ficheros AS (
    UPDATE gpx_file f SET sucursal_id = p.destino
    FROM perdedoras p WHERE f.sucursal_id = p.id
    RETURNING 1
),
mueve_dias AS (
    UPDATE track_day d SET sucursal_id = p.destino
    FROM perdedoras p WHERE d.sucursal_id = p.id
    RETURNING 1
),
mueve_puntos AS (
    UPDATE track_point tp SET sucursal_id = p.destino
    FROM perdedoras p WHERE tp.sucursal_id = p.id
    RETURNING 1
),
mueve_paradas AS (
    UPDATE stop st SET sucursal_id = p.destino
    FROM perdedoras p WHERE st.sucursal_id = p.id
    RETURNING 1
),
mueve_fuentes AS (
    UPDATE drive_source ds SET sucursal_id = p.destino
    FROM perdedoras p WHERE ds.sucursal_id = p.id
    RETURNING 1
)
DELETE FROM sucursal WHERE id IN (SELECT id FROM perdedoras);

-- Obligatoria: una sucursal sin clave sería una que se puede duplicar.
ALTER TABLE sucursal ALTER COLUMN clave SET NOT NULL;

-- Y que no vuelva a pasar: dos empujes a la vez de la misma sucursal chocan aquí
-- en vez de crear cada uno la suya.
CREATE UNIQUE INDEX IF NOT EXISTS sucursal_clave_uk ON sucursal (clave);
