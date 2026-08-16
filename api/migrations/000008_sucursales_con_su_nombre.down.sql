-- Volver a como se leían antes. La clave nunca se tocó, así que nada más cambia.
UPDATE sucursal SET nombre = 'santispiritus', updated_at = now() WHERE clave = 'santispiritus';
UPDATE sucursal SET nombre = 'Holguin',       updated_at = now() WHERE clave = 'holguin';
UPDATE sucursal SET nombre = 'Lastunas',      updated_at = now() WHERE clave = 'lastunas';
