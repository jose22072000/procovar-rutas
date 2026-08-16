-- Sancti Spíritus, Holguín y Las Tunas, escritas como se escriben.
--
-- El nombre de la sucursal sale del de la cuenta de Drive, y de "santispirituspro
-- covar" no hay forma de sacar dos palabras y un acento: quedaban "santispiritus",
-- "Holguin" y "Lastunas" en la cabecera del panel, al lado de "Camagüey" y
-- "Guantánamo", que sí salieron bien porque su cuenta ya venía con el acento. Es lo
-- que lee un gerente en su pantalla todo el día.
--
-- Se busca por CLAVE, no por nombre: la clave es lo que decide qué fila es qué
-- sucursal, no cambia aquí, y así esto vale aunque el nombre ya se hubiera tocado a
-- mano. Y no se inventa nada: sólo se renombra lo que existe.
UPDATE sucursal SET nombre = 'Sancti Spíritus', updated_at = now() WHERE clave = 'santispiritus';
UPDATE sucursal SET nombre = 'Holguín',         updated_at = now() WHERE clave = 'holguin';
UPDATE sucursal SET nombre = 'Las Tunas',       updated_at = now() WHERE clave = 'lastunas';
