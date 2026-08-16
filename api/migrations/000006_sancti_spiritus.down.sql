-- Se dan de baja, no se borran: si ya entraron rutas por ellas, la fila tiene
-- ficheros colgando. Los errores limpiados no se vuelven a escribir: no hay a qué
-- volver.
UPDATE drive_source SET activa = FALSE, updated_at = now()
WHERE folder_id IN (
    '1n1d0hEtEAXD984CjLXQLem4OmZhIa3fp',
    '1xjvZk-RZi3Xes4kQrx9BlhjPlCJZ78Yb',
    '1oc17SnVDWRGMCbsnF87ENGgsjO4yagLR',
    '1XyQrODAkH-U9aJY3rlUxtxR_65QRWHmS',
    '1JsW-JUnqg5feW635umGxiBf3MZWlg2Lz'
);
