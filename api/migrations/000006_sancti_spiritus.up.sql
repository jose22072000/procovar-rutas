-- Sancti Spíritus, que llevaba todo este tiempo fuera.
--
-- Sus cinco perfiles de GPS están en su Drive y compartidos con la cuenta padre
-- desde el principio, pero ninguna de las carpetas estaba dada de alta. Y como n8n
-- pide la lista de carpetas al panel, lo que no está dado de alta no se mira: de esa
-- provincia no había entrado una sola ruta, y en el panel ni siquiera aparecía como
-- vacía. Aparecía como si no existiera.
--
-- Se dan de alta sin sucursal a propósito. La sucursal sale de la cuenta dueña de la
-- carpeta, y eso lo sabe el primer empuje de n8n, que trae el `owners` de Drive: al
-- llegar el primer fichero la carpeta se coloca sola en su sucursal. Escribirla aquí
-- a mano sería adivinar cómo se llama la cuenta.
INSERT INTO drive_source (id, nombre, folder_id, tipo)
VALUES
    (replace(gen_random_uuid()::text, '-', ''), 'GPSMilka',     '1n1d0hEtEAXD984CjLXQLem4OmZhIa3fp', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'GPSVendedor1', '1xjvZk-RZi3Xes4kQrx9BlhjPlCJZ78Yb', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'GPSSheyla',    '1oc17SnVDWRGMCbsnF87ENGgsjO4yagLR', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'GPSVictor',    '1XyQrODAkH-U9aJY3rlUxtxR_65QRWHmS', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'GPS',          '1JsW-JUnqg5feW635umGxiBf3MZWlg2Lz', 'VENDEDOR')
ON CONFLICT (folder_id) DO UPDATE SET activa = TRUE, updated_at = now();

-- Y fuera los errores que no eran errores.
--
-- El barrido propio no tiene credenciales de Google —los ficheros los empuja n8n—,
-- así que cada pasada dejaba escrito "no hay acceso a Drive" en las cincuenta y tres
-- carpetas. Administración se veía entera en rojo describiendo el funcionamiento
-- normal, con lo que un error de verdad no lo iba a ver nadie. Desde ahora ya no se
-- apunta; esto limpia lo que quedó apuntado.
UPDATE drive_source
SET ultimo_error = NULL, updated_at = now()
WHERE ultimo_error LIKE 'no hay acceso a Drive%';
