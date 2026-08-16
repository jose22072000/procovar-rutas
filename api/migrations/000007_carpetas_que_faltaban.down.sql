-- De baja, no borradas: si ya entraron rutas por ellas, la fila tiene ficheros
-- colgando.
UPDATE drive_source SET activa = FALSE, updated_at = now()
WHERE folder_id IN (
    '1o1yQnfSaNYlPHW0_zUAL-vl72y08t4ZE',
    '1yOBFeXEaKssUeFT0yc_IXICKrwgNd7pu',
    '1d0L-6P3KRxlTyIhsngfr4vx0hP8_Hhwr',
    '1zvzOW7RyZAbKtyVSKzAtH1-rljjQTk50',
    '1vX4yy6FZM937iIFsE64kLSn2s8hLkMa1',
    '1T5xoCl8FyLcbGCyM6IEh3yuAEllK3Vay',
    '1R6sUiJTF4Fhp46SXrhxOeZWTjYEQ-sbd',
    '1sa4SviD6nupQDKALSVEHbFwN3jO_nKtP',
    '18BziU1-W84cOCtC41zl23GlEMx8E9Lj6',
    '1OVnNYuKSqCV9rXCm_9EAQhV83SBzDox_',
    '1Ukfu62zmRKqoONjIu3k1QFUCPq5eDP_u',
    '1gXbaj_-RjG4Kg5QBSDb4Z8Z52sY3WjSv'
);
