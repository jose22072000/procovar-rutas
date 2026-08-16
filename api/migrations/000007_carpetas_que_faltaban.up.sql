-- Las doce carpetas que tenían rutas dentro y el panel no miraba.
--
-- Salen de contar los .gpx de cada carpeta de las ocho cuentas (el flujo
-- `n8n/inventario-carpetas.py`) y quedarse con las que tienen ficheros y no estaban
-- dadas de alta. Por el nombre no se podía: en los mismos Drive viven las carpetas de
-- la ingesta de pedidos (PEDIDOS, PROCESADOS, ERRORES) y cosas sueltas ("Copia",
-- "Fotos de trabajo"), y dar de alta una de esas crea un vendedor llamado así.
--
-- La más gorda es GPSErnesto, con 63 días de ruta que llevaban ahí sin entrar. Las
-- STG de Santiago son pocas cada una pero son ocho.
--
-- Sin sucursal a propósito, igual que las de Sancti Spíritus: la trae el primer
-- empuje en el `owners` de Drive y la carpeta se coloca sola.
INSERT INTO drive_source (id, nombre, folder_id, tipo)
VALUES
    (replace(gen_random_uuid()::text, '-', ''), 'GPSErnesto',            '1o1yQnfSaNYlPHW0_zUAL-vl72y08t4ZE', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'GTM1',                  '1yOBFeXEaKssUeFT0yc_IXICKrwgNd7pu', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'GTMYoannis',            '1d0L-6P3KRxlTyIhsngfr4vx0hP8_Hhwr', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'VendedorNuevoHolguin1', '1zvzOW7RyZAbKtyVSKzAtH1-rljjQTk50', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG2',                  '1vX4yy6FZM937iIFsE64kLSn2s8hLkMa1', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG3',                  '1T5xoCl8FyLcbGCyM6IEh3yuAEllK3Vay', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG4',                  '1R6sUiJTF4Fhp46SXrhxOeZWTjYEQ-sbd', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG6',                  '1sa4SviD6nupQDKALSVEHbFwN3jO_nKtP', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG7',                  '18BziU1-W84cOCtC41zl23GlEMx8E9Lj6', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG8',                  '1OVnNYuKSqCV9rXCm_9EAQhV83SBzDox_', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG10',                 '1Ukfu62zmRKqoONjIu3k1QFUCPq5eDP_u', 'VENDEDOR'),
    (replace(gen_random_uuid()::text, '-', ''), 'STG11',                 '1gXbaj_-RjG4Kg5QBSDb4Z8Z52sY3WjSv', 'VENDEDOR')
ON CONFLICT (folder_id) DO UPDATE SET activa = TRUE, updated_at = now();
