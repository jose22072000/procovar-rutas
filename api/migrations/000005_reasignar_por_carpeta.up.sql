-- Cada fichero, cada vendedor y cada día, en la sucursal de SU carpeta.
--
-- La sucursal se aprendió carpeta a carpeta, y al aprenderla se arrastró lo que
-- colgaba de ella en ese momento. Pero quedó gente descolgada: en el panel se veía
-- un vendedor en "principal" mientras su carpeta ya salía en Camagüey en
-- Administración, que es la peor forma de estar mal — dos pantallas del mismo
-- sistema diciendo cosas distintas.
--
-- Esto no depende de cuándo entró nada: mira la carpeta de cada fichero y coloca
-- todo donde toca. Es idempotente, así que puede repetirse sin miedo.
WITH por_fichero AS (
    SELECT f.id AS fichero_id, f.trabajador_id, ds.sucursal_id
    FROM gpx_file f
    JOIN drive_source ds ON ds.id = f.source_id
    WHERE ds.sucursal_id IS NOT NULL
),
mueve_ficheros AS (
    UPDATE gpx_file f SET sucursal_id = p.sucursal_id
    FROM por_fichero p
    WHERE f.id = p.fichero_id AND f.sucursal_id IS DISTINCT FROM p.sucursal_id
    RETURNING 1
),
-- Un vendedor pertenece a la sucursal de la carpeta de la que salen sus ficheros.
-- Si tuviera ficheros de dos carpetas de sucursales distintas —que no debería—, se
-- queda con la que más ficheros suyos tiene.
vendedor_sucursal AS (
    SELECT DISTINCT ON (trabajador_id) trabajador_id, sucursal_id
    FROM por_fichero
    WHERE trabajador_id IS NOT NULL
    GROUP BY trabajador_id, sucursal_id
    ORDER BY trabajador_id, count(*) DESC
),
mueve_vendedores AS (
    UPDATE trabajador t SET sucursal_id = v.sucursal_id, updated_at = now()
    FROM vendedor_sucursal v
    WHERE t.id = v.trabajador_id AND t.sucursal_id IS DISTINCT FROM v.sucursal_id
    RETURNING 1
)
UPDATE track_day d SET sucursal_id = v.sucursal_id
FROM vendedor_sucursal v
WHERE d.trabajador_id = v.trabajador_id AND d.sucursal_id IS DISTINCT FROM v.sucursal_id;
