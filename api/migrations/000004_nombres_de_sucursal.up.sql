-- Los nombres de sucursal, legibles.
--
-- Al juntar las duplicadas sobrevivió el nombre menos malo de cada grupo, pero
-- cuando TODAS las variantes traían el apellido —"santiagoprocovar",
-- "bayamoprocovar"— no había nada mejor que elegir. Eso es lo que va a leer un
-- gerente en su panel, así que se limpia: fuera el apellido y la primera letra en
-- mayúscula.
--
-- La clave no se toca: ya era la correcta ("santiago"), y es la que decide qué es la
-- misma sucursal. Esto es solo cómo se lee.
UPDATE sucursal
SET nombre = initcap(regexp_replace(nombre, '(?i)[ ._-]*procovar$', ''))
WHERE nombre ~* 'procovar$'
  AND regexp_replace(nombre, '(?i)[ ._-]*procovar$', '') <> '';
